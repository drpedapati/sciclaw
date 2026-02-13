package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("create pipe: %v", err)
	}
	os.Stdout = w

	fn()

	_ = w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	_ = r.Close()
	return buf.String()
}

func TestPrintVersionUsesDisplayNameAndCliName(t *testing.T) {
	out := captureStdout(t, printVersion)

	if !strings.Contains(out, "sciClaw (sciclaw; picoclaw-compatible)") {
		t.Fatalf("version output missing sciClaw dual-command branding: %q", out)
	}
}

func TestPrintHelpIncludesCompatibilityCommand(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"sciclaw"}
	defer func() { os.Args = origArgs }()

	out := captureStdout(t, printHelp)

	if !strings.Contains(out, "Primary command: sciclaw") {
		t.Fatalf("help output missing primary command note: %q", out)
	}
	if !strings.Contains(out, "Compatibility alias: picoclaw") {
		t.Fatalf("help output missing compatibility alias note: %q", out)
	}
	if !strings.Contains(out, "Usage: sciclaw <command>") {
		t.Fatalf("help output should default usage to sciclaw: %q", out)
	}
	if !strings.Contains(out, "Initialize sciClaw configuration and workspace") {
		t.Fatalf("help output missing sciClaw onboarding wording: %q", out)
	}
}

func TestPrintHelpUsesInvokedCompatibilityAliasWhenCalledAsPicoclaw(t *testing.T) {
	origArgs := os.Args
	os.Args = []string{"picoclaw"}
	defer func() { os.Args = origArgs }()

	out := captureStdout(t, printHelp)
	if !strings.Contains(out, "Usage: picoclaw <command>") {
		t.Fatalf("help output should use invoked picoclaw command name: %q", out)
	}
}
