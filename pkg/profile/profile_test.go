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

func TestListEmpty(t *testing.T) {
	store := NewStore(t.TempDir())
	ids, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected empty list, got %v", ids)
	}
}

func TestListMissingDir(t *testing.T) {
	store := NewStore(filepath.Join(t.TempDir(), "does-not-exist"))
	ids, err := store.List()
	if err != nil {
		t.Fatalf("List should not error on missing dir, got %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected empty list, got %v", ids)
	}
}

func TestListSorted(t *testing.T) {
	store := NewStore(t.TempDir())
	for _, sender := range []string{"discord:zeta", "discord:alpha", "discord:mu"} {
		if err := store.SetAnswerTheme(sender, "", ThemeClear); err != nil {
			t.Fatalf("SetAnswerTheme %s: %v", sender, err)
		}
	}
	ids, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 3 {
		t.Fatalf("expected 3 entries, got %d: %v", len(ids), ids)
	}
	want := []string{"discord:alpha", "discord:mu", "discord:zeta"}
	for i, id := range ids {
		if id != want[i] {
			t.Errorf("ids[%d] = %q, want %q (full: %v)", i, id, want[i], ids)
		}
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

func TestOnProfileUpdatedCallback(t *testing.T) {
	store := NewStore(t.TempDir())

	var gotSender string
	var gotProfile *UserProfile
	var calls int
	store.OnProfileUpdated = func(senderID string, p *UserProfile) {
		calls++
		gotSender = senderID
		gotProfile = p
	}

	if err := store.SetAnswerTheme("user42", "Alice", ThemeFormal); err != nil {
		t.Fatalf("SetAnswerTheme: %v", err)
	}
	if calls != 1 {
		t.Fatalf("expected callback to fire once, got %d calls", calls)
	}
	if gotSender != "user42" {
		t.Errorf("callback sender = %q, want user42", gotSender)
	}
	if gotProfile == nil {
		t.Fatal("callback profile was nil")
	}
	if gotProfile.AnswerTheme != ThemeFormal {
		t.Errorf("callback profile theme = %q, want %q", gotProfile.AnswerTheme, ThemeFormal)
	}
	if gotProfile.DisplayName != "Alice" {
		t.Errorf("callback profile display name = %q, want Alice", gotProfile.DisplayName)
	}
	if gotProfile.UpdatedAt == "" {
		t.Error("callback profile UpdatedAt was empty; expected post-Save value")
	}
}

func TestOnProfileUpdatedNotCalledOnInvalidTheme(t *testing.T) {
	store := NewStore(t.TempDir())
	var calls int
	store.OnProfileUpdated = func(senderID string, p *UserProfile) { calls++ }

	if err := store.SetAnswerTheme("user1", "", "bogus"); err == nil {
		t.Fatal("expected error for invalid theme")
	}
	if calls != 0 {
		t.Fatalf("callback should not fire when SetAnswerTheme fails; got %d calls", calls)
	}
}

func TestOnProfileUpdatedNilIsNoOp(t *testing.T) {
	store := NewStore(t.TempDir())
	// Unset callback must not panic.
	if err := store.SetAnswerTheme("user1", "", ThemeClear); err != nil {
		t.Fatalf("SetAnswerTheme with nil callback: %v", err)
	}
}
