// PicoClaw - Ultra-lightweight personal AI agent
// Inspired by and based on nanobot: https://github.com/HKUDS/nanobot
// License: MIT
//
// Copyright (c) 2026 PicoClaw contributors

package heartbeat

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/state"
)

const (
	minIntervalMinutes    = 5
	defaultIntervalMinutes = 30
	heartbeatOK            = "HEARTBEAT_OK"
)

// ToolResult represents a structured result from tool execution.
// This is a minimal local definition to avoid circular dependencies.
type ToolResult struct {
	ForLLM  string `json:"for_llm"`
	ForUser string `json:"for_user,omitempty"`
	Silent  bool   `json:"silent"`
	IsError bool   `json:"is_error"`
	Async   bool   `json:"async"`
	Err     error  `json:"-"`
}

// HeartbeatHandler is the function type for handling heartbeat with tool support.
// It returns a ToolResult that can indicate async operations.
type HeartbeatHandler func(prompt string) *ToolResult

// ChannelSender defines the interface for sending messages to channels.
// This is used to send heartbeat results back to the user.
type ChannelSender interface {
	SendToChannel(ctx context.Context, channelName, chatID, content string) error
}

// HeartbeatService manages periodic heartbeat checks
type HeartbeatService struct {
	workspace            string
	channelSender        ChannelSender
	stateManager         *state.Manager
	onHeartbeat          func(string) (string, error)
	onHeartbeatWithTools HeartbeatHandler
	interval             time.Duration
	enabled              bool
	mu                   sync.RWMutex
	started              bool
	stopChan             chan struct{}
}

// NewHeartbeatService creates a new heartbeat service
func NewHeartbeatService(workspace string, onHeartbeat func(string) (string, error), intervalMinutes int, enabled bool) *HeartbeatService {
	// Apply minimum interval
	if intervalMinutes < minIntervalMinutes && intervalMinutes != 0 {
		intervalMinutes = minIntervalMinutes
	}

	if intervalMinutes == 0 {
		intervalMinutes = defaultIntervalMinutes
	}

	return &HeartbeatService{
		workspace:    workspace,
		onHeartbeat:  onHeartbeat,
		interval:     time.Duration(intervalMinutes) * time.Minute,
		enabled:      enabled,
		stateManager: state.NewManager(workspace),
		stopChan:     make(chan struct{}),
	}
}

// SetChannelSender sets the channel sender for delivering heartbeat results.
func (hs *HeartbeatService) SetChannelSender(sender ChannelSender) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.channelSender = sender
}

// SetOnHeartbeatWithTools sets the tool-supporting heartbeat handler.
// This handler returns a ToolResult that can indicate async operations.
// When set, this handler takes precedence over the legacy onHeartbeat callback.
func (hs *HeartbeatService) SetOnHeartbeatWithTools(handler HeartbeatHandler) {
	hs.mu.Lock()
	defer hs.mu.Unlock()
	hs.onHeartbeatWithTools = handler
}

// Start begins the heartbeat service
func (hs *HeartbeatService) Start() error {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if hs.started {
		logger.InfoC("heartbeat", "Heartbeat service already running")
		return nil
	}

	if !hs.enabled {
		logger.InfoC("heartbeat", "Heartbeat service disabled")
		return nil
	}

	hs.started = true
	hs.stopChan = make(chan struct{})

	go hs.runLoop()

	logger.InfoCF("heartbeat", "Heartbeat service started", map[string]any{
		"interval_minutes": hs.interval.Minutes(),
	})

	return nil
}

// Stop gracefully stops the heartbeat service
func (hs *HeartbeatService) Stop() {
	hs.mu.Lock()
	defer hs.mu.Unlock()

	if !hs.started {
		return
	}

	logger.InfoC("heartbeat", "Stopping heartbeat service")
	close(hs.stopChan)
	hs.started = false
}

// IsRunning returns whether the service is running
func (hs *HeartbeatService) IsRunning() bool {
	hs.mu.RLock()
	defer hs.mu.RUnlock()
	return hs.started
}

// runLoop runs the heartbeat ticker
func (hs *HeartbeatService) runLoop() {
	ticker := time.NewTicker(hs.interval)
	defer ticker.Stop()

	// Run first heartbeat after initial delay
	time.AfterFunc(time.Second, func() {
		hs.executeHeartbeat()
	})

	for {
		select {
		case <-hs.stopChan:
			return
		case <-ticker.C:
			hs.executeHeartbeat()
		}
	}
}

// executeHeartbeat performs a single heartbeat check
func (hs *HeartbeatService) executeHeartbeat() {
	hs.mu.RLock()
	enabled := hs.enabled && hs.started
	handler := hs.onHeartbeat
	handlerWithTools := hs.onHeartbeatWithTools
	hs.mu.RUnlock()

	if !enabled {
		return
	}

	logger.DebugC("heartbeat", "Executing heartbeat")

	prompt := hs.buildPrompt()
	if prompt == "" {
		logger.InfoC("heartbeat", "No heartbeat prompt (HEARTBEAT.md empty or missing)")
		return
	}

	// Prefer the new tool-supporting handler
	if handlerWithTools != nil {
		hs.executeHeartbeatWithTools(prompt)
	} else if handler != nil {
		response, err := handler(prompt)
		if err != nil {
			hs.logError("Heartbeat processing error: %v", err)
			return
		}

		// Check for HEARTBEAT_OK - completely silent response
		if isHeartbeatOK(response) {
			hs.logInfo("Heartbeat OK - silent")
			return
		}

		// Non-OK response - send to last channel
		hs.sendResponse(response)
	}
}

// ExecuteHeartbeatWithTools executes a heartbeat using the tool-supporting handler.
// This method processes ToolResult returns and handles async tasks appropriately.
func (hs *HeartbeatService) ExecuteHeartbeatWithTools(prompt string) {
	hs.executeHeartbeatWithTools(prompt)
}

// executeHeartbeatWithTools is the internal implementation of tool-supporting heartbeat.
func (hs *HeartbeatService) executeHeartbeatWithTools(prompt string) {
	result := hs.onHeartbeatWithTools(prompt)

	if result == nil {
		hs.logInfo("Heartbeat handler returned nil result")
		return
	}

	// Handle different result types
	if result.IsError {
		hs.logError("Heartbeat error: %s", result.ForLLM)
		return
	}

	if result.Async {
		// Async task started - log and return immediately
		hs.logInfo("Async task started: %s", result.ForLLM)
		logger.InfoCF("heartbeat", "Async heartbeat task started",
			map[string]interface{}{
				"message": result.ForLLM,
			})
		return
	}

	// Check if silent (HEARTBEAT_OK equivalent)
	if result.Silent {
		hs.logInfo("Heartbeat OK - silent")
		return
	}

	// Normal completion - send result to user if available
	if result.ForUser != "" {
		hs.sendResponse(result.ForUser)
	} else if result.ForLLM != "" {
		hs.sendResponse(result.ForLLM)
	}

	hs.logInfo("Heartbeat completed: %s", result.ForLLM)
}

// buildPrompt builds the heartbeat prompt from HEARTBEAT.md
func (hs *HeartbeatService) buildPrompt() string {
	// Use memory directory for HEARTBEAT.md
	notesDir := filepath.Join(hs.workspace, "memory")
	heartbeatPath := filepath.Join(notesDir, "HEARTBEAT.md")

	data, err := os.ReadFile(heartbeatPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Create default HEARTBEAT.md template
			hs.createDefaultHeartbeatTemplate()
			return ""
		}
		hs.logError("Error reading HEARTBEAT.md: %v", err)
		return ""
	}

	content := string(data)
	if len(content) == 0 {
		return ""
	}

	// Build prompt with system instructions
	now := time.Now().Format("2006-01-02 15:04:05")
	prompt := fmt.Sprintf(`# Heartbeat Check

Current time: %s

You are a proactive AI assistant. This is a scheduled heartbeat check.
Review the following tasks and execute any necessary actions using available skills.
If there is nothing that requires attention, respond ONLY with: HEARTBEAT_OK

%s
`, now, content)

	return prompt
}

// createDefaultHeartbeatTemplate creates the default HEARTBEAT.md file
func (hs *HeartbeatService) createDefaultHeartbeatTemplate() {
	notesDir := filepath.Join(hs.workspace, "memory")
	heartbeatPath := filepath.Join(notesDir, "HEARTBEAT.md")

	// Ensure memory directory exists
	if err := os.MkdirAll(notesDir, 0755); err != nil {
		hs.logError("Failed to create memory directory: %v", err)
		return
	}

	defaultContent := `# Heartbeat Check List

This file contains tasks for the heartbeat service to check periodically.

## Examples

- Check for unread messages
- Review upcoming calendar events
- Check device status (e.g., MaixCam)

## Instructions

If there's nothing that needs attention, respond with: HEARTBEAT_OK
This ensures the heartbeat runs silently when everything is fine.

---

Add your heartbeat tasks below this line:
`

	if err := os.WriteFile(heartbeatPath, []byte(defaultContent), 0644); err != nil {
		hs.logError("Failed to create default HEARTBEAT.md: %v", err)
	} else {
		hs.logInfo("Created default HEARTBEAT.md template")
	}
}

// sendResponse sends the heartbeat response to the last channel
func (hs *HeartbeatService) sendResponse(response string) {
	hs.mu.RLock()
	sender := hs.channelSender
	hs.mu.RUnlock()

	if sender == nil {
		hs.logInfo("No channel sender configured, heartbeat result not sent")
		return
	}

	// Get last channel from state
	lastChannel := hs.stateManager.GetLastChannel()
	if lastChannel == "" {
		hs.logInfo("No last channel recorded, heartbeat result not sent")
		return
	}

	// Parse channel format: "platform:user_id" (e.g., "telegram:123456")
	var platform, userID string
	n, err := fmt.Sscanf(lastChannel, "%[^:]:%s", &platform, &userID)
	if err != nil || n != 2 {
		hs.logError("Invalid last channel format: %s", lastChannel)
		return
	}

	// Send to channel
	ctx := context.Background()
	if err := sender.SendToChannel(ctx, platform, userID, response); err != nil {
		hs.logError("Error sending to channel %s: %v", platform, err)
		return
	}

	hs.logInfo("Heartbeat result sent to %s", platform)
}

// isHeartbeatOK checks if the response is HEARTBEAT_OK
func isHeartbeatOK(response string) bool {
	return response == heartbeatOK
}

// logInfo logs an informational message to the heartbeat log
func (hs *HeartbeatService) logInfo(format string, args ...any) {
	hs.log("INFO", format, args...)
}

// logError logs an error message to the heartbeat log
func (hs *HeartbeatService) logError(format string, args ...any) {
	hs.log("ERROR", format, args...)
}

// log writes a message to the heartbeat log file
func (hs *HeartbeatService) log(level, format string, args ...any) {
	// Ensure memory directory exists
	logDir := filepath.Join(hs.workspace, "memory")
	os.MkdirAll(logDir, 0755)

	logFile := filepath.Join(logDir, "heartbeat.log")
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()

	timestamp := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(f, "[%s] [%s] %s\n", timestamp, level, fmt.Sprintf(format, args...))
}
