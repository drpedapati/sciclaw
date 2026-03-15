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
	if !strings.Contains(txt, "xlsx_review_read") {
		t.Fatalf("xlsx typed-tool guidance missing after ensure")
	}
	if !strings.Contains(txt, "pptx_review_read") {
		t.Fatalf("pptx typed-tool guidance missing after ensure")
	}
	if !strings.Contains(txt, "pubmed_search") || !strings.Contains(txt, "pubmed_fetch") {
		t.Fatalf("typed pubmed guidance missing after ensure")
	}
	if !strings.Contains(txt, "weather_forecast") {
		t.Fatalf("weather tool guidance missing after ensure")
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

func TestEnsureToolsCLIFirstPolicy_RefreshesStaleSection(t *testing.T) {
	workspace := t.TempDir()
	toolsPath := filepath.Join(workspace, "TOOLS.md")
	initial := "# Tools\n\n## Critical CLI-First Rules\n\n- For common Word review workflows, prefer `docx_review_read`.\n\n### PubMed Examples (Preferred)\n\n```bash\npubmed search test\n```\n\n### Anti-Pattern (Avoid)\n\n```python\nsubprocess.check_output([\"pubmed\", \"search\", \"query\", \"--json\"])\n```\n"
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
	if !strings.Contains(txt, "pubmed_search") || !strings.Contains(txt, "pubmed_fetch") {
		t.Fatalf("expected refreshed pubmed guidance, got:\n%s", txt)
	}
	if !strings.Contains(txt, "weather_forecast") {
		t.Fatalf("expected refreshed weather guidance, got:\n%s", txt)
	}
	if !strings.Contains(txt, "xlsx_review_read") {
		t.Fatalf("expected refreshed xlsx guidance, got:\n%s", txt)
	}
	if !strings.Contains(txt, "pptx_review_read") {
		t.Fatalf("expected refreshed pptx guidance, got:\n%s", txt)
	}
	if strings.Count(txt, toolsCLIFirstPolicyHeading) != 1 {
		t.Fatalf("expected refreshed policy heading exactly once, got:\n%s", txt)
	}
}
