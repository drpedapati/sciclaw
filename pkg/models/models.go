package models

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/sipeed/picoclaw/pkg/auth"
	"github.com/sipeed/picoclaw/pkg/config"
)

const anthropicOAuthBetaHeader = "oauth-2025-04-20"

// DiscoverResult is returned by model discovery.
type DiscoverResult struct {
	Provider string   `json:"provider"`
	Source   string   `json:"source"`
	Models   []string `json:"models"`
	Warning  string   `json:"warning,omitempty"`
}

// ProviderInfo describes a configured provider and its auth status.
type ProviderInfo struct {
	Name       string
	HasAPIKey  bool
	AuthMethod string // "api_key", "oauth", "token", or ""
	Models     []string
}

// ResolveProvider returns the provider name that would handle the given model string.
func ResolveProvider(model string, cfg *config.Config) string {
	lower := strings.ToLower(model)

	// Explicit provider prefix
	if strings.Contains(lower, "claude") || strings.HasPrefix(model, "anthropic/") {
		return "anthropic"
	}
	if strings.Contains(lower, "gpt") || strings.Contains(lower, "o1") || strings.Contains(lower, "o3") || strings.Contains(lower, "o4") || strings.Contains(lower, "codex") || strings.HasPrefix(model, "openai/") {
		return "openai"
	}
	if strings.Contains(lower, "gemini") || strings.HasPrefix(model, "google/") {
		return "gemini"
	}
	if strings.Contains(lower, "glm") || strings.Contains(lower, "zhipu") {
		return "zhipu"
	}
	if strings.Contains(lower, "groq") || strings.HasPrefix(model, "groq/") {
		return "groq"
	}
	if strings.Contains(lower, "deepseek") {
		return "deepseek"
	}
	if strings.HasPrefix(model, "openrouter/") || strings.HasPrefix(model, "meta-llama/") {
		return "openrouter"
	}

	// Fall back to explicit provider config
	if cfg.Agents.Defaults.Provider != "" {
		return cfg.Agents.Defaults.Provider
	}
	return "unknown"
}

// ListProviders returns info about all configured providers.
func ListProviders(cfg *config.Config) []ProviderInfo {
	var providers []ProviderInfo

	add := func(name string, pc config.ProviderConfig, models []string) {
		if pc.APIKey == "" && pc.AuthMethod == "" {
			return
		}
		method := "api_key"
		if pc.AuthMethod == "oauth" || pc.AuthMethod == "token" {
			method = pc.AuthMethod
		}
		providers = append(providers, ProviderInfo{
			Name:       name,
			HasAPIKey:  pc.APIKey != "",
			AuthMethod: method,
			Models:     models,
		})
	}

	add("anthropic", cfg.Providers.Anthropic, []string{
		"claude-opus-4-6", "claude-sonnet-4-5-20250929", "claude-haiku-4-5-20251001",
	})
	add("openai", cfg.Providers.OpenAI, []string{
		"gpt-5.3-codex", "gpt-5.3-codex-spark", "gpt-5.2-codex", "gpt-5.2",
	})
	add("openrouter", cfg.Providers.OpenRouter, []string{
		"openrouter/<model>",
	})
	add("gemini", cfg.Providers.Gemini, []string{
		"gemini-2.5-pro", "gemini-2.5-flash",
	})
	add("groq", cfg.Providers.Groq, []string{
		"groq/llama-3.3-70b",
	})
	add("deepseek", cfg.Providers.DeepSeek, []string{
		"deepseek-chat", "deepseek-reasoner",
	})
	add("zhipu", cfg.Providers.Zhipu, []string{
		"glm-4.7",
	})

	return providers
}

// PrintList displays the current model, providers, and effort setting.
func PrintList(cfg *config.Config) {
	fmt.Printf("Current model: %s\n", cfg.Agents.Defaults.Model)

	provider := ResolveProvider(cfg.Agents.Defaults.Model, cfg)
	fmt.Printf("Resolved provider: %s\n", provider)

	if cfg.Agents.Defaults.ReasoningEffort != "" {
		fmt.Printf("Reasoning effort: %s\n", cfg.Agents.Defaults.ReasoningEffort)
	} else {
		fmt.Printf("Reasoning effort: (provider default)\n")
	}

	providers := ListProviders(cfg)
	if len(providers) == 0 {
		fmt.Println("\nNo providers configured. Add API keys to ~/.picoclaw/config.json")
		return
	}

	fmt.Println("\nConfigured providers:")
	for _, p := range providers {
		authStr := p.AuthMethod
		// Check OAuth credential status
		if p.AuthMethod == "oauth" || p.AuthMethod == "token" {
			cred, err := auth.GetCredential(p.Name)
			if err == nil && cred != nil {
				if cred.IsExpired() {
					authStr += " (expired)"
				} else if cred.NeedsRefresh() {
					authStr += " (needs refresh)"
				} else {
					authStr += " (valid)"
				}
			} else {
				authStr += " (not authenticated)"
			}
		}

		fmt.Printf("  %s (%s)\n", p.Name, authStr)
		for _, m := range p.Models {
			fmt.Printf("    - %s\n", m)
		}
	}
}

// SetModel validates and persists a new default model.
func SetModel(cfg *config.Config, configPath string, newModel string) error {
	oldModel := cfg.Agents.Defaults.Model
	oldProvider := cfg.Agents.Defaults.Provider
	provider := ResolveProvider(newModel, cfg)

	if provider == "unknown" {
		fmt.Printf("Warning: could not resolve a provider for model %q.\n", newModel)
		fmt.Printf("The model will be set but may fail at runtime if no matching provider is configured.\n\n")
	}

	cfg.Agents.Defaults.Model = newModel
	if provider != "unknown" {
		cfg.Agents.Defaults.Provider = provider
	}

	if err := config.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Model changed: %s → %s\n", oldModel, newModel)
	fmt.Printf("Provider: %s\n", provider)
	if provider != "unknown" && oldProvider != cfg.Agents.Defaults.Provider {
		old := oldProvider
		if strings.TrimSpace(old) == "" {
			old = "(auto)"
		}
		fmt.Printf("Pinned provider: %s → %s\n", old, cfg.Agents.Defaults.Provider)
	}
	return nil
}

// SetEffort validates and persists a new default reasoning effort.
func SetEffort(cfg *config.Config, configPath string, effort string) error {
	old := cfg.Agents.Defaults.ReasoningEffort
	if old == "" {
		old = "(none)"
	}

	cfg.Agents.Defaults.ReasoningEffort = effort

	if err := config.SaveConfig(configPath, cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	fmt.Printf("Reasoning effort changed: %s → %s\n", old, effort)
	return nil
}

// PrintStatus displays the current model status.
func PrintStatus(cfg *config.Config) {
	model := cfg.Agents.Defaults.Model
	provider := ResolveProvider(model, cfg)

	fmt.Printf("Model:            %s\n", model)
	fmt.Printf("Provider:         %s\n", provider)

	// Show auth method for the resolved provider
	authMethod := resolveAuthMethod(provider, cfg)
	if authMethod != "" {
		fmt.Printf("Auth:             %s\n", authMethod)
	}

	if cfg.Agents.Defaults.ReasoningEffort != "" {
		fmt.Printf("Reasoning Effort: %s\n", cfg.Agents.Defaults.ReasoningEffort)
	} else {
		fmt.Printf("Reasoning Effort: (provider default)\n")
	}
}

// GetConfigPath returns the standard config file path.
func GetConfigPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".picoclaw", "config.json")
}

func resolveAuthMethod(provider string, cfg *config.Config) string {
	var pc config.ProviderConfig
	switch provider {
	case "anthropic":
		pc = cfg.Providers.Anthropic
	case "openai":
		pc = cfg.Providers.OpenAI
	case "openrouter":
		pc = cfg.Providers.OpenRouter
	case "gemini":
		pc = cfg.Providers.Gemini
	case "groq":
		pc = cfg.Providers.Groq
	case "deepseek":
		pc = cfg.Providers.DeepSeek
	case "zhipu":
		pc = cfg.Providers.Zhipu
	default:
		return ""
	}

	if pc.AuthMethod == "oauth" || pc.AuthMethod == "token" {
		return pc.AuthMethod
	}
	if pc.APIKey != "" {
		return "api_key"
	}

	// If config.json wasn't updated (or was reset) but auth.json has valid credentials,
	// treat the provider as configured. This is common with idempotent onboard reruns.
	if provider == "openai" || provider == "anthropic" {
		if cred, err := auth.GetCredential(provider); err == nil && cred != nil && cred.AccessToken != "" {
			if cred.AuthMethod != "" {
				return cred.AuthMethod
			}
			return "token"
		}
	}

	return "not configured"
}

// Discover returns selectable model IDs for the active provider.
// It attempts provider endpoint discovery first, then falls back to known built-ins.
func Discover(cfg *config.Config) DiscoverResult {
	provider := resolveDiscoveryProvider(cfg)
	result := DiscoverResult{
		Provider: provider,
		Source:   "builtin",
		Models:   knownModelsForProvider(provider),
	}

	if provider == "anthropic" {
		models, err := discoverAnthropicModels(cfg)
		if err == nil && len(models) > 0 {
			result.Source = "endpoint"
			result.Models = models
			return result
		}
		if err != nil {
			result.Warning = err.Error()
		}
	}

	// If provider-specific builtins are empty, include known models from configured providers.
	if len(result.Models) == 0 {
		for _, p := range ListProviders(cfg) {
			result.Models = append(result.Models, p.Models...)
		}
		result.Models = dedupeNonEmpty(result.Models)
	}

	if len(result.Models) == 0 {
		current := strings.TrimSpace(cfg.Agents.Defaults.Model)
		if current != "" {
			result.Models = []string{current}
		}
	}
	return result
}

func PrintDiscover(result DiscoverResult) {
	fmt.Printf("Provider: %s\n", result.Provider)
	fmt.Printf("Source:   %s\n", result.Source)
	if strings.TrimSpace(result.Warning) != "" {
		fmt.Printf("Warning:  %s\n", result.Warning)
	}
	fmt.Println("Models:")
	for _, m := range result.Models {
		fmt.Printf("- %s\n", m)
	}
}

func resolveDiscoveryProvider(cfg *config.Config) string {
	byModel := strings.ToLower(strings.TrimSpace(ResolveProvider(cfg.Agents.Defaults.Model, cfg)))
	if byModel != "" && byModel != "unknown" {
		return byModel
	}

	pinned := strings.ToLower(strings.TrimSpace(cfg.Agents.Defaults.Provider))
	if pinned != "" && pinned != "unknown" {
		return pinned
	}

	if cred, err := auth.GetCredential("anthropic"); err == nil && cred != nil && strings.TrimSpace(cred.AccessToken) != "" {
		return "anthropic"
	}
	if cred, err := auth.GetCredential("openai"); err == nil && cred != nil && strings.TrimSpace(cred.AccessToken) != "" {
		return "openai"
	}
	return "unknown"
}

func knownModelsForProvider(provider string) []string {
	switch provider {
	case "anthropic":
		return []string{"claude-opus-4-6", "claude-sonnet-4-5-20250929", "claude-haiku-4-5-20251001"}
	case "openai":
		return []string{"gpt-5.3-codex", "gpt-5.3-codex-spark", "gpt-5.2-codex", "gpt-5.2"}
	case "gemini":
		return []string{"gemini-2.5-pro", "gemini-2.5-flash"}
	case "openrouter":
		return []string{"openrouter/<model>"}
	case "deepseek":
		return []string{"deepseek-chat", "deepseek-reasoner"}
	case "zhipu":
		return []string{"glm-4.7"}
	case "groq":
		return []string{"groq/llama-3.3-70b"}
	default:
		return nil
	}
}

func discoverAnthropicModels(cfg *config.Config) ([]string, error) {
	token := ""
	if cred, err := auth.GetCredential("anthropic"); err == nil && cred != nil {
		token = strings.TrimSpace(cred.AccessToken)
	}
	if token == "" {
		token = strings.TrimSpace(cfg.Providers.Anthropic.APIKey)
	}
	if token == "" {
		return nil, fmt.Errorf("anthropic credentials not configured")
	}

	baseURL := strings.TrimSpace(cfg.Providers.Anthropic.APIBase)
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	baseURL = strings.TrimSuffix(baseURL, "/v1")

	opts := []option.RequestOption{
		option.WithAuthToken(token),
		option.WithBaseURL(baseURL),
	}
	if isAnthropicOAuthToken(token) {
		opts = append(opts, option.WithHeader("anthropic-beta", anthropicOAuthBetaHeader))
	}

	client := anthropic.NewClient(opts...)
	params := anthropic.ModelListParams{
		Limit: anthropic.Int(1000),
	}
	if isAnthropicOAuthToken(token) {
		params.Betas = []anthropic.AnthropicBeta{
			anthropic.AnthropicBeta(anthropicOAuthBetaHeader),
		}
	}

	pager := client.Models.ListAutoPaging(context.Background(), params)
	var models []string
	for pager.Next() {
		modelID := strings.TrimSpace(pager.Current().ID)
		if modelID != "" {
			models = append(models, modelID)
		}
	}
	if err := pager.Err(); err != nil {
		return nil, err
	}

	models = dedupeNonEmpty(models)
	if len(models) == 0 {
		return nil, fmt.Errorf("anthropic endpoint returned zero models")
	}
	return models, nil
}

func isAnthropicOAuthToken(token string) bool {
	return strings.HasPrefix(strings.TrimSpace(token), "sk-ant-oat")
}

func dedupeNonEmpty(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
