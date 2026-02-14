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
	data, err := os.ReadFile(filepath.Join(ws, "hooks", "hook-events.jsonl"))
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	if !strings.Contains(string(data), "\"turn_id\":\"turn-1\"") {
		t.Fatalf("audit content missing turn_id: %s", string(data))
	}
}
