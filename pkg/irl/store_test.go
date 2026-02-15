package irl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCommandStoreWriteCreatesDatedPath(t *testing.T) {
	workspace := t.TempDir()
	store := newCommandStore(workspace).withNow(func() time.Time {
		return time.Date(2026, time.February, 15, 8, 0, 0, 0, time.UTC)
	})

	record := &CommandRecord{
		EventType: "irl_command",
		EventID:   "evt-123",
		Timestamp: "2026-02-15T08:00:00Z",
		Operation: "discover_projects",
		Command:   []string{"irl", "list", "--json"},
		CWD:       workspace,
		ExitCode:  0,
		Status:    StatusSuccess,
	}

	path, err := store.write(record)
	if err != nil {
		t.Fatalf("store.write returned error: %v", err)
	}

	wantSuffix := filepath.ToSlash(filepath.Join("irl", "commands", "2026", "02", "15", "evt-123.json"))
	if !strings.HasSuffix(filepath.ToSlash(path), wantSuffix) {
		t.Fatalf("unexpected store path: %s", path)
	}

	var stored CommandRecord
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read stored record: %v", err)
	}
	if err := json.Unmarshal(content, &stored); err != nil {
		t.Fatalf("unmarshal stored record: %v", err)
	}
	if stored.EventID != "evt-123" {
		t.Fatalf("stored event_id mismatch: got %s", stored.EventID)
	}
}
