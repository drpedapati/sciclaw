package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectWSLWith(t *testing.T) {
	fakeRead := func(_ string) ([]byte, error) { return []byte("Linux kernel"), nil }
	if !detectWSLWith(func(k string) string {
		if k == "WSL_INTEROP" {
			return "/run/WSL/interop"
		}
		return ""
	}, fakeRead) {
		t.Fatalf("expected WSL detection from env var")
	}

	if !detectWSLWith(func(string) string { return "" }, func(string) ([]byte, error) {
		return []byte("Linux version 5.15.90.1-microsoft-standard-WSL2"), nil
	}) {
		t.Fatalf("expected WSL detection from /proc/version")
	}

	if detectWSLWith(func(string) string { return "" }, func(string) ([]byte, error) {
		return []byte("Linux version 6.8.0"), nil
	}) {
		t.Fatalf("did not expect WSL detection")
	}
}

func TestRenderSystemdUnit(t *testing.T) {
	unit := renderSystemdUnit("/usr/local/bin/sciclaw", "/usr/local/bin:/usr/bin:/bin")
	mustContain(t, unit, "ExecStart=/usr/local/bin/sciclaw gateway")
	mustContain(t, unit, "Restart=always")
	mustContain(t, unit, "Environment=PATH=/usr/local/bin:/usr/bin:/bin")
	mustContain(t, unit, "WantedBy=default.target")
}

func TestBuildSystemdPath(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	sep := string(os.PathListSeparator)
	inputPath := strings.Join([]string{
		"/custom/bin",
		"/usr/bin",    // duplicate baseline
		"",            // empty should be ignored
		"/custom/bin", // duplicate custom
	}, sep)
	got := buildSystemdPath(inputPath, "/home/linuxbrew/.linuxbrew")
	parts := strings.Split(got, sep)

	if len(parts) == 0 {
		t.Fatalf("expected non-empty PATH")
	}

	// Baseline.
	mustContainPath(t, parts, "/usr/local/bin")
	mustContainPath(t, parts, "/usr/bin")
	mustContainPath(t, parts, "/bin")

	// Homebrew known + detected prefix.
	mustContainPath(t, parts, "/home/linuxbrew/.linuxbrew/bin")
	mustContainPath(t, parts, "/home/linuxbrew/.linuxbrew/sbin")

	// Managed venv candidates.
	mustContainPath(t, parts, "/home/tester/sciclaw/.venv/bin")
	mustContainPath(t, parts, "/home/tester/.picoclaw/workspace/.venv/bin")

	// Installer PATH is preserved.
	mustContainPath(t, parts, "/custom/bin")

	// No duplicates.
	seen := map[string]struct{}{}
	for _, p := range parts {
		if _, ok := seen[p]; ok {
			t.Fatalf("duplicate path in PATH output: %s", p)
		}
		seen[p] = struct{}{}
	}
}

func TestBuildSystemdPath_NoBrewPrefix(t *testing.T) {
	t.Setenv("HOME", "/home/tester")
	sep := string(os.PathListSeparator)
	got := buildSystemdPath(strings.Join([]string{"/alpha/bin", "/beta/bin"}, sep), "")
	parts := strings.Split(got, sep)
	mustContainPath(t, parts, "/alpha/bin")
	mustContainPath(t, parts, "/beta/bin")
	mustContainPath(t, parts, "/home/tester/sciclaw/.venv/bin")
}

func TestRenderLaunchdPlist(t *testing.T) {
	plist := renderLaunchdPlist("io.sciclaw.gateway", "/opt/homebrew/bin/sciclaw", "/tmp/out.log", "/tmp/err.log")
	mustContain(t, plist, "<string>io.sciclaw.gateway</string>")
	mustContain(t, plist, "<string>/opt/homebrew/bin/sciclaw</string>")
	mustContain(t, plist, "<string>gateway</string>")
	mustContain(t, plist, "<string>/tmp/out.log</string>")
}

func mustContain(t *testing.T, s, needle string) {
	t.Helper()
	if !strings.Contains(s, needle) {
		t.Fatalf("expected %q to contain %q", s, needle)
	}
}

func mustContainPath(t *testing.T, paths []string, needle string) {
	t.Helper()
	for _, p := range paths {
		if filepath.Clean(p) == filepath.Clean(needle) {
			return
		}
	}
	t.Fatalf("expected PATH to contain %q; got: %v", needle, paths)
}
