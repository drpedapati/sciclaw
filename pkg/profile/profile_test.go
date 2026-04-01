package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissing(t *testing.T) {
	store := NewStore(t.TempDir())
	p, err := store.Load("nonexistent")
	if err != nil {
		t.Fatalf("expected no error for missing profile, got %v", err)
	}
	if p.AnswerTheme != ThemeClear {
		t.Fatalf("expected clear default, got %q", p.AnswerTheme)
	}
}

func TestSaveAndLoad(t *testing.T) {
	store := NewStore(t.TempDir())
	err := store.SetAnswerTheme("user123", "Alice", ThemeBrief)
	if err != nil {
		t.Fatalf("SetAnswerTheme: %v", err)
	}

	p, err := store.Load("user123")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.AnswerTheme != ThemeBrief {
		t.Fatalf("expected brief, got %q", p.AnswerTheme)
	}
	if p.DisplayName != "Alice" {
		t.Fatalf("expected Alice, got %q", p.DisplayName)
	}
	if p.UpdatedAt == "" {
		t.Fatal("expected UpdatedAt to be set")
	}
}

func TestAnswerThemeDefault(t *testing.T) {
	store := NewStore(t.TempDir())
	theme := store.AnswerTheme("nobody")
	if theme != ThemeClear {
		t.Fatalf("expected clear, got %q", theme)
	}
}

func TestInvalidTheme(t *testing.T) {
	store := NewStore(t.TempDir())
	err := store.SetAnswerTheme("user1", "", "invalid")
	if err == nil {
		t.Fatal("expected error for invalid theme")
	}
}

func TestOverwrite(t *testing.T) {
	store := NewStore(t.TempDir())
	store.SetAnswerTheme("user1", "Bob", ThemeFormal)
	store.SetAnswerTheme("user1", "", ThemeBrief)

	p, _ := store.Load("user1")
	if p.AnswerTheme != ThemeBrief {
		t.Fatalf("expected brief after overwrite, got %q", p.AnswerTheme)
	}
	if p.DisplayName != "Bob" {
		t.Fatalf("expected Bob preserved, got %q", p.DisplayName)
	}
}

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	store := NewStore(dir)
	store.SetAnswerTheme("user1", "Test", ThemeClear)

	// No .tmp files should remain
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".tmp" {
			t.Fatalf("temp file not cleaned up: %s", e.Name())
		}
	}
}

func TestPathSanitization(t *testing.T) {
	store := NewStore(t.TempDir())
	err := store.SetAnswerTheme("../../etc/passwd", "hacker", ThemeClear)
	if err != nil {
		t.Fatalf("SetAnswerTheme: %v", err)
	}
	// Should create a safe filename, not traverse directories
	p, err := store.Load("../../etc/passwd")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if p.DisplayName != "hacker" {
		t.Fatalf("expected hacker, got %q", p.DisplayName)
	}
}

func TestIsValidTheme(t *testing.T) {
	for _, theme := range []string{"clear", "formal", "brief"} {
		if !IsValidTheme(theme) {
			t.Fatalf("expected %q to be valid", theme)
		}
	}
	for _, theme := range []string{"", "invalid", "CLEAR", "Clear"} {
		if IsValidTheme(theme) {
			t.Fatalf("expected %q to be invalid", theme)
		}
	}
}
