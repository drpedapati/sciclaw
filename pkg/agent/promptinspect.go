package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/session"
)

type PromptInspectOptions struct {
	SessionKey string
	Workspace  string
}

type PromptInspectReport struct {
	SessionKey       string                 `json:"sessionKey"`
	Workspace        string                 `json:"workspace"`
	SharedWorkspace  string                 `json:"sharedWorkspace,omitempty"`
	SessionPath      string                 `json:"sessionPath"`
	Channel          string                 `json:"channel,omitempty"`
	ChatID           string                 `json:"chatId,omitempty"`
	HistoryCount     int                    `json:"historyCount"`
	CurrentUser      string                 `json:"currentUser"`
	CurrentUserChars int                    `json:"currentUserChars"`
	SystemPrompt     PromptSectionBreakdown `json:"systemPrompt"`
	History          PromptHistoryBreakdown `json:"history"`
	ToolSchemas      PromptToolSchemaReport `json:"toolSchemas"`
	Payload          PromptPayloadBreakdown `json:"payload"`
}

type PromptSectionBreakdown struct {
	TotalChars          int                   `json:"totalChars"`
	EstimatedTokens     int                   `json:"estimatedTokens"`
	IdentityChars       int                   `json:"identityChars"`
	Bootstrap           []PromptFileBreakdown `json:"bootstrap"`
	BootstrapTotalChars int                   `json:"bootstrapTotalChars"`
	SkillsChars         int                   `json:"skillsChars"`
	Memory              PromptFileBreakdown   `json:"memory"`
	MemoryChars         int                   `json:"memoryChars"`
	SessionBlockChars   int                   `json:"sessionBlockChars"`
	SummaryChars        int                   `json:"summaryChars"`
	JoinSeparatorChars  int                   `json:"joinSeparatorChars"`
}

type PromptFileBreakdown struct {
	Name            string `json:"name"`
	Path            string `json:"path,omitempty"`
	SourceWorkspace string `json:"sourceWorkspace,omitempty"`
	Chars           int    `json:"chars"`
	EstimatedTokens int    `json:"estimatedTokens"`
}

type PromptHistoryBreakdown struct {
	MessageCount    int                           `json:"messageCount"`
	TotalChars      int                           `json:"totalChars"`
	EstimatedTokens int                           `json:"estimatedTokens"`
	ByRole          []PromptHistoryRoleBreakdown  `json:"byRole"`
	ToolMessages    []PromptHistoryToolBreakdown  `json:"toolMessages"`
	LargestMessage  PromptLargestHistoryBreakdown `json:"largestMessage"`
}

type PromptHistoryRoleBreakdown struct {
	Role            string `json:"role"`
	MessageCount    int    `json:"messageCount"`
	Chars           int    `json:"chars"`
	EstimatedTokens int    `json:"estimatedTokens"`
}

type PromptHistoryToolBreakdown struct {
	ToolName            string `json:"toolName"`
	MessageCount        int    `json:"messageCount"`
	Chars               int    `json:"chars"`
	EstimatedTokens     int    `json:"estimatedTokens"`
	LargestMessageChars int    `json:"largestMessageChars"`
	WouldCompactRawNow  bool   `json:"wouldCompactRawNow"`
	AlreadyCompacted    bool   `json:"alreadyCompacted"`
}

type PromptLargestHistoryBreakdown struct {
	Role         string `json:"role"`
	ToolName     string `json:"toolName,omitempty"`
	Chars        int    `json:"chars"`
	Preview      string `json:"preview"`
	MessageIndex int    `json:"messageIndex"`
}

type PromptToolSchemaReport struct {
	Count           int                       `json:"count"`
	TotalChars      int                       `json:"totalChars"`
	EstimatedTokens int                       `json:"estimatedTokens"`
	Largest         []PromptToolSchemaSummary `json:"largest"`
}

type PromptToolSchemaSummary struct {
	Name            string `json:"name"`
	Chars           int    `json:"chars"`
	EstimatedTokens int    `json:"estimatedTokens"`
}

type PromptPayloadBreakdown struct {
	MessagesJSONChars      int `json:"messagesJSONChars"`
	ToolSchemasJSONChars   int `json:"toolSchemasJSONChars"`
	WrapperChars           int `json:"wrapperChars"`
	EstimatedPayloadTokens int `json:"estimatedPayloadTokens"`
	ContentCharsBeforeJSON int `json:"contentCharsBeforeJSON"`
	EstimatedContentTokens int `json:"estimatedContentTokens"`
}

func InspectPrompt(cfg *config.Config, opts PromptInspectOptions) (*PromptInspectReport, error) {
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
	currentUser := sess.Messages[lastUser].Content
	channel, chatID := parseSessionKey(sessionKey)

	registry := createToolRegistry(workspace, cfg.Agents.Defaults.RestrictToWorkspace, cfg, bus.NewMessageBus(), ToolProfileDefault)
	cb := NewContextBuilder(workspace, cfg.SharedWorkspacePath())
	cb.SetToolsRegistry(registry)
	cb.SetIncludePromptToolSummaries(false)
	cb.SetVersion(Version)

	identity := cb.getIdentity()
	bootstrapFiles, _, bootstrapTotalChars := inspectBootstrapFiles(workspace, cfg.SharedWorkspacePath())
	skillsBlock := buildSkillsBlock(cb)
	memoryFile, memoryBlock := inspectMemoryFile(workspace, cfg.SharedWorkspacePath())
	sessionBlock := buildCurrentSessionBlock(channel, chatID)
	summaryBlock := buildSummaryBlock(sess.Summary)
	systemPrompt := cb.BuildSystemPrompt()
	if channel != "" && chatID != "" {
		systemPrompt += sessionBlock
	}
	if sess.Summary != "" {
		systemPrompt += summaryBlock
	}

	joinSeparatorChars := buildJoinSeparatorChars(identity, bootstrapTotalChars > 0, skillsBlock != "", memoryBlock != "")

	messages := cb.BuildMessages(history, sess.Summary, currentUser, nil, channel, chatID)
	providerToolDefs := registry.ToProviderDefs()
	messagesJSON, _ := json.Marshal(messages)
	toolDefsJSON, _ := json.Marshal(providerToolDefs)
	messageContentChars := 0
	for _, msg := range messages {
		messageContentChars += len(msg.Content)
	}
	wrapperChars := len(messagesJSON) - messageContentChars
	if wrapperChars < 0 {
		wrapperChars = 0
	}

	historyReport := buildHistoryBreakdown(history)
	toolSchemaReport := buildToolSchemaBreakdown(providerToolDefs)

	report := &PromptInspectReport{
		SessionKey:       sessionKey,
		Workspace:        workspace,
		SharedWorkspace:  filepath.Clean(strings.TrimSpace(cfg.SharedWorkspacePath())),
		SessionPath:      sessionPath,
		Channel:          channel,
		ChatID:           chatID,
		HistoryCount:     len(history),
		CurrentUser:      currentUser,
		CurrentUserChars: len(currentUser),
		SystemPrompt: PromptSectionBreakdown{
			TotalChars:          len(systemPrompt),
			EstimatedTokens:     estimateTokens(len(systemPrompt)),
			IdentityChars:       len(identity),
			Bootstrap:           bootstrapFiles,
			BootstrapTotalChars: bootstrapTotalChars,
			SkillsChars:         len(skillsBlock),
			Memory:              memoryFile,
			MemoryChars:         len(memoryBlock),
			SessionBlockChars:   len(sessionBlock),
			SummaryChars:        len(summaryBlock),
			JoinSeparatorChars:  joinSeparatorChars,
		},
		History:     historyReport,
		ToolSchemas: toolSchemaReport,
		Payload: PromptPayloadBreakdown{
			MessagesJSONChars:      len(messagesJSON),
			ToolSchemasJSONChars:   len(toolDefsJSON),
			WrapperChars:           wrapperChars,
			EstimatedPayloadTokens: estimateTokens(len(messagesJSON) + len(toolDefsJSON)),
			ContentCharsBeforeJSON: messageContentChars + len(toolDefsJSON),
			EstimatedContentTokens: estimateTokens(messageContentChars + len(toolDefsJSON)),
		},
	}
	return report, nil
}

func buildSkillsBlock(cb *ContextBuilder) string {
	skillsSummary := cb.skillsLoader.BuildSkillsSummary()
	if skillsSummary == "" {
		return ""
	}
	return fmt.Sprintf(`# Skills

The following skills extend your capabilities. To use a skill, read its SKILL.md file using the read_file tool.

%s`, skillsSummary)
}

func inspectBootstrapFiles(workspace, sharedWorkspace string) ([]PromptFileBreakdown, string, int) {
	files := []string{"AGENTS.md", "SOUL.md", "USER.md", "IDENTITY.md", "TOOLS.md"}
	primary := filepath.Clean(workspace)
	fallback := filepath.Clean(strings.TrimSpace(sharedWorkspace))
	out := make([]PromptFileBreakdown, 0, len(files))
	var joined strings.Builder
	total := 0
	for _, name := range files {
		candidates := []struct {
			path   string
			source string
		}{
			{path: filepath.Join(primary, name), source: primary},
		}
		if fallback != "" && fallback != "." && fallback != primary {
			candidates = append(candidates, struct {
				path   string
				source string
			}{path: filepath.Join(fallback, name), source: fallback})
		}
		for _, candidate := range candidates {
			data, err := os.ReadFile(candidate.path)
			if err != nil {
				continue
			}
			block := fmt.Sprintf("## %s\n\n%s\n\n", name, string(data))
			chars := len(block)
			out = append(out, PromptFileBreakdown{
				Name:            name,
				Path:            candidate.path,
				SourceWorkspace: candidate.source,
				Chars:           chars,
				EstimatedTokens: estimateTokens(chars),
			})
			joined.WriteString(block)
			total += chars
			break
		}
	}
	return out, joined.String(), total
}

func inspectMemoryFile(workspace, sharedWorkspace string) (PromptFileBreakdown, string) {
	primary := filepath.Join(filepath.Clean(workspace), "memory", "MEMORY.md")
	fallbackRoot := filepath.Clean(strings.TrimSpace(sharedWorkspace))
	candidates := []struct {
		path   string
		source string
	}{{path: primary, source: filepath.Dir(filepath.Dir(primary))}}
	if fallbackRoot != "" && fallbackRoot != "." && filepath.Clean(filepath.Dir(filepath.Dir(primary))) != fallbackRoot {
		candidates = append(candidates, struct {
			path   string
			source string
		}{path: filepath.Join(fallbackRoot, "memory", "MEMORY.md"), source: fallbackRoot})
	}
	for _, candidate := range candidates {
		data, err := os.ReadFile(candidate.path)
		if err != nil {
			continue
		}
		block := "# Memory\n\n" + string(data)
		return PromptFileBreakdown{
			Name:            "memory/MEMORY.md",
			Path:            candidate.path,
			SourceWorkspace: candidate.source,
			Chars:           len(block),
			EstimatedTokens: estimateTokens(len(block)),
		}, block
	}
	return PromptFileBreakdown{Name: "memory/MEMORY.md"}, ""
}

func buildCurrentSessionBlock(channel, chatID string) string {
	if channel == "" || chatID == "" {
		return ""
	}
	return fmt.Sprintf("\n\n## Current Session\nChannel: %s\nChat ID: %s", channel, chatID)
}

func buildSummaryBlock(summary string) string {
	if summary == "" {
		return ""
	}
	return "\n\n## Summary of Previous Conversation\n\n" + summary
}

func buildHistoryBreakdown(history []providers.Message) PromptHistoryBreakdown {
	roleAgg := map[string]*PromptHistoryRoleBreakdown{}
	toolAgg := map[string]*PromptHistoryToolBreakdown{}
	largest := PromptLargestHistoryBreakdown{}
	totalChars := 0
	for i, msg := range history {
		chars := len(msg.Content)
		totalChars += chars
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			role = "(unknown)"
		}
		bucket := roleAgg[role]
		if bucket == nil {
			bucket = &PromptHistoryRoleBreakdown{Role: role}
			roleAgg[role] = bucket
		}
		bucket.MessageCount++
		bucket.Chars += chars
		if msg.Role == "tool" {
			toolName := strings.TrimSpace(msg.ToolName)
			if toolName == "" {
				toolName = "(unknown)"
			}
			tb := toolAgg[toolName]
			if tb == nil {
				tb = &PromptHistoryToolBreakdown{ToolName: toolName}
				toolAgg[toolName] = tb
			}
			tb.MessageCount++
			tb.Chars += chars
			if chars > tb.LargestMessageChars {
				tb.LargestMessageChars = chars
			}
			if session.IsCompactedToolMessageContent(msg.Content) {
				tb.AlreadyCompacted = true
			} else if session.WouldCompactToolMessage(msg.ToolName, chars) {
				tb.WouldCompactRawNow = true
			}
		}
		if chars > largest.Chars {
			largest = PromptLargestHistoryBreakdown{
				Role:         role,
				ToolName:     msg.ToolName,
				Chars:        chars,
				Preview:      compactPreview(msg.Content, 160),
				MessageIndex: i,
			}
		}
	}
	roles := make([]PromptHistoryRoleBreakdown, 0, len(roleAgg))
	for _, item := range roleAgg {
		item.EstimatedTokens = estimateTokens(item.Chars)
		roles = append(roles, *item)
	}
	sort.Slice(roles, func(i, j int) bool { return roles[i].Chars > roles[j].Chars })
	tools := make([]PromptHistoryToolBreakdown, 0, len(toolAgg))
	for _, item := range toolAgg {
		item.EstimatedTokens = estimateTokens(item.Chars)
		tools = append(tools, *item)
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Chars > tools[j].Chars })
	return PromptHistoryBreakdown{
		MessageCount:    len(history),
		TotalChars:      totalChars,
		EstimatedTokens: estimateTokens(totalChars),
		ByRole:          roles,
		ToolMessages:    tools,
		LargestMessage:  largest,
	}
}

func buildToolSchemaBreakdown(defs []providers.ToolDefinition) PromptToolSchemaReport {
	largest := make([]PromptToolSchemaSummary, 0, len(defs))
	total := 0
	for _, def := range defs {
		data, _ := json.Marshal(def)
		chars := len(data)
		total += chars
		largest = append(largest, PromptToolSchemaSummary{
			Name:            def.Function.Name,
			Chars:           chars,
			EstimatedTokens: estimateTokens(chars),
		})
	}
	sort.Slice(largest, func(i, j int) bool { return largest[i].Chars > largest[j].Chars })
	if len(largest) > 10 {
		largest = largest[:10]
	}
	return PromptToolSchemaReport{
		Count:           len(defs),
		TotalChars:      total,
		EstimatedTokens: estimateTokens(total),
		Largest:         largest,
	}
}

func buildJoinSeparatorChars(identity string, hasBootstrap bool, hasSkills bool, hasMemory bool) int {
	parts := 0
	if identity != "" {
		parts++
	}
	if hasBootstrap {
		parts++
	}
	if hasSkills {
		parts++
	}
	if hasMemory {
		parts++
	}
	if parts <= 1 {
		return 0
	}
	return (parts - 1) * len("\n\n---\n\n")
}

func estimateTokens(chars int) int {
	if chars <= 0 {
		return 0
	}
	return (chars + 2) / 4
}

func compactPreview(s string, max int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func parseSessionKey(key string) (string, string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", ""
	}
	at := strings.LastIndex(key, "@")
	base := key
	if at > 0 {
		base = key[:at]
	}
	colon := strings.Index(base, ":")
	if colon <= 0 || colon >= len(base)-1 {
		return "", ""
	}
	return base[:colon], base[colon+1:]
}
