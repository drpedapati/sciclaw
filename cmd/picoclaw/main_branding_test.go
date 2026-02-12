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

	if !strings.Contains(out, "sciClaw (picoclaw)") {
		t.Fatalf("version output missing sciClaw display + picoclaw compatibility: %q", out)
	}
}

func TestPrintHelpIncludesCompatibilityCommand(t *testing.T) {
	out := captureStdout(t, printHelp)

	if !strings.Contains(out, "CLI compatibility command: picoclaw") {
		t.Fatalf("help output missing compatibility command note: %q", out)
	}
	if !strings.Contains(out, "Initialize sciClaw configuration and workspace") {
		t.Fatalf("help output missing sciClaw onboarding wording: %q", out)
	}
}
