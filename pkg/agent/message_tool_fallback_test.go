package agent

import (
	"context"
	"os"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
)

type sequenceProvider struct {
	responses []*providers.LLMResponse
	index     int
}

func (p *sequenceProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	if len(p.responses) == 0 {
		return &providers.LLMResponse{}, nil
	}
	if p.index >= len(p.responses) {
		return p.responses[len(p.responses)-1], nil
	}
	resp := p.responses[p.index]
	p.index++
	return resp, nil
}

func (p *sequenceProvider) GetDefaultModel() string {
	return "mock-model"
}

func TestProcessDirect_MessageToolFallbackForInternalChannel(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-message-fallback-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "mock-model",
				MaxTokens:         4096,
				MaxToolIterations: 3,
			},
		},
	}

	provider := &sequenceProvider{
		responses: []*providers.LLMResponse{
			{
				Content: "",
				ToolCalls: []providers.ToolCall{
					{
						ID:   "call-1",
						Name: "message",
						Arguments: map[string]interface{}{
							"content": "Hey! I'm here and ready to help.",
						},
					},
				},
			},
			{
				Content:   "",
				ToolCalls: nil,
			},
		},
	}

	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	got, err := al.ProcessDirectWithChannel(context.Background(), "hi", "tui:chat", "cli", "direct", "cli")
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel error: %v", err)
	}

	want := "Hey! I'm here and ready to help."
	if got != want {
		t.Fatalf("unexpected response: got %q want %q", got, want)
	}
}

func TestProcessDirect_MessageToolFallbackWhenLLMReturnsDefaultPlaceholder(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-message-fallback-default-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "mock-model",
				MaxTokens:         4096,
				MaxToolIterations: 3,
			},
		},
	}

	provider := &sequenceProvider{
		responses: []*providers.LLMResponse{
			{
				Content: "",
				ToolCalls: []providers.ToolCall{
					{
						ID:   "call-1",
						Name: "message",
						Arguments: map[string]interface{}{
							"content": "Still going strong!",
						},
					},
				},
			},
			{
				Content:   "I've completed processing but have no response to give.",
				ToolCalls: nil,
			},
		},
	}

	al := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	got, err := al.ProcessDirectWithChannel(context.Background(), "how are you", "tui:chat", "cli", "direct", "cli")
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel error: %v", err)
	}

	want := "Still going strong!"
	if got != want {
		t.Fatalf("unexpected response: got %q want %q", got, want)
	}
}
