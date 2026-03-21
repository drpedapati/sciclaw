package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/session"
)

type PromptEnvelope struct {
	Version      string                     `json:"version"`
	Workspace    string                     `json:"workspace,omitempty"`
	SessionKey   string                     `json:"session_key,omitempty"`
	SystemPrompt string                     `json:"system_prompt,omitempty"`
	Summary      string                     `json:"summary,omitempty"`
	Messages     []PromptEnvelopeMessage    `json:"messages"`
	ToolSchemas  []PromptEnvelopeToolSchema `json:"tool_schemas,omitempty"`
	Budget       PromptEnvelopeBudget       `json:"budget"`
	Options      PromptEnvelopeOptions      `json:"options,omitempty"`
}

type PromptEnvelopeBudget struct {
	MaxInputTokens      int     `json:"max_input_tokens"`
	ReserveTokens       int     `json:"reserve_tokens"`
	RecentBudgetTokens  int     `json:"recent_budget_tokens"`
	TokenDivisor        float64 `json:"token_divisor,omitempty"`
	ToolSchemaOverhead  int     `json:"tool_schema_overhead,omitempty"`
	BootstrapOverhead   int     `json:"bootstrap_overhead,omitempty"`
	SummaryReserveFloor int     `json:"summary_reserve_floor,omitempty"`
}

type PromptEnvelopeOptions struct {
	ArchiveDir                string `json:"archive_dir,omitempty"`
	KeepRecentMessages        int    `json:"keep_recent_messages,omitempty"`
	OldToolResultThreshold    int    `json:"old_tool_result_threshold,omitempty"`
	RecentToolResultThreshold int    `json:"recent_tool_result_threshold,omitempty"`
	CheckpointMaxBullets      int    `json:"checkpoint_max_bullets,omitempty"`
}

type PromptEnvelopeMessage struct {
	Role       string                   `json:"role"`
	Content    string                   `json:"content,omitempty"`
	ToolCallID string                   `json:"tool_call_id,omitempty"`
	ToolName   string                   `json:"tool_name,omitempty"`
	ToolCalls  []PromptEnvelopeToolCall `json:"tool_calls,omitempty"`
}

type PromptEnvelopeToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
}

type PromptEnvelopeToolSchema struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Schema      string `json:"schema,omitempty"`
}

type PromptOptimizePreview struct {
	Request  *PromptEnvelope `json:"request"`
	Response json.RawMessage `json:"response"`
}

func BuildPromptEnvelope(cfg *config.Config, opts PromptInspectOptions) (*PromptEnvelope, error) {
	workspace := strings.TrimSpace(opts.Workspace)
	if workspace == "" {
		return nil, fmt.Errorf("workspace is required")
	}
	workspace = filepath.Clean(workspace)
	sessionKey := strings.TrimSpace(opts.SessionKey)
	if sessionKey == "" {
		return nil, fmt.Errorf("session key is required")
	}
	sessionPath := filepath.Join(workspace, "sessions", sessionKey+".json")
	if _, err := os.Stat(sessionPath); err != nil {
		return nil, fmt.Errorf("session not found at %s: %w", sessionPath, err)
	}

	sm := session.NewSessionManager(filepath.Join(workspace, "sessions"))
	sess, ok := sm.Snapshot(sessionKey)
	if !ok {
		if loadErr := sm.LoadError(); loadErr != nil {
			if session.IsLoadTimedOut(loadErr) {
				return nil, fmt.Errorf("session %q exists on disk but session preload timed out; retry this command or inspect %s directly", sessionKey, sessionPath)
			}
			return nil, fmt.Errorf("session %q could not be loaded from disk: %w", sessionKey, loadErr)
		}
		return nil, fmt.Errorf("session %q exists on disk but was not loaded into memory; check session JSON validity at %s", sessionKey, sessionPath)
	}

	lastUser := -1
	for i := len(sess.Messages) - 1; i >= 0; i-- {
		if sess.Messages[i].Role == "user" {
			lastUser = i
			break
		}
	}
	if lastUser < 0 {
		return nil, fmt.Errorf("session %q has no user message to inspect", sessionKey)
	}

	history := append([]providers.Message(nil), sess.Messages[:lastUser]...)
	history, _ = trimLeadingOrphanedToolMessages(history)
	currentUser := sess.Messages[lastUser]
	channel, chatID := parseSessionKey(sessionKey)

	registry := createToolRegistry(workspace, cfg.Agents.Defaults.RestrictToWorkspace, cfg, bus.NewMessageBus(), ToolProfileDefault)
	cb := NewContextBuilder(workspace, cfg.SharedWorkspacePath())
	cb.SetToolsRegistry(registry)
	cb.SetIncludePromptToolSummaries(false)
	cb.SetVersion(Version)

	systemPrompt := cb.BuildSystemPrompt()
	if channel != "" && chatID != "" {
		systemPrompt += buildCurrentSessionBlock(channel, chatID)
	}

	bootstrapFiles, _, _ := inspectBootstrapFiles(workspace, cfg.SharedWorkspacePath())
	toolDefs := registry.ToProviderDefs()
	outSchemas := make([]PromptEnvelopeToolSchema, 0, len(toolDefs))
	for _, def := range toolDefs {
		paramsJSON, _ := json.Marshal(def.Function.Parameters)
		outSchemas = append(outSchemas, PromptEnvelopeToolSchema{
			Name:        def.Function.Name,
			Description: def.Function.Description,
			Schema:      string(paramsJSON),
		})
	}

	messages := make([]PromptEnvelopeMessage, 0, len(history)+1)
	for _, msg := range history {
		messages = append(messages, convertPromptEnvelopeMessage(msg))
	}
	messages = append(messages, convertPromptEnvelopeMessage(currentUser))

	env := &PromptEnvelope{
		Version:      "ctxclaw/v1",
		Workspace:    workspace,
		SessionKey:   sessionKey,
		SystemPrompt: systemPrompt,
		Summary:      sess.Summary,
		Messages:     messages,
		ToolSchemas:  outSchemas,
		Budget: PromptEnvelopeBudget{
			MaxInputTokens:      24000,
			ReserveTokens:       3000,
			RecentBudgetTokens:  1800,
			TokenDivisor:        4.0,
			ToolSchemaOverhead:  0,
			BootstrapOverhead:   0,
			SummaryReserveFloor: 400,
		},
		Options: PromptEnvelopeOptions{
			ArchiveDir:                filepath.Join(workspace, ".ctxclaw", "archive", "tool-results"),
			KeepRecentMessages:        6,
			OldToolResultThreshold:    1200,
			RecentToolResultThreshold: 30000,
			CheckpointMaxBullets:      4,
		},
	}
	// Preserve a small amount of runtime-derived signal by nudging recent budget when history is tiny.
	if len(messages) <= 4 {
		env.Budget.RecentBudgetTokens = 4000
	}
	_ = bootstrapFiles
	return env, nil
}

func convertPromptEnvelopeMessage(msg providers.Message) PromptEnvelopeMessage {
	out := PromptEnvelopeMessage{
		Role:       msg.Role,
		Content:    msg.Content,
		ToolCallID: msg.ToolCallID,
		ToolName:   msg.ToolName,
	}
	if len(msg.ToolCalls) > 0 {
		out.ToolCalls = make([]PromptEnvelopeToolCall, 0, len(msg.ToolCalls))
		for _, tc := range msg.ToolCalls {
			name := tc.Name
			if name == "" && tc.Function != nil {
				name = tc.Function.Name
			}
			args := ""
			if tc.Function != nil && strings.TrimSpace(tc.Function.Arguments) != "" {
				args = tc.Function.Arguments
			} else if len(tc.Arguments) > 0 {
				if data, err := json.Marshal(tc.Arguments); err == nil {
					args = string(data)
				}
			}
			out.ToolCalls = append(out.ToolCalls, PromptEnvelopeToolCall{
				ID:        tc.ID,
				Name:      name,
				Arguments: args,
			})
		}
	}
	return out
}
