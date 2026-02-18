package main

import "testing"

func TestParseSystemdExecStartPath(t *testing.T) {
	unit := `[Unit]
Description=sciClaw Gateway

[Service]
ExecStart=/home/linuxbrew/.linuxbrew/bin/sciclaw gateway
Restart=always
`
	got := parseSystemdExecStartPath(unit)
	want := "/home/linuxbrew/.linuxbrew/bin/sciclaw"
	if got != want {
		t.Fatalf("parseSystemdExecStartPath() = %q, want %q", got, want)
	}
}

func TestParseLaunchdProgramArg0(t *testing.T) {
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<plist version="1.0">
<dict>
  <key>ProgramArguments</key>
  <array>
    <string>/opt/homebrew/bin/sciclaw</string>
    <string>gateway</string>
  </array>
</dict>
</plist>`
	got := parseLaunchdProgramArg0(plist)
	want := "/opt/homebrew/bin/sciclaw"
	if got != want {
		t.Fatalf("parseLaunchdProgramArg0() = %q, want %q", got, want)
	}
}

func TestServicePathNeedsRefresh_CellarPath(t *testing.T) {
	configured := "/home/linuxbrew/.linuxbrew/Cellar/sciclaw/0.1.41/bin/sciclaw"
	expected := "/home/linuxbrew/.linuxbrew/bin/sciclaw"
	if !servicePathNeedsRefresh(configured, expected) {
		t.Fatalf("expected Cellar path to require refresh")
	}
}

func TestServicePathNeedsRefresh_CurrentPath(t *testing.T) {
	configured := "/home/linuxbrew/.linuxbrew/bin/sciclaw"
	expected := "/home/linuxbrew/.linuxbrew/bin/sciclaw"
	if servicePathNeedsRefresh(configured, expected) {
		t.Fatalf("expected identical paths to be current")
	}
}
