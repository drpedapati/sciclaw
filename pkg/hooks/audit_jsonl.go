package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// JSONLAuditSink appends hook entries as JSONL.
type JSONLAuditSink struct {
	mu   sync.Mutex
	path string
}

func NewJSONLAuditSink(workspace string) (*JSONLAuditSink, error) {
	return NewJSONLAuditSinkAt(filepath.Join(workspace, "hooks", "hook-events.jsonl"))
}

func NewJSONLAuditSinkAt(path string) (*JSONLAuditSink, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create hooks audit dir: %w", err)
	}
	return &JSONLAuditSink{path: path}, nil
}

func (s *JSONLAuditSink) Path() string {
	return s.path
}

func (s *JSONLAuditSink) Write(entry AuditEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}
