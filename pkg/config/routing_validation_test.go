package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDefaultConfig_RoutingDefaults(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.Routing.Enabled {
		t.Fatal("routing should be disabled by default")
	}
	if cfg.Routing.UnmappedBehavior != RoutingUnmappedBehaviorBlock {
		t.Fatalf("unexpected routing unmapped_behavior: %q", cfg.Routing.UnmappedBehavior)
	}
	if len(cfg.Routing.Mappings) != 0 {
		t.Fatalf("expected no default routing mappings, got %d", len(cfg.Routing.Mappings))
	}
}

func TestValidateRoutingConfig_Valid(t *testing.T) {
	workspace := t.TempDir()
	r := RoutingConfig{
		Enabled:          true,
		UnmappedBehavior: RoutingUnmappedBehaviorBlock,
		Mappings: []RoutingMapping{
			{
				Channel:        "discord",
				ChatID:         "12345",
				Workspace:      workspace,
				AllowedSenders: FlexibleStringSlice{"u1", "u2"},
				Label:          "lab-a",
			},
		},
	}

	if err := ValidateRoutingConfig(r); err != nil {
		t.Fatalf("expected valid routing config, got error: %v", err)
	}
}

func TestValidateRoutingConfig_InvalidUnmappedBehavior(t *testing.T) {
	err := ValidateRoutingConfig(RoutingConfig{
		UnmappedBehavior: "drop",
	})
	if err == nil {
		t.Fatal("expected validation error for invalid unmapped_behavior")
	}
	if !strings.Contains(err.Error(), "routing.unmapped_behavior") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRoutingConfig_RequiredFields(t *testing.T) {
	workspace := t.TempDir()
	tests := []struct {
		name string
		m    RoutingMapping
		want string
	}{
		{
			name: "missing channel",
			m: RoutingMapping{
				ChatID:         "100",
				Workspace:      workspace,
				AllowedSenders: FlexibleStringSlice{"u1"},
			},
			want: "channel is required",
		},
		{
			name: "missing chat_id",
			m: RoutingMapping{
				Channel:        "discord",
				Workspace:      workspace,
				AllowedSenders: FlexibleStringSlice{"u1"},
			},
			want: "chat_id is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRoutingConfig(RoutingConfig{Mappings: []RoutingMapping{tt.m}})
			if err == nil {
				t.Fatal("expected validation error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateRoutingConfig_DuplicateMapping(t *testing.T) {
	workspace := t.TempDir()
	err := ValidateRoutingConfig(RoutingConfig{
		Mappings: []RoutingMapping{
			{
				Channel:        "discord",
				ChatID:         "100",
				Workspace:      workspace,
				AllowedSenders: FlexibleStringSlice{"u1"},
			},
			{
				Channel:        "Discord",
				ChatID:         "100",
				Workspace:      workspace,
				AllowedSenders: FlexibleStringSlice{"u2"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected duplicate mapping validation error")
	}
	if !strings.Contains(err.Error(), "duplicates mapping") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRoutingConfig_RelativeWorkspaceRejected(t *testing.T) {
	err := ValidateRoutingConfig(RoutingConfig{
		Mappings: []RoutingMapping{
			{
				Channel:        "telegram",
				ChatID:         "abc",
				Workspace:      "relative/path",
				AllowedSenders: FlexibleStringSlice{"u1"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected workspace path validation error")
	}
	if !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRoutingConfig_MissingWorkspaceRejected(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	err := ValidateRoutingConfig(RoutingConfig{
		Mappings: []RoutingMapping{
			{
				Channel:        "telegram",
				ChatID:         "abc",
				Workspace:      missing,
				AllowedSenders: FlexibleStringSlice{"u1"},
			},
		},
	})
	if err == nil {
		t.Fatal("expected missing workspace validation error")
	}
	if !strings.Contains(err.Error(), "not accessible") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateRoutingConfig_EmptyAllowedSendersRejected(t *testing.T) {
	workspace := t.TempDir()
	err := ValidateRoutingConfig(RoutingConfig{
		Mappings: []RoutingMapping{
			{
				Channel:        "discord",
				ChatID:         "100",
				Workspace:      workspace,
				AllowedSenders: FlexibleStringSlice{},
			},
		},
	})
	if err == nil {
		t.Fatal("expected empty allowed_senders validation error")
	}
	if !strings.Contains(err.Error(), "allowed_senders") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfig_MissingRoutingSectionUsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Routing.UnmappedBehavior != RoutingUnmappedBehaviorBlock {
		t.Fatalf("expected default unmapped_behavior %q, got %q", RoutingUnmappedBehaviorBlock, cfg.Routing.UnmappedBehavior)
	}
}

func TestLoadConfig_InvalidRoutingRejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{
  "routing": {
    "enabled": true,
    "unmapped_behavior": "block",
    "mappings": [
      {
        "channel": "discord",
        "chat_id": "100",
        "workspace": "not/absolute",
        "allowed_senders": ["u1"]
      }
    ]
  }
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected LoadConfig to reject invalid routing config")
	}
	if !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSaveConfig_InvalidRoutingRejected(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Routing.Enabled = true
	cfg.Routing.Mappings = []RoutingMapping{
		{
			Channel:        "discord",
			ChatID:         "100",
			Workspace:      "not/absolute",
			AllowedSenders: FlexibleStringSlice{"u1"},
		},
	}

	path := filepath.Join(t.TempDir(), "config.json")
	err := SaveConfig(path, cfg)
	if err == nil {
		t.Fatal("expected SaveConfig to reject invalid routing config")
	}
	if !strings.Contains(err.Error(), "absolute path") {
		t.Fatalf("unexpected error: %v", err)
	}
}
