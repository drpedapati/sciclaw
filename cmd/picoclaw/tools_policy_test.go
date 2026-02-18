package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureToolsCLIFirstPolicy_AppendsWhenMissing(t *testing.T) {
	workspace := t.TempDir()
	toolsPath := filepath.Join(workspace, "TOOLS.md")
	initial := "# Tools\n\nCustom heading.\n"
	if err := os.WriteFile(toolsPath, []byte(initial), 0644); err != nil {
		t.Fatalf("seed TOOLS.md: %v", err)
	}

	if err := ensureToolsCLIFirstPolicy(workspace); err != nil {
		t.Fatalf("ensureToolsCLIFirstPolicy returned error: %v", err)
	}

	after, err := os.ReadFile(toolsPath)
	if err != nil {
		t.Fatalf("read TOOLS.md: %v", err)
	}
	txt := string(after)
	if !strings.Contains(txt, "Custom heading.") {
		t.Fatalf("original TOOLS.md content was lost")
	}
	if !strings.Contains(txt, toolsCLIFirstPolicyHeading) {
		t.Fatalf("CLI-first section missing after ensure")
	}
}

func TestEnsureToolsCLIFirstPolicy_Idempotent(t *testing.T) {
	workspace := t.TempDir()
	toolsPath := filepath.Join(workspace, "TOOLS.md")
	initial := "# Tools\n\n" + strings.TrimSpace(toolsCLIFirstPolicySection) + "\n"
	if err := os.WriteFile(toolsPath, []byte(initial), 0644); err != nil {
		t.Fatalf("seed TOOLS.md: %v", err)
	}

	if err := ensureToolsCLIFirstPolicy(workspace); err != nil {
		t.Fatalf("ensureToolsCLIFirstPolicy returned error: %v", err)
	}

	after, err := os.ReadFile(toolsPath)
	if err != nil {
		t.Fatalf("read TOOLS.md: %v", err)
	}
	if strings.Count(string(after), toolsCLIFirstPolicyHeading) != 1 {
		t.Fatalf("expected policy heading exactly once, got:\n%s", string(after))
	}
}
