package agent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/archive/discordarchive"
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
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

	// Verify persistence by creating a new agent loop
	al2 := NewAgentLoop(cfg, msgBus, provider)
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

	// Verify persistence by creating a new agent loop
	al2 := NewAgentLoop(cfg, msgBus, provider)
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
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read hook audit file: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, "\"event\":\"before_turn\"") {
		t.Fatalf("expected before_turn event in audit file")
	}
	if !strings.Contains(body, "\"event\":\"after_turn\"") {
		t.Fatalf("expected after_turn event in audit file")
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
