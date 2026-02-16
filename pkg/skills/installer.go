package skills

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxSkillSize             = 256 << 10 // 256 KB
	maxCatalogSize           = 1 << 20   // 1 MB
	skillsCatalogOwner       = "drpedapati"
	skillsCatalogRepo        = "sciclaw-skills"
	skillsCatalogFile        = "skills.json"
	skillsCatalogPinnedRef   = "9131876fda43b968e96e64fc4b11534fef85a27d"
	defaultHTTPClientTimeout = 15 * time.Second
)

type SkillProvenance struct {
	SourceURL   string `json:"source_url"`
	SHA256      string `json:"sha256"`
	InstalledAt string `json:"installed_at"`
	SizeBytes   int    `json:"size_bytes"`
}

type SkillInstaller struct {
	workspace  string
	catalogURL string
	httpClient *http.Client
}

type AvailableSkill struct {
	Name        string   `json:"name"`
	Repository  string   `json:"repository"`
	Description string   `json:"description"`
	Author      string   `json:"author"`
	Tags        []string `json:"tags"`
}

type BuiltinSkill struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Enabled bool   `json:"enabled"`
}

func NewSkillInstaller(workspace string) *SkillInstaller {
	return &SkillInstaller{
		workspace:  workspace,
		catalogURL: defaultCatalogURL(),
		httpClient: &http.Client{Timeout: defaultHTTPClientTimeout},
	}
}

// defaultCatalogURL returns the immutable pinned catalog URL.
//
// Rotation process:
// 1. Verify the new commit in drpedapati/sciclaw-skills.
// 2. Update skillsCatalogPinnedRef in this file.
// 3. Run tests and release.
func defaultCatalogURL() string {
	return fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s",
		skillsCatalogOwner, skillsCatalogRepo, skillsCatalogPinnedRef, skillsCatalogFile)
}

func (si *SkillInstaller) client() *http.Client {
	if si.httpClient != nil {
		return si.httpClient
	}
	return &http.Client{Timeout: defaultHTTPClientTimeout}
}

func (si *SkillInstaller) InstallFromGitHub(ctx context.Context, repo string) error {
	url := fmt.Sprintf("https://raw.githubusercontent.com/%s/main/SKILL.md", repo)
	return si.installFromURL(ctx, url, filepath.Base(repo))
}

func (si *SkillInstaller) installFromURL(ctx context.Context, url string, skillName string) error {
	skillDir := filepath.Join(si.workspace, "skills", skillName)

	if _, err := os.Stat(skillDir); err == nil {
		return fmt.Errorf("skill '%s' already exists", skillName)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := si.client().Do(req)
	if err != nil {
		return fmt.Errorf("failed to fetch skill: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("failed to fetch skill: HTTP %d", resp.StatusCode)
	}

	// Read with size limit to prevent memory exhaustion.
	limitedReader := io.LimitReader(resp.Body, maxSkillSize+1)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}
	if len(body) > maxSkillSize {
		return fmt.Errorf("skill content exceeds maximum size (%d KB)", maxSkillSize>>10)
	}

	// Reject binary content (NUL bytes indicate non-text data).
	if bytes.ContainsRune(body, 0) {
		return fmt.Errorf("skill content appears to be binary, not markdown")
	}

	// Require YAML frontmatter with a name field.
	content := string(body)
	if !strings.HasPrefix(content, "---\n") {
		return fmt.Errorf("skill is missing YAML frontmatter (must start with ---)")
	}
	if !strings.Contains(content, "name:") {
		return fmt.Errorf("skill frontmatter is missing required 'name' field")
	}

	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("failed to create skill directory: %w", err)
	}

	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, body, 0644); err != nil {
		return fmt.Errorf("failed to write skill file: %w", err)
	}

	// Write provenance record for auditability.
	hash := sha256.Sum256(body)
	provenance := SkillProvenance{
		SourceURL:   url,
		SHA256:      fmt.Sprintf("%x", hash),
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
		SizeBytes:   len(body),
	}
	provData, _ := json.MarshalIndent(provenance, "", "  ")
	_ = os.WriteFile(filepath.Join(skillDir, ".provenance.json"), provData, 0644)

	return nil
}

func (si *SkillInstaller) Uninstall(skillName string) error {
	skillDir := filepath.Join(si.workspace, "skills", skillName)

	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		return fmt.Errorf("skill '%s' not found", skillName)
	}

	if err := os.RemoveAll(skillDir); err != nil {
		return fmt.Errorf("failed to remove skill: %w", err)
	}

	return nil
}

func (si *SkillInstaller) ListAvailableSkills(ctx context.Context) ([]AvailableSkill, error) {
	url := si.catalogURL
	if strings.TrimSpace(url) == "" {
		url = defaultCatalogURL()
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := si.client().Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch skills list: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("failed to fetch skills list: HTTP %d", resp.StatusCode)
	}

	limitedReader := io.LimitReader(resp.Body, maxCatalogSize+1)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}
	if len(body) > maxCatalogSize {
		return nil, fmt.Errorf("skills catalog exceeds maximum size (%d MB)", maxCatalogSize>>20)
	}

	var skills []AvailableSkill
	if err := json.Unmarshal(body, &skills); err != nil {
		return nil, fmt.Errorf("failed to parse skills list: %w", err)
	}

	return skills, nil
}

func (si *SkillInstaller) ListBuiltinSkills() []BuiltinSkill {
	builtinSkillsDir := filepath.Join(filepath.Dir(si.workspace), "picoclaw", "skills")

	entries, err := os.ReadDir(builtinSkillsDir)
	if err != nil {
		return nil
	}

	var skills []BuiltinSkill
	for _, entry := range entries {
		if entry.IsDir() {
			_ = entry
			skillName := entry.Name()
			skillFile := filepath.Join(builtinSkillsDir, skillName, "SKILL.md")

			data, err := os.ReadFile(skillFile)
			description := ""
			if err == nil {
				content := string(data)
				if idx := strings.Index(content, "\n"); idx > 0 {
					firstLine := content[:idx]
					if strings.Contains(firstLine, "description:") {
						descLine := strings.Index(content[idx:], "\n")
						if descLine > 0 {
							description = strings.TrimSpace(content[idx+descLine : idx+descLine])
						}
					}
				}
			}

			// skill := BuiltinSkill{
			// 	Name:    skillName,
			// 	Path:    description,
			// 	Enabled: true,
			// }

			status := "✓"
			fmt.Printf("  %s  %s\n", status, entry.Name())
			if description != "" {
				fmt.Printf("    %s\n", description)
			}
		}
	}
	return skills
}
