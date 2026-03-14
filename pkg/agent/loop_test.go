package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/archive/discordarchive"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/logger"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/tools"
)

// mockProvider is a simple mock LLM provider for testing
type mockProvider struct{}

func (m *mockProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, opts map[string]interface{}) (*providers.LLMResponse, error) {
	return &providers.LLMResponse{
		Content:   "Mock response",
		ToolCalls: []providers.ToolCall{},
	}, nil
}

func (m *mockProvider) GetDefaultModel() string {
	return "mock-model"
}

type diagnosticMockProvider struct {
	response    string
	diagnostics *providers.ResponseDiagnostics
}

func (m *diagnosticMockProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, opts map[string]interface{}) (*providers.LLMResponse, error) {
	return &providers.LLMResponse{
		Content:     m.response,
		ToolCalls:   []providers.ToolCall{},
		Diagnostics: m.diagnostics,
	}, nil
}

func (m *diagnosticMockProvider) GetDefaultModel() string {
	return "mock-model"
}

func TestLocalTurnDiagnostics_RecordLLMResponseTracksFallbacks(t *testing.T) {
	diag := newLocalTurnDiagnostics()
	diag.recordLLMResponse(1500*time.Millisecond, &providers.LLMResponse{
		ToolCalls: []providers.ToolCall{{ID: "call_1", Name: "read_file"}},
		Diagnostics: &providers.ResponseDiagnostics{
			ContentSource:  "thinking",
			ToolCallSource: "thinking",
		},
	})

	if diag.LLMCalls != 1 {
		t.Fatalf("llm_calls=%d", diag.LLMCalls)
	}
	if diag.LLMTotalMS != 1500 {
		t.Fatalf("llm_total_ms=%d", diag.LLMTotalMS)
	}
	if diag.ToolCallsRequested != 1 {
		t.Fatalf("tool_calls_requested=%d", diag.ToolCallsRequested)
	}
	if diag.FallbackContentCount != 1 {
		t.Fatalf("fallback_content_count=%d", diag.FallbackContentCount)
	}
	if diag.FallbackToolCallCount != 1 {
		t.Fatalf("fallback_tool_call_count=%d", diag.FallbackToolCallCount)
	}
	if diag.ContentSources["thinking"] != 1 {
		t.Fatalf("content_sources=%v", diag.ContentSources)
	}
	if diag.ToolCallSources["thinking"] != 1 {
		t.Fatalf("tool_call_sources=%v", diag.ToolCallSources)
	}
}

func TestNewAgentLoopWithExternalReadOnlyProfileRestrictsTools(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = t.TempDir()
	al := NewAgentLoopWithOptions(cfg, bus.NewMessageBus(), &mockProvider{}, LoopOptions{
		ToolProfile: ToolProfileExternalReadOnly,
	})

	if _, ok := al.tools.Get("web_search"); !ok {
		t.Fatal("expected web_search tool")
	}
	if _, ok := al.tools.Get("web_fetch"); !ok {
		t.Fatal("expected web_fetch tool")
	}
	if _, ok := al.tools.Get("pubmed_search"); !ok {
		t.Fatal("expected pubmed_search tool")
	}
	if _, ok := al.tools.Get("pubmed_fetch"); !ok {
		t.Fatal("expected pubmed_fetch tool")
	}
	for _, blocked := range []string{"exec", "message", "read_file", "write_file", "spawn", "subagent"} {
		if _, ok := al.tools.Get(blocked); ok {
			t.Fatalf("did not expect %s tool in external readonly profile", blocked)
		}
	}
}

func TestExternalReadOnlyRunJobDoesNotPersistSession(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = t.TempDir()
	al := NewAgentLoopWithOptions(cfg, bus.NewMessageBus(), &mockProvider{}, LoopOptions{
		ToolProfile: ToolProfileExternalReadOnly,
	})

	msg := bus.InboundMessage{
		Channel:    "discord",
		ChatID:     "room-1",
		SenderID:   "user-1",
		SessionKey: "discord:room-1",
		Content:    "what species do neuronexus probes record from?",
	}
	if _, err := al.RunJob(context.Background(), msg, nil); err != nil {
		t.Fatalf("RunJob: %v", err)
	}
	if history := al.sessions.GetHistory(msg.SessionKey); len(history) != 0 {
		t.Fatalf("expected no persisted session history, got %d messages", len(history))
	}
	if summary := al.sessions.GetSummary(msg.SessionKey); summary != "" {
		t.Fatalf("expected no persisted summary, got %q", summary)
	}
}

func TestExternalReadOnlyRunJobAddsRuntimeConstraintPrompt(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = t.TempDir()
	provider := &captureMockProvider{response: "ok"}
	al := NewAgentLoopWithOptions(cfg, bus.NewMessageBus(), provider, LoopOptions{
		ToolProfile: ToolProfileExternalReadOnly,
	})

	msg := bus.InboundMessage{
		Channel:    "discord",
		ChatID:     "room-1",
		SenderID:   "user-1",
		SessionKey: "discord:room-1",
		Content:    "what species do neuronexus probes record from?",
	}
	if _, err := al.RunJob(context.Background(), msg, nil); err != nil {
		t.Fatalf("RunJob: %v", err)
	}

	systemPrompt := provider.LastSystemPrompt()
	if !strings.Contains(systemPrompt, "## Runtime Constraints") {
		t.Fatalf("expected runtime constraints in system prompt, got: %s", systemPrompt)
	}
	if !strings.Contains(systemPrompt, "`pubmed_search`") || !strings.Contains(systemPrompt, "`pubmed_fetch`") {
		t.Fatalf("expected external readonly tool list in system prompt, got: %s", systemPrompt)
	}
	if !strings.Contains(systemPrompt, "`exec`, file mutation, and outbound message tools are unavailable") {
		t.Fatalf("expected exec/file/outbound unavailability note in system prompt, got: %s", systemPrompt)
	}
	if !strings.Contains(systemPrompt, "silently skip those steps unless the user explicitly asked for them") {
		t.Fatalf("expected silent skill adaptation guidance in system prompt, got: %s", systemPrompt)
	}
	if !strings.Contains(systemPrompt, "Do not mention external-readonly mode") {
		t.Fatalf("expected no-leak guidance in system prompt, got: %s", systemPrompt)
	}
}

func TestLocalTurnDiagnostics_RecordToolExecutionTracksErrors(t *testing.T) {
	diag := newLocalTurnDiagnostics()
	diag.recordToolExecution(275*time.Millisecond, &tools.ToolResult{IsError: true, Async: true})

	if diag.ToolCalls != 1 {
		t.Fatalf("tool_calls=%d", diag.ToolCalls)
	}
	if diag.ToolTotalMS != 275 {
		t.Fatalf("tool_total_ms=%d", diag.ToolTotalMS)
	}
	if diag.ToolErrorCount != 1 {
		t.Fatalf("tool_error_count=%d", diag.ToolErrorCount)
	}
	if diag.ToolAsyncCount != 1 {
		t.Fatalf("tool_async_count=%d", diag.ToolAsyncCount)
	}
	if diag.FailurePhase != "tool_execution" {
		t.Fatalf("failure_phase=%q", diag.FailurePhase)
	}
}

func TestLocalTurnDiagnosticsFieldsIncludeSourceBreakdown(t *testing.T) {
	diag := newLocalTurnDiagnostics()
	diag.ContextBuildMS = 18
	diag.LLMTotalMS = 2400
	diag.ToolTotalMS = 510
	diag.SessionSaveMS = 6
	diag.SummaryMS = 2
	diag.OutboundMS = 1
	diag.LLMCalls = 2
	diag.ToolCallsRequested = 1
	diag.ToolCalls = 1
	diag.FallbackContentCount = 1
	diag.ContentSources["content"] = 1
	diag.ContentSources["thinking"] = 1
	diag.ToolCallSources["native"] = 1

	fields := diag.fields()
	if fields["context_build_ms"] != int64(18) {
		t.Fatalf("fields=%v", fields)
	}
	if fields["fallback_used"] != true {
		t.Fatalf("fields=%v", fields)
	}
	if fields["tool_calls_requested"] != 1 {
		t.Fatalf("fields=%v", fields)
	}
	if fields["tool_calls_executed"] != 1 {
		t.Fatalf("fields=%v", fields)
	}
	contentSources, ok := fields["content_sources"].(map[string]int)
	if !ok {
		t.Fatalf("expected content_sources map, got %T", fields["content_sources"])
	}
	if contentSources["thinking"] != 1 {
		t.Fatalf("content_sources=%v", contentSources)
	}
}

func TestLocalTurnDiagnostics_RecordFailurePreservesFirstCause(t *testing.T) {
	diag := newLocalTurnDiagnostics()
	diag.recordFailure("provider_chat", "deadline exceeded")
	diag.recordFailure("llm_iteration", "later wrapper")

	fields := diag.fields()
	if fields["failure_phase"] != "provider_chat" {
		t.Fatalf("fields=%v", fields)
	}
	if fields["failure_reason"] != "deadline exceeded" {
		t.Fatalf("fields=%v", fields)
	}
}

func TestProcessDirect_LocalModeEmitsRuntimeSummaryLog(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-local-runtime-log-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	logPath := filepath.Join(tmpDir, "gateway.log")
	prevLevel := logger.GetLevel()
	logger.SetLevel(logger.INFO)
	defer logger.SetLevel(prevLevel)
	if err := logger.EnableFileLogging(logPath); err != nil {
		t.Fatalf("EnableFileLogging() error = %v", err)
	}
	defer logger.DisableFileLogging()

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Mode:              config.ModePhi,
				LocalBackend:      "ollama",
				LocalModel:        "qwen3.5:4b",
				MaxTokens:         4096,
				MaxToolIterations: 4,
			},
		},
	}

	al := NewAgentLoop(cfg, bus.NewMessageBus(), &diagnosticMockProvider{
		response: "Local reply",
		diagnostics: &providers.ResponseDiagnostics{
			ContentSource: "thinking",
		},
	})
	defer al.Stop()

	if _, err := al.ProcessDirect(context.Background(), "hello local mode", "local-runtime-test"); err != nil {
		t.Fatalf("ProcessDirect() error = %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	var found bool
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry logger.LogEntry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("json.Unmarshal() error = %v for line %q", err, line)
		}
		if entry.Message != "Local runtime summary" {
			continue
		}
		found = true
		if got := entry.Fields["local_backend"]; got != "ollama" {
			t.Fatalf("local_backend=%v fields=%v", got, entry.Fields)
		}
		if got := entry.Fields["fallback_used"]; got != true {
			t.Fatalf("fallback_used=%v fields=%v", got, entry.Fields)
		}
		if got := entry.Fields["llm_calls"]; got != float64(1) {
			t.Fatalf("llm_calls=%v fields=%v", got, entry.Fields)
		}
	}
	if !found {
		t.Fatalf("expected Local runtime summary entry in log:\n%s", string(data))
	}
}

func TestRecordLastChannel(t *testing.T) {
	// Create temp workspace
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test config
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	// Create agent loop
	msgBus := bus.NewMessageBus()
	provider := &mockProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)

	// Test RecordLastChannel
	testChannel := "test-channel"
	err = al.RecordLastChannel(testChannel)
	if err != nil {
		t.Fatalf("RecordLastChannel failed: %v", err)
	}

	// Verify channel was saved
	lastChannel := al.state.GetLastChannel()
	if lastChannel != testChannel {
		t.Errorf("Expected channel '%s', got '%s'", testChannel, lastChannel)
	}

	// Ensure async state writes are flushed before checking persisted state.
	al.Stop()

	// Verify persistence by creating a new agent loop
	al2 := NewAgentLoop(cfg, msgBus, provider)
	defer al2.Stop()
	if al2.state.GetLastChannel() != testChannel {
		t.Errorf("Expected persistent channel '%s', got '%s'", testChannel, al2.state.GetLastChannel())
	}
}

func TestRecordLastChatID(t *testing.T) {
	// Create temp workspace
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test config
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	// Create agent loop
	msgBus := bus.NewMessageBus()
	provider := &mockProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)

	// Test RecordLastChatID
	testChatID := "test-chat-id-123"
	err = al.RecordLastChatID(testChatID)
	if err != nil {
		t.Fatalf("RecordLastChatID failed: %v", err)
	}

	// Verify chat ID was saved
	lastChatID := al.state.GetLastChatID()
	if lastChatID != testChatID {
		t.Errorf("Expected chat ID '%s', got '%s'", testChatID, lastChatID)
	}

	// Ensure async state writes are flushed before checking persisted state.
	al.Stop()

	// Verify persistence by creating a new agent loop
	al2 := NewAgentLoop(cfg, msgBus, provider)
	defer al2.Stop()
	if al2.state.GetLastChatID() != testChatID {
		t.Errorf("Expected persistent chat ID '%s', got '%s'", testChatID, al2.state.GetLastChatID())
	}
}

func TestNewAgentLoop_StateInitialized(t *testing.T) {
	// Create temp workspace
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test config
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	// Create agent loop
	msgBus := bus.NewMessageBus()
	provider := &mockProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)

	// Verify state manager is initialized
	if al.state == nil {
		t.Error("Expected state manager to be initialized")
	}

	// Verify state directory was created
	stateDir := filepath.Join(tmpDir, "state")
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		t.Error("Expected state directory to exist")
	}
}

// TestToolRegistry_ToolRegistration verifies tools can be registered and retrieved
func TestToolRegistry_ToolRegistration(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &mockProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)

	// Register a custom tool
	customTool := &mockCustomTool{}
	al.RegisterTool(customTool)

	// Verify tool is registered by checking it doesn't panic on GetStartupInfo
	// (actual tool retrieval is tested in tools package tests)
	info := al.GetStartupInfo()
	toolsInfo := info["tools"].(map[string]interface{})
	toolsList := toolsInfo["names"].([]string)

	// Check that our custom tool name is in the list
	found := false
	for _, name := range toolsList {
		if name == "mock_custom" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected custom tool to be registered")
	}
}

// TestToolContext_Updates verifies tool context is updated with channel/chatID
func TestToolContext_Updates(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &simpleMockProvider{response: "OK"}
	_ = NewAgentLoop(cfg, msgBus, provider)

	// Verify that ContextualTool interface is defined and can be implemented
	// This test validates the interface contract exists
	ctxTool := &mockContextualTool{}

	// Verify the tool implements the interface correctly
	var _ tools.ContextualTool = ctxTool
}

// TestToolRegistry_GetDefinitions verifies tool definitions can be retrieved
func TestToolRegistry_GetDefinitions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &mockProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)

	// Register a test tool and verify it shows up in startup info
	testTool := &mockCustomTool{}
	al.RegisterTool(testTool)

	info := al.GetStartupInfo()
	toolsInfo := info["tools"].(map[string]interface{})
	toolsList := toolsInfo["names"].([]string)

	// Check that our custom tool name is in the list
	found := false
	for _, name := range toolsList {
		if name == "mock_custom" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected custom tool to be registered")
	}
}

// TestAgentLoop_GetStartupInfo verifies startup info contains tools
func TestAgentLoop_GetStartupInfo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &mockProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)

	info := al.GetStartupInfo()

	// Verify tools info exists
	toolsInfo, ok := info["tools"]
	if !ok {
		t.Fatal("Expected 'tools' key in startup info")
	}

	toolsMap, ok := toolsInfo.(map[string]interface{})
	if !ok {
		t.Fatal("Expected 'tools' to be a map")
	}

	count, ok := toolsMap["count"]
	if !ok {
		t.Fatal("Expected 'count' in tools info")
	}

	// Should have default tools registered
	if count.(int) == 0 {
		t.Error("Expected at least some tools to be registered")
	}

	hooksInfo, ok := info["hooks"]
	if !ok {
		t.Fatal("Expected 'hooks' key in startup info")
	}
	hooksMap, ok := hooksInfo.(map[string]interface{})
	if !ok {
		t.Fatal("Expected 'hooks' to be a map")
	}
	if enabled, ok := hooksMap["enabled"].(bool); !ok || !enabled {
		t.Fatal("Expected hooks to be enabled by default")
	}
	if handlers, ok := hooksMap["handlers"].(int); !ok || handlers == 0 {
		t.Fatal("Expected hook handlers to be registered by default")
	}
}

func TestProcessDirectWithChannel_DiscordAutoArchiveTrim(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-discord-archive-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = tmpDir
	cfg.Agents.Defaults.Model = "test-model"
	cfg.Agents.Defaults.MaxTokens = 4096
	cfg.Channels.Discord.Archive.Enabled = true
	cfg.Channels.Discord.Archive.AutoArchive = true
	cfg.Channels.Discord.Archive.MaxSessionMessages = 8
	cfg.Channels.Discord.Archive.MaxSessionTokens = 60
	cfg.Channels.Discord.Archive.KeepUserPairs = 2
	cfg.Channels.Discord.Archive.MinTailMessages = 4

	msgBus := bus.NewMessageBus()
	provider := &mockProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)

	sessionKey := "discord:test-channel"
	for i := 0; i < 8; i++ {
		_, err := al.ProcessDirectWithChannel(context.Background(), "alpha archival pressure message", sessionKey, "discord", "test-channel", "user-1")
		if err != nil {
			t.Fatalf("ProcessDirectWithChannel failed at turn %d: %v", i, err)
		}
	}

	history := al.sessions.GetHistory(sessionKey)
	if len(history) >= 16 {
		t.Fatalf("expected trimmed history, got %d messages", len(history))
	}

	archiveDir := filepath.Join(tmpDir, "memory", "archive", "discord", "sessions")
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatalf("expected archive directory %s: %v", archiveDir, err)
	}
	mdCount := 0
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".md") {
			mdCount++
		}
	}
	if mdCount == 0 {
		t.Fatalf("expected at least one archive markdown file in %s", archiveDir)
	}
}

func TestProcessDirectWithChannel_DiscordAutoRecallInjectedAfterArchiveExists(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-discord-recall-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = tmpDir
	cfg.Agents.Defaults.Model = "test-model"
	cfg.Agents.Defaults.MaxTokens = 4096
	cfg.Channels.Discord.Archive.Enabled = true
	cfg.Channels.Discord.Archive.AutoArchive = true
	cfg.Channels.Discord.Archive.MaxSessionMessages = 8
	cfg.Channels.Discord.Archive.MaxSessionTokens = 60
	cfg.Channels.Discord.Archive.KeepUserPairs = 2
	cfg.Channels.Discord.Archive.MinTailMessages = 4
	cfg.Channels.Discord.Archive.RecallTopK = 3
	cfg.Channels.Discord.Archive.RecallMaxChars = 1000

	msgBus := bus.NewMessageBus()
	provider := &captureMockProvider{response: "Mock response"}
	al := NewAgentLoop(cfg, msgBus, provider)

	sessionKey := "discord:test-channel"
	marker := "rare-marker-phase2"
	for i := 0; i < 8; i++ {
		_, err := al.ProcessDirectWithChannel(
			context.Background(),
			"alpha "+marker+" archival pressure message",
			sessionKey,
			"discord",
			"test-channel",
			"user-1",
		)
		if err != nil {
			t.Fatalf("ProcessDirectWithChannel failed at turn %d: %v", i, err)
		}
	}

	_, err = al.ProcessDirectWithChannel(
		context.Background(),
		"please recall "+marker,
		sessionKey,
		"discord",
		"test-channel",
		"user-1",
	)
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel recall turn failed: %v", err)
	}

	systemPrompt := provider.LastSystemPrompt()
	if !strings.Contains(systemPrompt, "## Discord Archive Recall (Auto)") {
		t.Fatal("expected auto recall section in system prompt for discord turn")
	}
	if !strings.Contains(strings.ToLower(systemPrompt), strings.ToLower(marker)) {
		t.Fatalf("expected recall section to contain marker %q", marker)
	}
}

func TestProcessDirectWithChannel_DiscordAutoRecallInjectionCappedByBudget(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-discord-recall-cap-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = tmpDir
	cfg.Agents.Defaults.Model = "test-model"
	cfg.Channels.Discord.Archive.Enabled = true
	cfg.Channels.Discord.Archive.AutoArchive = true
	cfg.Channels.Discord.Archive.RecallTopK = 3
	cfg.Channels.Discord.Archive.RecallMaxChars = 220
	cfg.Channels.Discord.Archive.MaxSessionMessages = 8
	cfg.Channels.Discord.Archive.MaxSessionTokens = 60

	msgBus := bus.NewMessageBus()
	provider := &captureMockProvider{response: "Mock response"}
	al := NewAgentLoop(cfg, msgBus, provider)

	al.discordRecallFn = func(query, sessionKey string, topK, maxChars int) ([]discordarchive.RecallHit, error) {
		return []discordarchive.RecallHit{
			{
				SessionKey: sessionKey,
				SourcePath: "/tmp/archive-one.md",
				Score:      99,
				Text:       strings.Repeat("token-heavy-recall-context ", 40),
			},
		}, nil
	}

	_, err = al.ProcessDirectWithChannel(
		context.Background(),
		"token-heavy recall test",
		"discord:test-channel",
		"discord",
		"test-channel",
		"user-1",
	)
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel failed: %v", err)
	}

	systemPrompt := provider.LastSystemPrompt()
	const recallHeader = "## Discord Archive Recall (Auto)"
	start := strings.Index(systemPrompt, recallHeader)
	if start < 0 {
		t.Fatal("expected auto recall header in system prompt")
	}
	recallSection := systemPrompt[start:]
	if len(recallSection) > cfg.Channels.Discord.Archive.RecallMaxChars {
		t.Fatalf(
			"expected recall section <= %d chars, got %d",
			cfg.Channels.Discord.Archive.RecallMaxChars,
			len(recallSection),
		)
	}
}

func TestProcessDirectWithChannel_DiscordAutoRecallFailureIsFailOpen(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-discord-recall-failopen-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = tmpDir
	cfg.Agents.Defaults.Model = "test-model"
	cfg.Channels.Discord.Archive.Enabled = true

	msgBus := bus.NewMessageBus()
	provider := &captureMockProvider{response: "LLM still replied"}
	al := NewAgentLoop(cfg, msgBus, provider)

	al.discordRecallFn = func(query, sessionKey string, topK, maxChars int) ([]discordarchive.RecallHit, error) {
		return nil, errors.New("synthetic recall failure")
	}

	got, err := al.ProcessDirectWithChannel(
		context.Background(),
		"does recall failure block chat",
		"discord:test-channel",
		"discord",
		"test-channel",
		"user-1",
	)
	if err != nil {
		t.Fatalf("expected fail-open behavior, got error: %v", err)
	}
	if got != "LLM still replied" {
		t.Fatalf("unexpected response after recall failure: %q", got)
	}
}

func TestProcessDirectWithChannel_NonDiscordNoAutoRecallInjection(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-recall-nondiscord-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = tmpDir
	cfg.Agents.Defaults.Model = "test-model"
	cfg.Channels.Discord.Archive.Enabled = true

	msgBus := bus.NewMessageBus()
	provider := &captureMockProvider{response: "Mock response"}
	al := NewAgentLoop(cfg, msgBus, provider)

	al.discordRecallFn = func(query, sessionKey string, topK, maxChars int) ([]discordarchive.RecallHit, error) {
		return []discordarchive.RecallHit{
			{
				SessionKey: "discord:test-channel",
				SourcePath: "/tmp/archive.md",
				Score:      7,
				Text:       "discord-only auto recall context",
			},
		}, nil
	}

	_, err = al.ProcessDirectWithChannel(
		context.Background(),
		"telegram turn should not inject discord archive",
		"telegram:test-channel",
		"telegram",
		"test-channel",
		"user-1",
	)
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel failed: %v", err)
	}

	systemPrompt := provider.LastSystemPrompt()
	if strings.Contains(systemPrompt, "## Discord Archive Recall (Auto)") {
		t.Fatal("did not expect discord recall section for non-discord channel")
	}
}

func TestProcessDirectWithChannel_LocalModePrefetchesURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><article>Local prefetch test payload</article></body></html>`))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = tmpDir
	cfg.Agents.Defaults.Model = "test-model"
	cfg.Agents.Defaults.Mode = config.ModePhi

	msgBus := bus.NewMessageBus()
	provider := &captureMockProvider{response: "prefetch-ok"}
	al := NewAgentLoop(cfg, msgBus, provider)

	_, err := al.ProcessDirectWithChannel(
		context.Background(),
		"Please summarize this page: "+server.URL,
		"cli:prefetch",
		"cli",
		"direct",
		"user-1",
	)
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel failed: %v", err)
	}

	userPrompt := provider.LastUserPrompt()
	if !strings.Contains(userPrompt, "## Prefetched Context (web_fetch)") {
		t.Fatalf("expected local prefetch context in user prompt, got: %s", userPrompt)
	}
	if !strings.Contains(userPrompt, server.URL) {
		t.Fatalf("expected prefetched payload to include URL, got: %s", userPrompt)
	}
}

func TestPickLocalPrefetchTool_SkipsPubMedCitationQueries(t *testing.T) {
	tests := []string{
		"What's the latest PubMed on triple GLP agonists?",
		"Can you verify PMID 24001701?",
		`<@bot> the "Detecting" paper is Jeon S et al 2013. Isn't that correct?`,
		"Please check this citation: https://pubmed.ncbi.nlm.nih.gov/24001701/",
	}
	for _, input := range tests {
		if toolName, args, ok := pickLocalPrefetchTool(input); ok {
			t.Fatalf("expected no prefetch for %q, got %s %+v", input, toolName, args)
		}
	}
}

func TestPickLocalPrefetchTool_StillPrefetchesGeneralCurrentInfo(t *testing.T) {
	toolName, args, ok := pickLocalPrefetchTool("What's the latest FDA news on tirzepatide?")
	if !ok {
		t.Fatal("expected prefetch for general current-info query")
	}
	if toolName != "web_search" {
		t.Fatalf("expected web_search, got %s", toolName)
	}
	if got := args["query"]; got != "What's the latest FDA news on tirzepatide?" {
		t.Fatalf("unexpected query arg: %#v", got)
	}
}

func TestProcessDirectWithChannel_CloudModeSkipsLocalPrefetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><article>cloud no prefetch</article></body></html>`))
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = tmpDir
	cfg.Agents.Defaults.Model = "test-model"
	cfg.Agents.Defaults.Mode = config.ModeCloud

	msgBus := bus.NewMessageBus()
	provider := &captureMockProvider{response: "cloud-ok"}
	al := NewAgentLoop(cfg, msgBus, provider)

	_, err := al.ProcessDirectWithChannel(
		context.Background(),
		"Please summarize this page: "+server.URL,
		"cli:cloud",
		"cli",
		"direct",
		"user-1",
	)
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel failed: %v", err)
	}

	userPrompt := provider.LastUserPrompt()
	if strings.Contains(userPrompt, "## Prefetched Context (web_fetch)") {
		t.Fatalf("did not expect local prefetch context in cloud mode, got: %s", userPrompt)
	}
}

func TestRunLLMIteration_TruncatesToolOutputForLocalMode(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = tmpDir
	cfg.Agents.Defaults.Model = "test-model"
	cfg.Agents.Defaults.Mode = config.ModePhi
	cfg.Agents.Defaults.MaxToolIterations = 5

	provider := &toolThenDoneProvider{}
	msgBus := bus.NewMessageBus()
	al := NewAgentLoop(cfg, msgBus, provider)
	al.RegisterTool(&bigOutputTool{
		content: strings.Repeat("x", localToolResultMaxChars+3000),
	})

	_, err := al.ProcessDirectWithChannel(
		context.Background(),
		"trigger big output tool",
		"cli:truncate",
		"cli",
		"direct",
		"user-1",
	)
	if err != nil {
		t.Fatalf("ProcessDirectWithChannel failed: %v", err)
	}

	secondCall := provider.CallMessages(1)
	if len(secondCall) == 0 {
		t.Fatalf("expected second provider call with tool result context")
	}

	var toolMsg providers.Message
	found := false
	for i := len(secondCall) - 1; i >= 0; i-- {
		if secondCall[i].Role == "tool" {
			toolMsg = secondCall[i]
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected tool message in second provider call")
	}
	if !strings.Contains(toolMsg.Content, "truncated") {
		t.Fatalf("expected truncation marker in tool content, got len=%d", len(toolMsg.Content))
	}
	if len(toolMsg.Content) > localToolResultMaxChars+200 {
		t.Fatalf("expected truncated content near cap, got len=%d", len(toolMsg.Content))
	}
}

func TestAgentLoop_HooksCanBeDisabledByPolicy(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Disable hooks completely via workspace policy.
	policyYAML := "enabled: false\naudit:\n  enabled: false\n"
	if err := os.WriteFile(filepath.Join(tmpDir, "hooks.yaml"), []byte(policyYAML), 0644); err != nil {
		t.Fatalf("write hooks.yaml: %v", err)
	}

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &mockProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)
	info := al.GetStartupInfo()

	hooksMap, ok := info["hooks"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected hooks info map")
	}
	if enabled, ok := hooksMap["enabled"].(bool); !ok || enabled {
		t.Fatal("Expected hooks to be disabled by policy")
	}
	if handlers, ok := hooksMap["handlers"].(int); !ok || handlers != 0 {
		t.Fatalf("Expected no hook handlers when disabled, got %#v", hooksMap["handlers"])
	}
	if auditPath, ok := hooksMap["audit_path"].(string); !ok || auditPath != "" {
		t.Fatalf("Expected empty audit path when audit disabled, got %#v", hooksMap["audit_path"])
	}
}

func TestAgentLoop_WritesHookAuditEvents(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &mockProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)

	_, err = al.processMessage(context.Background(), bus.InboundMessage{
		Channel:    "cli",
		SenderID:   "tester",
		ChatID:     "direct",
		Content:    "hello",
		SessionKey: "audit-test",
	})
	if err != nil {
		t.Fatalf("processMessage failed: %v", err)
	}

	auditPath := filepath.Join(tmpDir, "hooks", "hook-events.jsonl")
	deadline := time.Now().Add(2 * time.Second)
	var body string
	for {
		data, readErr := os.ReadFile(auditPath)
		if readErr == nil {
			body = string(data)
			if strings.Contains(body, "\"event\":\"before_turn\"") && strings.Contains(body, "\"event\":\"after_turn\"") {
				break
			}
		}
		if time.Now().After(deadline) {
			if readErr != nil {
				t.Fatalf("read hook audit file after wait: %v", readErr)
			}
			t.Fatalf("expected before_turn and after_turn events in audit file, got: %s", body)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestAgentLoop_Stop verifies Stop() sets running to false
func TestAgentLoop_Stop(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &mockProvider{}
	al := NewAgentLoop(cfg, msgBus, provider)

	// Note: running is only set to true when Run() is called
	// We can't test that without starting the event loop
	// Instead, verify the Stop method can be called safely
	al.Stop()

	// Verify running is false (initial state or after Stop)
	if al.running.Load() {
		t.Error("Expected agent to be stopped (or never started)")
	}
}

// Mock implementations for testing

type simpleMockProvider struct {
	response string
}

func (m *simpleMockProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, opts map[string]interface{}) (*providers.LLMResponse, error) {
	return &providers.LLMResponse{
		Content:   m.response,
		ToolCalls: []providers.ToolCall{},
	}, nil
}

func (m *simpleMockProvider) GetDefaultModel() string {
	return "mock-model"
}

type captureMockProvider struct {
	response string
	mu       sync.Mutex
	last     []providers.Message
}

func (m *captureMockProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, opts map[string]interface{}) (*providers.LLMResponse, error) {
	m.mu.Lock()
	m.last = cloneMessages(messages)
	m.mu.Unlock()
	return &providers.LLMResponse{
		Content:   m.response,
		ToolCalls: []providers.ToolCall{},
	}, nil
}

func (m *captureMockProvider) GetDefaultModel() string {
	return "mock-model"
}

func (m *captureMockProvider) LastSystemPrompt() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.last) == 0 {
		return ""
	}
	if m.last[0].Role != "system" {
		return ""
	}
	return m.last[0].Content
}

func (m *captureMockProvider) LastUserPrompt() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.last) - 1; i >= 0; i-- {
		if m.last[i].Role == "user" {
			return m.last[i].Content
		}
	}
	return ""
}

type toolThenDoneProvider struct {
	mu    sync.Mutex
	calls [][]providers.Message
}

func (m *toolThenDoneProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, opts map[string]interface{}) (*providers.LLMResponse, error) {
	m.mu.Lock()
	m.calls = append(m.calls, cloneMessages(messages))
	callIndex := len(m.calls) - 1
	m.mu.Unlock()

	if callIndex == 0 {
		return &providers.LLMResponse{
			Content: "",
			ToolCalls: []providers.ToolCall{
				{
					ID:        "call-big-output",
					Name:      "big_output_tool",
					Arguments: map[string]interface{}{},
				},
			},
		}, nil
	}
	return &providers.LLMResponse{
		Content:   "done",
		ToolCalls: []providers.ToolCall{},
	}, nil
}

func (m *toolThenDoneProvider) GetDefaultModel() string {
	return "mock-model"
}

func (m *toolThenDoneProvider) CallMessages(index int) []providers.Message {
	m.mu.Lock()
	defer m.mu.Unlock()
	if index < 0 || index >= len(m.calls) {
		return nil
	}
	return cloneMessages(m.calls[index])
}

type bigOutputTool struct {
	content string
}

func (t *bigOutputTool) Name() string {
	return "big_output_tool"
}

func (t *bigOutputTool) Description() string {
	return "Returns very large output for truncation tests"
}

func (t *bigOutputTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *bigOutputTool) Execute(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	return tools.NewToolResult(t.content)
}

func cloneMessages(in []providers.Message) []providers.Message {
	if len(in) == 0 {
		return nil
	}
	out := make([]providers.Message, len(in))
	for i, msg := range in {
		out[i] = msg
		if len(msg.ToolCalls) > 0 {
			out[i].ToolCalls = make([]providers.ToolCall, len(msg.ToolCalls))
			copy(out[i].ToolCalls, msg.ToolCalls)
		}
	}
	return out
}

// mockCustomTool is a simple mock tool for registration testing
type mockCustomTool struct{}

func (m *mockCustomTool) Name() string {
	return "mock_custom"
}

func (m *mockCustomTool) Description() string {
	return "Mock custom tool for testing"
}

func (m *mockCustomTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (m *mockCustomTool) Execute(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	return tools.SilentResult("Custom tool executed")
}

// mockContextualTool tracks context updates
type mockContextualTool struct {
	lastChannel string
	lastChatID  string
}

func (m *mockContextualTool) Name() string {
	return "mock_contextual"
}

func (m *mockContextualTool) Description() string {
	return "Mock contextual tool"
}

func (m *mockContextualTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (m *mockContextualTool) Execute(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	return tools.SilentResult("Contextual tool executed")
}

func (m *mockContextualTool) SetContext(channel, chatID string) {
	m.lastChannel = channel
	m.lastChatID = chatID
}

// testHelper executes a message and returns the response
type testHelper struct {
	al *AgentLoop
}

func (h testHelper) executeAndGetResponse(tb testing.TB, ctx context.Context, msg bus.InboundMessage) string {
	// Use a short timeout to avoid hanging
	timeoutCtx, cancel := context.WithTimeout(ctx, responseTimeout)
	defer cancel()

	response, err := h.al.processMessage(timeoutCtx, msg)
	if err != nil {
		tb.Fatalf("processMessage failed: %v", err)
	}
	return response
}

const responseTimeout = 3 * time.Second

// TestToolResult_SilentToolDoesNotSendUserMessage verifies silent tools don't trigger outbound
func TestToolResult_SilentToolDoesNotSendUserMessage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &simpleMockProvider{response: "File operation complete"}
	al := NewAgentLoop(cfg, msgBus, provider)
	helper := testHelper{al: al}

	// ReadFileTool returns SilentResult, which should not send user message
	ctx := context.Background()
	msg := bus.InboundMessage{
		Channel:    "test",
		SenderID:   "user1",
		ChatID:     "chat1",
		Content:    "read test.txt",
		SessionKey: "test-session",
	}

	response := helper.executeAndGetResponse(t, ctx, msg)

	// Silent tool should return the LLM's response directly
	if response != "File operation complete" {
		t.Errorf("Expected 'File operation complete', got: %s", response)
	}
}

// TestToolResult_UserFacingToolDoesSendMessage verifies user-facing tools trigger outbound
func TestToolResult_UserFacingToolDoesSendMessage(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "agent-test-*")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         tmpDir,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}

	msgBus := bus.NewMessageBus()
	provider := &simpleMockProvider{response: "Command output: hello world"}
	al := NewAgentLoop(cfg, msgBus, provider)
	helper := testHelper{al: al}

	// ExecTool returns UserResult, which should send user message
	ctx := context.Background()
	msg := bus.InboundMessage{
		Channel:    "test",
		SenderID:   "user1",
		ChatID:     "chat1",
		Content:    "run hello",
		SessionKey: "test-session",
	}

	response := helper.executeAndGetResponse(t, ctx, msg)

	// User-facing tool should include the output in final response
	if response != "Command output: hello world" {
		t.Errorf("Expected 'Command output: hello world', got: %s", response)
	}
}
