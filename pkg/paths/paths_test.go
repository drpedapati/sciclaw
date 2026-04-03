package paths

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAppHomeEnvOverride(t *testing.T) {
	t.Setenv("SCICLAW_HOME", "~/custom-sciclaw")
	ResetForTest()
	defer ResetForTest()

	home, _ := os.UserHomeDir()
	got := AppHome()
	want := filepath.Join(home, "custom-sciclaw")
	if got != want {
		t.Fatalf("AppHome()=%q want %q", got, want)
	}
}

func TestAppHomeLegacyFallback(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("SCICLAW_HOME", "")
	ResetForTest()
	defer ResetForTest()

	oldPath := filepath.Join(root, ".picoclaw")
	if err := os.MkdirAll(oldPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(oldPath, "config.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	got := AppHome()
	if got != oldPath {
		t.Fatalf("AppHome()=%q want %q", got, oldPath)
	}
}

func TestAppHomeDefaultsToSciclaw(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("SCICLAW_HOME", "")
	ResetForTest()
	defer ResetForTest()

	got := AppHome()
	want := filepath.Join(root, "sciclaw")
	if got != want {
		t.Fatalf("AppHome()=%q want %q", got, want)
	}
}
