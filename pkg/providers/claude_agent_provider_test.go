package providers

import (
	"context"
	"encoding/json"
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

func createSequentialMockClaudeAgentBridge(t *testing.T, outputs []string, requestCapturePrefix string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("mock bridge scripts not supported on Windows")
	}
	if len(outputs) == 0 {
		t.Fatal("outputs must not be empty")
	}

	dir := t.TempDir()
	for i, out := range outputs {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("stdout-%d.txt", i+1)), []byte(out), 0644); err != nil {
			t.Fatal(err)
		}
	}

	var sb strings.Builder
	sb.WriteString("#!/bin/sh\n")
	sb.WriteString(fmt.Sprintf("COUNT_FILE='%s/count'\n", dir))
	sb.WriteString("COUNT=0\n")
	sb.WriteString("if [ -f \"$COUNT_FILE\" ]; then COUNT=$(cat \"$COUNT_FILE\"); fi\n")
	sb.WriteString("COUNT=$((COUNT+1))\n")
	sb.WriteString("printf '%s' \"$COUNT\" > \"$COUNT_FILE\"\n")
	if requestCapturePrefix != "" {
		sb.WriteString(fmt.Sprintf("cat > '%s'.$COUNT.json\n", requestCapturePrefix))
	} else {
		sb.WriteString("cat >/dev/null\n")
	}
	sb.WriteString(fmt.Sprintf("if [ \"$COUNT\" -gt %d ]; then COUNT=%d; fi\n", len(outputs), len(outputs)))
	sb.WriteString(fmt.Sprintf("cat '%s/stdout-'$COUNT'.txt'\n", dir))
	sb.WriteString("exit 0\n")

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

func TestClaudeAgentProvider_ChatPreservesResultOnlySuccessPayload(t *testing.T) {
	mockJSON := `{"type":"result","subtype":"success","is_error":false,"result":"Compiled answer from bridge","content":"","tool_calls":[],"finish_reason":"stop","session_id":"sess_1"}`
	script := createMockClaudeAgentBridge(t, mockJSON, "", 0, "")

	p := NewClaudeAgentProvider(".", "sk-ant-oat-test")
	p.command = script

	resp, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil, "anthropic/claude-sonnet-4.6", nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "Compiled answer from bridge" {
		t.Fatalf("Content = %q, want result payload", resp.Content)
	}
}

func TestClaudeAgentProvider_ChatRetriesEmptyDirectAnswerWithoutThinking(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	requestPrefix := filepath.Join(t.TempDir(), "request")
	first := `{"type":"result","subtype":"success","is_error":false,"result":"","content":"","tool_calls":[],"finish_reason":"stop","session_id":"sess_1","usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`
	second := `{"type":"result","subtype":"success","is_error":false,"content":"Recovered answer","tool_calls":[],"finish_reason":"stop","session_id":"sess_1","usage":{"input_tokens":8,"output_tokens":4,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`
	script := createSequentialMockClaudeAgentBridge(t, []string{first, second}, requestPrefix)

	p := NewClaudeAgentProvider(".", "sk-ant-oat-test")
	p.command = script

	resp, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil, "anthropic/claude-sonnet-4.6", map[string]interface{}{
		"reasoning_effort": "high",
		"thinking":         map[string]interface{}{"type": "enabled", "budgetTokens": 2048},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "Recovered answer" {
		t.Fatalf("Content = %q, want recovered retry answer", resp.Content)
	}
	if resp.Diagnostics == nil || resp.Diagnostics.ContentSource != "claude_agent_bridge_retry_no_thinking" {
		t.Fatalf("Diagnostics = %#v, want retry content source", resp.Diagnostics)
	}

	firstPayload, err := os.ReadFile(requestPrefix + ".1.json")
	if err != nil {
		t.Fatalf("ReadFile(first request) error = %v", err)
	}
	secondPayload, err := os.ReadFile(requestPrefix + ".2.json")
	if err != nil {
		t.Fatalf("ReadFile(second request) error = %v", err)
	}
	var firstReq map[string]interface{}
	if err := json.Unmarshal(firstPayload, &firstReq); err != nil {
		t.Fatalf("Unmarshal(first request) error = %v", err)
	}
	if firstReq["effort"] != "high" {
		t.Fatalf("first request effort = %#v, want high", firstReq["effort"])
	}
	if _, ok := firstReq["thinking"]; !ok {
		t.Fatalf("first request missing thinking payload: %s", string(firstPayload))
	}

	var secondReq map[string]interface{}
	if err := json.Unmarshal(secondPayload, &secondReq); err != nil {
		t.Fatalf("Unmarshal(second request) error = %v", err)
	}
	if _, ok := secondReq["effort"]; ok {
		t.Fatalf("second request should omit effort: %s", string(secondPayload))
	}
	if _, ok := secondReq["thinking"]; ok {
		t.Fatalf("second request should omit thinking: %s", string(secondPayload))
	}
}

func TestClaudeAgentProvider_ChatDoesNotRetryEmptyDirectAnswerWithoutThinking(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	requestPath := filepath.Join(t.TempDir(), "request.json")
	mockJSON := `{"type":"result","subtype":"success","is_error":false,"result":"","content":"","tool_calls":[],"finish_reason":"stop","session_id":"sess_1","usage":{"input_tokens":10,"output_tokens":5,"cache_creation_input_tokens":0,"cache_read_input_tokens":0}}`
	script := createMockClaudeAgentBridge(t, mockJSON, "", 0, requestPath)

	p := NewClaudeAgentProvider(".", "sk-ant-oat-test")
	p.command = script

	resp, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "Hi"}}, nil, "anthropic/claude-sonnet-4.6", nil)
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}
	if resp.Content != "" {
		t.Fatalf("Content = %q, want empty direct answer without retry", resp.Content)
	}
	if _, err := os.ReadFile(requestPath + ".2"); err == nil {
		t.Fatal("unexpected second request capture")
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

func TestClaudeAgentProvider_ChatRequestPassthrough(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	requestPath := filepath.Join(t.TempDir(), "request.json")
	mockJSON := `{"type":"result","subtype":"success","is_error":false,"content":"OK","tool_calls":[],"finish_reason":"stop"}`
	script := createMockClaudeAgentBridge(t, mockJSON, "", 0, requestPath)

	p := NewClaudeAgentProvider("~/workspace", "sk-ant-oat-test")
	p.command = script

	_, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "Hello"}}, nil, "anthropic/claude-sonnet-4.6", map[string]interface{}{
		"reasoning_effort":       "high",
		"persist_session":        true,
		"additional_directories": []string{"~/notes", "/tmp/shared"},
		"max_tokens":             2048,
		"temperature":            0.3,
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	payload, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile(request) error = %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(payload, &req); err != nil {
		t.Fatalf("Unmarshal(request) error = %v", err)
	}
	if req["effort"] != "high" {
		t.Fatalf("effort = %#v, want high", req["effort"])
	}
	thinking, ok := req["thinking"].(map[string]interface{})
	if !ok || thinking["type"] != "adaptive" {
		t.Fatalf("thinking = %#v, want adaptive", req["thinking"])
	}
	if req["persist_session"] != true {
		t.Fatalf("persist_session = %#v, want true", req["persist_session"])
	}
	dirs, ok := req["additional_directories"].([]interface{})
	if !ok || len(dirs) != 2 {
		t.Fatalf("additional_directories = %#v, want 2 entries", req["additional_directories"])
	}
	if dirs[0] != filepath.Join(os.Getenv("HOME"), "notes") {
		t.Fatalf("additional_directories[0] = %#v, want expanded home path", dirs[0])
	}
	if _, ok := req["max_tokens"]; ok {
		t.Fatalf("request should not include unsupported max_tokens: %s", string(payload))
	}
	if _, ok := req["temperature"]; ok {
		t.Fatalf("request should not include unsupported temperature: %s", string(payload))
	}
}

func TestClaudeAgentProvider_ChatThinkingBudgetPassthrough(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	requestPath := filepath.Join(t.TempDir(), "request.json")
	mockJSON := `{"type":"result","subtype":"success","is_error":false,"content":"OK","tool_calls":[],"finish_reason":"stop"}`
	script := createMockClaudeAgentBridge(t, mockJSON, "", 0, requestPath)

	p := NewClaudeAgentProvider("~/workspace", "sk-ant-oat-test")
	p.command = script

	_, err := p.Chat(context.Background(), []Message{{Role: "user", Content: "Hello"}}, nil, "anthropic/claude-sonnet-4.6", map[string]interface{}{
		"thinking": map[string]interface{}{
			"type":         "enabled",
			"budgetTokens": 2048,
		},
	})
	if err != nil {
		t.Fatalf("Chat() error = %v", err)
	}

	payload, err := os.ReadFile(requestPath)
	if err != nil {
		t.Fatalf("ReadFile(request) error = %v", err)
	}
	var req map[string]interface{}
	if err := json.Unmarshal(payload, &req); err != nil {
		t.Fatalf("Unmarshal(request) error = %v", err)
	}
	thinking, ok := req["thinking"].(map[string]interface{})
	if !ok {
		t.Fatalf("thinking = %#v, want object", req["thinking"])
	}
	if thinking["type"] != "enabled" {
		t.Fatalf("thinking.type = %#v, want enabled", thinking["type"])
	}
	if thinking["budgetTokens"] != float64(2048) {
		t.Fatalf("thinking.budgetTokens = %#v, want 2048", thinking["budgetTokens"])
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

func TestCreateProvider_AnthropicOAuthAuthMethodOverridesStaleAPIKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Model = "anthropic/claude-sonnet-4.6"
	cfg.Providers.Anthropic.AuthMethod = "oauth"
	cfg.Providers.Anthropic.APIKey = "stale-anthropic-api-key"

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
	agentProvider, ok := provider.(*ClaudeAgentProvider)
	if !ok {
		t.Fatalf("provider type=%T want *ClaudeAgentProvider", provider)
	}
	if agentProvider.tokenSource == nil {
		t.Fatal("expected oauth auth_method to use stored credential token source")
	}
}

func TestCreateProvider_AnthropicTokenAuthMethodOverridesStaleAPIKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Model = "anthropic/claude-sonnet-4.6"
	cfg.Providers.Anthropic.AuthMethod = "token"
	cfg.Providers.Anthropic.APIKey = "stale-anthropic-api-key"

	if err := auth.SetCredential("anthropic", &auth.AuthCredential{
		AccessToken: "sk-ant-api-fresh-token",
		Provider:    "anthropic",
		AuthMethod:  "token",
	}); err != nil {
		t.Fatalf("SetCredential error = %v", err)
	}

	provider, err := CreateProvider(cfg)
	if err != nil {
		t.Fatalf("CreateProvider returned error: %v", err)
	}
	claudeProvider, ok := provider.(*ClaudeProvider)
	if !ok {
		t.Fatalf("provider type=%T want *ClaudeProvider", provider)
	}
	if claudeProvider.tokenSource == nil {
		t.Fatal("expected token auth_method to use stored credential token source")
	}
}

func TestCreateProvider_AnthropicOAuthAuthMethodDoesNotFallbackToStaleAPIKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Model = "anthropic/claude-sonnet-4.6"
	cfg.Providers.Anthropic.AuthMethod = "oauth"
	cfg.Providers.Anthropic.APIKey = "stale-anthropic-api-key"

	_, err := CreateProvider(cfg)
	if err == nil {
		t.Fatal("expected missing stored oauth credential to fail")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "no credentials for anthropic") {
		t.Fatalf("unexpected error: %v", err)
	}
}
