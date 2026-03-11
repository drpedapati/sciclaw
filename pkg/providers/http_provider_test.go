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

func TestCreateProvider_PrefixedOpenAIModelUsesDirectOpenAI(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Model = "openai/gpt-5.4"
	cfg.Providers.OpenAI.APIKey = "test-openai-key"
	cfg.Providers.OpenRouter.APIKey = "test-openrouter-key"

	provider, err := CreateProvider(cfg)
	if err != nil {
		t.Fatalf("CreateProvider returned error: %v", err)
	}
	if _, ok := provider.(*HTTPProvider); !ok {
		t.Fatalf("provider type=%T want *HTTPProvider", provider)
	}
	hp := provider.(*HTTPProvider)
	if hp.apiBase != "https://api.openai.com/v1" {
		t.Fatalf("apiBase=%q want direct OpenAI base", hp.apiBase)
	}
}

func TestCreateProvider_PrefixedOpenAIModelOverridesPinnedAnthropic(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = "anthropic"
	cfg.Agents.Defaults.Model = "openai/gpt-5.4"
	cfg.Providers.OpenAI.APIKey = "test-openai-key"
	cfg.Providers.Anthropic.APIKey = "test-anthropic-key"

	provider, err := CreateProvider(cfg)
	if err != nil {
		t.Fatalf("CreateProvider returned error: %v", err)
	}
	hp, ok := provider.(*HTTPProvider)
	if !ok {
		t.Fatalf("provider type=%T want *HTTPProvider", provider)
	}
	if hp.apiBase != "https://api.openai.com/v1" {
		t.Fatalf("apiBase=%q want direct OpenAI base", hp.apiBase)
	}
}

func TestCreateProvider_PrefixedAnthropicModelUsesDirectAnthropic(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Model = "anthropic/claude-sonnet-4.6"
	cfg.Providers.Anthropic.APIKey = "test-anthropic-key"
	cfg.Providers.OpenRouter.APIKey = "test-openrouter-key"

	provider, err := CreateProvider(cfg)
	if err != nil {
		t.Fatalf("CreateProvider returned error: %v", err)
	}
	if _, ok := provider.(*ClaudeProvider); !ok {
		t.Fatalf("provider type=%T want *ClaudeProvider", provider)
	}
}

func TestCreateProvider_PrefixedAnthropicModelOverridesPinnedOpenAI(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = "openai"
	cfg.Agents.Defaults.Model = "anthropic/claude-sonnet-4.6"
	cfg.Providers.Anthropic.APIKey = "test-anthropic-key"
	cfg.Providers.OpenAI.APIKey = "test-openai-key"

	provider, err := CreateProvider(cfg)
	if err != nil {
		t.Fatalf("CreateProvider returned error: %v", err)
	}
	if _, ok := provider.(*ClaudeProvider); !ok {
		t.Fatalf("provider type=%T want *ClaudeProvider", provider)
	}
}

func TestCreateProvider_PrefixedOpenAIModelFallsBackToOpenRouterWhenDirectUnavailable(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Model = "openai/gpt-5.4"
	cfg.Providers.OpenRouter.APIKey = "test-openrouter-key"

	provider, err := CreateProvider(cfg)
	if err != nil {
		t.Fatalf("CreateProvider returned error: %v", err)
	}
	hp, ok := provider.(*HTTPProvider)
	if !ok {
		t.Fatalf("provider type=%T want *HTTPProvider", provider)
	}
	if hp.apiBase != "https://openrouter.ai/api/v1" {
		t.Fatalf("apiBase=%q want OpenRouter base", hp.apiBase)
	}
}
