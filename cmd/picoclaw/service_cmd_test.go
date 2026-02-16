package main

import "testing"

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
