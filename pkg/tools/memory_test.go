package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRememberTool_AcceptsMethodDecision(t *testing.T) {
	workspace := t.TempDir()
	tool := NewRememberTool(workspace, true)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"category":         "method_decision",
		"content":          "Use subject suffix 03a for fixed PreSMART EEG FIF files.",
		"reason":           "Future analyses need the corrected subject identifier across sessions.",
		"source_artifacts": []interface{}{"data/PreSMART_R61_03/EEG/PreSMART_03a_SST_fixed_raw.fif"},
	})

	if result.IsError {
		t.Fatalf("expected memory write to succeed, got: %s", result.ForLLM)
	}

	content, err := os.ReadFile(filepath.Join(workspace, longTermMemoryRelativePath))
	if err != nil {
		t.Fatalf("read memory: %v", err)
	}
	memory := string(content)
	for _, want := range []string{
		"## Curated Memory",
		"### Method Decisions",
		"Use subject suffix 03a",
		"Reason: Future analyses need",
		"Sources: data/PreSMART_R61_03/EEG/PreSMART_03a_SST_fixed_raw.fif",
	} {
		if !strings.Contains(memory, want) {
			t.Fatalf("memory missing %q:\n%s", want, memory)
		}
	}
}

func TestRememberTool_WritesUnderExistingStableSection(t *testing.T) {
	workspace := t.TempDir()
	memoryPath := filepath.Join(workspace, longTermMemoryRelativePath)
	if err := os.MkdirAll(filepath.Dir(memoryPath), 0755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	initial := "# Long-term Memory\n\n## Method Decisions\n\n- Decision:\n\n## Data Provenance\n\n- Dataset/source:\n"
	if err := os.WriteFile(memoryPath, []byte(initial), 0644); err != nil {
		t.Fatalf("write memory: %v", err)
	}

	tool := NewRememberTool(workspace, true)
	result := tool.Execute(context.Background(), map[string]interface{}{
		"category": "method_decision",
		"content":  "Use the fixed FIF file for subject 03a when available.",
		"reason":   "The corrected EEG identifier changes future file selection.",
	})
	if result.IsError {
		t.Fatalf("expected memory write to succeed, got: %s", result.ForLLM)
	}

	content, err := os.ReadFile(memoryPath)
	if err != nil {
		t.Fatalf("read memory: %v", err)
	}
	memory := string(content)
	entryIndex := strings.Index(memory, "Use the fixed FIF file")
	nextSectionIndex := strings.Index(memory, "## Data Provenance")
	if entryIndex < 0 || nextSectionIndex < 0 || entryIndex > nextSectionIndex {
		t.Fatalf("entry was not written under Method Decisions:\n%s", memory)
	}
	if strings.Contains(memory, "## Curated Memory") {
		t.Fatalf("unexpected duplicate curated section:\n%s", memory)
	}
}

func TestRememberTool_WritesKnownIssueUnderTemplateSection(t *testing.T) {
	workspace := t.TempDir()
	memoryPath := filepath.Join(workspace, longTermMemoryRelativePath)
	if err := os.MkdirAll(filepath.Dir(memoryPath), 0755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	initial := "# Long-term Memory\n\n## Known Recurring Issues\n\n- Issue:\n\n## Open Questions\n\n- Question:\n"
	if err := os.WriteFile(memoryPath, []byte(initial), 0644); err != nil {
		t.Fatalf("write memory: %v", err)
	}

	tool := NewRememberTool(workspace, true)
	result := tool.Execute(context.Background(), map[string]interface{}{
		"category": "known_issue",
		"content":  "PreSMART fixed FIF subject identifiers may include a letter suffix.",
		"reason":   "This affects future subject matching decisions.",
	})
	if result.IsError {
		t.Fatalf("expected memory write to succeed, got: %s", result.ForLLM)
	}

	content, err := os.ReadFile(memoryPath)
	if err != nil {
		t.Fatalf("read memory: %v", err)
	}
	memory := string(content)
	entryIndex := strings.Index(memory, "letter suffix")
	nextSectionIndex := strings.Index(memory, "## Open Questions")
	if entryIndex < 0 || nextSectionIndex < 0 || entryIndex > nextSectionIndex {
		t.Fatalf("entry was not written under Known Recurring Issues:\n%s", memory)
	}
	if strings.Contains(memory, "### Known Issues") {
		t.Fatalf("unexpected duplicate known-issues section:\n%s", memory)
	}
}

func TestRememberTool_RejectsExecutionLogCategory(t *testing.T) {
	workspace := t.TempDir()
	tool := NewRememberTool(workspace, true)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"category": "execution_log",
		"content":  "The consistency check completed successfully.",
		"reason":   "Record the run.",
	})

	if !result.IsError {
		t.Fatalf("expected execution_log category to be rejected")
	}
	if !strings.Contains(result.ForLLM, "Routine execution logs belong") {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
	if _, err := os.Stat(filepath.Join(workspace, longTermMemoryRelativePath)); !os.IsNotExist(err) {
		t.Fatalf("expected no memory file, stat err=%v", err)
	}
}

func TestRememberTool_RejectsRoutineSuccessContent(t *testing.T) {
	workspace := t.TempDir()
	tool := NewRememberTool(workspace, true)

	result := tool.Execute(context.Background(), map[string]interface{}{
		"category": "data_provenance",
		"content":  "Command completed successfully and workbook exists with 36,309 bytes.",
		"reason":   "The user may ask about this run later.",
	})

	if !result.IsError {
		t.Fatalf("expected routine success content to be rejected")
	}
	if !strings.Contains(result.ForLLM, "routine execution log") {
		t.Fatalf("unexpected error: %s", result.ForLLM)
	}
}
