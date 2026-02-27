package session

import (
	"testing"

	"github.com/sipeed/picoclaw/pkg/providers"
)

func TestSessionManagerListKeysSorted(t *testing.T) {
	sm := NewSessionManager("")
	sm.AddMessage("discord:z", "user", "z")
	sm.AddMessage("discord:a", "user", "a")
	sm.AddMessage("discord:m", "user", "m")

	keys := sm.ListKeys()
	if len(keys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(keys))
	}
	if keys[0] != "discord:a" || keys[1] != "discord:m" || keys[2] != "discord:z" {
		t.Fatalf("unexpected key order: %#v", keys)
	}
}

func TestSessionManagerSnapshotDeepCopy(t *testing.T) {
	sm := NewSessionManager("")
	sm.AddMessage("discord:test", "user", "hello")
	sm.AddMessage("discord:test", "assistant", "world")

	snap, ok := sm.Snapshot("discord:test")
	if !ok {
		t.Fatal("expected snapshot to exist")
	}
	if len(snap.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(snap.Messages))
	}

	// Mutate snapshot and ensure manager state is unchanged.
	snap.Messages[0].Content = "mutated"
	history := sm.GetHistory("discord:test")
	if history[0].Content != "hello" {
		t.Fatalf("manager history should remain unchanged, got %q", history[0].Content)
	}
}

func TestSessionManagerReplaceHistory(t *testing.T) {
	sm := NewSessionManager("")
	sm.AddMessage("discord:test", "user", "old")

	newHistory := []providers.Message{
		{Role: "user", Content: "u1"},
		{Role: "assistant", Content: "a1"},
	}
	sm.ReplaceHistory("discord:test", newHistory)

	history := sm.GetHistory("discord:test")
	if len(history) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(history))
	}
	if history[0].Content != "u1" || history[1].Content != "a1" {
		t.Fatalf("unexpected replaced history: %#v", history)
	}

	// Mutating caller slice should not mutate stored history.
	newHistory[0].Content = "changed"
	history = sm.GetHistory("discord:test")
	if history[0].Content != "u1" {
		t.Fatalf("stored history mutated by caller slice change: %#v", history)
	}
}
