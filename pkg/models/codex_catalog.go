package models

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/codexidentity"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/transport"
)

// ModelMetadata augments the legacy ID list without changing its JSON shape.
type ModelMetadata struct {
	Name            string   `json:"name,omitempty"`
	Provider        string   `json:"provider"`
	Source          string   `json:"source"`
	ReasoningLevels []string `json:"reasoning_levels,omitempty"`
}

func usesCodexCatalog(cfg *config.Config) bool {
	p := cfg.Providers.OpenAI
	if p.AuthMethod == "oauth" || p.AuthMethod == "token" {
		return true
	}
	cred, _ := auth.GetCredential("openai")
	return p.APIKey == "" && cred != nil && cred.AccessToken != ""
}

func discoverCodexModels() ([]string, map[string]ModelMetadata, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cred, err := auth.ValidOpenAICredential(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("Codex credentials unavailable; check OpenAI login")
	}
	return fetchCodexCatalog(ctx, transport.NewCloudflareClient(), "https://chatgpt.com/backend-api/codex", cred.AccessToken, cred.AccountID, codexidentity.Version)
}

func fetchCodexCatalog(ctx context.Context, client *http.Client, base, token, account, version string) ([]string, map[string]ModelMetadata, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models?client_version="+version, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid Codex catalog URL")
	}
	codexidentity.Headers(req.Header)
	req.Header.Set("version", version)
	req.Header.Set("User-Agent", codexidentity.Originator+"/"+version)
	req.Header.Set("Authorization", "Bearer "+token)
	if account != "" {
		req.Header.Set("ChatGPT-Account-ID", account)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("Codex catalog unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("Codex catalog HTTP %d", resp.StatusCode)
	}
	const limit = 4 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil || len(raw) > limit {
		return nil, nil, fmt.Errorf("Codex catalog response too large or unreadable")
	}
	var payload struct {
		Models []struct {
			Slug       string `json:"slug"`
			Name       string `json:"display_name"`
			Visibility string `json:"visibility"`
			Minimum    string `json:"minimal_client_version"`
			Efforts    []struct {
				Effort string `json:"effort"`
			} `json:"supported_reasoning_levels"`
		} `json:"models"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return nil, nil, fmt.Errorf("invalid Codex catalog JSON")
	}
	var ids []string
	metadata := map[string]ModelMetadata{}
	for _, m := range payload.Models {
		if strings.TrimSpace(m.Slug) == "" || m.Visibility != "list" || !clientCompatible(m.Minimum, version) {
			continue
		}
		if _, ok := metadata[m.Slug]; ok {
			continue
		}
		levels := []string{}
		for _, e := range m.Efforts {
			// ultra requests automatic delegation, which sciClaw does not implement.
			if e.Effort != "ultra" {
				levels = append(levels, e.Effort)
			}
		}
		ids = append(ids, m.Slug)
		metadata[m.Slug] = ModelMetadata{Name: m.Name, Provider: "openai", Source: "endpoint", ReasoningLevels: dedupeNonEmpty(levels)}
	}
	if len(ids) == 0 {
		return nil, nil, fmt.Errorf("Codex catalog has no selectable models")
	}
	return ids, metadata, nil
}

func clientCompatible(minimum, client string) bool {
	if minimum == "" {
		return true
	}
	a, b := strings.Split(minimum, "."), strings.Split(client, ".")
	if len(a) != 3 || len(b) != 3 {
		return false
	}
	for i := range a {
		x, ex := strconv.Atoi(a[i])
		y, ey := strconv.Atoi(b[i])
		if ex != nil || ey != nil || x < 0 || y < 0 {
			return false
		}
		if x != y {
			return x < y
		}
	}
	return true
}

// ReasoningLevels supplies safe fallback choices when discovery is unavailable.
func ReasoningLevels(model string) []string {
	m := strings.TrimPrefix(strings.ToLower(model), "openai/")
	if strings.HasPrefix(m, "gpt-6") || strings.HasPrefix(m, "gpt-5.6") {
		return []string{"low", "medium", "high", "xhigh", "max"}
	}
	return []string{"none", "minimal", "low", "medium", "high", "xhigh"}
}

func ValidateEffort(cfg *config.Config, effort string) error {
	return validateEffortForModel(cfg, cfg.Agents.Defaults.Model, effort)
}

func validateEffortForModel(cfg *config.Config, model, effort string) error {
	if effort == "" {
		return nil
	}
	levels := ReasoningLevels(model)
	if ResolveProvider(model, cfg) == "openai" && usesCodexCatalog(cfg) {
		_, metadata, _ := discoverCodexModels()
		if info, ok := metadata[strings.TrimPrefix(model, "openai/")]; ok && len(info.ReasoningLevels) > 0 {
			levels = info.ReasoningLevels
		}
	}
	for _, level := range levels {
		if effort == level {
			return nil
		}
	}
	return fmt.Errorf("reasoning effort %q is not supported for %s; choose %s", effort, model, strings.Join(levels, ", "))
}
