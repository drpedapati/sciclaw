package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestJSONLAuditSinkWrite(t *testing.T) {
	ws := t.TempDir()
	sink, err := NewJSONLAuditSink(ws)
	if err != nil {
		t.Fatalf("NewJSONLAuditSink: %v", err)
	}
	entry := AuditEntry{TurnID: "turn-1", Event: EventBeforeTurn, Handler: "h", Status: StatusOK, Timestamp: time.Now()}
	if err := sink.Write(entry); err != nil {
		t.Fatalf("Write: %v", err)
	}

	auditPath := filepath.Join(ws, "hooks", "hook-events.jsonl")
	deadline := time.Now().Add(2 * time.Second)
	for {
		data, err := os.ReadFile(auditPath)
		if err == nil && strings.Contains(string(data), "\"turn_id\":\"turn-1\"") {
			return
		}
		if time.Now().After(deadline) {
			if err != nil {
				t.Fatalf("read audit file after wait: %v", err)
			}
			t.Fatalf("audit content missing turn_id after wait: %s", string(data))
		}
		time.Sleep(10 * time.Millisecond)
	}
}
