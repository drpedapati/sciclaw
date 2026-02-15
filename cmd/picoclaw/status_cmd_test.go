package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveIRLRuntimePath_UsesEnvOverrideWhenPathMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a shell script as a fake irl binary")
	}

	tmp := t.TempDir()
	fake := filepath.Join(tmp, "irl")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexit 0\n"), 0755); err != nil {
		t.Fatalf("write fake irl: %v", err)
	}

	orig := os.Getenv("PICOCLAW_IRL_BINARY")
	t.Cleanup(func() { _ = os.Setenv("PICOCLAW_IRL_BINARY", orig) })
	if err := os.Setenv("PICOCLAW_IRL_BINARY", fake); err != nil {
		t.Fatalf("set env: %v", err)
	}

	origPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", origPath) })
	if err := os.Setenv("PATH", "/usr/bin:/bin"); err != nil {
		t.Fatalf("set PATH: %v", err)
	}

	p, err := resolveIRLRuntimePath(t.TempDir())
	if err != nil {
		t.Fatalf("resolveIRLRuntimePath error: %v", err)
	}
	if p != fake {
		t.Fatalf("got %q, want %q", p, fake)
	}
}
