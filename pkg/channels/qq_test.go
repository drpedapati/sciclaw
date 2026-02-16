package channels

import (
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
)

func newTestQQChannel(t *testing.T) *QQChannel {
	t.Helper()
	ch, err := NewQQChannel(config.QQConfig{}, bus.NewMessageBus())
	if err != nil {
		t.Fatalf("new qq channel: %v", err)
	}
	return ch
}

func TestQQIsDuplicate_WithTTL(t *testing.T) {
	ch := newTestQQChannel(t)
	now := time.Date(2026, 2, 16, 0, 0, 0, 0, time.UTC)
	ch.nowFn = func() time.Time { return now }
	ch.dedupTTL = 1 * time.Minute

	if ch.isDuplicate("m1") {
		t.Fatalf("first message should not be duplicate")
	}
	if !ch.isDuplicate("m1") {
		t.Fatalf("second message within ttl should be duplicate")
	}

	now = now.Add(2 * time.Minute)
	if ch.isDuplicate("m1") {
		t.Fatalf("message after ttl should not be duplicate")
	}
}

func TestQQIsDuplicate_DeterministicBoundedEviction(t *testing.T) {
	ch := newTestQQChannel(t)
	now := time.Date(2026, 2, 16, 0, 0, 0, 0, time.UTC)
	ch.nowFn = func() time.Time { return now }
	ch.dedupTTL = 1 * time.Hour
	ch.dedupMax = 3

	if ch.isDuplicate("a") || ch.isDuplicate("b") || ch.isDuplicate("c") {
		t.Fatalf("first inserts should not be duplicates")
	}
	if len(ch.processedIDs) != 3 {
		t.Fatalf("expected map size 3, got %d", len(ch.processedIDs))
	}

	// Insert d, should evict oldest valid entry (a).
	if ch.isDuplicate("d") {
		t.Fatalf("new id d should not be duplicate")
	}
	if len(ch.processedIDs) != 3 {
		t.Fatalf("expected bounded map size 3 after eviction, got %d", len(ch.processedIDs))
	}
	if _, ok := ch.processedIDs["a"]; ok {
		t.Fatalf("expected oldest id a to be evicted")
	}
	if _, ok := ch.processedIDs["b"]; !ok {
		t.Fatalf("expected b to remain")
	}
	if _, ok := ch.processedIDs["c"]; !ok {
		t.Fatalf("expected c to remain")
	}
	if _, ok := ch.processedIDs["d"]; !ok {
		t.Fatalf("expected d to be present")
	}
}
