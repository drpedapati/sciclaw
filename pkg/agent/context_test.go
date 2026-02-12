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
