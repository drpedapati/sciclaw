package models

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestSetModelSyncsProvider(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Model = "gpt-5.2"
	cfg.Agents.Defaults.Provider = "openai"

	configPath := filepath.Join(t.TempDir(), "config.json")
	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	if err := SetModel(cfg, configPath, "claude-opus-4-6"); err != nil {
		t.Fatalf("SetModel: %v", err)
	}

	if got, want := cfg.Agents.Defaults.Provider, "anthropic"; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}

	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	text := string(raw)
	if !strings.Contains(text, `"provider": "anthropic"`) {
		t.Fatalf("saved config missing provider sync: %s", text)
	}
}

func TestResolveDiscoveryProviderPrefersModel(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Model = "claude-sonnet-4-5-20250929"
	cfg.Agents.Defaults.Provider = "openai"

	if got, want := resolveDiscoveryProvider(cfg), "anthropic"; got != want {
		t.Fatalf("resolveDiscoveryProvider() = %q, want %q", got, want)
	}
}

func TestDiscoverFallsBackToBuiltinModels(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Model = "claude-opus-4-6"
	cfg.Agents.Defaults.Provider = ""
	cfg.Providers.Anthropic.APIKey = ""
	cfg.Providers.Anthropic.AuthMethod = ""

	result := Discover(cfg)
	if got, want := result.Provider, "anthropic"; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}
	if result.Source != "builtin" && result.Source != "endpoint" {
		t.Fatalf("unexpected source %q", result.Source)
	}
	if len(result.Models) == 0 {
		t.Fatalf("expected non-empty model list")
	}
	found := false
	for _, m := range result.Models {
		if m == "claude-opus-4-6" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("builtin anthropic models missing expected id; got %v", result.Models)
	}
}
