package main

import (
	"errors"
	"testing"
)

func TestParseServiceLogsOptions(t *testing.T) {
	opts, showHelp, err := parseServiceLogsOptions([]string{"--lines", "250"})
	if err != nil {
		t.Fatalf("parseServiceLogsOptions returned error: %v", err)
	}
	if showHelp {
		t.Fatalf("expected showHelp=false")
	}
	if opts.Lines != 250 {
		t.Fatalf("expected Lines=250, got %d", opts.Lines)
	}
}

func TestParseServiceLogsOptionsDefault(t *testing.T) {
	opts, showHelp, err := parseServiceLogsOptions(nil)
	if err != nil {
		t.Fatalf("parseServiceLogsOptions returned error: %v", err)
	}
	if showHelp {
		t.Fatalf("expected showHelp=false")
	}
	if opts.Lines != 100 {
		t.Fatalf("expected default Lines=100, got %d", opts.Lines)
	}
}

func TestParseServiceLogsOptionsHelp(t *testing.T) {
	_, showHelp, err := parseServiceLogsOptions([]string{"--help"})
	if err != nil {
		t.Fatalf("parseServiceLogsOptions returned error: %v", err)
	}
	if !showHelp {
		t.Fatalf("expected showHelp=true")
	}
}

func TestParseServiceLogsOptionsInvalid(t *testing.T) {
	_, _, err := parseServiceLogsOptions([]string{"--lines", "0"})
	if err == nil {
		t.Fatalf("expected error for invalid line count")
	}
}

func TestResolveServiceExecutablePath_PrefersLookPath(t *testing.T) {
	got, err := resolveServiceExecutablePath(
		"/home/linuxbrew/.linuxbrew/Cellar/sciclaw/0.1.39/bin/sciclaw",
		func(file string) (string, error) {
			if file != "sciclaw" {
				t.Fatalf("expected lookup for sciclaw, got %q", file)
			}
			return "/home/linuxbrew/.linuxbrew/bin/sciclaw", nil
		},
		func() (string, error) {
			return "/home/linuxbrew/.linuxbrew/Cellar/sciclaw/0.1.39/bin/sciclaw", nil
		},
	)
	if err != nil {
		t.Fatalf("resolveServiceExecutablePath returned error: %v", err)
	}
	if got != "/home/linuxbrew/.linuxbrew/bin/sciclaw" {
		t.Fatalf("expected stable Homebrew path, got %q", got)
	}
}

func TestResolveServiceExecutablePath_FallbackToExecutable(t *testing.T) {
	got, err := resolveServiceExecutablePath(
		"sciclaw",
		func(string) (string, error) { return "", errors.New("not found") },
		func() (string, error) { return "/opt/tools/sciclaw", nil },
	)
	if err != nil {
		t.Fatalf("resolveServiceExecutablePath returned error: %v", err)
	}
	if got != "/opt/tools/sciclaw" {
		t.Fatalf("expected executable fallback, got %q", got)
	}
}
