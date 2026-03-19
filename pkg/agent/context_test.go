package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/tools"
)

func TestBuildSystemPromptUsesSciClawIdentity(t *testing.T) {
	workspace := t.TempDir()
	cb := NewContextBuilder(workspace)

	prompt := cb.BuildSystemPrompt()

	if !strings.Contains(prompt, "# sciClaw") {
		t.Fatalf("system prompt missing sciClaw identity header")
	}
	if !strings.Contains(prompt, "paired-scientist assistant") {
		t.Fatalf("system prompt missing paired-scientist identity description")
	}
	if !strings.Contains(prompt, "Reproducibility") {
		t.Fatalf("system prompt missing reproducibility rule")
	}
	if !strings.Contains(prompt, "PubMed-first verification") {
		t.Fatalf("system prompt missing PubMed-first verification rule")
	}
	if !strings.Contains(prompt, "start with the dedicated `pubmed_search` and `pubmed_fetch` tools") {
		t.Fatalf("system prompt missing explicit typed PubMed guidance")
	}
}

func TestLoadBootstrapFilesIncludesTools(t *testing.T) {
	workspace := t.TempDir()
	files := map[string]string{
		"AGENTS.md":   "# Agents\n",
		"SOUL.md":     "# Soul\n",
		"USER.md":     "# User\n",
		"IDENTITY.md": "# Identity\n",
		"TOOLS.md":    "# Tools\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	cb := NewContextBuilder(workspace)
	bootstrap := cb.LoadBootstrapFiles()

	if !strings.Contains(bootstrap, "## TOOLS.md") {
		t.Fatalf("bootstrap content missing TOOLS.md section")
	}
	if !strings.Contains(bootstrap, "# Tools") {
		t.Fatalf("bootstrap content missing TOOLS.md body")
	}
}

func TestLoadBootstrapFilesFallsBackToGlobalWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	globalWorkspace := filepath.Join(home, "sciclaw")
	if err := os.MkdirAll(globalWorkspace, 0755); err != nil {
		t.Fatalf("mkdir global workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(globalWorkspace, "AGENTS.md"), []byte("# Global Agents\n"), 0644); err != nil {
		t.Fatalf("write global AGENTS.md: %v", err)
	}

	routedWorkspace := t.TempDir()
	cb := NewContextBuilder(routedWorkspace)
	bootstrap := cb.LoadBootstrapFiles()

	if !strings.Contains(bootstrap, "# Global Agents") {
		t.Fatalf("expected fallback AGENTS.md from global workspace, got: %q", bootstrap)
	}
}

func TestContextBuilderLoadsGlobalWorkspaceSkillsForRoutedWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	skillDir := filepath.Join(home, "sciclaw", "skills", "baseline")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Baseline\n"), 0644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	routedWorkspace := t.TempDir()
	cb := NewContextBuilder(routedWorkspace)
	info := cb.GetSkillsInfo()

	total, _ := info["total"].(int)
	if total < 1 {
		t.Fatalf("expected at least one skill from global workspace, got %v", info)
	}
}

func TestContextBuilderCanSuppressPromptToolSummaries(t *testing.T) {
	workspace := t.TempDir()
	cb := NewContextBuilder(workspace)
	registry := tools.NewToolRegistry()
	registry.Register(tools.NewWordCountTool(workspace, true))
	cb.SetToolsRegistry(registry)

	withSummaries := cb.BuildSystemPrompt()
	if !strings.Contains(withSummaries, "## Available Tools") {
		t.Fatalf("expected tool summaries in default system prompt")
	}

	cb.SetIncludePromptToolSummaries(false)
	withoutSummaries := cb.BuildSystemPrompt()
	if strings.Contains(withoutSummaries, "## Available Tools") {
		t.Fatalf("expected prompt tool summaries to be omitted when disabled")
	}
	if !strings.Contains(withoutSummaries, "ALWAYS use tools") {
		t.Fatalf("expected core tool-use rule to remain in system prompt")
	}
}
