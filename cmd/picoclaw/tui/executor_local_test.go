package tui

import (
	"strings"
	"testing"
)

func TestParseServiceStatusFlag(t *testing.T) {
	out := `
Gateway service status:
  Backend:   launchd
  Installed: yes
  Running:   yes
  Enabled:   yes
`
	if v, ok := parseServiceStatusFlag(out, "installed"); !ok || !v {
		t.Fatalf("installed parse = (%v,%v), want (true,true)", v, ok)
	}
	if v, ok := parseServiceStatusFlag(out, "running"); !ok || !v {
		t.Fatalf("running parse = (%v,%v), want (true,true)", v, ok)
	}
}

func TestParseServiceStatusFlag_No(t *testing.T) {
	out := `
Gateway service status:
  Installed: no
  Running:   no
`
	if v, ok := parseServiceStatusFlag(out, "installed"); !ok || v {
		t.Fatalf("installed parse = (%v,%v), want (false,true)", v, ok)
	}
	if v, ok := parseServiceStatusFlag(out, "running"); !ok || v {
		t.Fatalf("running parse = (%v,%v), want (false,true)", v, ok)
	}
}

func TestMergePathList_PrefersFrontAndDedupes(t *testing.T) {
	got := mergePathList(
		[]string{"/Users/tester/.local/bin", "/opt/homebrew/bin", "/usr/local/bin"},
		"/usr/local/bin:/usr/bin:/bin:/opt/homebrew/bin",
	)
	parts := strings.Split(got, ":")
	if len(parts) < 5 {
		t.Fatalf("merged path too short: %q", got)
	}
	if parts[0] != "/Users/tester/.local/bin" {
		t.Fatalf("first path = %q, want %q", parts[0], "/Users/tester/.local/bin")
	}
	if parts[1] != "/opt/homebrew/bin" {
		t.Fatalf("second path = %q, want %q", parts[1], "/opt/homebrew/bin")
	}
	if parts[2] != "/usr/local/bin" {
		t.Fatalf("third path = %q, want %q", parts[2], "/usr/local/bin")
	}
	if strings.Count(got, "/opt/homebrew/bin") != 1 {
		t.Fatalf("expected deduped /opt/homebrew/bin in %q", got)
	}
	if strings.Count(got, "/usr/local/bin") != 1 {
		t.Fatalf("expected deduped /usr/local/bin in %q", got)
	}
}
