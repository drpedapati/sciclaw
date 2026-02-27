package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveBundledSkillsDirForExecutableFindsDevFormulaShareDir(t *testing.T) {
	root := t.TempDir()
	exePath := filepath.Join(root, "Cellar", "sciclaw-dev", "0.1.66-dev.15", "bin", "sciclaw")
	want := filepath.Join(root, "Cellar", "sciclaw-dev", "0.1.66-dev.15", "share", "sciclaw-dev", "skills")

	if err := os.MkdirAll(filepath.Dir(exePath), 0o755); err != nil {
		t.Fatalf("mkdir exe dir: %v", err)
	}
	if err := os.WriteFile(exePath, []byte(""), 0o755); err != nil {
		t.Fatalf("write exe: %v", err)
	}

	skillDir := filepath.Join(want, "pandoc-docx")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir skills dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# test"), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	got := resolveBundledSkillsDirForExecutable(exePath)
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("resolveBundledSkillsDirForExecutable() = %q, want %q", got, want)
	}
}

