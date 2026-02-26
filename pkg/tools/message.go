package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sipeed/picoclaw/pkg/bus"
)

type SendCallback func(channel, chatID, content string, attachments []bus.OutboundAttachment) error

type MessageTool struct {
	sendCallback            SendCallback
	defaultChannel          string
	defaultChatID           string
	workspace               string
	restrict                bool
	sharedWorkspace         string
	sharedWorkspaceReadOnly bool
	sentInRound             bool // Tracks whether a message was sent in the current processing round
}

func NewMessageTool(workspace string, restrict bool) *MessageTool {
	return &MessageTool{
		workspace: workspace,
		restrict:  restrict,
	}
}

func (t *MessageTool) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	t.sharedWorkspace = strings.TrimSpace(sharedWorkspace)
	t.sharedWorkspaceReadOnly = sharedWorkspaceReadOnly
}

func (t *MessageTool) Name() string {
	return "message"
}

func (t *MessageTool) Description() string {
	return "Send a message to user on a chat channel. Use this when you want to communicate something."
}

func (t *MessageTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"content": map[string]interface{}{
				"type":        "string",
				"description": "The message content to send",
			},
			"channel": map[string]interface{}{
				"type":        "string",
				"description": "Optional: target channel (telegram, whatsapp, etc.)",
			},
			"chat_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional: target chat/user ID",
			},
			"attachments": map[string]interface{}{
				"type":        "array",
				"description": "Optional: files to attach (currently supported on Discord and Telegram)",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"path": map[string]interface{}{
							"type":        "string",
							"description": "Path to local file (relative paths are resolved from workspace)",
						},
						"filename": map[string]interface{}{
							"type":        "string",
							"description": "Optional override filename shown in chat",
						},
					},
					"required": []string{"path"},
				},
			},
		},
		"required": []string{"content"},
	}
}

func (t *MessageTool) SetContext(channel, chatID string) {
	t.defaultChannel = channel
	t.defaultChatID = chatID
	t.sentInRound = false // Reset send tracking for new processing round
}

// HasSentInRound returns true if the message tool sent a message during the current round.
func (t *MessageTool) HasSentInRound() bool {
	return t.sentInRound
}

func (t *MessageTool) SetSendCallback(callback SendCallback) {
	t.sendCallback = callback
}

func (t *MessageTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	content, ok := args["content"].(string)
	if !ok {
		return &ToolResult{ForLLM: "content is required", IsError: true}
	}

	channel, _ := args["channel"].(string)
	chatID, _ := args["chat_id"].(string)

	if channel == "" {
		channel = t.defaultChannel
	}
	if chatID == "" {
		chatID = t.defaultChatID
	}

	if channel == "" || chatID == "" {
		return &ToolResult{ForLLM: "No target channel/chat specified", IsError: true}
	}

	attachments, err := t.parseAttachments(args)
	if err != nil {
		return &ToolResult{
			ForLLM:  err.Error(),
			ForUser: "⚠️ " + err.Error(),
			IsError: true,
			Err:     err,
		}
	}

	if len(attachments) > 0 && channel != "discord" && channel != "telegram" {
		return &ToolResult{
			ForLLM:  fmt.Sprintf("attachments are currently supported only on discord/telegram (got %q)", channel),
			IsError: true,
		}
	}

	if t.sendCallback == nil {
		return &ToolResult{ForLLM: "Message sending not configured", IsError: true}
	}

	if err := t.sendCallback(channel, chatID, content, attachments); err != nil {
		return &ToolResult{
			ForLLM:  fmt.Sprintf("sending message: %v", err),
			IsError: true,
			Err:     err,
		}
	}

	t.sentInRound = true
	// Silent: user already received the message directly
	status := fmt.Sprintf("Message sent to %s:%s", channel, chatID)
	if len(attachments) > 0 {
		status += fmt.Sprintf(" with %d attachment(s)", len(attachments))
	}
	return &ToolResult{
		ForLLM: status,
		Silent: true,
	}
}

func (t *MessageTool) parseAttachments(args map[string]interface{}) ([]bus.OutboundAttachment, error) {
	raw, ok := args["attachments"]
	if !ok || raw == nil {
		return nil, nil
	}

	items, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("attachments must be an array")
	}

	attachments := make([]bus.OutboundAttachment, 0, len(items))
	for i, item := range items {
		obj, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("attachments[%d] must be an object", i)
		}

		pathRaw, ok := obj["path"]
		if !ok {
			return nil, fmt.Errorf("attachments[%d].path is required", i)
		}
		pathStr, ok := pathRaw.(string)
		if !ok || strings.TrimSpace(pathStr) == "" {
			return nil, fmt.Errorf("attachments[%d].path must be a non-empty string", i)
		}

		resolvedPath, err := validatePathWithPolicy(pathStr, t.workspace, t.restrict, AccessRead, t.sharedWorkspace, t.sharedWorkspaceReadOnly)
		if err != nil {
			return nil, fmt.Errorf("attachments[%d]: %w", i, err)
		}

		stat, err := os.Stat(resolvedPath)
		if err != nil {
			return nil, fmt.Errorf("attachments[%d]: cannot access %q: %w", i, pathStr, err)
		}
		if stat.IsDir() {
			return nil, fmt.Errorf("attachments[%d]: %q is a directory, expected file", i, pathStr)
		}

		filename := filepath.Base(resolvedPath)
		if rawName, ok := obj["filename"]; ok {
			if name, ok := rawName.(string); ok && strings.TrimSpace(name) != "" {
				filename = filepath.Base(strings.TrimSpace(name))
			}
		}

		attachments = append(attachments, bus.OutboundAttachment{
			Path:     resolvedPath,
			Filename: filename,
		})
	}

	return attachments, nil
}
