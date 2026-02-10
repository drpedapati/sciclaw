// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/session"
	"github.com/sipeed/picoclaw/pkg/tools"
)

type AgentLoop struct {
	bus            *bus.MessageBus
	provider       providers.LLMProvider
	workspace      string
	model          string
	maxIterations  int
	sessions       *session.SessionManager
	contextBuilder *ContextBuilder
	tools          *tools.ToolRegistry
	running        bool
}

func NewAgentLoop(cfg *config.Config, msgBus *bus.MessageBus, provider providers.LLMProvider) *AgentLoop {
	workspace := cfg.WorkspacePath()
	os.MkdirAll(workspace, 0755)

	toolsRegistry := tools.NewToolRegistry()
	toolsRegistry.Register(&tools.ReadFileTool{})
	toolsRegistry.Register(&tools.WriteFileTool{})
	toolsRegistry.Register(&tools.ListDirTool{})
	toolsRegistry.Register(tools.NewExecTool(workspace))

	braveAPIKey := cfg.Tools.Web.Search.APIKey
	toolsRegistry.Register(tools.NewWebSearchTool(braveAPIKey, cfg.Tools.Web.Search.MaxResults))
	toolsRegistry.Register(tools.NewWebFetchTool(50000))

	// Register message tool
	messageTool := tools.NewMessageTool()
	messageTool.SetSendCallback(func(channel, chatID, content string) error {
		msgBus.PublishOutbound(bus.OutboundMessage{
			Channel: channel,
			ChatID:  chatID,
			Content: content,
		})
		return nil
	})
	toolsRegistry.Register(messageTool)

	// Register spawn tool
	subagentManager := tools.NewSubagentManager(provider, workspace, msgBus)
	spawnTool := tools.NewSpawnTool(subagentManager)
	toolsRegistry.Register(spawnTool)

	// Register edit file tool
	editFileTool := tools.NewEditFileTool(workspace)
	toolsRegistry.Register(editFileTool)

	sessionsManager := session.NewSessionManager(filepath.Join(filepath.Dir(cfg.WorkspacePath()), "sessions"))

	return &AgentLoop{
		bus:            msgBus,
		provider:       provider,
		workspace:      workspace,
		model:          cfg.Agents.Defaults.Model,
		maxIterations:  cfg.Agents.Defaults.MaxToolIterations,
		sessions:       sessionsManager,
		contextBuilder: NewContextBuilder(workspace),
		tools:          toolsRegistry,
		running:        false,
	}
}

func (al *AgentLoop) Run(ctx context.Context) error {
	al.running = true

	for al.running {
		select {
		case <-ctx.Done():
			return nil
		default:
			msg, ok := al.bus.ConsumeInbound(ctx)
			if !ok {
				continue
			}

			response, err := al.processMessage(ctx, msg)
			if err != nil {
				response = fmt.Sprintf("Error processing message: %v", err)
			}

			if response != "" {
				al.bus.PublishOutbound(bus.OutboundMessage{
					Channel: msg.Channel,
					ChatID:  msg.ChatID,
					Content: response,
				})
			}
		}
	}

	return nil
}

func (al *AgentLoop) Stop() {
	al.running = false
}

func (al *AgentLoop) ProcessDirect(ctx context.Context, content, sessionKey string) (string, error) {
	msg := bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "user",
		ChatID:     "direct",
		Content:    content,
		SessionKey: sessionKey,
	}

	return al.processMessage(ctx, msg)
}

func (al *AgentLoop) processMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	// Add message preview to log
	preview := truncate(msg.Content, 80)
	logger.InfoCF("agent", fmt.Sprintf("Processing message from %s:%s: %s", msg.Channel, msg.SenderID, preview),
		map[string]interface{}{
			"channel":     msg.Channel,
			"chat_id":     msg.ChatID,
			"sender_id":   msg.SenderID,
			"session_key": msg.SessionKey,
		})

	// Route system messages to processSystemMessage
	if msg.Channel == "system" {
		return al.processSystemMessage(ctx, msg)
	}

	// Update tool contexts
	if tool, ok := al.tools.Get("message"); ok {
		if mt, ok := tool.(*tools.MessageTool); ok {
			mt.SetContext(msg.Channel, msg.ChatID)
		}
	}
	if tool, ok := al.tools.Get("spawn"); ok {
		if st, ok := tool.(*tools.SpawnTool); ok {
			st.SetContext(msg.Channel, msg.ChatID)
		}
	}

	history := al.sessions.GetHistory(msg.SessionKey)
	summary := al.sessions.GetSummary(msg.SessionKey)

	messages := al.contextBuilder.BuildMessages(
		history,
		summary,
		msg.Content,
		nil,
		msg.Channel,
		msg.ChatID,
	)

	iteration := 0
	var finalContent string

	for iteration < al.maxIterations {
		iteration++

		logger.DebugCF("agent", "LLM iteration",
			map[string]interface{}{
				"iteration": iteration,
				"max":       al.maxIterations,
			})

		toolDefs := al.tools.GetDefinitions()
		providerToolDefs := make([]providers.ToolDefinition, 0, len(toolDefs))
		for _, td := range toolDefs {
			providerToolDefs = append(providerToolDefs, providers.ToolDefinition{
				Type: td["type"].(string),
				Function: providers.ToolFunctionDefinition{
					Name:        td["function"].(map[string]interface{})["name"].(string),
					Description: td["function"].(map[string]interface{})["description"].(string),
					Parameters:  td["function"].(map[string]interface{})["parameters"].(map[string]interface{}),
				},
			})
		}

		response, err := al.provider.Chat(ctx, messages, providerToolDefs, al.model, map[string]interface{}{
			"max_tokens":  8192,
			"temperature": 0.7,
		})

		if err != nil {
			logger.ErrorCF("agent", "LLM call failed",
				map[string]interface{}{
					"iteration": iteration,
					"error":     err.Error(),
				})
			return "", fmt.Errorf("LLM call failed: %w", err)
		}

		if len(response.ToolCalls) == 0 {
			finalContent = response.Content
			logger.InfoCF("agent", "LLM response without tool calls (direct answer)",
				map[string]interface{}{
					"iteration":     iteration,
					"content_chars": len(finalContent),
				})
			break
		}

		toolNames := make([]string, 0, len(response.ToolCalls))
		for _, tc := range response.ToolCalls {
			toolNames = append(toolNames, tc.Name)
		}
		logger.InfoCF("agent", "LLM requested tool calls",
			map[string]interface{}{
				"tools":     toolNames,
				"count":     len(toolNames),
				"iteration": iteration,
			})

		assistantMsg := providers.Message{
			Role:    "assistant",
			Content: response.Content,
		}

		for _, tc := range response.ToolCalls {
			argumentsJSON, _ := json.Marshal(tc.Arguments)
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, providers.ToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: &providers.FunctionCall{
					Name:      tc.Name,
					Arguments: string(argumentsJSON),
				},
			})
		}
		messages = append(messages, assistantMsg)

		for _, tc := range response.ToolCalls {
			// Log tool call with arguments preview
			argsJSON, _ := json.Marshal(tc.Arguments)
			argsPreview := truncate(string(argsJSON), 200)
			logger.InfoCF("agent", fmt.Sprintf("Tool call: %s(%s)", tc.Name, argsPreview),
				map[string]interface{}{
					"tool":       tc.Name,
					"iteration":  iteration,
				})

			result, err := al.tools.Execute(ctx, tc.Name, tc.Arguments)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}

			toolResultMsg := providers.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			}
			messages = append(messages, toolResultMsg)
		}
	}

	if finalContent == "" {
		finalContent = "I've completed processing but have no response to give."
	}

	al.sessions.AddMessage(msg.SessionKey, "user", msg.Content)
	al.sessions.AddMessage(msg.SessionKey, "assistant", finalContent)
	al.sessions.Save(al.sessions.GetOrCreate(msg.SessionKey))

	// Log response preview
	responsePreview := truncate(finalContent, 120)
	logger.InfoCF("agent", fmt.Sprintf("Response to %s:%s: %s", msg.Channel, msg.SenderID, responsePreview),
		map[string]interface{}{
			"iterations":   iteration,
			"final_length": len(finalContent),
		})

	return finalContent, nil
}

func (al *AgentLoop) processSystemMessage(ctx context.Context, msg bus.InboundMessage) (string, error) {
	// Verify this is a system message
	if msg.Channel != "system" {
		return "", fmt.Errorf("processSystemMessage called with non-system message channel: %s", msg.Channel)
	}

	logger.InfoCF("agent", "Processing system message",
		map[string]interface{}{
			"sender_id": msg.SenderID,
			"chat_id":   msg.ChatID,
		})

	// Parse origin from chat_id (format: "channel:chat_id")
	var originChannel, originChatID string
	if idx := strings.Index(msg.ChatID, ":"); idx > 0 {
		originChannel = msg.ChatID[:idx]
		originChatID = msg.ChatID[idx+1:]
	} else {
		// Fallback
		originChannel = "cli"
		originChatID = msg.ChatID
	}

	// Use the origin session for context
	sessionKey := fmt.Sprintf("%s:%s", originChannel, originChatID)

	// Update tool contexts to original channel/chatID
	if tool, ok := al.tools.Get("message"); ok {
		if mt, ok := tool.(*tools.MessageTool); ok {
			mt.SetContext(originChannel, originChatID)
		}
	}
	if tool, ok := al.tools.Get("spawn"); ok {
		if st, ok := tool.(*tools.SpawnTool); ok {
			st.SetContext(originChannel, originChatID)
		}
	}

	// Build messages with the announce content
	history := al.sessions.GetHistory(sessionKey)
	summary := al.sessions.GetSummary(sessionKey)
	messages := al.contextBuilder.BuildMessages(
		history,
		summary,
		msg.Content,
		nil,
		originChannel,
		originChatID,
	)

	iteration := 0
	var finalContent string

	for iteration < al.maxIterations {
		iteration++

		toolDefs := al.tools.GetDefinitions()
		providerToolDefs := make([]providers.ToolDefinition, 0, len(toolDefs))
		for _, td := range toolDefs {
			providerToolDefs = append(providerToolDefs, providers.ToolDefinition{
				Type: td["type"].(string),
				Function: providers.ToolFunctionDefinition{
					Name:        td["function"].(map[string]interface{})["name"].(string),
					Description: td["function"].(map[string]interface{})["description"].(string),
					Parameters:  td["function"].(map[string]interface{})["parameters"].(map[string]interface{}),
				},
			})
		}

		response, err := al.provider.Chat(ctx, messages, providerToolDefs, al.model, map[string]interface{}{
			"max_tokens":  8192,
			"temperature": 0.7,
		})

		if err != nil {
			logger.ErrorCF("agent", "LLM call failed in system message",
				map[string]interface{}{
					"iteration": iteration,
					"error":     err.Error(),
				})
			return "", fmt.Errorf("LLM call failed: %w", err)
		}

		if len(response.ToolCalls) == 0 {
			finalContent = response.Content
			break
		}

		assistantMsg := providers.Message{
			Role:    "assistant",
			Content: response.Content,
		}

		for _, tc := range response.ToolCalls {
			argumentsJSON, _ := json.Marshal(tc.Arguments)
			assistantMsg.ToolCalls = append(assistantMsg.ToolCalls, providers.ToolCall{
				ID:   tc.ID,
				Type: "function",
				Function: &providers.FunctionCall{
					Name:      tc.Name,
					Arguments: string(argumentsJSON),
				},
			})
		}
		messages = append(messages, assistantMsg)

		for _, tc := range response.ToolCalls {
			result, err := al.tools.Execute(ctx, tc.Name, tc.Arguments)
			if err != nil {
				result = fmt.Sprintf("Error: %v", err)
			}

			toolResultMsg := providers.Message{
				Role:       "tool",
				Content:    result,
				ToolCallID: tc.ID,
			}
			messages = append(messages, toolResultMsg)
		}
	}

	if finalContent == "" {
		finalContent = "Background task completed."
	}

	// Save to session with system message marker
	al.sessions.AddMessage(sessionKey, "user", fmt.Sprintf("[System: %s] %s", msg.SenderID, msg.Content))
	al.sessions.AddMessage(sessionKey, "assistant", finalContent)
	al.sessions.Save(al.sessions.GetOrCreate(sessionKey))

	logger.InfoCF("agent", "System message processing completed",
		map[string]interface{}{
			"iterations":   iteration,
			"final_length": len(finalContent),
		})

	return finalContent, nil
}

// truncate returns a truncated version of s with at most maxLen characters.
// If the string is truncated, "..." is appended to indicate truncation.
// If the string fits within maxLen, it is returned unchanged.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	// Reserve 3 chars for "..."
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// GetStartupInfo returns information about loaded tools and skills for logging.
func (al *AgentLoop) GetStartupInfo() map[string]interface{} {
	info := make(map[string]interface{})

	// Tools info
	tools := al.tools.List()
	info["tools"] = map[string]interface{}{
		"count": len(tools),
		"names": tools,
	}

	// Skills info
	info["skills"] = al.contextBuilder.GetSkillsInfo()

	return info
}

func (al *AgentLoop) summarizeSession(sessionKey string) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	history := al.sessions.GetHistory(sessionKey)
	summary := al.sessions.GetSummary(sessionKey)

	// Keep last 4 messages for continuity
	if len(history) <= 4 {
		return
	}

	toSummarize := history[:len(history)-4]

	// Oversized Message Guard (Dynamic)
	// Skip messages larger than 50% of context window to prevent summarizer overflow.
	maxMessageTokens := al.contextWindow / 2
	validMessages := make([]providers.Message, 0)
	omitted := false

	for _, m := range toSummarize {
		if m.Role != "user" && m.Role != "assistant" {
			continue
		}
		// Estimate tokens for this message
		msgTokens := len(m.Content) / 4
		if msgTokens > maxMessageTokens {
			omitted = true
			continue
		}
		validMessages = append(validMessages, m)
	}

	if len(validMessages) == 0 {
		return
	}

	// Multi-Part Summarization
	// Split into two parts if history is significant
	var finalSummary string
	if len(validMessages) > 10 {
		mid := len(validMessages) / 2
		part1 := validMessages[:mid]
		part2 := validMessages[mid:]

		s1, _ := al.summarizeBatch(ctx, part1, "")
		s2, _ := al.summarizeBatch(ctx, part2, "")
		
		// Merge them
		mergePrompt := fmt.Sprintf("Merge these two conversation summaries into one cohesive summary:\n\n1: %s\n\n2: %s", s1, s2)
		resp, err := al.provider.Chat(ctx, []providers.Message{{Role: "user", Content: mergePrompt}}, nil, al.model, map[string]interface{}{
			"max_tokens":  1024,
			"temperature": 0.3,
		})
		if err == nil {
			finalSummary = resp.Content
		} else {
			finalSummary = s1 + " " + s2
		}
	} else {
		finalSummary, _ = al.summarizeBatch(ctx, validMessages, summary)
	}

	if omitted && finalSummary != "" {
		finalSummary += "\n[Note: Some oversized messages were omitted from this summary for efficiency.]"
	}

	if finalSummary != "" {
		al.sessions.SetSummary(sessionKey, finalSummary)
		al.sessions.TruncateHistory(sessionKey, 4)
		al.sessions.Save(al.sessions.GetOrCreate(sessionKey))
	}
}

func (al *AgentLoop) summarizeBatch(ctx context.Context, batch []providers.Message, existingSummary string) (string, error) {
	prompt := "Provide a concise summary of this conversation segment, preserving core context and key points.\n"
	if existingSummary != "" {
		prompt += "Existing context: " + existingSummary + "\n"
	}
	prompt += "\nCONVERSATION:\n"
	for _, m := range batch {
		prompt += fmt.Sprintf("%s: %s\n", m.Role, m.Content)
	}

	response, err := al.provider.Chat(ctx, []providers.Message{{Role: "user", Content: prompt}}, nil, al.model, map[string]interface{}{
		"max_tokens":  1024,
		"temperature": 0.3,
	})
	if err != nil {
		return "", err
	}
	return response.Content, nil
}

func (al *AgentLoop) estimateTokens(messages []providers.Message) int {
	total := 0
	for _, m := range messages {
		total += len(m.Content) / 4 // Simple heuristic: 4 chars per token
	}
	return total
}
