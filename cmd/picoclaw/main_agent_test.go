package main

import (
	"context"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/agent"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/tools"
)

func TestApplyAgentCLIOverrides_RePinsProviderFromModelOverride(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = "anthropic"
	cfg.Agents.Defaults.Model = "claude-opus-4-6"
	cfg.Agents.Defaults.ReasoningEffort = "high"

	applyAgentCLIOverrides(cfg, "openai/gpt-5.4", "medium")

	if got, want := cfg.Agents.Defaults.Model, "openai/gpt-5.4"; got != want {
		t.Fatalf("model = %q, want %q", got, want)
	}
	if got, want := cfg.Agents.Defaults.Provider, "openai"; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}
	if got, want := cfg.Agents.Defaults.ReasoningEffort, "medium"; got != want {
		t.Fatalf("reasoning effort = %q, want %q", got, want)
	}
}

func TestApplyAgentCLIOverrides_LeavesProviderForUnknownModel(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = "openrouter"
	cfg.Agents.Defaults.Model = "openrouter/deepseek/test"

	applyAgentCLIOverrides(cfg, "custom-model-name", "")

	if got, want := cfg.Agents.Defaults.Model, "custom-model-name"; got != want {
		t.Fatalf("model = %q, want %q", got, want)
	}
	if got, want := cfg.Agents.Defaults.Provider, "openrouter"; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}
}

type channelHistoryScriptProvider struct {
	t             *testing.T
	toolResultLog string
	callCount     int
}

func (p *channelHistoryScriptProvider) Chat(ctx context.Context, messages []providers.Message, _ []providers.ToolDefinition, _ string, _ map[string]interface{}) (*providers.LLMResponse, error) {
	p.callCount++
	if p.callCount == 1 {
		return &providers.LLMResponse{
			ToolCalls: []providers.ToolCall{{
				ID:        "call_history",
				Name:      "channel_history",
				Arguments: map[string]interface{}{"limit": float64(1)},
			}},
		}, nil
	}
	if len(messages) == 0 {
		p.t.Fatal("expected tool result message on second provider call")
	}
	last := messages[len(messages)-1]
	if last.Role != "tool" {
		p.t.Fatalf("last message role = %q, want tool", last.Role)
	}
	p.toolResultLog = last.Content
	return &providers.LLMResponse{Content: "done"}, nil
}

func (p *channelHistoryScriptProvider) GetDefaultModel() string {
	return "mock-model"
}

func TestAttachChannelHistoryCallback_WiresSideLaneLoop(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = t.TempDir()

	provider := &channelHistoryScriptProvider{t: t}
	al := agent.NewAgentLoopWithOptions(cfg, bus.NewMessageBus(), provider, agent.LoopOptions{
		ToolProfile: agent.ToolProfileSideLane,
	})

	attachChannelHistoryCallback(al, func(channelID string, limit int, beforeID string) ([]tools.ChannelHistoryMessage, error) {
		if channelID != "room-1" {
			t.Fatalf("channelID = %q, want room-1", channelID)
		}
		if limit != 1 {
			t.Fatalf("limit = %d, want 1", limit)
		}
		if beforeID != "" {
			t.Fatalf("beforeID = %q, want empty", beforeID)
		}
		return []tools.ChannelHistoryMessage{{
			ID:        "msg-1",
			Author:    "ernie",
			Content:   "hello from history",
			Timestamp: "2026-03-15 10:00",
		}}, nil
	})

	out, err := al.RunJob(context.Background(), bus.InboundMessage{
		Channel:    "discord",
		ChatID:     "room-1",
		SenderID:   "user-1",
		SessionKey: "discord:room-1",
		Content:    "/btw summarize the latest thread",
	}, nil)
	if err != nil {
		t.Fatalf("RunJob error = %v", err)
	}
	if out != "done" {
		t.Fatalf("RunJob output = %q, want done", out)
	}
	if !strings.Contains(provider.toolResultLog, "Channel history (1 messages):") {
		t.Fatalf("tool result missing channel history output: %q", provider.toolResultLog)
	}
	if !strings.Contains(provider.toolResultLog, "hello from history") {
		t.Fatalf("tool result missing fetched content: %q", provider.toolResultLog)
	}
}
