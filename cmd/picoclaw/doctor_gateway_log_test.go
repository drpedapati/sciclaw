package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckGatewayLogSkipsTelegramConflictWhenDisabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	logPath := filepath.Join(home, ".picoclaw", "gateway.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	content := `[Sat Feb 21 22:16:27 EST 2026] ERROR Getting updates: telego: getUpdates: api: 409 "Conflict: terminated by other getUpdates request; make sure that only one bot instance is running"`
	if err := os.WriteFile(logPath, []byte(content), 0644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	got := checkGatewayLog(false)
	if got.Status != doctorSkip {
		t.Fatalf("status = %q, want %q", got.Status, doctorSkip)
	}
	if got.Name != "gateway.log" {
		t.Fatalf("name = %q, want gateway.log", got.Name)
	}
}

func TestCheckGatewayLogFlagsConflictWhenTelegramEnabled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	logPath := filepath.Join(home, ".picoclaw", "gateway.log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	content := `[Sat Feb 21 22:16:27 EST 2026] ERROR Getting updates: telego: getUpdates: api: 409 "Conflict: terminated by other getUpdates request; make sure that only one bot instance is running"`
	if err := os.WriteFile(logPath, []byte(content), 0644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	got := checkGatewayLog(true)
	if got.Status != doctorErr {
		t.Fatalf("status = %q, want %q", got.Status, doctorErr)
	}
	if got.Name != "gateway.telegram" {
		t.Fatalf("name = %q, want gateway.telegram", got.Name)
	}
}

