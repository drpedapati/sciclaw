package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	defaultClaudeAgentBinary = "sciclaw-claude-agent"
	claudeAgentBinaryEnvVar  = "PICOCLAW_CLAUDE_AGENT_BINARY"
)

// ClaudeAgentProvider implements LLMProvider using the sciclaw-claude-agent sidecar.
// It is intended for Anthropic oat-token users who need the Claude Code / Agent SDK path.
type ClaudeAgentProvider struct {
	command     string
	workspace   string
	oauthToken  string
	tokenSource func() (string, error)
}

type bridgeThinkingConfig struct {
	Type         string `json:"type,omitempty"`
	BudgetTokens int    `json:"budgetTokens,omitempty"`
}

func NewClaudeAgentProvider(workspace, oauthToken string) *ClaudeAgentProvider {
	return &ClaudeAgentProvider{
		command:    resolveClaudeAgentCommand(),
		workspace:  workspace,
		oauthToken: oauthToken,
	}
}

func NewClaudeAgentProviderWithTokenSource(workspace, oauthToken string, tokenSource func() (string, error)) *ClaudeAgentProvider {
	p := NewClaudeAgentProvider(workspace, oauthToken)
	p.tokenSource = tokenSource
	return p
}

func resolveClaudeAgentCommand() string {
	if explicit := strings.TrimSpace(os.Getenv(claudeAgentBinaryEnvVar)); explicit != "" {
		return explicit
	}
	return defaultClaudeAgentBinary
}

func expandProviderHomePath(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if raw == "~" {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			return home
		}
		return raw
	}
	if strings.HasPrefix(raw, "~/") {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			return filepath.Join(home, raw[2:])
		}
	}
	return raw
}

func (p *ClaudeAgentProvider) GetDefaultModel() string {
	return "claude-sonnet-4.6"
}

func (p *ClaudeAgentProvider) currentToken() (string, error) {
	if p.tokenSource != nil {
		tok, err := p.tokenSource()
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(tok) != "" {
			return tok, nil
		}
	}
	if strings.TrimSpace(p.oauthToken) != "" {
		return p.oauthToken, nil
	}
	return "", fmt.Errorf("missing Anthropic oauth token")
}

func (p *ClaudeAgentProvider) Chat(ctx context.Context, messages []Message, tools []ToolDefinition, model string, options map[string]interface{}) (*LLMResponse, error) {
	tok, err := p.currentToken()
	if err != nil {
		return nil, fmt.Errorf("claude agent bridge requires an Anthropic oauth token: %w", err)
	}
	if !isAnthropicOAuthToken(tok) {
		return nil, fmt.Errorf("claude agent bridge only supports Anthropic oauth tokens")
	}

	cmdPath, err := exec.LookPath(p.command)
	if err != nil {
		return nil, fmt.Errorf(
			"Anthropic oauth tokens require %s. Install the sciclaw-claude-agent companion or set %s",
			defaultClaudeAgentBinary,
			claudeAgentBinaryEnvVar,
		)
	}

	req := buildClaudeAgentBridgeRequest(messages, tools, model, options, p.workspace, tok)

	payload, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal claude agent request: %w", err)
	}

	cmd := exec.CommandContext(ctx, cmdPath)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()
	resp, parseErr := parseClaudeAgentBridgeResponse(stdout.Bytes())
	if parseErr == nil && resp != nil {
		if resp.IsError {
			return nil, wrapClaudeAgentBridgeError(resp.Error)
		}
		return resp.toLLMResponse(), nil
	}

	if runErr != nil {
		if stderrStr := strings.TrimSpace(stderr.String()); stderrStr != "" {
			return nil, fmt.Errorf("claude agent bridge error: %s", stderrStr)
		}
		return nil, fmt.Errorf("claude agent bridge error: %w", runErr)
	}
	return nil, fmt.Errorf("failed to parse claude agent bridge response: %w", parseErr)
}

func expandedWorkspaceOrDefault(workspace string) string {
	expanded := expandProviderHomePath(workspace)
	if expanded == "" {
		return "."
	}
	return expanded
}

func wrapClaudeAgentBridgeError(msg string) error {
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "oauth authentication is currently not supported"):
		return fmt.Errorf(
			"claude agent bridge could not use the Anthropic oauth token. Use a fresh Claude.ai oat token (the bridge expects the pasted token, not a Console API key)",
		)
	case strings.Contains(lower, "failed to authenticate"):
		return fmt.Errorf(
			"claude agent bridge authentication failed. Re-import a fresh Anthropic oauth token with `sciclaw auth import-op --provider anthropic ...` or update config.providers.anthropic.api_key",
		)
	case strings.Contains(lower, "not logged in"):
		return fmt.Errorf(
			"claude agent bridge is missing Anthropic oauth credentials. Re-import a fresh Claude.ai oat token",
		)
	default:
		return fmt.Errorf("claude agent bridge: %s", msg)
	}
}

type claudeAgentBridgeRequest struct {
	OAuthToken            string                `json:"oauth_token,omitempty"`
	Model                 string                `json:"model,omitempty"`
	Workspace             string                `json:"workspace,omitempty"`
	Messages              []Message             `json:"messages"`
	Tools                 []ToolDefinition      `json:"tools,omitempty"`
	Effort                string                `json:"effort,omitempty"`
	Thinking              *bridgeThinkingConfig `json:"thinking,omitempty"`
	PersistSession        bool                  `json:"persist_session"`
	AdditionalDirectories []string              `json:"additional_directories,omitempty"`
}

type claudeAgentBridgeResponse struct {
	Type         string       `json:"type"`
	Subtype      string       `json:"subtype"`
	IsError      bool         `json:"is_error"`
	Error        string       `json:"error"`
	Result       string       `json:"result"`
	Content      string       `json:"content"`
	ToolCalls    []ToolCall   `json:"tool_calls"`
	FinishReason string       `json:"finish_reason"`
	SessionID    string       `json:"session_id"`
	Usage        *bridgeUsage `json:"usage"`
}

type bridgeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

func parseClaudeAgentBridgeResponse(data []byte) (*claudeAgentBridgeResponse, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("empty response")
	}
	var resp claudeAgentBridgeResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (r *claudeAgentBridgeResponse) toLLMResponse() *LLMResponse {
	var usage *UsageInfo
	if r.Usage != nil {
		usage = &UsageInfo{
			PromptTokens:     r.Usage.InputTokens + r.Usage.CacheCreationInputTokens + r.Usage.CacheReadInputTokens,
			CompletionTokens: r.Usage.OutputTokens,
			TotalTokens:      r.Usage.InputTokens + r.Usage.CacheCreationInputTokens + r.Usage.CacheReadInputTokens + r.Usage.OutputTokens,
		}
	}

	content := strings.TrimSpace(r.Content)
	if content == "" {
		content = strings.TrimSpace(r.Result)
	}

	return &LLMResponse{
		Content:      content,
		ToolCalls:    r.ToolCalls,
		FinishReason: r.FinishReason,
		Usage:        usage,
		Diagnostics: &ResponseDiagnostics{
			ContentSource:  "claude_agent_bridge",
			ToolCallSource: "claude_agent_bridge",
		},
	}
}

func buildClaudeAgentBridgeRequest(messages []Message, tools []ToolDefinition, model string, options map[string]interface{}, workspace, oauthToken string) claudeAgentBridgeRequest {
	req := claudeAgentBridgeRequest{
		OAuthToken:     oauthToken,
		Model:          model,
		Workspace:      expandedWorkspaceOrDefault(workspace),
		Messages:       messages,
		Tools:          tools,
		PersistSession: false,
	}

	if effort, ok := getStringOption(options, "reasoning_effort"); ok && effort != "" {
		req.Effort = effort
	}
	if thinking := buildClaudeAgentBridgeThinking(options, req.Effort); thinking != nil {
		req.Thinking = thinking
	}
	if persist, ok := getBoolOption(options, "persist_session", "persistence"); ok {
		req.PersistSession = persist
	}
	if dirs := getStringSliceOption(options, "additional_directories", "additionalDirectories"); len(dirs) > 0 {
		req.AdditionalDirectories = expandProviderHomePaths(dirs)
	}

	return req
}

func buildClaudeAgentBridgeThinking(options map[string]interface{}, effort string) *bridgeThinkingConfig {
	if options == nil {
		if effort != "" {
			return &bridgeThinkingConfig{Type: "adaptive"}
		}
		return nil
	}
	if raw, ok := options["thinking"]; ok {
		switch v := raw.(type) {
		case string:
			return normalizeBridgeThinkingConfig(v, 0)
		case map[string]interface{}:
			kind, _ := v["type"].(string)
			budget := intOption(v["budgetTokens"])
			if budget == 0 {
				budget = intOption(v["budget_tokens"])
			}
			return normalizeBridgeThinkingConfig(kind, budget)
		case map[string]string:
			return normalizeBridgeThinkingConfig(v["type"], 0)
		}
	}
	if effort != "" {
		return &bridgeThinkingConfig{Type: "adaptive"}
	}
	return nil
}

func normalizeBridgeThinkingConfig(kind string, budget int) *bridgeThinkingConfig {
	switch strings.TrimSpace(kind) {
	case "adaptive":
		return &bridgeThinkingConfig{Type: "adaptive"}
	case "enabled":
		cfg := &bridgeThinkingConfig{Type: "enabled"}
		if budget > 0 {
			cfg.BudgetTokens = budget
		}
		return cfg
	case "disabled":
		return &bridgeThinkingConfig{Type: "disabled"}
	default:
		return nil
	}
}

func getStringOption(options map[string]interface{}, key string) (string, bool) {
	if options == nil {
		return "", false
	}
	raw, ok := options[key]
	if !ok {
		return "", false
	}
	s, ok := raw.(string)
	return strings.TrimSpace(s), ok
}

func getBoolOption(options map[string]interface{}, keys ...string) (bool, bool) {
	if options == nil {
		return false, false
	}
	for _, key := range keys {
		if raw, ok := options[key]; ok {
			if v, ok := raw.(bool); ok {
				return v, true
			}
		}
	}
	return false, false
}

func getStringSliceOption(options map[string]interface{}, keys ...string) []string {
	if options == nil {
		return nil
	}
	for _, key := range keys {
		raw, ok := options[key]
		if !ok {
			continue
		}
		switch v := raw.(type) {
		case []string:
			return compactStrings(v)
		case []interface{}:
			out := make([]string, 0, len(v))
			for _, item := range v {
				if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
					out = append(out, strings.TrimSpace(s))
				}
			}
			return out
		}
	}
	return nil
}

func compactStrings(in []string) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func expandProviderHomePaths(in []string) []string {
	out := make([]string, 0, len(in))
	for _, item := range in {
		out = append(out, expandProviderHomePath(item))
	}
	return out
}

func intOption(raw interface{}) int {
	switch v := raw.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}
