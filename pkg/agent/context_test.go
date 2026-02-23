package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	globalWorkspace := filepath.Join(home, ".picoclaw", "workspace")
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

	skillDir := filepath.Join(home, ".picoclaw", "workspace", "skills", "baseline")
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
