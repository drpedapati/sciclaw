package state

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func newTestManager(t *testing.T, workspace string) *Manager {
	t.Helper()
	sm := NewManager(workspace)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = sm.Close(ctx)
	})
	return sm
}

func TestAtomicSave(t *testing.T) {
	// Create temp workspace
	tmpDir := t.TempDir()
	var err error

	sm := newTestManager(t, tmpDir)

	// Test SetLastChannel
	err = sm.SetLastChannel("test-channel")
	if err != nil {
		t.Fatalf("SetLastChannel failed: %v", err)
	}

	// Verify the channel was saved
	lastChannel := sm.GetLastChannel()
	if lastChannel != "test-channel" {
		t.Errorf("Expected channel 'test-channel', got '%s'", lastChannel)
	}

	// Verify timestamp was updated
	if sm.GetTimestamp().IsZero() {
		t.Error("Expected timestamp to be updated")
	}

	// Verify state file exists
	stateFile := filepath.Join(tmpDir, "state", "state.json")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sm.Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("expected state file to exist: %v", err)
	}

	// Create a new manager to verify persistence.
	sm2 := NewManager(tmpDir)
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		_ = sm2.Close(closeCtx)
	}()
	if sm2.GetLastChannel() != "test-channel" {
		t.Fatalf("expected persistent channel 'test-channel', got %q", sm2.GetLastChannel())
	}
}

func TestSetLastChatID(t *testing.T) {
	tmpDir := t.TempDir()
	var err error

	sm := newTestManager(t, tmpDir)

	// Test SetLastChatID
	err = sm.SetLastChatID("test-chat-id")
	if err != nil {
		t.Fatalf("SetLastChatID failed: %v", err)
	}

	// Verify the chat ID was saved
	lastChatID := sm.GetLastChatID()
	if lastChatID != "test-chat-id" {
		t.Errorf("Expected chat ID 'test-chat-id', got '%s'", lastChatID)
	}

	// Verify timestamp was updated
	if sm.GetTimestamp().IsZero() {
		t.Error("Expected timestamp to be updated")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sm.Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Create a new manager to verify persistence.
	sm2 := NewManager(tmpDir)
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		_ = sm2.Close(closeCtx)
	}()
	if sm2.GetLastChatID() != "test-chat-id" {
		t.Fatalf("expected persistent chat ID 'test-chat-id', got %q", sm2.GetLastChatID())
	}
}

func TestAtomicity_NoCorruptionOnInterrupt(t *testing.T) {
	tmpDir := t.TempDir()
	var err error

	sm := newTestManager(t, tmpDir)

	// Write initial state
	err = sm.SetLastChannel("initial-channel")
	if err != nil {
		t.Fatalf("SetLastChannel failed: %v", err)
	}

	// Simulate a crash scenario by manually creating a corrupted temp file
	tempFile := filepath.Join(tmpDir, "state", "state.json.tmp")
	err = os.WriteFile(tempFile, []byte("corrupted data"), 0644)
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}

	// Verify that the original state is still intact
	lastChannel := sm.GetLastChannel()
	if lastChannel != "initial-channel" {
		t.Errorf("Expected channel 'initial-channel' after corrupted temp file, got '%s'", lastChannel)
	}

	// Clean up the temp file manually
	os.Remove(tempFile)

	// Now do a proper save
	err = sm.SetLastChannel("new-channel")
	if err != nil {
		t.Fatalf("SetLastChannel failed: %v", err)
	}

	// Verify the new state was saved
	if sm.GetLastChannel() != "new-channel" {
		t.Errorf("Expected channel 'new-channel', got '%s'", sm.GetLastChannel())
	}
}

func TestConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	var err error

	sm := newTestManager(t, tmpDir)

	// Test concurrent writes
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			channel := fmt.Sprintf("channel-%d", idx)
			sm.SetLastChannel(channel)
			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify the final state is consistent
	lastChannel := sm.GetLastChannel()
	if lastChannel == "" {
		t.Error("Expected non-empty channel after concurrent writes")
	}

	// Flush to make persistence deterministic before reading the file.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sm.Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Verify state file is valid JSON.
	stateFile := filepath.Join(tmpDir, "state", "state.json")
	if _, err := os.Stat(stateFile); err != nil {
		t.Fatalf("expected state file to exist after concurrent writes: %v", err)
	}

	data, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("Failed to read state file: %v", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		t.Errorf("State file contains invalid JSON: %v", err)
	}
}

func TestNewManager_ExistingState(t *testing.T) {
	tmpDir := t.TempDir()

	// Create initial state
	sm1 := newTestManager(t, tmpDir)
	sm1.SetLastChannel("existing-channel")
	sm1.SetLastChatID("existing-chat-id")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sm1.Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	// Create new manager with same workspace.
	sm2 := NewManager(tmpDir)
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		_ = sm2.Close(closeCtx)
	}()
	if sm2.GetLastChannel() != "existing-channel" || sm2.GetLastChatID() != "existing-chat-id" {
		t.Fatalf("expected existing state to be loaded, got channel=%q chat=%q", sm2.GetLastChannel(), sm2.GetLastChatID())
	}
}

func TestNewManager_EmptyWorkspace(t *testing.T) {
	tmpDir := t.TempDir()

	sm := newTestManager(t, tmpDir)

	// Verify default state
	if sm.GetLastChannel() != "" {
		t.Errorf("Expected empty channel, got '%s'", sm.GetLastChannel())
	}

	if sm.GetLastChatID() != "" {
		t.Errorf("Expected empty chat ID, got '%s'", sm.GetLastChatID())
	}

	if !sm.GetTimestamp().IsZero() {
		t.Error("Expected zero timestamp for new state")
	}
}

func TestNewManager_LoadsLegacyStateFile(t *testing.T) {
	tmpDir := t.TempDir()
	var err error

	legacy := State{
		LastChannel: "legacy-channel",
		LastChatID:  "legacy-chat",
		Timestamp:   time.Now().UTC(),
	}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatalf("marshal legacy state: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpDir, "state.json"), data, 0o644); err != nil {
		t.Fatalf("write legacy state: %v", err)
	}

	sm := newTestManager(t, tmpDir)
	if sm.GetLastChannel() != "legacy-channel" {
		t.Fatalf("expected legacy channel, got %q", sm.GetLastChannel())
	}
	if sm.GetLastChatID() != "legacy-chat" {
		t.Fatalf("expected legacy chat id, got %q", sm.GetLastChatID())
	}
}

func TestNewManager_BootstrapTimeoutDoesNotBlock(t *testing.T) {
	tmpDir := t.TempDir()

	prevRead := stateReadFile
	prevTimeout := stateBootstrapTimeout
	defer func() {
		stateReadFile = prevRead
		stateBootstrapTimeout = prevTimeout
	}()

	block := make(chan struct{})
	stateReadFile = func(string) ([]byte, error) {
		<-block
		return nil, os.ErrNotExist
	}
	stateBootstrapTimeout = 20 * time.Millisecond

	start := time.Now()
	sm := newTestManager(t, tmpDir)
	_ = sm
	elapsed := time.Since(start)
	if elapsed > 150*time.Millisecond {
		t.Fatalf("expected constructor to return quickly, took %v", elapsed)
	}

	close(block)
	time.Sleep(5 * time.Millisecond)
}

func TestFlushPersistsLatestSnapshot(t *testing.T) {
	tmpDir := t.TempDir()

	sm := newTestManager(t, tmpDir)
	if err := sm.SetLastChannel("flush-channel"); err != nil {
		t.Fatalf("SetLastChannel failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sm.Flush(ctx); err != nil {
		t.Fatalf("Flush failed: %v", err)
	}

	sm2 := NewManager(tmpDir)
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		_ = sm2.Close(closeCtx)
	}()
	if got := sm2.GetLastChannel(); got != "flush-channel" {
		t.Fatalf("expected flushed channel %q, got %q", "flush-channel", got)
	}
}

func TestCloseFlushesPendingState(t *testing.T) {
	tmpDir := t.TempDir()

	sm := NewManager(tmpDir)
	if err := sm.SetLastChatID("close-chat-id"); err != nil {
		t.Fatalf("SetLastChatID failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sm.Close(ctx); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	sm2 := NewManager(tmpDir)
	defer func() {
		closeCtx, closeCancel := context.WithTimeout(context.Background(), time.Second)
		defer closeCancel()
		_ = sm2.Close(closeCtx)
	}()
	if got := sm2.GetLastChatID(); got != "close-chat-id" {
		t.Fatalf("expected persisted chat id %q, got %q", "close-chat-id", got)
	}
}

func TestSetAfterCloseReturnsClosedError(t *testing.T) {
	tmpDir := t.TempDir()

	sm := NewManager(tmpDir)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sm.Close(ctx); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	if err := sm.SetLastChannel("blocked"); !errors.Is(err, errManagerClosed) {
		t.Fatalf("expected errManagerClosed from SetLastChannel, got %v", err)
	}
	if err := sm.SetLastChatID("blocked"); !errors.Is(err, errManagerClosed) {
		t.Fatalf("expected errManagerClosed from SetLastChatID, got %v", err)
	}
}

func TestFlushAfterCloseReturnsClosedError(t *testing.T) {
	tmpDir := t.TempDir()

	sm := NewManager(tmpDir)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := sm.Close(ctx); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	flushCtx, flushCancel := context.WithTimeout(context.Background(), time.Second)
	defer flushCancel()
	if err := sm.Flush(flushCtx); !errors.Is(err, errManagerClosed) {
		t.Fatalf("expected errManagerClosed from Flush after Close, got %v", err)
	}
}
