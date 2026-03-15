package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckToolsPolicyReportsStaleAndFixesIt(t *testing.T) {
	workspace := t.TempDir()
	toolsPath := filepath.Join(workspace, "TOOLS.md")
	initial := "# Tools\n\n## Critical CLI-First Rules\n\n- For PubMed literature tasks, use the installed `pubmed`/`pubmed-cli` directly.\n- Use `docx-review` only for tracked-change edits/comments/diff on existing documents.\n\n### PubMed Examples (Preferred)\n\n```bash\npubmed search \"query\" --json\n```\n"
	if err := os.WriteFile(toolsPath, []byte(initial), 0o644); err != nil {
		t.Fatalf("seed TOOLS.md: %v", err)
	}

	check := checkToolsPolicy(workspace, doctorOptions{})
	if check.Status != doctorWarn || !strings.Contains(check.Message, "stale") {
		t.Fatalf("checkToolsPolicy() = %#v, want stale warning", check)
	}

	check = checkToolsPolicy(workspace, doctorOptions{Fix: true})
	if check.Status != doctorOK || !strings.Contains(check.Message, "refreshed") {
		t.Fatalf("checkToolsPolicy(fix) = %#v, want refreshed ok", check)
	}

	after, err := os.ReadFile(toolsPath)
	if err != nil {
		t.Fatalf("read TOOLS.md: %v", err)
	}
	txt := string(after)
	if !strings.Contains(txt, "pubmed_search") || !strings.Contains(txt, "pubmed_fetch") {
		t.Fatalf("refreshed TOOLS.md missing typed PubMed guidance:\n%s", txt)
	}
	if !strings.Contains(txt, "xlsx_review_read") || !strings.Contains(txt, "pptx_review_read") {
		t.Fatalf("refreshed TOOLS.md missing xlsx/pptx guidance:\n%s", txt)
	}
}
