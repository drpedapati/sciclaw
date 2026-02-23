package tools

import (
	"context"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"
)

type WordCountTool struct {
	workspace               string
	restrict                bool
	sharedWorkspace         string
	sharedWorkspaceReadOnly bool
}

func NewWordCountTool(workspace string, restrict bool) *WordCountTool {
	return &WordCountTool{
		workspace: workspace,
		restrict:  restrict,
	}
}

func (t *WordCountTool) SetSharedWorkspacePolicy(sharedWorkspace string, sharedWorkspaceReadOnly bool) {
	t.sharedWorkspace = strings.TrimSpace(sharedWorkspace)
	t.sharedWorkspaceReadOnly = sharedWorkspaceReadOnly
}

func (t *WordCountTool) Name() string {
	return "word_count"
}

func (t *WordCountTool) Description() string {
	return "Count words/chars/lines from text or a file path without relying on shell stdin or pipes."
}

func (t *WordCountTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"text": map[string]interface{}{
				"type":        "string",
				"description": "Text to analyze directly",
			},
			"path": map[string]interface{}{
				"type":        "string",
				"description": "Optional file path to analyze",
			},
		},
	}
}

func (t *WordCountTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	_ = ctx

	text := getString(args, "text")
	path := getString(args, "path")
	if strings.TrimSpace(text) == "" && strings.TrimSpace(path) == "" {
		return ErrorResult("provide either text or path")
	}

	source := "text"
	if strings.TrimSpace(path) != "" {
		resolved, err := validatePathWithPolicy(path, t.workspace, t.restrict, AccessRead, t.sharedWorkspace, t.sharedWorkspaceReadOnly)
		if err != nil {
			return ErrorResult(err.Error())
		}
		data, err := os.ReadFile(resolved)
		if err != nil {
			return ErrorResult(fmt.Sprintf("failed to read file: %v", err))
		}
		text = string(data)
		path = resolved
		source = "file"
	}

	words := len(strings.Fields(text))
	lines := 0
	if text != "" {
		lines = strings.Count(text, "\n") + 1
	}

	result := map[string]interface{}{
		"status": "ok",
		"source": source,
		"words":  words,
		"lines":  lines,
		"chars":  utf8.RuneCountInString(text),
		"bytes":  len(text),
	}
	if source == "file" {
		result["path"] = path
	}

	return NewToolResult(mustJSON(result))
}
