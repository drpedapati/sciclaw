package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/paths"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/skills"
	"github.com/sipeed/picoclaw/pkg/tools"
)

type ContextBuilder struct {
	workspace                  string
	sharedWorkspace            string
	version                    string
	skillsLoader               *skills.SkillsLoader
	memory                     *MemoryStore
	tools                      *tools.ToolRegistry // Direct reference to tool registry
	includePromptToolSummaries bool

	// Per-turn sender context (set before each BuildMessages call)
	senderID    string
	senderName  string
	answerTheme string
}

// SetSenderContext sets per-turn sender identity and answer theme.
// Call this before BuildMessages on each turn.
func (cb *ContextBuilder) SetSenderContext(senderID, displayName, theme string) {
	cb.senderID = senderID
	// Sanitize display name: single line, bounded length, no control chars
	name := strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r < 32 {
			return ' '
		}
		return r
	}, strings.TrimSpace(displayName))
	if len(name) > 64 {
		name = name[:64]
	}
	cb.senderName = name
	cb.answerTheme = theme
}

func getGlobalConfigDir() string {
	return paths.AppHome()
}

func defaultSharedWorkspaceDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, "sciclaw")
}

func resolveGlobalSkillsDir(sharedWorkspace string) string {
	sharedRoot := strings.TrimSpace(sharedWorkspace)
	if sharedRoot != "" {
		sharedSkills := filepath.Join(sharedRoot, "global-skills")
		if hasSkillsDir(sharedSkills) {
			return sharedSkills
		}
		// Default to shared workspace path even if skills are missing;
		// onboard/bootstrap should populate this location.
		return sharedSkills
	}

	base := strings.TrimSpace(getGlobalConfigDir())
	if base == "" {
		return ""
	}
	workspaceSkills := filepath.Join(base, "workspace", "skills")
	legacySkills := paths.GlobalSkillsDir()

	// Legacy fallback only when shared workspace is unspecified.
	if hasSkillsDir(workspaceSkills) {
		return workspaceSkills
	}
	if hasSkillsDir(legacySkills) {
		return legacySkills
	}

	// Default to workspace-based legacy path when nothing is present.
	return workspaceSkills
}

func hasSkillsDir(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(path, entry.Name(), "SKILL.md")); err == nil {
			return true
		}
	}
	return false
}

func NewContextBuilder(workspace string, sharedWorkspace ...string) *ContextBuilder {
	// builtin skills: skills directory in current project
	// Use the skills/ directory under the current working directory
	wd, _ := os.Getwd()
	builtinSkillsDir := filepath.Join(wd, "skills")
	sharedRoot := ""
	if len(sharedWorkspace) > 0 {
		sharedRoot = strings.TrimSpace(sharedWorkspace[0])
	}
	if sharedRoot == "" {
		sharedRoot = defaultSharedWorkspaceDir()
	}
	globalSkillsDir := resolveGlobalSkillsDir(sharedRoot)

	return &ContextBuilder{
		workspace:                  workspace,
		sharedWorkspace:            strings.TrimSpace(sharedRoot),
		skillsLoader:               skills.NewSkillsLoader(workspace, globalSkillsDir, builtinSkillsDir),
		memory:                     NewMemoryStore(workspace),
		includePromptToolSummaries: true,
	}
}

// SetToolsRegistry sets the tools registry for dynamic tool summary generation.
func (cb *ContextBuilder) SetToolsRegistry(registry *tools.ToolRegistry) {
	cb.tools = registry
}

// SetIncludePromptToolSummaries controls whether human-readable tool summaries
// are embedded into the system prompt. When the provider already receives native
// tool schemas, duplicating the tool list in prose wastes prompt tokens.
func (cb *ContextBuilder) SetIncludePromptToolSummaries(include bool) {
	cb.includePromptToolSummaries = include
}

// SetVersion sets the runtime version for inclusion in the system prompt.
func (cb *ContextBuilder) SetVersion(v string) {
	cb.version = v
}

func (cb *ContextBuilder) getIdentity() string {
	now := time.Now().Format("2006-01-02 15:04 (Monday)")
	workspacePath, _ := filepath.Abs(filepath.Join(cb.workspace))
	runtime := fmt.Sprintf("%s %s, Go %s", runtime.GOOS, runtime.GOARCH, runtime.Version())

	// Build tools section dynamically
	toolsSection := cb.buildToolsSection()

	versionStr := cb.version
	if versionStr == "" {
		versionStr = "dev"
	}

	return fmt.Sprintf(`# sciClaw v%s

You are sciClaw v%s, an autonomous paired-scientist execution assistant.

## Current Time
%s

## Runtime
%s

## Workspace
Your workspace is at: %s
- Memory: %s/memory/MEMORY.md
- Skills: %s/skills/{skill-name}/SKILL.md

%s

## Important Rules

1. **ALWAYS use tools** - When you need to perform an action (execute commands, read/write files, run searches, etc.), you MUST call the appropriate tool. Do NOT just say you'll do it or pretend to do it.

2. **Be scientifically rigorous** - Distinguish hypotheses from verified findings and cite evidence paths.

3. **Memory** - When remembering something, write to %s/memory/MEMORY.md

4. **Reproducibility** - Prefer idempotent actions and report assumptions/uncertainty.

5. **PubMed-first verification** - For citation checks, PMID lookup, and PubMed literature verification, start with the dedicated `+"`pubmed_search`"+` and `+"`pubmed_fetch`"+` tools. Use raw `+"`exec`"+` with the installed `+"`pubmed`"+` CLI only for advanced PubMed flags not covered by the typed tools. Do not start with `+"`web_fetch`"+` on PubMed or publisher pages when the task is bibliographic verification.`,
		versionStr, versionStr, now, runtime, workspacePath, workspacePath, workspacePath, toolsSection, workspacePath)
}

func (cb *ContextBuilder) buildToolsSection() string {
	if !cb.includePromptToolSummaries {
		return ""
	}
	if cb.tools == nil {
		return ""
	}

	summaries := cb.tools.GetSummaries()
	if len(summaries) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Available Tools\n\n")
	sb.WriteString("**CRITICAL**: You MUST use tools to perform actions. Do NOT pretend to execute commands or schedule tasks.\n\n")
	sb.WriteString("You have access to the following tools:\n\n")
	for _, s := range summaries {
		sb.WriteString(s)
		sb.WriteString("\n")
	}

	return sb.String()
}

func (cb *ContextBuilder) BuildSystemPrompt() string {
	parts := []string{}

	// Core identity section
	parts = append(parts, cb.getIdentity())

	// Bootstrap files
	bootstrapContent := cb.LoadBootstrapFiles()
	if bootstrapContent != "" {
		parts = append(parts, bootstrapContent)
	}

	// Active user profile (injected after bootstrap so theme definitions are in context)
	if cb.answerTheme != "" {
		userSection := "## Active User\n"
		if cb.senderName != "" {
			userSection += fmt.Sprintf("Name: %s\n", cb.senderName)
		}
		userSection += fmt.Sprintf("Answer Theme: %s\n\nIMPORTANT: Follow the %s theme rules from the Answer Themes section above.",
			cb.answerTheme, cb.answerTheme)
		parts = append(parts, userSection)
	}

	// Skills - show summary, AI can read full content with read_file tool
	skillsSummary := cb.skillsLoader.BuildSkillsSummary()
	if skillsSummary != "" {
		parts = append(parts, fmt.Sprintf(`# Skills

The following skills extend your capabilities. To use a skill, read its SKILL.md file using the read_file tool.

%s`, skillsSummary))
	}

	// Memory context
	memoryContext := cb.memory.GetMemoryContext()
	if memoryContext != "" {
		parts = append(parts, "# Memory\n\n"+memoryContext)
	}

	// Join with "---" separator
	return strings.Join(parts, "\n\n---\n\n")
}

func (cb *ContextBuilder) LoadBootstrapFiles() string {
	bootstrapFiles := []string{
		"AGENTS.md",
		"SOUL.md",
		"USER.md",
		"IDENTITY.md",
		"TOOLS.md",
	}

	primaryWorkspace := filepath.Clean(cb.workspace)
	fallbackWorkspace := filepath.Clean(cb.sharedWorkspace)

	var result strings.Builder
	for _, filename := range bootstrapFiles {
		candidates := []string{filepath.Join(primaryWorkspace, filename)}
		if fallbackWorkspace != "" && fallbackWorkspace != "." && fallbackWorkspace != primaryWorkspace {
			candidates = append(candidates, filepath.Join(fallbackWorkspace, filename))
		}

		for _, filePath := range candidates {
			if data, err := os.ReadFile(filePath); err == nil {
				result.WriteString(fmt.Sprintf("## %s\n\n%s\n\n", filename, string(data)))
				break
			}
		}
	}

	return result.String()
}

func (cb *ContextBuilder) BuildMessages(history []providers.Message, summary string, currentMessage string, media []string, channel, chatID string) []providers.Message {
	messages := []providers.Message{}

	systemPrompt := cb.BuildSystemPrompt()

	// Add Current Session info if provided
	if channel != "" && chatID != "" {
		systemPrompt += fmt.Sprintf("\n\n## Current Session\nChannel: %s\nChat ID: %s", channel, chatID)
	}

	// Log system prompt summary for debugging (debug mode only)
	logger.DebugCF("agent", "System prompt built",
		map[string]interface{}{
			"total_chars":   len(systemPrompt),
			"total_lines":   strings.Count(systemPrompt, "\n") + 1,
			"section_count": strings.Count(systemPrompt, "\n\n---\n\n") + 1,
		})

	// Log preview of system prompt (avoid logging huge content)
	preview := systemPrompt
	if len(preview) > 500 {
		preview = preview[:500] + "... (truncated)"
	}
	logger.DebugCF("agent", "System prompt preview",
		map[string]interface{}{
			"preview": preview,
		})

	if summary != "" {
		systemPrompt += "\n\n## Summary of Previous Conversation\n\n" + summary
	}

	history, removed := trimLeadingOrphanedToolMessages(history)
	for i := 0; i < removed; i++ {
		logger.DebugCF("agent", "Removing orphaned tool message from history to prevent LLM error",
			map[string]interface{}{"role": "tool"})
	}

	messages = append(messages, providers.Message{
		Role:    "system",
		Content: systemPrompt,
	})

	messages = append(messages, history...)

	messages = append(messages, providers.Message{
		Role:    "user",
		Content: currentMessage,
	})

	return messages
}

func trimLeadingOrphanedToolMessages(history []providers.Message) ([]providers.Message, int) {
	removed := 0
	for len(history) > 0 && history[0].Role == "tool" {
		history = history[1:]
		removed++
	}
	return history, removed
}

func (cb *ContextBuilder) AddToolResult(messages []providers.Message, toolCallID, toolName, result string) []providers.Message {
	messages = append(messages, providers.Message{
		Role:       "tool",
		Content:    result,
		ToolCallID: toolCallID,
	})
	return messages
}

func (cb *ContextBuilder) AddAssistantMessage(messages []providers.Message, content string, toolCalls []map[string]interface{}) []providers.Message {
	msg := providers.Message{
		Role:    "assistant",
		Content: content,
	}
	// Always add assistant message, whether or not it has tool calls
	messages = append(messages, msg)
	return messages
}

func (cb *ContextBuilder) loadSkills() string {
	allSkills := cb.skillsLoader.ListSkills()
	if len(allSkills) == 0 {
		return ""
	}

	var skillNames []string
	for _, s := range allSkills {
		skillNames = append(skillNames, s.Name)
	}

	content := cb.skillsLoader.LoadSkillsForContext(skillNames)
	if content == "" {
		return ""
	}

	return "# Skill Definitions\n\n" + content
}

// GetSkillsInfo returns information about loaded skills.
func (cb *ContextBuilder) GetSkillsInfo() map[string]interface{} {
	allSkills := cb.skillsLoader.ListSkills()
	skillNames := make([]string, 0, len(allSkills))
	for _, s := range allSkills {
		skillNames = append(skillNames, s.Name)
	}
	return map[string]interface{}{
		"total":     len(allSkills),
		"available": len(allSkills),
		"names":     skillNames,
	}
}
