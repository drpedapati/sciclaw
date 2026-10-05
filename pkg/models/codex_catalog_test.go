package models

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/codexidentity"
	"github.com/sipeed/picoclaw/pkg/config"
)

func TestCodexCatalogCapabilitiesAndIdentity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" || r.URL.Query().Get("client_version") != codexidentity.Version {
			t.Error("incorrect request URL")
		}
		for name, want := range map[string]string{"Authorization": "Bearer synthetic", "ChatGPT-Account-ID": "account", "originator": codexidentity.Originator, "version": codexidentity.Version, "User-Agent": codexidentity.Originator + "/" + codexidentity.Version} {
			if r.Header.Get(name) != want {
				t.Errorf("incorrect header %s", name)
			}
		}
		fmt.Fprint(w, `{"models":[{"slug":"gpt-6.1-sol","display_name":"Sol","visibility":"list","minimal_client_version":"0.153.0","supported_reasoning_levels":[{"effort":"medium"},{"effort":"max"},{"effort":"ultra"}]},{"slug":"hidden","visibility":"hide"},{"slug":"future","visibility":"list","minimal_client_version":"0.161.0"},{"slug":"gpt-6.1-sol","visibility":"list"}]}`)
	}))
	defer srv.Close()
	ids, info, err := fetchCodexCatalog(t.Context(), srv.Client(), srv.URL, "synthetic", "account", codexidentity.Version)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "gpt-6.1-sol" || info[ids[0]].Name != "Sol" || strings.Join(info[ids[0]].ReasoningLevels, ",") != "medium,max" {
		t.Fatalf("incorrect capabilities: %v %#v", ids, info)
	}
}

func TestCodexCatalogFailureBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{{"401", 401, "synthetic-secret"}, {"429", 429, "synthetic-secret"}, {"invalid", 200, "synthetic-secret"}, {"empty", 200, `{"models":[]}`}, {"oversized", 200, strings.Repeat("x", (4<<20)+1)}} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer srv.Close()
			_, _, err := fetchCodexCatalog(t.Context(), srv.Client(), srv.URL, "synthetic-secret", "", codexidentity.Version)
			if err == nil || strings.Contains(err.Error(), "synthetic-secret") {
				t.Fatalf("expected sanitized failure, got %v", err)
			}
		})
	}
	t.Run("timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
		defer srv.Close()
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()
		if _, _, err := fetchCodexCatalog(ctx, srv.Client(), srv.URL, "token", "", codexidentity.Version); err == nil {
			t.Fatal("deadline ignored")
		}
	})
}

func TestDiscoverCodexMetadataWithoutChangingSelection(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	cfg.Providers.OpenAI.AuthMethod = "oauth"
	fetch := func() ([]string, map[string]ModelMetadata, error) {
		return []string{"gpt-6.1-sol"}, map[string]ModelMetadata{"gpt-6.1-sol": {Name: "Sol", Provider: "openai", Source: "endpoint", ReasoningLevels: []string{"medium", "max"}}}, nil
	}
	got := discoverWith(cfg, fetch)
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var decoded DiscoverResult
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if got.Source != "endpoint" || len(got.Models) != 1 || len(decoded.Metadata["gpt-6.1-sol"].ReasoningLevels) != 2 {
		t.Fatalf("metadata lost: %s", raw)
	}
	if cfg.Agents.Defaults.ReasoningEffort != "medium" || cfg.Agents.Defaults.Model != "gpt-6.1-sol" {
		t.Fatal("discovery changed selection")
	}
	// Configured Codex is also discovered when another provider is selected.
	cfg.Agents.Defaults.Model = "claude-sonnet-4.6"
	got = discoverWith(cfg, fetch)
	if got.Metadata["gpt-6.1-sol"].Source != "endpoint" || got.Source != "endpoint+builtin" {
		t.Fatal("secondary Codex discovery lost")
	}
	// An API key never goes to the ChatGPT catalog transport.
	cfg.Agents.Defaults.Model = "gpt-6.1-sol"
	cfg.Providers.OpenAI.AuthMethod = ""
	cfg.Providers.OpenAI.APIKey = "synthetic"
	got = discoverWith(cfg, func() ([]string, map[string]ModelMetadata, error) {
		t.Fatal("API-key mode used Codex transport")
		return nil, nil, nil
	})
	if got.Source != "builtin" {
		t.Fatal("unexpected API-key source")
	}
	// Failure remains visible and leaves built-ins usable.
	cfg.Providers.OpenAI.AuthMethod = "oauth"
	got = discoverWith(cfg, func() ([]string, map[string]ModelMetadata, error) { return nil, nil, fmt.Errorf("catalog HTTP 401") })
	if got.Source != "builtin" || got.Warning == "" || len(got.Models) == 0 {
		t.Fatal("missing fallback warning")
	}
}

func TestClientCompatibleNumeric(t *testing.T) {
	for _, tc := range []struct {
		minimum, client string
		want            bool
	}{{"0.153.0", "0.144.1", false}, {"0.153.0", "0.160.0", true}, {"0.9.0", "0.10.0", true}, {"bad", "0.160.0", false}} {
		if got := clientCompatible(tc.minimum, tc.client); got != tc.want {
			t.Errorf("%s/%s compatibility=%v", tc.minimum, tc.client, got)
		}
	}
}

func TestSolRejectsUnsupportedEffort(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := config.DefaultConfig()
	if err := ValidateEffort(cfg, "none"); err == nil {
		t.Fatal("Sol accepted none")
	}
	if err := ValidateEffort(cfg, "medium"); err != nil {
		t.Fatal(err)
	}
}
