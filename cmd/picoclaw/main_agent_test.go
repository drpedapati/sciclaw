package main

import (
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func TestApplyAgentCLIOverrides_RePinsProviderFromModelOverride(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = "anthropic"
	cfg.Agents.Defaults.Model = "claude-opus-4-6"
	cfg.Agents.Defaults.ReasoningEffort = "high"

	applyAgentCLIOverrides(cfg, "openai/gpt-5.4", "medium")

	if got, want := cfg.Agents.Defaults.Model, "openai/gpt-5.4"; got != want {
		t.Fatalf("model = %q, want %q", got, want)
	}
	if got, want := cfg.Agents.Defaults.Provider, "openai"; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}
	if got, want := cfg.Agents.Defaults.ReasoningEffort, "medium"; got != want {
		t.Fatalf("reasoning effort = %q, want %q", got, want)
	}
}

func TestApplyAgentCLIOverrides_LeavesProviderForUnknownModel(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Provider = "openrouter"
	cfg.Agents.Defaults.Model = "openrouter/deepseek/test"

	applyAgentCLIOverrides(cfg, "custom-model-name", "")

	if got, want := cfg.Agents.Defaults.Model, "custom-model-name"; got != want {
		t.Fatalf("model = %q, want %q", got, want)
	}
	if got, want := cfg.Agents.Defaults.Provider, "openrouter"; got != want {
		t.Fatalf("provider = %q, want %q", got, want)
	}
}
