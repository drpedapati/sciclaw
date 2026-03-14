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

	req := claudeAgentBridgeRequest{
		OAuthToken: tok,
		Model:      model,
		Workspace:  expandedWorkspaceOrDefault(p.workspace),
		Messages:   messages,
		Tools:      tools,
	}
	if effort, ok := options["reasoning_effort"].(string); ok && effort != "" {
		req.Effort = effort
	}

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
	OAuthToken string           `json:"oauth_token,omitempty"`
	Model      string           `json:"model,omitempty"`
	Workspace  string           `json:"workspace,omitempty"`
	Messages   []Message        `json:"messages"`
	Tools      []ToolDefinition `json:"tools,omitempty"`
	Effort     string           `json:"effort,omitempty"`
}

type claudeAgentBridgeResponse struct {
	Type         string         `json:"type"`
	Subtype      string         `json:"subtype"`
	IsError      bool           `json:"is_error"`
	Error        string         `json:"error"`
	Result       string         `json:"result"`
	Content      string         `json:"content"`
	ToolCalls    []ToolCall     `json:"tool_calls"`
	FinishReason string         `json:"finish_reason"`
	SessionID    string         `json:"session_id"`
	Usage        *bridgeUsage   `json:"usage"`
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

	return &LLMResponse{
		Content:      strings.TrimSpace(r.Content),
		ToolCalls:    r.ToolCalls,
		FinishReason: r.FinishReason,
		Usage:        usage,
		Diagnostics: &ResponseDiagnostics{
			ContentSource:  "claude_agent_bridge",
			ToolCallSource: "claude_agent_bridge",
		},
	}
}
