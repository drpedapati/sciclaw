package tools

import (
	"context"
	"fmt"
	"strings"
)

// ChannelHistoryMessage is a single message returned by the fetch callback.
type ChannelHistoryMessage struct {
	ID        string
	Author    string
	Content   string
	Timestamp string
}

// FetchChannelHistoryCallback fetches recent messages from a channel.
// channelID is the platform-specific channel/chat ID.
// limit is the max number of messages to return (capped by the callback).
// beforeID, if non-empty, fetches messages before this message ID (for pagination).
type FetchChannelHistoryCallback func(channelID string, limit int, beforeID string) ([]ChannelHistoryMessage, error)

type ChannelHistoryTool struct {
	fetchCallback  FetchChannelHistoryCallback
	defaultChannel string
	defaultChatID  string
}

func NewChannelHistoryTool() *ChannelHistoryTool {
	return &ChannelHistoryTool{}
}

func (t *ChannelHistoryTool) Name() string {
	return "channel_history"
}

func (t *ChannelHistoryTool) Description() string {
	return "Fetch recent message history from the current chat channel. Returns messages in chronological order."
}

func (t *ChannelHistoryTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Number of messages to fetch (default 50, max 100)",
			},
			"before": map[string]interface{}{
				"type":        "string",
				"description": "Fetch messages before this message ID (for pagination)",
			},
		},
	}
}

func (t *ChannelHistoryTool) SetContext(channel, chatID string) {
	t.defaultChannel = channel
	t.defaultChatID = chatID
}

func (t *ChannelHistoryTool) SetFetchCallback(cb FetchChannelHistoryCallback) {
	t.fetchCallback = cb
}

func (t *ChannelHistoryTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	if t.fetchCallback == nil {
		return ErrorResult("channel history not available for this channel type")
	}

	if t.defaultChatID == "" {
		return ErrorResult("no channel context available")
	}

	limit := 50
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = int(l)
	}
	if limit > 100 {
		limit = 100
	}

	beforeID, _ := args["before"].(string)

	messages, err := t.fetchCallback(t.defaultChatID, limit, beforeID)
	if err != nil {
		return ErrorResult(fmt.Sprintf("failed to fetch channel history: %v", err))
	}

	if len(messages) == 0 {
		return NewToolResult("No messages found in channel.")
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Channel history (%d messages):\n\n", len(messages)))
	for _, msg := range messages {
		ts := msg.Timestamp
		author := msg.Author
		content := msg.Content
		if content == "" {
			content = "[no text content]"
		}
		sb.WriteString(fmt.Sprintf("[%s] %s: %s\n", ts, author, content))
	}

	return NewToolResult(sb.String())
}
