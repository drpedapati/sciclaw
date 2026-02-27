package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// Buffer audit writes so hook dispatch never blocks on slow filesystems.
	auditQueueSize = 256
)

// JSONLAuditSink appends hook entries as JSONL.
type JSONLAuditSink struct {
	path  string
	queue chan []byte
}

func NewJSONLAuditSink(workspace string) (*JSONLAuditSink, error) {
	return NewJSONLAuditSinkAt(filepath.Join(workspace, "hooks", "hook-events.jsonl"))
}

func NewJSONLAuditSinkAt(path string) (*JSONLAuditSink, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create hooks audit dir: %w", err)
	}
	sink := &JSONLAuditSink{
		path:  path,
		queue: make(chan []byte, auditQueueSize),
	}
	go sink.writeLoop()
	return sink, nil
}

func (s *JSONLAuditSink) Path() string {
	return s.path
}

func (s *JSONLAuditSink) Write(entry AuditEntry) error {
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}

	line := append(b, '\n')
	select {
	case s.queue <- line:
		return nil
	default:
	}

	// Queue full: drop oldest pending line so current hook event can proceed.
	select {
	case <-s.queue:
	default:
	}
	select {
	case s.queue <- line:
	default:
	}
	return nil
}

func (s *JSONLAuditSink) writeLoop() {
	for line := range s.queue {
		_ = s.appendLine(line)
	}
}

func (s *JSONLAuditSink) appendLine(line []byte) error {
	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	if _, err := f.Write(line); err != nil {
		return err
	}
	return nil
}
