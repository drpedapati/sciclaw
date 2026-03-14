package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestRunDoctorReportsAnthropicOATBridge(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("mock binary script not supported on Windows")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := config.DefaultConfig()
	cfg.Providers.Anthropic.APIKey = "sk-ant-oat-test"

	configPath := filepath.Join(home, ".picoclaw", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	bindir := t.TempDir()
	bin := filepath.Join(bindir, "sciclaw-claude-agent")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho 0.1.0\n"), 0o755); err != nil {
		t.Fatalf("write mock bridge: %v", err)
	}
	t.Setenv("PATH", bindir+string(os.PathListSeparator)+os.Getenv("PATH"))

	rep := runDoctor(doctorOptions{})

	authCheck, ok := doctorCheckByName(rep.Checks, "auth.anthropic")
	if !ok || authCheck.Status != doctorOK {
		t.Fatalf("auth.anthropic check=%#v", authCheck)
	}
	if !strings.Contains(strings.ToLower(authCheck.Message), "rerouted through sciclaw-claude-agent") {
		t.Fatalf("auth.anthropic message=%q", authCheck.Message)
	}

	bridgeCheck, ok := doctorCheckByName(rep.Checks, "sciclaw-claude-agent")
	if !ok || bridgeCheck.Status != doctorOK {
		t.Fatalf("sciclaw-claude-agent check=%#v", bridgeCheck)
	}
}
