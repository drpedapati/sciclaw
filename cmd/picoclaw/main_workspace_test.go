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
		"HOOKS.md",
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
	if !strings.Contains(string(agentsContent), "Baseline Scientific Skills") {
		t.Fatalf("AGENTS.md missing baseline skill manifest")
	}

	toolsContent, err := os.ReadFile(filepath.Join(workspace, "TOOLS.md"))
	if err != nil {
		t.Fatalf("read TOOLS.md: %v", err)
	}
	if !strings.Contains(string(toolsContent), "Critical CLI-First Rules") {
		t.Fatalf("TOOLS.md missing CLI-first policy section")
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

func TestEnsureBaselineScienceSkillsFromSourcesInstallsMissingSkills(t *testing.T) {
	workspace := t.TempDir()
	sourceRoot := t.TempDir()

	for _, skillName := range baselineScienceSkillNames {
		skillDir := filepath.Join(sourceRoot, skillName)
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			t.Fatalf("create source skill dir %s: %v", skillName, err)
		}
		content := "# " + skillName + "\n"
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0644); err != nil {
			t.Fatalf("write source SKILL.md for %s: %v", skillName, err)
		}
	}

	ensureBaselineScienceSkillsFromSources(workspace, []string{sourceRoot})

	for _, skillName := range baselineScienceSkillNames {
		installedSkill := filepath.Join(workspace, "skills", skillName, "SKILL.md")
		if _, err := os.Stat(installedSkill); err != nil {
			t.Fatalf("expected baseline skill %s to be installed: %v", skillName, err)
		}
	}
}

func TestEnsureBaselineScienceSkillsFromSourcesDoesNotOverwriteExistingSkill(t *testing.T) {
	workspace := t.TempDir()
	sourceRoot := t.TempDir()

	skillName := baselineScienceSkillNames[0]
	sourceSkillDir := filepath.Join(sourceRoot, skillName)
	if err := os.MkdirAll(sourceSkillDir, 0755); err != nil {
		t.Fatalf("create source skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceSkillDir, "SKILL.md"), []byte("# Source Skill\n"), 0644); err != nil {
		t.Fatalf("write source SKILL.md: %v", err)
	}

	existingSkillDir := filepath.Join(workspace, "skills", skillName)
	if err := os.MkdirAll(existingSkillDir, 0755); err != nil {
		t.Fatalf("create existing workspace skill dir: %v", err)
	}
	custom := "# Custom Skill\n"
	existingSkillFile := filepath.Join(existingSkillDir, "SKILL.md")
	if err := os.WriteFile(existingSkillFile, []byte(custom), 0644); err != nil {
		t.Fatalf("write existing SKILL.md: %v", err)
	}

	ensureBaselineScienceSkillsFromSources(workspace, []string{sourceRoot})

	got, err := os.ReadFile(existingSkillFile)
	if err != nil {
		t.Fatalf("read existing SKILL.md: %v", err)
	}
	if string(got) != custom {
		t.Fatalf("existing baseline skill was overwritten unexpectedly")
	}
}
