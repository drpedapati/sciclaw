package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
)

func doctorCheckByName(checks []doctorCheck, name string) (doctorCheck, bool) {
	for _, c := range checks {
		if c.Name == name {
			return c, true
		}
	}
	return doctorCheck{}, false
}

func TestCheckConfigHealthReportsIssuesWithoutFix(t *testing.T) {
	workspace := t.TempDir()
	f := false
	cfg := config.DefaultConfig()
	cfg.Routing.Enabled = true
	cfg.Routing.UnmappedBehavior = config.RoutingUnmappedBehaviorDefault
	cfg.Routing.Mappings = []config.RoutingMapping{
		{
			Channel:        "discord",
			ChatID:         "chan-1",
			Workspace:      workspace,
			AllowedSenders: config.FlexibleStringSlice{"u2", "u1"},
			RequireMention: &f,
		},
	}
	cfg.Channels.Discord.Enabled = true
	cfg.Channels.Discord.AllowFrom = config.FlexibleStringSlice{}

	checks := checkConfigHealth(cfg, filepath.Join(t.TempDir(), "config.json"), doctorOptions{Fix: false})

	if _, ok := doctorCheckByName(checks, "config.health.fix"); ok {
		t.Fatalf("did not expect config.health.fix summary when --fix is false")
	}

	requireMention, ok := doctorCheckByName(checks, "config.health.routing.require_mention")
	if !ok || requireMention.Status != doctorWarn {
		t.Fatalf("require_mention check = %#v, want warn", requireMention)
	}

	unmapped, ok := doctorCheckByName(checks, "config.health.routing.unmapped_behavior")
	if !ok || unmapped.Status != doctorWarn {
		t.Fatalf("unmapped_behavior check = %#v, want warn", unmapped)
	}

	allow, ok := doctorCheckByName(checks, "config.health.discord.allow_from")
	if !ok || allow.Status != doctorWarn {
		t.Fatalf("discord.allow_from check = %#v, want warn", allow)
	}
}

func TestCheckConfigHealthAppliesFixesWithDoctorFix(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	workspace := filepath.Join(home, "ws")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}

	configPath := filepath.Join(home, ".picoclaw", "config.json")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	f := false
	cfg := config.DefaultConfig()
	cfg.Routing.Enabled = true
	cfg.Routing.UnmappedBehavior = config.RoutingUnmappedBehaviorDefault
	cfg.Routing.Mappings = []config.RoutingMapping{
		{
			Channel:        "discord",
			ChatID:         "chan-1",
			Workspace:      workspace,
			AllowedSenders: config.FlexibleStringSlice{"u2", "u1"},
			RequireMention: &f,
		},
	}
	cfg.Channels.Discord.Enabled = true
	cfg.Channels.Discord.AllowFrom = config.FlexibleStringSlice{}

	if err := config.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("seed config: %v", err)
	}

	checks := checkConfigHealth(cfg, configPath, doctorOptions{Fix: true})

	fixSummary, ok := doctorCheckByName(checks, "config.health.fix")
	if !ok || (fixSummary.Status != doctorOK && fixSummary.Status != doctorWarn) {
		t.Fatalf("config.health.fix = %#v, want ok/warn", fixSummary)
	}

	requireMention, ok := doctorCheckByName(checks, "config.health.routing.require_mention")
	if !ok || requireMention.Status != doctorOK {
		t.Fatalf("require_mention check = %#v, want ok", requireMention)
	}

	unmapped, ok := doctorCheckByName(checks, "config.health.routing.unmapped_behavior")
	if !ok || unmapped.Status != doctorOK {
		t.Fatalf("unmapped_behavior check = %#v, want ok", unmapped)
	}

	allow, ok := doctorCheckByName(checks, "config.health.discord.allow_from")
	if !ok || allow.Status != doctorOK {
		t.Fatalf("discord.allow_from check = %#v, want ok", allow)
	}

	if cfg.Routing.Mappings[0].RequireMention == nil || !*cfg.Routing.Mappings[0].RequireMention {
		t.Fatalf("require_mention not fixed in-memory: %#v", cfg.Routing.Mappings[0])
	}
	if cfg.Routing.UnmappedBehavior != config.RoutingUnmappedBehaviorMentionOnly {
		t.Fatalf("unmapped_behavior=%q want %q", cfg.Routing.UnmappedBehavior, config.RoutingUnmappedBehaviorMentionOnly)
	}
	if len(cfg.Channels.Discord.AllowFrom) != 2 {
		t.Fatalf("discord allow_from len=%d want 2 (%#v)", len(cfg.Channels.Discord.AllowFrom), cfg.Channels.Discord.AllowFrom)
	}

	reloaded, err := config.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if reloaded.Routing.Mappings[0].RequireMention == nil || !*reloaded.Routing.Mappings[0].RequireMention {
		t.Fatalf("require_mention not persisted: %#v", reloaded.Routing.Mappings[0])
	}
	if reloaded.Routing.UnmappedBehavior != config.RoutingUnmappedBehaviorMentionOnly {
		t.Fatalf("persisted unmapped_behavior=%q want %q", reloaded.Routing.UnmappedBehavior, config.RoutingUnmappedBehaviorMentionOnly)
	}
	if len(reloaded.Channels.Discord.AllowFrom) != 2 {
		t.Fatalf("persisted allow_from len=%d want 2", len(reloaded.Channels.Discord.AllowFrom))
	}

	if _, err := os.Stat(filepath.Join(filepath.Dir(configPath), "routing.reload")); err != nil {
		t.Fatalf("routing.reload missing: %v", err)
	}
}
