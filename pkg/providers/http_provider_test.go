package providers

import (
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestCreateLocalProvider_UsesNativeOllamaProvider(t *testing.T) {
	cfg := &config.Config{}
	cfg.Agents.Defaults.Mode = config.ModePhi
	cfg.Agents.Defaults.LocalBackend = config.BackendOllama
	cfg.Agents.Defaults.LocalModel = "qwen3.5:4b"

	provider, err := createLocalProvider(cfg)
	if err != nil {
		t.Fatalf("createLocalProvider returned error: %v", err)
	}
	if _, ok := provider.(*OllamaProvider); !ok {
		t.Fatalf("provider type=%T want *OllamaProvider", provider)
	}
}

func TestCreateLocalProvider_RejectsMLX(t *testing.T) {
	cfg := &config.Config{}
	cfg.Agents.Defaults.Mode = config.ModePhi
	cfg.Agents.Defaults.LocalBackend = config.BackendMLX
	cfg.Agents.Defaults.LocalModel = "qwen3.5:4b"

	_, err := createLocalProvider(cfg)
	if err == nil {
		t.Fatal("expected mlx local provider to be rejected")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not supported") {
		t.Fatalf("unexpected error: %v", err)
	}
}
