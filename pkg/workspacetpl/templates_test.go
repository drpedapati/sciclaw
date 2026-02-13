package workspacetpl

import (
	"strings"
	"testing"
)

func TestLoadWorkspaceTemplates(t *testing.T) {
	templates, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	want := map[string]bool{
		"AGENTS.md":        true,
		"SOUL.md":          true,
		"TOOLS.md":         true,
		"USER.md":          true,
		"IDENTITY.md":      true,
		"memory/MEMORY.md": true,
	}

	if len(templates) != len(want) {
		t.Fatalf("template count = %d, want %d", len(templates), len(want))
	}

	for _, tpl := range templates {
		if !want[tpl.RelativePath] {
			t.Fatalf("unexpected template path: %s", tpl.RelativePath)
		}
		delete(want, tpl.RelativePath)
		if strings.TrimSpace(tpl.Content) == "" {
			t.Fatalf("empty template content: %s", tpl.RelativePath)
		}
	}

	for missing := range want {
		t.Fatalf("missing template path: %s", missing)
	}
}

func TestTemplateContentBranding(t *testing.T) {
	templates, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	byPath := map[string]string{}
	for _, tpl := range templates {
		byPath[tpl.RelativePath] = tpl.Content
	}

	if !strings.Contains(byPath["AGENTS.md"], "You are sciClaw") {
		t.Fatalf("AGENTS.md missing sciClaw identity")
	}
	if !strings.Contains(byPath["AGENTS.md"], "Baseline Scientific Skills") {
		t.Fatalf("AGENTS.md missing baseline skill manifest")
	}
	if !strings.Contains(byPath["TOOLS.md"], "Baseline Skill Policy") {
		t.Fatalf("TOOLS.md missing baseline skill policy")
	}
	if !strings.Contains(byPath["SOUL.md"], "I am sciClaw") {
		t.Fatalf("SOUL.md missing sciClaw identity")
	}
	if !strings.Contains(byPath["memory/MEMORY.md"], "Long-term Memory") {
		t.Fatalf("MEMORY template missing expected heading")
	}
}
