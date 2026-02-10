package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/skills"
)

type ContextBuilder struct {
	workspace    string
	skillsLoader *skills.SkillsLoader
	memory       *MemoryStore
}

func NewContextBuilder(workspace string) *ContextBuilder {
	builtinSkillsDir := filepath.Join(filepath.Dir(workspace), "picoclaw", "skills")
	return &ContextBuilder{
		workspace:    workspace,
		skillsLoader: skills.NewSkillsLoader(workspace, builtinSkillsDir),
		memory:       NewMemoryStore(workspace),
	}
}

func (cb *ContextBuilder) getIdentity() string {
	now := time.Now().Format("2006-01-02 15:04 (Monday)")
	workspacePath, _ := filepath.Abs(filepath.Join(cb.workspace))
	runtime := fmt.Sprintf("%s %s, Go %s", runtime.GOOS, runtime.GOARCH, runtime.Version())

	return fmt.Sprintf(`# picoclaw 🦞

You are picoclaw, a helpful AI assistant. You have access to tools that allow you to:
- Read, write, and edit files
- Execute shell commands
- Search the web and fetch web pages
- Send messages to users on chat channels
- Spawn subagents for complex background tasks

## Current Time
%s

## Runtime
%s

## Workspace
Your workspace is at: %s
- Memory files: %s/memory/MEMORY.md
- Daily notes: %s/memory/2006-01-02.md
- Custom skills: %s/skills/{skill-name}/SKILL.md

## Weather Information
When users ask about weather, use the web_fetch tool with wttr.in URLs:
- Current weather: https://wttr.in/{city}?format=j1
- Beijing: https://wttr.in/Beijing?format=j1
- Shanghai: https://wttr.in/Shanghai?format=j1
- New York: https://wttr.in/New_York?format=j1
- London: https://wttr.in/London?format=j1
- Tokyo: https://wttr.in/Tokyo?format=j1

IMPORTANT: When responding to direct questions or conversations, reply directly with your text response.
Only use the 'message' tool when you need to send a message to a specific chat channel (like WhatsApp).
For normal conversation, just respond with text - do not call the message tool.

Always be helpful, accurate, and concise. When using tools, explain what you're doing.
When remembering something, write to %s/memory/MEMORY.md`,
		now, runtime, workspacePath, workspacePath, workspacePath, workspacePath, workspacePath)
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

	// Skills - progressive loading
	// 1. Always skills: load full content
	alwaysSkills := cb.skillsLoader.GetAlwaysSkills()
	if len(alwaysSkills) > 0 {
		alwaysContent := cb.skillsLoader.LoadSkillsForContext(alwaysSkills)
		if alwaysContent != "" {
			parts = append(parts, "# Active Skills\n\n"+alwaysContent)
		}
	}

	// 2. Available skills: only show summary
	skillsSummary := cb.skillsLoader.BuildSkillsSummary()
	if skillsSummary != "" {
		parts = append(parts, fmt.Sprintf(`# Skills

The following skills extend your capabilities. To use a skill, read its SKILL.md file.

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
		"TOOLS.md",
		"IDENTITY.md",
		"MEMORY.md",
	}

	var result string
	for _, filename := range bootstrapFiles {
		filePath := filepath.Join(cb.workspace, filename)
		if data, err := os.ReadFile(filePath); err == nil {
			result += fmt.Sprintf("## %s\n\n%s\n\n", filename, string(data))
		}
	}

	return result
}

<<<<<<< HEAD
func (cb *ContextBuilder) BuildMessages(history []providers.Message, summary string, currentMessage string, media []string) []providers.Message {
=======
func (cb *ContextBuilder) BuildMessages(history []providers.Message, summary string, currentMessage string, media []string, channel, chatID string) []providers.Message {
>>>>>>> fd1dd87 (Add memory system, debug mode, and tools)
	messages := []providers.Message{}

	systemPrompt := cb.BuildSystemPrompt()

	// Add Current Session info if provided
	if channel != "" && chatID != "" {
		systemPrompt += fmt.Sprintf("\n\n## Current Session\nChannel: %s\nChat ID: %s", channel, chatID)
	}

	// Log system prompt for debugging
	logger.InfoCF("agent", "System prompt built",
		map[string]interface{}{
			"total_chars": len(systemPrompt),
			"total_lines": strings.Count(systemPrompt, "\n") + 1,
			"section_count": strings.Count(systemPrompt, "\n\n---\n\n") + 1,
		})
	logger.DebugCF("agent", "Full system prompt:\n"+systemPrompt, nil)

	if summary != "" {
		systemPrompt += "\n\n## Summary of Previous Conversation\n\n" + summary
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
	if len(toolCalls) > 0 {
		messages = append(messages, msg)
	}
	return messages
}

func (cb *ContextBuilder) loadSkills() string {
	allSkills := cb.skillsLoader.ListSkills(true)
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
	allSkills := cb.skillsLoader.ListSkills(true)
	skillNames := make([]string, 0, len(allSkills))
	availableCount := 0
	for _, s := range allSkills {
		skillNames = append(skillNames, s.Name)
		if s.Available {
			availableCount++
		}
	}
	return map[string]interface{}{
		"total":     len(allSkills),
		"available": availableCount,
		"names":     skillNames,
	}
}
