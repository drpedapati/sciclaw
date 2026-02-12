package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCreateWorkspaceTemplatesCreatesExpectedStructure(t *testing.T) {
	workspace := t.TempDir()

	createWorkspaceTemplates(workspace)

	dirs := []string{"memory", "skills", "sessions", "cron"}
	for _, dir := range dirs {
		p := filepath.Join(workspace, dir)
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("expected directory %s to exist: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", dir)
		}
	}

	files := []string{
		"AGENTS.md",
		"SOUL.md",
		"TOOLS.md",
		"USER.md",
		"IDENTITY.md",
		filepath.Join("memory", "MEMORY.md"),
	}
	for _, file := range files {
		p := filepath.Join(workspace, file)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected file %s to exist: %v", file, err)
		}
	}

	agentsContent, err := os.ReadFile(filepath.Join(workspace, "AGENTS.md"))
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if !strings.Contains(string(agentsContent), "paired-scientist") {
		t.Fatalf("AGENTS.md missing paired-scientist guidance")
	}
}

func TestCreateWorkspaceTemplatesDoesNotOverwriteExistingFiles(t *testing.T) {
	workspace := t.TempDir()

	customAgents := "# Agent Instructions\n\nCustom content.\n"
	agentsPath := filepath.Join(workspace, "AGENTS.md")
	if err := os.WriteFile(agentsPath, []byte(customAgents), 0644); err != nil {
		t.Fatalf("seed AGENTS.md: %v", err)
	}

	customMemory := "# Long-term Memory\n\nDo not overwrite.\n"
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0755); err != nil {
		t.Fatalf("create memory dir: %v", err)
	}
	memoryPath := filepath.Join(workspace, "memory", "MEMORY.md")
	if err := os.WriteFile(memoryPath, []byte(customMemory), 0644); err != nil {
		t.Fatalf("seed MEMORY.md: %v", err)
	}

	createWorkspaceTemplates(workspace)

	gotAgents, err := os.ReadFile(agentsPath)
	if err != nil {
		t.Fatalf("read AGENTS.md: %v", err)
	}
	if string(gotAgents) != customAgents {
		t.Fatalf("AGENTS.md was overwritten unexpectedly")
	}

	gotMemory, err := os.ReadFile(memoryPath)
	if err != nil {
		t.Fatalf("read MEMORY.md: %v", err)
	}
	if string(gotMemory) != customMemory {
		t.Fatalf("MEMORY.md was overwritten unexpectedly")
	}
}
