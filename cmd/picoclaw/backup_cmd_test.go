package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestParseBackupOptions(t *testing.T) {
	opts, showHelp, err := parseBackupOptions([]string{"--with-sessions", "-o", "~/x.tar.gz"})
	if err != nil {
		t.Fatalf("parseBackupOptions returned error: %v", err)
	}
	if showHelp {
		t.Fatalf("expected showHelp=false")
	}
	if !opts.WithSessions {
		t.Fatalf("expected WithSessions=true")
	}
	if opts.OutputPath != "~/x.tar.gz" {
		t.Fatalf("unexpected OutputPath: %q", opts.OutputPath)
	}
}

func TestParseBackupOptionsHelp(t *testing.T) {
	_, showHelp, err := parseBackupOptions([]string{"--help"})
	if err != nil {
		t.Fatalf("parseBackupOptions returned error: %v", err)
	}
	if !showHelp {
		t.Fatalf("expected showHelp=true for --help")
	}
}

func TestCollectBackupEntries(t *testing.T) {
	homeDir := t.TempDir()
	workspace := filepath.Join(homeDir, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	mustWriteFile(t, filepath.Join(homeDir, ".picoclaw", "config.json"), "{}")
	mustWriteFile(t, filepath.Join(homeDir, ".picoclaw", "auth.json"), "{}")
	mustWriteFile(t, filepath.Join(homeDir, ".config", "sciclaw", "op.env"), "OP_SERVICE_ACCOUNT_TOKEN=x")
	mustWriteFile(t, filepath.Join(workspace, "AGENTS.md"), "# AGENTS")
	mustWriteFile(t, filepath.Join(workspace, "HOOKS.md"), "# HOOKS")
	mustWriteFile(t, filepath.Join(workspace, "IDENTITY.md"), "# IDENTITY")
	mustWriteFile(t, filepath.Join(workspace, "SOUL.md"), "# SOUL")
	mustWriteFile(t, filepath.Join(workspace, "TOOLS.md"), "# TOOLS")
	mustWriteFile(t, filepath.Join(workspace, "USER.md"), "# USER")
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "skills"), 0755); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "cron"), 0755); err != nil {
		t.Fatalf("mkdir cron: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(workspace, "sessions"), 0755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace: workspace,
			},
		},
	}

	entriesNoSessions := collectBackupEntries(cfg, homeDir, false)
	if !hasArchivePath(entriesNoSessions, "workspace/AGENTS.md") {
		t.Fatalf("expected workspace/AGENTS.md in backup entries")
	}
	if hasArchivePath(entriesNoSessions, "workspace/sessions") {
		t.Fatalf("did not expect workspace/sessions without --with-sessions")
	}

	entriesWithSessions := collectBackupEntries(cfg, homeDir, true)
	if !hasArchivePath(entriesWithSessions, "workspace/sessions") {
		t.Fatalf("expected workspace/sessions with --with-sessions")
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir parent for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func hasArchivePath(entries []backupEntry, archivePath string) bool {
	for _, entry := range entries {
		if entry.ArchivePath == archivePath {
			return true
		}
	}
	return false
}
