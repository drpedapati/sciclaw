package providers

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/config"
)

func createMockClaudeAgentBridge(t *testing.T, stdout, stderr string, exitCode int, requestCapture string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("mock bridge scripts not supported on Windows")
	}

	dir := t.TempDir()
	if stdout != "" {
		if err := os.WriteFile(filepath.Join(dir, "stdout.txt"), []byte(stdout), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if stderr != "" {
		if err := os.WriteFile(filepath.Join(dir, "stderr.txt"), []byte(stderr), 0644); err != nil {
			t.Fatal(err)
		}
	}

	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	if requestCapture != "" {
		sb.WriteString(fmt.Sprintf("cat > '%s'\n", requestCapture))
	} else {
		sb.WriteString("cat >/dev/null\n")
	}
	if stderr != "" {
		sb.WriteString(fmt.Sprintf("cat '%s/stderr.txt' >&2\n", dir))
	}
	if stdout != "" {
		sb.WriteString(fmt.Sprintf("cat '%s/stdout.txt'\n", dir))
	}
	sb.WriteString(fmt.Sprintf("exit %d\n", exitCode))

	script := filepath.Join(dir, "sciclaw-claude-agent")
	if err := os.WriteFile(script, []byte(sb.String()), 0755); err != nil {
		t.Fatal(err)
	}
	return script
}

func TestClaudeAgentProvider_ChatSuccess(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	requestPath := filepath.Join(t.TempDir(), "request.json")
	mockJSON := `{"type":"result","subtype":"success","is_error":false,"content":"OK","tool_calls":[],"finish_reason":"stop","session_id":"sess_1","usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`
	script := createMockClaudeAgentBridge(t, mockJSON, "", 0, requestPath)

	p := NewClaudeAgentProvider("~/workspace", "sk-ant-oat-test")
	p.command = script

	resp, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "Hello"}}, nil, "anthropic/claude-sonnet-4.6", nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "OK" {
		t.Fatalf("Content = %q, want OK", resp.Content)
	}
	if resp.FinishReason != "stop" {
		t.Fatalf("FinishReason = %q, want stop", resp.FinishReason)
	}

	payload, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile(request) error = %v", err)
	}
	got := string(payload)
	if !strings.Contains(got, `"oauth_token":"sk-ant-oat-test"`) {
		t.Fatalf("request missing oauth token: %s", got)
	}
	if !strings.Contains(got, `"workspace":"`+filepath.Join(os.Getenv("HOME"), "workspace")+`"`) {
		t.Fatalf("request missing expanded workspace: %s", got)
	}
}

func TestClaudeAgentProvider_ChatToolCalls(t *testing.T) {
	mockJSON := `{"type":"result","subtype":"success","is_error":false,"content":"","tool_calls":[{"id":"call_1","type":"function","name":"lookup","arguments":{"query":"abc"},"function":{"name":"lookup","arguments":"{\"query\":\"abc\"}"}}],"finish_reason":"tool_calls","session_id":"sess_1"}`
	script := createMockClaudeAgentBridge(t, mockJSON, "", 0, "")

	p := NewClaudeAgentProvider(".", "sk-ant-oat-test")
	p.command = script

	resp, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil, "anthropic/claude-sonnet-4.6", nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.FinishReason != "tool_calls" {
		t.Fatalf("FinishReason = %q, want tool_calls", resp.FinishReason)
	}
	if len(resp.ToolCalls) != 1 || resp.ToolCalls[0].Name != "lookup" {
		t.Fatalf("ToolCalls = %#v, want one lookup call", resp.ToolCalls)
	}
}

func TestClaudeAgentProvider_ChatBridgeError(t *testing.T) {
	mockJSON := `{"type":"result","subtype":"success","is_error":true,"error":"Failed to authenticate.","content":"","tool_calls":[],"finish_reason":"error","session_id":"sess_1"}`
	script := createMockClaudeAgentBridge(t, mockJSON, "", 0, "")

	p := NewClaudeAgentProvider(".", "sk-ant-oat-test")
	p.command = script

	_, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil, "anthropic/claude-sonnet-4.6", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "fresh anthropic oauth token") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateProvider_AnthropicOATAPIKeyUsesClaudeAgentProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Model = "anthropic/claude-sonnet-4.6"
	cfg.Providers.Anthropic.APIKey = "sk-ant-oat-test"

	provider, err := CreateProvider(cfg)
	if err != nil {
		t.Fatalf("CreateProvider returned error: %v", err)
	}
	if _, ok := provider.(*ClaudeAgentProvider); !ok {
		t.Fatalf("provider type=%T want *ClaudeAgentProvider", provider)
	}
}

func TestCreateProvider_StoredAnthropicOATUsesClaudeAgentProvider(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Model = "anthropic/claude-sonnet-4.6"
	cfg.Providers.Anthropic.AuthMethod = "oauth"

	if err := auth.SetCredential("anthropic", &auth.AuthCredential{
		AccessToken: "sk-ant-oat-test",
		Provider:    "anthropic",
		AuthMethod:  "oauth",
	}); err != nil {
		t.Fatalf("SetCredential error = %v", err)
	}

	provider, err := CreateProvider(cfg)
	if err != nil {
		t.Fatalf("CreateProvider returned error: %v", err)
	}
	if _, ok := provider.(*ClaudeAgentProvider); !ok {
		t.Fatalf("provider type=%T want *ClaudeAgentProvider", provider)
	}
}
