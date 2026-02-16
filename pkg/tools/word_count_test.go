package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWordCountTool_Text(t *testing.T) {
	tool := NewWordCountTool("", false)
	res := tool.Execute(context.Background(), map[string]interface{}{
		"text": "alpha beta\ngamma",
	})
	if res.IsError {
		t.Fatalf("expected success, got: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, `"words": 3`) {
		t.Fatalf("expected word count in response, got: %s", res.ForLLM)
	}
}

func TestWordCountTool_File(t *testing.T) {
	workspace := t.TempDir()
	path := filepath.Join(workspace, "note.txt")
	if err := os.WriteFile(path, []byte("one two three\nfour"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	tool := NewWordCountTool(workspace, true)
	res := tool.Execute(context.Background(), map[string]interface{}{
		"path": path,
	})
	if res.IsError {
		t.Fatalf("expected success, got: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, `"words": 4`) {
		t.Fatalf("expected 4 words, got: %s", res.ForLLM)
	}
}

func TestWordCountTool_PathRestriction(t *testing.T) {
	workspace := t.TempDir()
	tool := NewWordCountTool(workspace, true)
	res := tool.Execute(context.Background(), map[string]interface{}{
		"path": filepath.Join("..", "outside.txt"),
	})
	if !res.IsError {
		t.Fatalf("expected restriction error")
	}
}
