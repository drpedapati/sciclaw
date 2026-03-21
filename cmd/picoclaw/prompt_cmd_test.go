package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/agent"
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

func TestApplyPromptEnvelopeOverrides(t *testing.T) {
	env := &agent.PromptEnvelope{
		Budget: agent.PromptEnvelopeBudget{
			RecentBudgetTokens: 1800,
		},
		Options: agent.PromptEnvelopeOptions{
			KeepRecentMessages:        6,
			OldToolResultThreshold:    1200,
			RecentToolResultThreshold: 30000,
			CheckpointMaxBullets:      4,
		},
	}

	applyPromptEnvelopeOverrides(env, 2, 120, 500, 9000, 3)

	if env.Options.KeepRecentMessages != 2 {
		t.Fatalf("expected keep recent override, got %d", env.Options.KeepRecentMessages)
	}
	if env.Budget.RecentBudgetTokens != 120 {
		t.Fatalf("expected recent budget override, got %d", env.Budget.RecentBudgetTokens)
	}
	if env.Options.OldToolResultThreshold != 500 {
		t.Fatalf("expected old threshold override, got %d", env.Options.OldToolResultThreshold)
	}
	if env.Options.RecentToolResultThreshold != 9000 {
		t.Fatalf("expected recent threshold override, got %d", env.Options.RecentToolResultThreshold)
	}
	if env.Options.CheckpointMaxBullets != 3 {
		t.Fatalf("expected checkpoint bullet override, got %d", env.Options.CheckpointMaxBullets)
	}
}

func TestResolvePromptHelperBinaryPathPrefersStableBrewSymlinkForCellarPath(t *testing.T) {
	root := t.TempDir()
	cellar := filepath.Join(root, "Cellar", "ctxclaw", "0.1.0", "bin")
	stableBin := filepath.Join(root, "bin")
	if err := os.MkdirAll(cellar, 0755); err != nil {
		t.Fatalf("mkdir cellar: %v", err)
	}
	if err := os.MkdirAll(stableBin, 0755); err != nil {
		t.Fatalf("mkdir stable bin: %v", err)
	}
	cellarPath := filepath.Join(cellar, "ctxclaw")
	stablePath := filepath.Join(stableBin, "ctxclaw")
	for _, path := range []string{cellarPath, stablePath} {
		if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0755); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	got, err := resolvePromptHelperBinaryPath(cellarPath, "ctxclaw", nil, nil)
	if err != nil {
		t.Fatalf("resolvePromptHelperBinaryPath: %v", err)
	}
	if got != stablePath {
		t.Fatalf("expected stable path %q, got %q", stablePath, got)
	}
}

func TestResolvePromptHelperBinaryPathFallsBackToStableHomebrewDir(t *testing.T) {
	root := t.TempDir()
	fallbackDir := filepath.Join(root, "brew", "bin")
	if err := os.MkdirAll(fallbackDir, 0755); err != nil {
		t.Fatalf("mkdir fallback: %v", err)
	}
	fallbackPath := filepath.Join(fallbackDir, "ctxclaw")
	if err := os.WriteFile(fallbackPath, []byte("#!/bin/sh\n"), 0755); err != nil {
		t.Fatalf("write fallback binary: %v", err)
	}

	got, err := resolvePromptHelperBinaryPath("ctxclaw", "ctxclaw", func(string) (string, error) {
		return "", os.ErrNotExist
	}, []string{fallbackDir})
	if err != nil {
		t.Fatalf("resolvePromptHelperBinaryPath: %v", err)
	}
	if got != fallbackPath {
		t.Fatalf("expected fallback path %q, got %q", fallbackPath, got)
	}
}
