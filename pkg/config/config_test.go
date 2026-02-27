package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDefaultConfig_HeartbeatEnabled verifies heartbeat is disabled by default
func TestDefaultConfig_HeartbeatEnabled(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Heartbeat.Enabled {
		t.Error("Heartbeat should be disabled by default")
	}
}

// TestDefaultConfig_WorkspacePath verifies workspace path is correctly set
func TestDefaultConfig_WorkspacePath(t *testing.T) {
	cfg := DefaultConfig()

	// Just verify the workspace is set, don't compare exact paths
	// since expandHome behavior may differ based on environment
	if cfg.Agents.Defaults.Workspace == "" {
		t.Error("Workspace should not be empty")
	}
	if cfg.Agents.Defaults.SharedWorkspace == "" {
		t.Error("SharedWorkspace should not be empty")
	}
	if cfg.Agents.Defaults.SharedWorkspaceReadOnly {
		t.Error("SharedWorkspaceReadOnly should default to false")
	}
}

func TestDefaultConfig_SharedWorkspacePath(t *testing.T) {
	cfg := DefaultConfig()
	if strings.TrimSpace(cfg.SharedWorkspacePath()) == "" {
		t.Fatal("SharedWorkspacePath should not be empty")
	}
}

// TestDefaultConfig_Model verifies model is set
func TestDefaultConfig_Model(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Agents.Defaults.Model == "" {
		t.Error("Model should not be empty")
	}
}

// TestDefaultConfig_MaxTokens verifies max tokens has default value
func TestDefaultConfig_MaxTokens(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Agents.Defaults.MaxTokens == 0 {
		t.Error("MaxTokens should not be zero")
	}
}

// TestDefaultConfig_MaxToolIterations verifies max tool iterations default is unbounded.
func TestDefaultConfig_MaxToolIterations(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Agents.Defaults.MaxToolIterations != 0 {
		t.Errorf("MaxToolIterations default should be 0 (unbounded), got %d", cfg.Agents.Defaults.MaxToolIterations)
	}
}

// TestDefaultConfig_Temperature verifies temperature has default value
func TestDefaultConfig_Temperature(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Agents.Defaults.Temperature == 0 {
		t.Error("Temperature should not be zero")
	}
}

// TestDefaultConfig_Gateway verifies gateway defaults
func TestDefaultConfig_Gateway(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Gateway.Host != "0.0.0.0" {
		t.Error("Gateway host should have default value")
	}
	if cfg.Gateway.Port == 0 {
		t.Error("Gateway port should have default value")
	}
}

// TestDefaultConfig_Providers verifies provider structure
func TestDefaultConfig_Providers(t *testing.T) {
	cfg := DefaultConfig()

	// Verify all providers are empty by default
	if cfg.Providers.Anthropic.APIKey != "" {
		t.Error("Anthropic API key should be empty by default")
	}
	if cfg.Providers.OpenAI.APIKey != "" {
		t.Error("OpenAI API key should be empty by default")
	}
	if cfg.Providers.OpenRouter.APIKey != "" {
		t.Error("OpenRouter API key should be empty by default")
	}
	if cfg.Providers.Groq.APIKey != "" {
		t.Error("Groq API key should be empty by default")
	}
	if cfg.Providers.Zhipu.APIKey != "" {
		t.Error("Zhipu API key should be empty by default")
	}
	if cfg.Providers.VLLM.APIKey != "" {
		t.Error("VLLM API key should be empty by default")
	}
	if cfg.Providers.Gemini.APIKey != "" {
		t.Error("Gemini API key should be empty by default")
	}
}

// TestDefaultConfig_Channels verifies channels are disabled by default
func TestDefaultConfig_Channels(t *testing.T) {
	cfg := DefaultConfig()

	// Verify all channels are disabled by default
	if cfg.Channels.WhatsApp.Enabled {
		t.Error("WhatsApp should be disabled by default")
	}
	if cfg.Channels.Telegram.Enabled {
		t.Error("Telegram should be disabled by default")
	}
	if cfg.Channels.Feishu.Enabled {
		t.Error("Feishu should be disabled by default")
	}
	if cfg.Channels.Discord.Enabled {
		t.Error("Discord should be disabled by default")
	}
	if cfg.Channels.MaixCam.Enabled {
		t.Error("MaixCam should be disabled by default")
	}
	if cfg.Channels.QQ.Enabled {
		t.Error("QQ should be disabled by default")
	}
	if cfg.Channels.DingTalk.Enabled {
		t.Error("DingTalk should be disabled by default")
	}
	if cfg.Channels.Slack.Enabled {
		t.Error("Slack should be disabled by default")
	}
}

func TestDefaultConfig_DiscordArchive(t *testing.T) {
	cfg := DefaultConfig()
	archive := cfg.Channels.Discord.Archive
	if !archive.Enabled {
		t.Fatal("Discord archive should be enabled by default")
	}
	if !archive.AutoArchive {
		t.Fatal("Discord auto_archive should be enabled by default")
	}
	if archive.MaxSessionTokens != 24000 {
		t.Fatalf("MaxSessionTokens=%d, want 24000", archive.MaxSessionTokens)
	}
	if archive.MaxSessionMessages != 120 {
		t.Fatalf("MaxSessionMessages=%d, want 120", archive.MaxSessionMessages)
	}
	if archive.KeepUserPairs != 12 {
		t.Fatalf("KeepUserPairs=%d, want 12", archive.KeepUserPairs)
	}
	if archive.MinTailMessages != 4 {
		t.Fatalf("MinTailMessages=%d, want 4", archive.MinTailMessages)
	}
	if archive.RecallTopK != 6 {
		t.Fatalf("RecallTopK=%d, want 6", archive.RecallTopK)
	}
	if archive.RecallMaxChars != 3000 {
		t.Fatalf("RecallMaxChars=%d, want 3000", archive.RecallMaxChars)
	}
	if archive.RecallMinScore != 0.20 {
		t.Fatalf("RecallMinScore=%f, want 0.20", archive.RecallMinScore)
	}
}

func TestLoadConfig_NormalizesDiscordArchive(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	raw := `{
  "channels": {
    "discord": {
      "archive": {
        "enabled": true,
        "auto_archive": true,
        "max_session_tokens": 0,
        "max_session_messages": -1,
        "keep_user_pairs": 0,
        "min_tail_messages": -5,
        "recall_top_k": 0,
        "recall_max_chars": -1,
        "recall_min_score": 2.4
      }
    }
  }
}`
	if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	got := cfg.Channels.Discord.Archive
	if got.MaxSessionTokens != 24000 {
		t.Fatalf("MaxSessionTokens=%d, want 24000", got.MaxSessionTokens)
	}
	if got.MaxSessionMessages != 120 {
		t.Fatalf("MaxSessionMessages=%d, want 120", got.MaxSessionMessages)
	}
	if got.KeepUserPairs != 12 {
		t.Fatalf("KeepUserPairs=%d, want 12", got.KeepUserPairs)
	}
	if got.MinTailMessages != 4 {
		t.Fatalf("MinTailMessages=%d, want 4", got.MinTailMessages)
	}
	if got.RecallTopK != 6 {
		t.Fatalf("RecallTopK=%d, want 6", got.RecallTopK)
	}
	if got.RecallMaxChars != 3000 {
		t.Fatalf("RecallMaxChars=%d, want 3000", got.RecallMaxChars)
	}
	if got.RecallMinScore != 0.20 {
		t.Fatalf("RecallMinScore=%f, want 0.20", got.RecallMinScore)
	}
}

// TestDefaultConfig_WebTools verifies web tools config
func TestDefaultConfig_WebTools(t *testing.T) {
	cfg := DefaultConfig()

	// Verify web tools defaults
	if cfg.Tools.Web.Brave.MaxResults != 5 {
		t.Error("Expected Brave MaxResults 5, got ", cfg.Tools.Web.Brave.MaxResults)
	}
	if cfg.Tools.Web.Brave.APIKey != "" {
		t.Error("Brave API key should be empty by default")
	}
	if cfg.Tools.Web.DuckDuckGo.MaxResults != 5 {
		t.Error("Expected DuckDuckGo MaxResults 5, got ", cfg.Tools.Web.DuckDuckGo.MaxResults)
	}
}

func TestSaveConfig_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}

	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	cfg := DefaultConfig()
	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("config file has permission %04o, want 0600", perm)
	}
}

// TestConfig_Complete verifies all config fields are set
func TestConfig_Complete(t *testing.T) {
	cfg := DefaultConfig()

	// Verify complete config structure
	if cfg.Agents.Defaults.Workspace == "" {
		t.Error("Workspace should not be empty")
	}
	if cfg.Agents.Defaults.Model == "" {
		t.Error("Model should not be empty")
	}
	if cfg.Agents.Defaults.Temperature == 0 {
		t.Error("Temperature should have default value")
	}
	if cfg.Agents.Defaults.MaxTokens == 0 {
		t.Error("MaxTokens should not be zero")
	}
	if cfg.Agents.Defaults.MaxToolIterations < 0 {
		t.Error("MaxToolIterations should not be negative")
	}
	if cfg.Gateway.Host != "0.0.0.0" {
		t.Error("Gateway host should have default value")
	}
	if cfg.Gateway.Port == 0 {
		t.Error("Gateway port should have default value")
	}
	if cfg.Heartbeat.Enabled {
		t.Error("Heartbeat should be disabled by default")
	}
}

func TestSaveConfig_UsesRestrictiveFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission semantics differ on windows")
	}

	path := filepath.Join(t.TempDir(), "config.json")
	cfg := DefaultConfig()

	if err := SaveConfig(path, cfg); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config file failed: %v", err)
	}

	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("expected config mode 0600, got %o", got)
	}
}
