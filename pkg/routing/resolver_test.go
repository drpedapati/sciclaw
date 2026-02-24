package routing

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
)

func TestResolve_Match(t *testing.T) {
	ws := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Routing.Enabled = true
	cfg.Routing.Mappings = []config.RoutingMapping{
		{
			Channel:        "discord",
			ChatID:         "123",
			Workspace:      ws,
			AllowedSenders: []string{"u1"},
			Label:          "team-a",
		},
	}

	resolver, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver error: %v", err)
	}

	d := resolver.Resolve(bus.InboundMessage{
		Channel:  "discord",
		ChatID:   "123",
		SenderID: "u1",
		Metadata: map[string]string{"is_mention": "true"},
	})

	if !d.Allowed {
		t.Fatalf("expected allowed decision, got %+v", d)
	}
	if d.Event != EventRouteMatch {
		t.Fatalf("unexpected event: %s", d.Event)
	}
	if d.Workspace != ws {
		t.Fatalf("workspace = %q, want %q", d.Workspace, ws)
	}
	if !strings.HasPrefix(d.SessionKey, "discord:123@") {
		t.Fatalf("unexpected session key: %q", d.SessionKey)
	}
}

func TestResolve_UnmappedBlock(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Routing.Enabled = true
	cfg.Routing.UnmappedBehavior = config.RoutingUnmappedBehaviorBlock

	resolver, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver error: %v", err)
	}

	d := resolver.Resolve(bus.InboundMessage{
		Channel:  "discord",
		ChatID:   "missing",
		SenderID: "u1",
	})
	if d.Allowed {
		t.Fatalf("expected blocked decision, got %+v", d)
	}
	if d.Event != EventRouteUnmapped {
		t.Fatalf("unexpected event: %s", d.Event)
	}
}

func TestResolve_UnmappedDefaultFallback(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Routing.Enabled = true
	cfg.Routing.UnmappedBehavior = config.RoutingUnmappedBehaviorDefault

	resolver, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver error: %v", err)
	}

	d := resolver.Resolve(bus.InboundMessage{
		Channel:  "discord",
		ChatID:   "missing",
		SenderID: "u1",
	})
	if !d.Allowed {
		t.Fatalf("expected fallback decision to be allowed, got %+v", d)
	}
	if d.Event != EventRouteUnmapped {
		t.Fatalf("unexpected event: %s", d.Event)
	}
	if d.Workspace != cfg.WorkspacePath() {
		t.Fatalf("workspace = %q, want %q", d.Workspace, cfg.WorkspacePath())
	}
}

func TestResolve_DenySender(t *testing.T) {
	ws := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Routing.Enabled = true
	cfg.Routing.Mappings = []config.RoutingMapping{
		{
			Channel:        "discord",
			ChatID:         "123",
			Workspace:      ws,
			AllowedSenders: []string{"u1"},
		},
	}

	resolver, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver error: %v", err)
	}
	d := resolver.Resolve(bus.InboundMessage{
		Channel:  "discord",
		ChatID:   "123",
		SenderID: "u2",
	})
	if d.Allowed {
		t.Fatalf("expected denied decision, got %+v", d)
	}
	if d.Event != EventRouteDeny {
		t.Fatalf("unexpected event: %s", d.Event)
	}
}

func TestResolve_InvalidWorkspace(t *testing.T) {
	root := t.TempDir()
	ws := filepath.Join(root, "gone")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Routing.Enabled = true
	cfg.Routing.Mappings = []config.RoutingMapping{
		{
			Channel:        "discord",
			ChatID:         "123",
			Workspace:      ws,
			AllowedSenders: []string{"u1"},
		},
	}

	resolver, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver error: %v", err)
	}

	if err := os.RemoveAll(ws); err != nil {
		t.Fatalf("remove workspace: %v", err)
	}

	d := resolver.Resolve(bus.InboundMessage{
		Channel:  "discord",
		ChatID:   "123",
		SenderID: "u1",
		Metadata: map[string]string{"is_mention": "true"},
	})
	if d.Allowed {
		t.Fatalf("expected invalid mapping to block, got %+v", d)
	}
	if d.Event != EventRouteInvalid {
		t.Fatalf("unexpected event: %s", d.Event)
	}
}

func TestResolve_SystemMessageUsesOriginMapping(t *testing.T) {
	ws := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Routing.Enabled = true
	cfg.Routing.Mappings = []config.RoutingMapping{
		{
			Channel:        "discord",
			ChatID:         "123",
			Workspace:      ws,
			AllowedSenders: []string{"u1"},
		},
	}

	resolver, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver error: %v", err)
	}
	d := resolver.Resolve(bus.InboundMessage{
		Channel:  "system",
		ChatID:   "discord:123",
		SenderID: "subagent:abc",
	})
	if !d.Allowed {
		t.Fatalf("expected system message routing to succeed, got %+v", d)
	}
	if d.Workspace != ws {
		t.Fatalf("workspace = %q, want %q", d.Workspace, ws)
	}
}

func TestResolve_MentionRequired_SkipsWithoutMention(t *testing.T) {
	ws := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Routing.Enabled = true
	cfg.Routing.Mappings = []config.RoutingMapping{
		{
			Channel:        "discord",
			ChatID:         "123",
			Workspace:      ws,
			AllowedSenders: []string{"u1"},
		},
	}

	resolver, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver error: %v", err)
	}

	d := resolver.Resolve(bus.InboundMessage{
		Channel:  "discord",
		ChatID:   "123",
		SenderID: "u1",
		Metadata: map[string]string{"is_mention": "false", "is_dm": "false"},
	})
	if d.Allowed {
		t.Fatalf("expected mention skip, got %+v", d)
	}
	if d.Event != EventRouteMentionSkip {
		t.Fatalf("expected event %s, got %s", EventRouteMentionSkip, d.Event)
	}
}

func TestResolve_MentionRequired_AllowsWithMention(t *testing.T) {
	ws := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Routing.Enabled = true
	cfg.Routing.Mappings = []config.RoutingMapping{
		{
			Channel:        "discord",
			ChatID:         "123",
			Workspace:      ws,
			AllowedSenders: []string{"u1"},
		},
	}

	resolver, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver error: %v", err)
	}

	d := resolver.Resolve(bus.InboundMessage{
		Channel:  "discord",
		ChatID:   "123",
		SenderID: "u1",
		Metadata: map[string]string{"is_mention": "true", "is_dm": "false"},
	})
	if !d.Allowed {
		t.Fatalf("expected allowed with mention, got %+v", d)
	}
}

func TestResolve_MentionRequired_DMBypass(t *testing.T) {
	ws := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Routing.Enabled = true
	cfg.Routing.Mappings = []config.RoutingMapping{
		{
			Channel:        "discord",
			ChatID:         "123",
			Workspace:      ws,
			AllowedSenders: []string{"u1"},
		},
	}

	resolver, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver error: %v", err)
	}

	d := resolver.Resolve(bus.InboundMessage{
		Channel:  "discord",
		ChatID:   "123",
		SenderID: "u1",
		Metadata: map[string]string{"is_mention": "false", "is_dm": "true"},
	})
	if !d.Allowed {
		t.Fatalf("expected DM bypass, got %+v", d)
	}
}

func TestResolve_NoMentionOverride(t *testing.T) {
	ws := t.TempDir()
	noMention := false
	cfg := config.DefaultConfig()
	cfg.Routing.Enabled = true
	cfg.Routing.Mappings = []config.RoutingMapping{
		{
			Channel:        "discord",
			ChatID:         "123",
			Workspace:      ws,
			AllowedSenders: []string{"u1"},
			RequireMention: &noMention,
		},
	}

	resolver, err := NewResolver(cfg)
	if err != nil {
		t.Fatalf("NewResolver error: %v", err)
	}

	d := resolver.Resolve(bus.InboundMessage{
		Channel:  "discord",
		ChatID:   "123",
		SenderID: "u1",
		Metadata: map[string]string{"is_mention": "false", "is_dm": "false"},
	})
	if !d.Allowed {
		t.Fatalf("expected --no-mention override to allow, got %+v", d)
	}
}

func TestResolve_SessionNamespaceChangesWhenWorkspaceChanges(t *testing.T) {
	ws1 := t.TempDir()
	ws2 := t.TempDir()

	cfg1 := config.DefaultConfig()
	cfg1.Routing.Enabled = true
	cfg1.Routing.Mappings = []config.RoutingMapping{
		{
			Channel:        "telegram",
			ChatID:         "555",
			Workspace:      ws1,
			AllowedSenders: []string{"u1"},
		},
	}
	r1, err := NewResolver(cfg1)
	if err != nil {
		t.Fatalf("NewResolver cfg1 error: %v", err)
	}

	cfg2 := config.DefaultConfig()
	cfg2.Routing.Enabled = true
	cfg2.Routing.Mappings = []config.RoutingMapping{
		{
			Channel:        "telegram",
			ChatID:         "555",
			Workspace:      ws2,
			AllowedSenders: []string{"u1"},
		},
	}
	r2, err := NewResolver(cfg2)
	if err != nil {
		t.Fatalf("NewResolver cfg2 error: %v", err)
	}

	msg := bus.InboundMessage{Channel: "telegram", ChatID: "555", SenderID: "u1", Metadata: map[string]string{"is_mention": "true"}}
	d1 := r1.Resolve(msg)
	d2 := r2.Resolve(msg)
	if d1.SessionKey == d2.SessionKey {
		t.Fatalf("session key should change when workspace changes: %q", d1.SessionKey)
	}
}
