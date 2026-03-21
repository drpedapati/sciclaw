package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestResolvePromptInspectWorkspaceFindsRoutingWorkspace(t *testing.T) {
	workspace := t.TempDir()
	other := t.TempDir()
	sessionKey := "discord:channel@session"
	if err := os.MkdirAll(filepath.Join(other, "sessions"), 0755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}
	if err := os.WriteFile(filepath.Join(other, "sessions", sessionKey+".json"), []byte(`{"key":"`+sessionKey+`"}`), 0644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Agents.Defaults.SharedWorkspace = workspace
	cfg.Routing.Mappings = []config.RoutingMapping{
		{Channel: "discord", ChatID: "channel", Workspace: other},
	}

	resolved, err := resolvePromptInspectWorkspace(cfg, sessionKey, "")
	if err != nil {
		t.Fatalf("resolvePromptInspectWorkspace: %v", err)
	}
	if resolved != other {
		t.Fatalf("expected routing workspace %q, got %q", other, resolved)
	}
}

func TestResolvePromptInspectWorkspaceErrorsOnMultipleMatches(t *testing.T) {
	workspace := t.TempDir()
	shared := t.TempDir()
	sessionKey := "discord:channel@session"
	for _, dir := range []string{workspace, shared} {
		if err := os.MkdirAll(filepath.Join(dir, "sessions"), 0755); err != nil {
			t.Fatalf("mkdir sessions in %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sessions", sessionKey+".json"), []byte(`{"key":"`+sessionKey+`"}`), 0644); err != nil {
			t.Fatalf("write session in %s: %v", dir, err)
		}
	}

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Agents.Defaults.SharedWorkspace = shared

	_, err := resolvePromptInspectWorkspace(cfg, sessionKey, "")
	if err == nil {
		t.Fatal("expected error for multiple matching workspaces")
	}
	if !strings.Contains(err.Error(), "multiple workspaces") {
		t.Fatalf("unexpected error: %v", err)
	}
}
