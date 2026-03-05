package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestHasLikelyRelativePathToken(t *testing.T) {
	tcs := []struct {
		name string
		raw  string
		want bool
	}{
		{
			name: "unprefixed relative path",
			raw:  `pandoc memory/in.md -o memory/out.docx`,
			want: true,
		},
		{
			name: "dot relative path",
			raw:  `pandoc ./memory/in.md -o ./memory/out.docx`,
			want: false,
		},
		{
			name: "absolute path only",
			raw:  `find /home/ernie -name "*.md"`,
			want: false,
		},
	}

	for _, tc := range tcs {
		if got := hasLikelyRelativePathToken(tc.raw); got != tc.want {
			t.Fatalf("%s: got %v want %v (raw=%q)", tc.name, got, tc.want, tc.raw)
		}
	}
}

func TestFindRelativePathGuardBlocks(t *testing.T) {
	log := `
{"level":"WARN","component":"tool.exec","message":"Exec blocked by safety guard","fields":{"command":"find /home/ernie -name \"*.md\"","error":"Command blocked by safety guard (path outside working dir)"}}
{"level":"WARN","component":"tool.exec","message":"Exec blocked by safety guard","fields":{"command":"pandoc memory/in.md -o memory/out.docx","error":"Command blocked by safety guard (path outside working dir)"}}
`
	matches := findRelativePathGuardBlocks(log)
	if len(matches) != 1 {
		t.Fatalf("matches=%d want 1: %#v", len(matches), matches)
	}
	if !hasLikelyRelativePathToken(matches[0]) {
		t.Fatalf("expected matched line to include relative path token: %s", matches[0])
	}
}

func TestCheckExecGuardRelativePathWarnsOnDetectedBlocks(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	logPath := filepath.Join(home, ".picoclaw", "gateway.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	line := `{"level":"WARN","component":"tool.exec","message":"Exec blocked by safety guard","fields":{"command":"pandoc memory/letter.md -o memory/out.docx","error":"Command blocked by safety guard (path outside working dir)"}}`
	if err := os.WriteFile(logPath, []byte(line+"\n"), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.RestrictToWorkspace = true

	check := checkExecGuardRelativePath(cfg)
	if check.Status != doctorWarn {
		t.Fatalf("status=%q want %q (%#v)", check.Status, doctorWarn, check)
	}
	if check.Name != "exec.guard.relative_path" {
		t.Fatalf("name=%q", check.Name)
	}
	if check.Data["count"] != "1" {
		t.Fatalf("count=%q want 1", check.Data["count"])
	}
}

func TestCheckExecGuardRelativePathSkipWhenRestrictionDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.RestrictToWorkspace = false

	check := checkExecGuardRelativePath(cfg)
	if check.Status != doctorSkip {
		t.Fatalf("status=%q want %q", check.Status, doctorSkip)
	}
}
