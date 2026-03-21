package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractSemver(t *testing.T) {
	got, ok := extractSemver("ctxclaw v0.1.1 (built 2026-03-21T12:04:52-0400, go1.26.0)")
	if !ok {
		t.Fatal("expected semver match")
	}
	if got != "v0.1.1" {
		t.Fatalf("extractSemver = %q, want v0.1.1", got)
	}
}

func TestCompareSemver(t *testing.T) {
	if compareSemver("v0.1.1", "v0.1.1") != 0 {
		t.Fatal("expected equal versions")
	}
	if compareSemver("v0.1.2", "v0.1.1") <= 0 {
		t.Fatal("expected newer version to compare greater")
	}
	if compareSemver("v0.1.0", "v0.1.1") >= 0 {
		t.Fatal("expected older version to compare smaller")
	}
}

func TestInspectCtxclawBinaryRecognizesCompatibleVersion(t *testing.T) {
	path := writeFakeCtxclawBinary(t, "ctxclaw v0.1.1 (built now, go1.26.0)")
	info, err := inspectCtxclawBinary(path)
	if err != nil {
		t.Fatalf("inspectCtxclawBinary: %v", err)
	}
	if !info.Compatible {
		t.Fatalf("expected compatible info, got %#v", info)
	}
	if info.Parsed != "v0.1.1" {
		t.Fatalf("parsed version = %q, want v0.1.1", info.Parsed)
	}
}

func TestInspectCtxclawBinaryAllowsDevBuild(t *testing.T) {
	path := writeFakeCtxclawBinary(t, "ctxclaw dev")
	info, err := inspectCtxclawBinary(path)
	if err != nil {
		t.Fatalf("inspectCtxclawBinary: %v", err)
	}
	if !info.DevBuild || !info.Compatible {
		t.Fatalf("expected compatible dev build, got %#v", info)
	}
}

func TestCheckCtxclawBinaryWarnsOnOldVersion(t *testing.T) {
	orig := promptLookPath
	t.Cleanup(func() { promptLookPath = orig })
	path := writeFakeCtxclawBinary(t, "ctxclaw v0.1.0 (built then, go1.26.0)")
	promptLookPath = func(name string) (string, error) {
		if name == "ctxclaw" {
			return path, nil
		}
		return "", os.ErrNotExist
	}

	check := checkCtxclawBinary()
	if check.Status != doctorWarn {
		t.Fatalf("status = %q, want %q", check.Status, doctorWarn)
	}
	if !strings.Contains(check.Message, "need "+minimumCtxclawVersion+" or newer") {
		t.Fatalf("message = %q", check.Message)
	}
}

func TestCheckCtxclawBinarySkipsWhenMissing(t *testing.T) {
	orig := promptLookPath
	t.Cleanup(func() { promptLookPath = orig })
	promptLookPath = func(string) (string, error) { return "", os.ErrNotExist }

	check := checkCtxclawBinary()
	if check.Status != doctorSkip {
		t.Fatalf("status = %q, want %q", check.Status, doctorSkip)
	}
}

func writeFakeCtxclawBinary(t *testing.T, output string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "ctxclaw")
	script := "#!/bin/sh\n" +
		"echo '" + strings.ReplaceAll(output, "'", "'\"'\"'") + "'\n"
	if err := os.WriteFile(path, []byte(script), 0755); err != nil {
		t.Fatalf("write fake ctxclaw: %v", err)
	}
	return path
}
