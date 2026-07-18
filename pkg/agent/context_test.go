package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/tools"
)

func TestBuildSystemPromptIncludesHostedToolNotes(t *testing.T) {
	workspace := t.TempDir()
	cb := NewContextBuilder(workspace)
	cb.SetIncludePromptToolSummaries(false)
	cb.SetHostedToolNotes("`image_generation` (OpenAI Responses) — generate images in chat")

	prompt := cb.BuildSystemPrompt()
	if !strings.Contains(prompt, "## Available Tools") {
		t.Fatalf("expected Available Tools section for hosted notes")
	}
	if !strings.Contains(prompt, "### Provider-hosted tools") {
		t.Fatalf("expected Provider-hosted tools subsection")
	}
	if !strings.Contains(prompt, "`image_generation` (OpenAI Responses)") {
		t.Fatalf("expected image_generation hosted note in prompt")
	}
}

func TestCodexImageGenerationToolNotePreservesFinalArtifactContract(t *testing.T) {
	for _, required := range []string{
		"read and follow the `image-generation` skill",
		"preserves the user's requested final artifact",
	} {
		if !strings.Contains(codexImageGenerationToolNote, required) {
			t.Fatalf("image generation note missing %q", required)
		}
	}
	if strings.Contains(strings.ToLower(codexImageGenerationToolNote), "real experimental data") {
		t.Fatal("image generation note still contains the removed provenance warning")
	}
}

func TestWorkspaceTemplateOmitsRemovedImageGenerationWarning(t *testing.T) {
	templatePath := filepath.Join("..", "workspacetpl", "templates", "workspace", "AGENTS.md")
	data, err := os.ReadFile(templatePath)
	if err != nil {
		t.Fatalf("read workspace AGENTS template: %v", err)
	}
	text := string(data)
	if strings.Contains(strings.ToLower(text), "real experimental data") {
		t.Fatal("workspace template still contains the removed provenance warning")
	}
	if !strings.Contains(text, "`image-generation`") {
		t.Fatal("workspace template missing canonical image-generation skill routing")
	}
}

func TestImageGenerationSkillProtectsArtifactRoutes(t *testing.T) {
	skillPath := filepath.Join("..", "..", "skills", "image-generation", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("read image-generation skill: %v", err)
	}
	text := string(data)
	for _, required := range []string{
		"### Standalone image",
		"### Visual asset inside another artifact",
		"### Editable presentation",
		"### Complete image-generated slide",
		"finished 16:9 slide as one image",
		"read and follow the installed presentation and academic-presentation skills",
		"action title stating the takeaway",
		"successful stress-capacity pattern",
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("image-generation skill missing %q", required)
		}
	}
	if strings.Contains(strings.ToLower(text), "real experimental data") {
		t.Fatal("image-generation skill contains the removed provenance warning")
	}
}

func TestBuildSystemPromptUsesSciClawIdentity(t *testing.T) {
	workspace := t.TempDir()
	cb := NewContextBuilder(workspace)

	prompt := cb.BuildSystemPrompt()

	if !strings.Contains(prompt, "# sciClaw") {
		t.Fatalf("system prompt missing sciClaw identity header")
	}
	if !strings.Contains(prompt, "paired-scientist") {
		t.Fatalf("system prompt missing paired-scientist identity description")
	}
	if !strings.Contains(prompt, "Reproducibility") {
		t.Fatalf("system prompt missing reproducibility rule")
	}
	if !strings.Contains(prompt, "PubMed-first verification") {
		t.Fatalf("system prompt missing PubMed-first verification rule")
	}
	if !strings.Contains(prompt, "start with the dedicated `pubmed_search` and `pubmed_fetch` tools") {
		t.Fatalf("system prompt missing explicit typed PubMed guidance")
	}
	if !strings.Contains(prompt, "Use the remember tool for curated long-term memory") {
		t.Fatalf("system prompt missing remember guidance")
	}
	if strings.Contains(prompt, "write here instead of MEMORY.md") {
		t.Fatalf("system prompt still redirects execution logs into daily notes")
	}
}

func TestLoadBootstrapFilesIncludesTools(t *testing.T) {
	workspace := t.TempDir()
	files := map[string]string{
		"AGENTS.md":   "# Agents\n",
		"SOUL.md":     "# Soul\n",
		"USER.md":     "# User\n",
		"IDENTITY.md": "# Identity\n",
		"TOOLS.md":    "# Tools\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(workspace, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	cb := NewContextBuilder(workspace)
	bootstrap := cb.LoadBootstrapFiles()

	if !strings.Contains(bootstrap, "## TOOLS.md") {
		t.Fatalf("bootstrap content missing TOOLS.md section")
	}
	if !strings.Contains(bootstrap, "# Tools") {
		t.Fatalf("bootstrap content missing TOOLS.md body")
	}
}

func TestLoadBootstrapFilesFallsBackToGlobalWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	globalWorkspace := filepath.Join(home, "sciclaw")
	if err := os.MkdirAll(globalWorkspace, 0755); err != nil {
		t.Fatalf("mkdir global workspace: %v", err)
	}
	if err := os.WriteFile(filepath.Join(globalWorkspace, "AGENTS.md"), []byte("# Global Agents\n"), 0644); err != nil {
		t.Fatalf("write global AGENTS.md: %v", err)
	}

	routedWorkspace := t.TempDir()
	cb := NewContextBuilder(routedWorkspace)
	bootstrap := cb.LoadBootstrapFiles()

	if !strings.Contains(bootstrap, "# Global Agents") {
		t.Fatalf("expected fallback AGENTS.md from global workspace, got: %q", bootstrap)
	}
}

func TestContextBuilderLoadsGlobalWorkspaceSkillsForRoutedWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	skillDir := filepath.Join(home, "sciclaw", "skills", "baseline")
	if err := os.MkdirAll(skillDir, 0755); err != nil {
		t.Fatalf("mkdir skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# Baseline\n"), 0644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}

	routedWorkspace := t.TempDir()
	cb := NewContextBuilder(routedWorkspace)
	info := cb.GetSkillsInfo()

	total, _ := info["total"].(int)
	if total < 1 {
		t.Fatalf("expected at least one skill from global workspace, got %v", info)
	}
}

func TestContextBuilderCanSuppressPromptToolSummaries(t *testing.T) {
	workspace := t.TempDir()
	cb := NewContextBuilder(workspace)
	registry := tools.NewToolRegistry()
	registry.Register(tools.NewWordCountTool(workspace, true))
	cb.SetToolsRegistry(registry)

	withSummaries := cb.BuildSystemPrompt()
	if !strings.Contains(withSummaries, "## Available Tools") {
		t.Fatalf("expected tool summaries in default system prompt")
	}

	cb.SetIncludePromptToolSummaries(false)
	withoutSummaries := cb.BuildSystemPrompt()
	if strings.Contains(withoutSummaries, "## Available Tools") {
		t.Fatalf("expected prompt tool summaries to be omitted when disabled")
	}
	if !strings.Contains(withoutSummaries, "ALWAYS use tools") {
		t.Fatalf("expected core tool-use rule to remain in system prompt")
	}
}
