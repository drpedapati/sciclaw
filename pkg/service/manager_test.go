package service

import (
	"strings"
	"testing"
)

func TestDetectWSLWith(t *testing.T) {
	fakeRead := func(_ string) ([]byte, error) { return []byte("Linux kernel"), nil }
	if !detectWSLWith(func(k string) string {
		if k == "WSL_INTEROP" {
			return "/run/WSL/interop"
		}
		return ""
	}, fakeRead) {
		t.Fatalf("expected WSL detection from env var")
	}

	if !detectWSLWith(func(string) string { return "" }, func(string) ([]byte, error) {
		return []byte("Linux version 5.15.90.1-microsoft-standard-WSL2"), nil
	}) {
		t.Fatalf("expected WSL detection from /proc/version")
	}

	if detectWSLWith(func(string) string { return "" }, func(string) ([]byte, error) {
		return []byte("Linux version 6.8.0"), nil
	}) {
		t.Fatalf("did not expect WSL detection")
	}
}

func TestRenderSystemdUnit(t *testing.T) {
	unit := renderSystemdUnit("/usr/local/bin/sciclaw")
	mustContain(t, unit, "ExecStart=/usr/local/bin/sciclaw gateway")
	mustContain(t, unit, "Restart=always")
	mustContain(t, unit, "WantedBy=default.target")
}

func TestRenderLaunchdPlist(t *testing.T) {
	plist := renderLaunchdPlist("io.sciclaw.gateway", "/opt/homebrew/bin/sciclaw", "/tmp/out.log", "/tmp/err.log")
	mustContain(t, plist, "<string>io.sciclaw.gateway</string>")
	mustContain(t, plist, "<string>/opt/homebrew/bin/sciclaw</string>")
	mustContain(t, plist, "<string>gateway</string>")
	mustContain(t, plist, "<string>/tmp/out.log</string>")
}

func mustContain(t *testing.T, s, needle string) {
	t.Helper()
	if !strings.Contains(s, needle) {
		t.Fatalf("expected %q to contain %q", s, needle)
	}
}
