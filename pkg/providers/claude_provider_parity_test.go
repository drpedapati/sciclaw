package providers

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeProviderParity_ReasoningEffortEnablesThinkingForBothProviders(t *testing.T) {
	options := map[string]interface{}{"reasoning_effort": "high"}
	messages := []Message{{Role: "user", Content: "Hello"}}

	params, err := buildClaudeParams(messages, nil, "anthropic/claude-sonnet-4.6", options)
	if err != nil {
		t.Fatalf("buildClaudeParams() error: %v", err)
	}
	if params.Thinking.OfAdaptive == nil {
		t.Fatalf("expected Claude API params to enable adaptive thinking")
	}
	if got := string(params.OutputConfig.Effort); got != "high" {
		t.Fatalf("Claude API effort = %q, want high", got)
	}

	req := buildClaudeAgentBridgeRequest(messages, nil, "anthropic/claude-sonnet-4.6", options, "/workspace", "sk-ant-oat-test")
	if req.Thinking == nil || req.Thinking.Type != "adaptive" {
		t.Fatalf("bridge request thinking = %#v, want adaptive", req.Thinking)
	}
	if req.Effort != "high" {
		t.Fatalf("bridge request effort = %q, want high", req.Effort)
	}
}

func TestClaudeAgentBridgeRequest_PassthroughSupportedOptions(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	options := map[string]interface{}{
		"reasoning_effort":       "medium",
		"thinking":               map[string]interface{}{"type": "enabled", "budget_tokens": 2048},
		"persistence":            true,
		"additional_directories": []interface{}{"~/notes", "/tmp/shared"},
	}

	req := buildClaudeAgentBridgeRequest([]Message{{Role: "user", Content: "Hello"}}, nil, "claude-sonnet-4.6", options, "~/workspace", "sk-ant-oat-test")
	if req.Thinking == nil || req.Thinking.Type != "enabled" || req.Thinking.BudgetTokens != 2048 {
		t.Fatalf("bridge thinking = %#v, want enabled/2048", req.Thinking)
	}
	if !req.PersistSession {
		t.Fatal("expected persist_session=true")
	}
	if len(req.AdditionalDirectories) != 2 {
		t.Fatalf("additional_directories = %#v, want 2 entries", req.AdditionalDirectories)
	}
	if req.AdditionalDirectories[0] != filepath.Join(os.Getenv("HOME"), "notes") {
		t.Fatalf("additional_directories[0] = %q, want expanded home path", req.AdditionalDirectories[0])
	}
	if req.Effort != "medium" {
		t.Fatalf("bridge effort = %q, want medium", req.Effort)
	}
}

func TestClaudeAgentBridgeRequest_OmitsUnsupportedDirectAPIOptions(t *testing.T) {
	options := map[string]interface{}{
		"max_tokens":  1024,
		"temperature": 0.2,
	}

	req := buildClaudeAgentBridgeRequest([]Message{{Role: "user", Content: "Hello"}}, nil, "claude-sonnet-4.6", options, "/workspace", "sk-ant-oat-test")
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("Marshal() error: %v", err)
	}
	jsonStr := string(payload)
	if strings.Contains(jsonStr, "max_tokens") {
		t.Fatalf("bridge request should not include unsupported max_tokens: %s", jsonStr)
	}
	if strings.Contains(jsonStr, "temperature") {
		t.Fatalf("bridge request should not include unsupported temperature: %s", jsonStr)
	}
}
