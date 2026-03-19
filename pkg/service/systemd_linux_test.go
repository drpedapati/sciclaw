//go:build linux

package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeCommandRunner struct {
	calls []string
}

func (f *fakeCommandRunner) Run(_ context.Context, name string, args ...string) ([]byte, error) {
	cmd := name
	if len(args) > 0 {
		cmd += " " + strings.Join(args, " ")
	}
	f.calls = append(f.calls, cmd)
	return []byte("ok"), nil
}

func TestSystemdInstall_WritesPathEnvironment(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/custom/bin:/usr/bin")

	runner := &fakeCommandRunner{}
	mgr := newSystemdUserManager("/tmp/sciclaw", runner).(*systemdUserManager)

	if err := mgr.Install(); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	unitPath := filepath.Join(home, ".config", "systemd", "user", "sciclaw-gateway.service")
	b, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	unit := string(b)
	if !strings.Contains(unit, "Environment=PATH=") {
		t.Fatalf("expected unit to include PATH environment, got:\n%s", unit)
	}
	if !strings.Contains(unit, "/custom/bin") {
		t.Fatalf("expected PATH to include installer custom path, got:\n%s", unit)
	}
	if !strings.Contains(unit, "ExecStart=/tmp/sciclaw gateway") {
		t.Fatalf("expected ExecStart line, got:\n%s", unit)
	}

	if len(runner.calls) < 2 {
		t.Fatalf("expected daemon-reload and enable calls, got: %v", runner.calls)
	}
}

func TestSystemdInstall_WritesWebExecStart(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("PATH", "/custom/bin:/usr/bin")

	runner := &fakeCommandRunner{}
	mgr := newSystemdUserManagerForSpec("/tmp/sciclaw", runner, WebSpec("10.0.0.5:4142")).(*systemdUserManager)

	if err := mgr.Install(); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	unitPath := filepath.Join(home, ".config", "systemd", "user", "sciclaw-web.service")
	b, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatalf("read unit: %v", err)
	}
	unit := string(b)
	if !strings.Contains(unit, "Description=sciClaw Web UI") {
		t.Fatalf("expected web description, got:\n%s", unit)
	}
	if !strings.Contains(unit, "ExecStart=/tmp/sciclaw web --listen 10.0.0.5:4142") {
		t.Fatalf("expected web ExecStart line, got:\n%s", unit)
	}
}
