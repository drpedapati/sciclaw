package cron

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSaveStore_FilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permission bits are not enforced on Windows")
	}

	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "cron", "jobs.json")

	cs := NewCronService(storePath, nil)

	_, err := cs.AddJob("test", CronSchedule{Kind: "every", EveryMS: int64Ptr(60000)}, "hello", false, "cli", "direct")
	if err != nil {
		t.Fatalf("AddJob failed: %v", err)
	}

	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}

	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("cron store has permission %04o, want 0600", perm)
	}
}

func TestLoadStore_EmptyFileStartsClean(t *testing.T) {
	tmpDir := t.TempDir()
	storePath := filepath.Join(tmpDir, "cron", "jobs.json")
	if err := os.MkdirAll(filepath.Dir(storePath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(storePath, []byte(""), 0o600); err != nil {
		t.Fatalf("write empty store: %v", err)
	}

	cs := NewCronService(storePath, nil)
	if err := cs.Start(); err != nil {
		t.Fatalf("Start() with empty store = %v", err)
	}
	defer cs.Stop()

	jobs := cs.ListJobs(false)
	if len(jobs) != 0 {
		t.Fatalf("jobs=%d want 0", len(jobs))
	}
}

func int64Ptr(v int64) *int64 {
	return &v
}
