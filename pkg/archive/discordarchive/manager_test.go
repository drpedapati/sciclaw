package discordarchive

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/session"
)

func TestArchiveAndRecallRoundTrip(t *testing.T) {
	workspace := t.TempDir()
	sm := session.NewSessionManager(filepath.Join(workspace, "sessions"))

	key := "discord:12345"
	for i := 0; i < 20; i++ {
		sm.AddMessage(key, "user", "discuss alpha design and token pressure")
		sm.AddMessage(key, "assistant", "alpha response with implementation details")
	}
	if err := sm.Save(key); err != nil {
		t.Fatalf("save session: %v", err)
	}

	cfg := config.DiscordArchiveConfig{
		Enabled:            true,
		AutoArchive:        true,
		MaxSessionTokens:   50,
		MaxSessionMessages: 8,
		KeepUserPairs:      3,
		MinTailMessages:    4,
		RecallTopK:         5,
		RecallMaxChars:     2000,
		RecallMinScore:     0.2,
	}
	mgr := NewManager(workspace, sm, cfg)

	result, err := mgr.MaybeArchiveSession(key)
	if err != nil {
		t.Fatalf("MaybeArchiveSession error: %v", err)
	}
	if result == nil {
		t.Fatal("expected archive result, got nil")
	}
	if result.ArchivedMessages == 0 {
		t.Fatal("expected archived messages > 0")
	}
	if result.KeptMessages == 0 {
		t.Fatal("expected kept messages > 0")
	}
	if result.TokensAfter >= result.TokensBefore {
		t.Fatalf("expected token reduction, before=%d after=%d", result.TokensBefore, result.TokensAfter)
	}
	if _, err := os.Stat(result.ArchivePath); err != nil {
		t.Fatalf("expected archive file at %s: %v", result.ArchivePath, err)
	}

	history := sm.GetHistory(key)
	if len(history) == 0 {
		t.Fatal("expected non-empty trimmed history")
	}
	if len(history) >= 40 {
		t.Fatalf("expected trimmed history < 40, got %d", len(history))
	}

	hits := mgr.Recall("alpha token", key, 3, 600)
	if len(hits) == 0 {
		t.Fatal("expected recall hits")
	}
	if hits[0].Score <= 0 {
		t.Fatalf("expected positive recall score, got %d", hits[0].Score)
	}
	if !strings.Contains(strings.ToLower(hits[0].Text), "alpha") {
		t.Fatalf("expected hit text to mention alpha, got %q", hits[0].Text)
	}
}

func TestListDiscordSessionsOverLimit(t *testing.T) {
	workspace := t.TempDir()
	sm := session.NewSessionManager(filepath.Join(workspace, "sessions"))

	overKey := "discord:over"
	for i := 0; i < 10; i++ {
		sm.AddMessage(overKey, "user", strings.Repeat("x", 80))
	}
	sm.AddMessage("discord:small", "user", "tiny")
	sm.AddMessage("telegram:small", "user", "tiny")

	cfg := config.DiscordArchiveConfig{
		MaxSessionTokens:   30,
		MaxSessionMessages: 8,
		KeepUserPairs:      2,
		MinTailMessages:    2,
		RecallTopK:         3,
		RecallMaxChars:     1000,
	}
	mgr := NewManager(workspace, sm, cfg)

	all := mgr.ListDiscordSessions(false)
	if len(all) != 2 {
		t.Fatalf("expected 2 discord sessions, got %d", len(all))
	}
	overOnly := mgr.ListDiscordSessions(true)
	if len(overOnly) != 1 {
		t.Fatalf("expected 1 over-limit session, got %d", len(overOnly))
	}
	if overOnly[0].SessionKey != overKey {
		t.Fatalf("expected over-limit key %q, got %q", overKey, overOnly[0].SessionKey)
	}
}

func TestArchiveSessionDryRunDoesNotMutateHistory(t *testing.T) {
	workspace := t.TempDir()
	sm := session.NewSessionManager(filepath.Join(workspace, "sessions"))
	key := "discord:dryrun"
	for i := 0; i < 12; i++ {
		sm.AddMessage(key, "user", "dry run content")
		sm.AddMessage(key, "assistant", "dry run answer")
	}

	cfg := config.DiscordArchiveConfig{
		MaxSessionTokens:   10,
		MaxSessionMessages: 8,
		KeepUserPairs:      2,
		MinTailMessages:    4,
		RecallTopK:         3,
		RecallMaxChars:     1000,
	}
	mgr := NewManager(workspace, sm, cfg)
	before := sm.GetHistory(key)
	result, err := mgr.ArchiveSession(key, true)
	if err != nil {
		t.Fatalf("ArchiveSession dry-run error: %v", err)
	}
	if result == nil || !result.DryRun {
		t.Fatalf("expected dry-run result, got %#v", result)
	}
	after := sm.GetHistory(key)
	if len(before) != len(after) {
		t.Fatalf("dry-run should not mutate history length: before=%d after=%d", len(before), len(after))
	}
}

func TestCalculateKeepStartContinuityFloor(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "u2"},
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "u3"},
		{Role: "assistant", Content: "a3"},
	}
	got := calculateKeepStart(msgs, 1, 4)
	if got != 2 {
		t.Fatalf("keepStart=%d, want 2", got)
	}
}
