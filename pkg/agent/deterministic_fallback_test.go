package agent

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/tools"
)

type mockIRLProjectTool struct {
	result *tools.ToolResult
}

func (m *mockIRLProjectTool) Name() string {
	return "irl_project"
}

func (m *mockIRLProjectTool) Description() string {
	return "mock irl tool"
}

func (m *mockIRLProjectTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (m *mockIRLProjectTool) Execute(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	return m.result
}

func newTestAgentWithEmptyProvider(t *testing.T) *AgentLoop {
	t.Helper()

	tmpDir, err := os.MkdirTemp("", "agent-fallback-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 5,
			},
		},
	}
	msgBus := bus.NewMessageBus()
	provider := &simpleMockProvider{response: ""}
	return NewAgentLoop(cfg, msgBus, provider)
}

func TestDeterministicFallback_ListProjects(t *testing.T) {
	al := newTestAgentWithEmptyProvider(t)
	al.tools.Register(&mockIRLProjectTool{
		result: tools.NewToolResult(`{"status":"success","data":{"projects":[{"name":"sciclaw-manuscript","path":"/tmp/sciclaw-manuscript","template":"default"}]}}`),
	})

	resp, err := al.ProcessDirectWithChannel(context.Background(), "list projects", "test-session", "telegram", "chat-1")
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel error: %v", err)
	}
	if !strings.Contains(resp, "Found 1 IRL-managed project(s):") {
		t.Fatalf("expected project count in response, got: %q", resp)
	}
	if !strings.Contains(resp, "sciclaw-manuscript") {
		t.Fatalf("expected project name in response, got: %q", resp)
	}
}

func TestDeterministicFallback_NonIntentUsesDefaultResponse(t *testing.T) {
	al := newTestAgentWithEmptyProvider(t)
	al.tools.Register(&mockIRLProjectTool{
		result: tools.NewToolResult(`{"status":"success","data":{"projects":[{"name":"ignored","path":"/tmp/ignored"}]}}`),
	})

	resp, err := al.ProcessDirectWithChannel(context.Background(), "hello there", "test-session", "telegram", "chat-1")
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel error: %v", err)
	}
	want := "I've completed processing but have no response to give."
	if resp != want {
		t.Fatalf("expected default response %q, got %q", want, resp)
	}
}
