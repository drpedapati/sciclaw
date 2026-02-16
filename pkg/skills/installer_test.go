package skills

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallFromGitHub_SizeLimit(t *testing.T) {
	// Serve content that exceeds maxSkillSize.
	oversized := "---\nname: big\n---\n" + strings.Repeat("x", maxSkillSize+1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, oversized)
	}))
	defer srv.Close()

	workspace := t.TempDir()
	si := NewSkillInstaller(workspace)

	// Patch the URL by installing from a repo that won't match — we test via
	// a helper that accepts a URL directly.
	err := si.installFromURL(context.Background(), srv.URL, "big")
	if err == nil {
		t.Fatal("expected size limit error, got nil")
	}
	if !strings.Contains(err.Error(), "exceeds maximum size") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallFromGitHub_RejectsBinary(t *testing.T) {
	binary := "---\nname: evil\n---\n\x00\x01\x02binary content"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, binary)
	}))
	defer srv.Close()

	workspace := t.TempDir()
	si := NewSkillInstaller(workspace)

	err := si.installFromURL(context.Background(), srv.URL, "evil")
	if err == nil {
		t.Fatal("expected binary rejection error, got nil")
	}
	if !strings.Contains(err.Error(), "binary") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestInstallFromGitHub_RequiresFrontmatter(t *testing.T) {
	tests := []struct {
		name    string
		content string
		errMsg  string
	}{
		{
			name:    "no frontmatter",
			content: "# Just a heading\nSome content",
			errMsg:  "missing YAML frontmatter",
		},
		{
			name:    "frontmatter without name",
			content: "---\ndescription: something\n---\nContent",
			errMsg:  "missing required 'name' field",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprint(w, tc.content)
			}))
			defer srv.Close()

			workspace := t.TempDir()
			si := NewSkillInstaller(workspace)

			err := si.installFromURL(context.Background(), srv.URL, "test-skill")
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.errMsg) {
				t.Fatalf("expected error containing %q, got: %v", tc.errMsg, err)
			}
		})
	}
}

func TestInstallFromGitHub_ValidSkill(t *testing.T) {
	content := "---\nname: good-skill\ndescription: A valid skill\n---\n\n# Good Skill\n\nDo things."
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, content)
	}))
	defer srv.Close()

	workspace := t.TempDir()
	si := NewSkillInstaller(workspace)

	err := si.installFromURL(context.Background(), srv.URL, "good-skill")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify SKILL.md was written.
	skillPath := filepath.Join(workspace, "skills", "good-skill", "SKILL.md")
	data, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatalf("SKILL.md not written: %v", err)
	}
	if string(data) != content {
		t.Errorf("SKILL.md content mismatch")
	}

	// Verify provenance was written.
	provPath := filepath.Join(workspace, "skills", "good-skill", ".provenance.json")
	if _, err := os.Stat(provPath); os.IsNotExist(err) {
		t.Error("provenance file not created")
	}
}

func TestInstallFromGitHub_AlreadyExists(t *testing.T) {
	workspace := t.TempDir()
	si := NewSkillInstaller(workspace)

	// Pre-create the skill dir.
	os.MkdirAll(filepath.Join(workspace, "skills", "existing"), 0755)

	err := si.installFromURL(context.Background(), "http://unused", "existing")
	if err == nil {
		t.Fatal("expected already-exists error, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDefaultCatalogURL_IsPinnedToImmutableRef(t *testing.T) {
	url := defaultCatalogURL()
	if !strings.Contains(url, skillsCatalogPinnedRef) {
		t.Fatalf("expected pinned ref in catalog url, got: %s", url)
	}
	if strings.Contains(url, "/main/") {
		t.Fatalf("catalog url must not use mutable main ref: %s", url)
	}
}

func TestListAvailableSkills_Success(t *testing.T) {
	payload := `[{"name":"test-skill","repository":"drpedapati/sciclaw-skills/test-skill","description":"demo","author":"sciclaw","tags":["demo"]}]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, payload)
	}))
	defer srv.Close()

	si := NewSkillInstaller(t.TempDir())
	si.catalogURL = srv.URL

	skills, err := si.ListAvailableSkills(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(skills) != 1 {
		t.Fatalf("expected 1 skill, got %d", len(skills))
	}
	if skills[0].Name != "test-skill" {
		t.Fatalf("unexpected skill name: %s", skills[0].Name)
	}
}

func TestListAvailableSkills_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "{")
	}))
	defer srv.Close()

	si := NewSkillInstaller(t.TempDir())
	si.catalogURL = srv.URL

	_, err := si.ListAvailableSkills(context.Background())
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	if !strings.Contains(err.Error(), "failed to parse skills list") {
		t.Fatalf("unexpected error: %v", err)
	}
}
