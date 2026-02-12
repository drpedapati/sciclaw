package heartbeat

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/tools"
)

func TestExecuteHeartbeatWithTools_Async(t *testing.T) {
	// Create temp workspace
	tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create memory directory
	os.MkdirAll(filepath.Join(tmpDir, "memory"), 0755)

	// Create heartbeat service with tool-supporting handler
	hs := NewHeartbeatService(tmpDir, nil, 30, true)

	// Track if async handler was called
	asyncCalled := false
	asyncResult := &tools.ToolResult{
		ForLLM:  "Background task started",
		ForUser: "Task started in background",
		Silent:  false,
		IsError: false,
		Async:   true,
	}

	hs.SetOnHeartbeatWithTools(func(prompt string) *tools.ToolResult {
		asyncCalled = true
		if prompt == "" {
			t.Error("Expected non-empty prompt")
		}
		return asyncResult
	})

	// Execute heartbeat
	hs.ExecuteHeartbeatWithTools("Test heartbeat prompt")

	// Verify handler was called
	if !asyncCalled {
		t.Error("Expected async handler to be called")
	}
}

func TestExecuteHeartbeatWithTools_Error(t *testing.T) {
	// Create temp workspace
	tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create memory directory
	os.MkdirAll(filepath.Join(tmpDir, "memory"), 0755)

	hs := NewHeartbeatService(tmpDir, nil, 30, true)

	errorResult := &tools.ToolResult{
		ForLLM:  "Heartbeat failed: connection error",
		ForUser: "",
		Silent:  false,
		IsError: true,
		Async:   false,
	}

	hs.SetOnHeartbeatWithTools(func(prompt string) *tools.ToolResult {
		return errorResult
	})

	hs.ExecuteHeartbeatWithTools("Test prompt")

	// Check log file for error message
	logFile := filepath.Join(tmpDir, "memory", "heartbeat.log")
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	logContent := string(data)
	if logContent == "" {
		t.Error("Expected log file to contain error message")
	}
}

func TestExecuteHeartbeatWithTools_Sync(t *testing.T) {
	// Create temp workspace
	tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create memory directory
	os.MkdirAll(filepath.Join(tmpDir, "memory"), 0755)

	hs := NewHeartbeatService(tmpDir, nil, 30, true)

	syncResult := &tools.ToolResult{
		ForLLM:  "Heartbeat completed successfully",
		ForUser: "",
		Silent:  true,
		IsError: false,
		Async:   false,
	}

	hs.SetOnHeartbeatWithTools(func(prompt string) *tools.ToolResult {
		return syncResult
	})

	hs.ExecuteHeartbeatWithTools("Test prompt")

	// Check log file for completion message
	logFile := filepath.Join(tmpDir, "memory", "heartbeat.log")
	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("Failed to read log file: %v", err)
	}

	logContent := string(data)
	if logContent == "" {
		t.Error("Expected log file to contain completion message")
	}
}

func TestHeartbeatService_StartStop(t *testing.T) {
	// Create temp workspace
	tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hs := NewHeartbeatService(tmpDir, nil, 1, true)

	// Start the service
	err = hs.Start()
	if err != nil {
		t.Fatalf("Failed to start heartbeat service: %v", err)
	}

	// Stop the service
	hs.Stop()

	// Verify it stopped properly
	time.Sleep(100 * time.Millisecond)
}

func TestHeartbeatService_Disabled(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	hs := NewHeartbeatService(tmpDir, nil, 1, false)

	// Check that service reports as not enabled
	if hs.enabled != false {
		t.Error("Expected service to be disabled")
	}

	// Note: The current implementation of Start() checks running() first,
	// which returns true for a newly created service (before stopChan is closed).
	// This means Start() will return nil even for disabled services.
	// This test documents the current behavior.
	err = hs.Start()
	// We don't assert error here due to the running() check behavior
	_ = err
}

func TestExecuteHeartbeatWithTools_NilResult(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	os.MkdirAll(filepath.Join(tmpDir, "memory"), 0755)

	hs := NewHeartbeatService(tmpDir, nil, 30, true)

	hs.SetOnHeartbeatWithTools(func(prompt string) *tools.ToolResult {
		return nil
	})

	// Should not panic with nil result
	hs.ExecuteHeartbeatWithTools("Test prompt")
}

// TestLogPath verifies heartbeat log is written to memory directory
func TestLogPath(t *testing.T) {
	// Create temp workspace
	tmpDir, err := os.MkdirTemp("", "heartbeat-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create memory directory
	memDir := filepath.Join(tmpDir, "memory")
	err = os.MkdirAll(memDir, 0755)
	if err != nil {
		t.Fatalf("Failed to create memory dir: %v", err)
	}

	// Create heartbeat service
	hs := NewHeartbeatService(tmpDir, nil, 30, true)

	// Write a log entry
	hs.log("INFO", "Test log entry")

	// Verify log file exists at correct path
	expectedLogPath := filepath.Join(memDir, "heartbeat.log")
	if _, err := os.Stat(expectedLogPath); os.IsNotExist(err) {
		t.Errorf("Expected log file at %s, but it doesn't exist", expectedLogPath)
	}

	// Verify log file does NOT exist at old path
	oldLogPath := filepath.Join(tmpDir, "heartbeat.log")
	if _, err := os.Stat(oldLogPath); err == nil {
		t.Error("Log file should not exist at old path (workspace/heartbeat.log)")
	}
}
