package irl

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type commandStore struct {
	baseDir string
	nowFn   func() time.Time
}

func newCommandStore(workspace string) *commandStore {
	return &commandStore{
		baseDir: filepath.Join(workspace, "irl", "commands"),
		nowFn:   time.Now,
	}
}

func (s *commandStore) withNow(nowFn func() time.Time) *commandStore {
	if nowFn != nil {
		s.nowFn = nowFn
	}
	return s
}

func (s *commandStore) write(record *CommandRecord) (string, error) {
	now := s.nowFn().UTC()
	dayDir := filepath.Join(
		s.baseDir,
		now.Format("2006"),
		now.Format("01"),
		now.Format("02"),
	)
	if err := os.MkdirAll(dayDir, 0755); err != nil {
		return "", err
	}

	recordPath := filepath.Join(dayDir, record.EventID+".json")
	tmpPath := recordPath + ".tmp"

	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, recordPath); err != nil {
		return "", err
	}
	return recordPath, nil
}
