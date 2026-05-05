package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	longTermMemoryRelativePath = "memory/MEMORY.md"
	longTermMemoryGuardMessage = "Use the `remember` tool for long-term memory updates. Routine execution logs belong in session history, artifacts, jobs, hooks, or output files."
	maxMemoryContentChars      = 1200
	maxMemoryReasonChars       = 400
)

var memoryCategories = map[string]string{
	"user_preference":    "User Preferences",
	"project_convention": "Project Conventions",
	"canonical_artifact": "Canonical Artifacts",
	"method_decision":    "Method Decisions",
	"known_issue":        "Known Recurring Issues",
	"open_question":      "Open Questions",
	"data_provenance":    "Data Provenance",
}

var rejectedMemoryCategories = map[string]string{
	"execution_log":      "Routine execution logs belong in session history, artifacts, jobs, hooks, or output files.",
	"routine_success":    "Successful runs are already recorded by outputs and session history.",
	"temporary_artifact": "Temporary artifacts should stay in output paths, plans, or session notes.",
}

var executionLogPhrases = []string{
	"completed successfully",
	"command completed",
	"ran successfully",
	"script ran successfully",
	"workbook exists",
	"file exists",
	"file size",
	"bytes",
	"row count",
	"row counts",
	"smoke run",
	"validation:",
	"counts:",
	"overwrote canonical",
	"temporary output",
	"temporary artifact",
}

// RememberTool writes curated long-term memory entries.
type RememberTool struct {
	workspace string
	restrict  bool
}

func NewRememberTool(workspace string, restrict bool) *RememberTool {
	return &RememberTool{workspace: workspace, restrict: restrict}
}

func (t *RememberTool) Name() string {
	return "remember"
}

func (t *RememberTool) Description() string {
	return "Add a curated long-term memory entry for durable project context. Do not use for routine execution logs or successful run summaries."
}

func (t *RememberTool) Parameters() map[string]interface{} {
	categoryValues := make([]string, 0, len(memoryCategories))
	for category := range memoryCategories {
		categoryValues = append(categoryValues, category)
	}
	sort.Strings(categoryValues)
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"category": map[string]interface{}{
				"type":        "string",
				"description": "Durable memory category.",
				"enum":        categoryValues,
			},
			"content": map[string]interface{}{
				"type":        "string",
				"description": "Concise durable fact, decision, convention, issue, open question, or provenance note.",
			},
			"reason": map[string]interface{}{
				"type":        "string",
				"description": "Why this needs to persist across sessions.",
			},
			"source_artifacts": map[string]interface{}{
				"type":        "array",
				"description": "Optional artifact paths, job IDs, or files that support this memory entry.",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
		},
		"required": []string{"category", "content", "reason"},
	}
}

func (t *RememberTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	_ = ctx

	category, ok := args["category"].(string)
	if !ok || strings.TrimSpace(category) == "" {
		return ErrorResult("category is required")
	}
	category = strings.TrimSpace(category)

	if reason, rejected := rejectedMemoryCategories[category]; rejected {
		return UserErrorResult("Memory rejected: " + reason)
	}

	heading, ok := memoryCategories[category]
	if !ok {
		return UserErrorResult("Memory rejected: category must be one of user_preference, project_convention, canonical_artifact, method_decision, known_issue, open_question, or data_provenance.")
	}

	content, ok := args["content"].(string)
	if !ok || strings.TrimSpace(content) == "" {
		return ErrorResult("content is required")
	}
	content = strings.TrimSpace(content)
	if len(content) > maxMemoryContentChars {
		return UserErrorResult(fmt.Sprintf("Memory rejected: content is %d characters; keep entries under %d characters.", len(content), maxMemoryContentChars))
	}

	reason, ok := args["reason"].(string)
	if !ok || strings.TrimSpace(reason) == "" {
		return ErrorResult("reason is required")
	}
	reason = strings.TrimSpace(reason)
	if len(reason) > maxMemoryReasonChars {
		return UserErrorResult(fmt.Sprintf("Memory rejected: reason is %d characters; keep reasons under %d characters.", len(reason), maxMemoryReasonChars))
	}

	if looksLikeExecutionLog(content) || looksLikeExecutionLog(reason) {
		return UserErrorResult("Memory rejected: this looks like a routine execution log. Keep successful run details in session history, artifacts, jobs, hooks, or output files.")
	}

	memoryPath, err := validatePathWithPolicy(longTermMemoryRelativePath, t.workspace, t.restrict, AccessWrite, "", false)
	if err != nil {
		return UserErrorResult(err.Error())
	}

	if err := os.MkdirAll(filepath.Dir(memoryPath), 0755); err != nil {
		return ErrorResult(fmt.Sprintf("failed to create memory directory: %v", err))
	}

	existingBytes, err := os.ReadFile(memoryPath)
	if err != nil && !os.IsNotExist(err) {
		return ErrorResult(fmt.Sprintf("failed to read memory file: %v", err))
	}

	sources := parseStringList(args["source_artifacts"])
	entry := formatMemoryEntry(content, reason, sources, time.Now())
	updated := appendMemoryEntry(string(existingBytes), heading, entry)

	if err := os.WriteFile(memoryPath, []byte(updated), 0644); err != nil {
		return ErrorResult(fmt.Sprintf("failed to write memory file: %v", err))
	}

	return SilentResult(fmt.Sprintf("Remembered %s in %s", category, longTermMemoryRelativePath))
}

func appendMemoryEntry(existing, heading, entry string) string {
	existing = strings.TrimRight(existing, "\n")
	if strings.TrimSpace(existing) == "" {
		return "# Long-term Memory\n\n## Curated Memory\n\n### " + heading + "\n\n" + entry + "\n"
	}

	curatedHeading := "## Curated Memory"
	categoryHeading := "### " + heading
	if updated, ok := insertUnderHeading(existing, "## "+heading, []string{"## "}, entry); ok {
		return updated
	}
	if !strings.Contains(existing, curatedHeading) {
		return existing + "\n\n" + curatedHeading + "\n\n" + categoryHeading + "\n\n" + entry + "\n"
	}
	if updated, ok := insertUnderHeading(existing, categoryHeading, []string{"### ", "## "}, entry); ok {
		return updated
	}
	if updated, ok := insertUnderHeading(existing, curatedHeading, []string{"## "}, categoryHeading+"\n\n"+entry); ok {
		return updated
	}
	return existing + "\n\n" + categoryHeading + "\n\n" + entry + "\n"
}

func insertUnderHeading(existing, heading string, nextHeadingPrefixes []string, entry string) (string, bool) {
	idx := lineStartIndex(existing, heading)
	if idx < 0 {
		return "", false
	}

	searchStart := idx + len(heading)
	insertAt := len(existing)
	for _, prefix := range nextHeadingPrefixes {
		if nextRel := strings.Index(existing[searchStart:], "\n"+prefix); nextRel >= 0 && searchStart+nextRel < insertAt {
			insertAt = searchStart + nextRel
		}
	}

	before := strings.TrimRight(existing[:insertAt], "\n")
	after := existing[insertAt:]
	return before + "\n\n" + entry + after + "\n", true
}

func lineStartIndex(value, needle string) int {
	if strings.HasPrefix(value, needle) {
		return 0
	}
	idx := strings.Index(value, "\n"+needle)
	if idx < 0 {
		return -1
	}
	return idx + 1
}

func formatMemoryEntry(content, reason string, sources []string, now time.Time) string {
	content = oneLine(content)
	reason = oneLine(reason)
	lines := []string{
		fmt.Sprintf("- %s: %s", now.Format("2006-01-02"), content),
		fmt.Sprintf("  - Reason: %s", reason),
	}
	if len(sources) > 0 {
		lines = append(lines, "  - Sources: "+strings.Join(sources, ", "))
	}
	return strings.Join(lines, "\n")
}

func parseStringList(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		return cleanStringList(typed)
	case []interface{}:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if s, ok := item.(string); ok {
				values = append(values, s)
			}
		}
		return cleanStringList(values)
	default:
		return nil
	}
}

func cleanStringList(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = oneLine(strings.TrimSpace(value))
		if value != "" {
			cleaned = append(cleaned, value)
		}
	}
	return cleaned
}

func oneLine(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	parts := strings.Fields(value)
	return strings.Join(parts, " ")
}

func looksLikeExecutionLog(value string) bool {
	lower := strings.ToLower(value)
	for _, phrase := range executionLogPhrases {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

func isWorkspaceLongTermMemoryPath(path, workspace string) bool {
	if strings.TrimSpace(workspace) == "" {
		return false
	}

	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return false
	}
	target := filepath.Join(absWorkspace, longTermMemoryRelativePath)

	absPath := path
	if !filepath.IsAbs(absPath) {
		absPath = filepath.Join(absWorkspace, absPath)
	}
	absPath = filepath.Clean(absPath)
	target = filepath.Clean(target)
	if samePath(absPath, target) {
		return true
	}

	realTarget, targetErr := filepath.EvalSymlinks(target)
	realPath, pathErr := filepath.EvalSymlinks(absPath)
	return targetErr == nil && pathErr == nil && samePath(realPath, realTarget)
}
