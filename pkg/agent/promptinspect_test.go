package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/session"
)

func TestInspectPromptBuildsBucketedReport(t *testing.T) {
	workspace := t.TempDir()
	shared := t.TempDir()

	bootstrap := map[string]string{
		"AGENTS.md":   "# Agents\nshared agents\n",
		"SOUL.md":     "# Soul\nshared soul\n",
		"USER.md":     "# User\nshared user\n",
		"IDENTITY.md": "# Identity\nshared identity\n",
		"TOOLS.md":    "# Tools\nshared tools\n",
	}
	for name, content := range bootstrap {
		if err := os.WriteFile(filepath.Join(shared, name), []byte(content), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	if err := os.MkdirAll(filepath.Join(workspace, "memory"), 0755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "memory", "MEMORY.md"), []byte("# Workspace Memory\nremember this\n"), 0644); err != nil {
		t.Fatalf("write memory: %v", err)
	}

	sessionKey := "discord:allen-ernie@abc123"
	sm := session.NewSessionManager(filepath.Join(workspace, "sessions"))
	sess := sm.GetOrCreate(sessionKey)
	sess.Messages = []providers.Message{
		{Role: "user", Content: "earlier user prompt"},
		{Role: "tool", ToolName: "read_file", Content: strings.Repeat("x", 1800)},
		{Role: "assistant", Content: "previous answer"},
		{Role: "user", Content: "latest user prompt"},
	}
	sess.Summary = "Earlier summary"
	if err := sm.Save(sessionKey); err != nil {
		t.Fatalf("save session: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Agents.Defaults.SharedWorkspace = shared

	report, err := InspectPrompt(cfg, PromptInspectOptions{
		SessionKey: sessionKey,
		Workspace:  workspace,
	})
	if err != nil {
		t.Fatalf("InspectPrompt: %v", err)
	}

	if report.SessionKey != sessionKey {
		t.Fatalf("unexpected session key: %q", report.SessionKey)
	}
	if report.Workspace != workspace {
		t.Fatalf("unexpected workspace: %q", report.Workspace)
	}
	if report.Channel != "discord" || report.ChatID != "allen-ernie" {
		t.Fatalf("unexpected route: %s:%s", report.Channel, report.ChatID)
	}
	if report.CurrentUser != "latest user prompt" {
		t.Fatalf("unexpected current user: %q", report.CurrentUser)
	}
	if report.History.MessageCount != 3 {
		t.Fatalf("expected 3 history messages, got %d", report.History.MessageCount)
	}
	if report.SystemPrompt.SummaryChars == 0 {
		t.Fatalf("expected summary block chars to be non-zero")
	}
	if report.SystemPrompt.SessionBlockChars == 0 {
		t.Fatalf("expected current session block chars to be non-zero")
	}
	if len(report.SystemPrompt.Bootstrap) != 5 {
		t.Fatalf("expected 5 bootstrap files, got %d", len(report.SystemPrompt.Bootstrap))
	}
	for _, item := range report.SystemPrompt.Bootstrap {
		if item.SourceWorkspace != shared {
			t.Fatalf("expected bootstrap file %s to come from shared workspace, got %q", item.Name, item.SourceWorkspace)
		}
	}
	if report.SystemPrompt.Memory.SourceWorkspace != workspace {
		t.Fatalf("expected memory to come from workspace, got %q", report.SystemPrompt.Memory.SourceWorkspace)
	}
	if len(report.History.ToolMessages) != 1 {
		t.Fatalf("expected one tool message bucket, got %d", len(report.History.ToolMessages))
	}
	toolBucket := report.History.ToolMessages[0]
	if toolBucket.ToolName != "read_file" {
		t.Fatalf("unexpected tool bucket: %#v", toolBucket)
	}
	if !toolBucket.WouldCompactRawNow {
		t.Fatalf("expected read_file history to be marked as compactable")
	}
	if toolBucket.AlreadyCompacted {
		t.Fatalf("expected raw legacy history, not already-compacted placeholder")
	}
	if report.ToolSchemas.Count == 0 || report.ToolSchemas.TotalChars == 0 {
		t.Fatalf("expected non-empty tool schema report: %#v", report.ToolSchemas)
	}
	if report.Payload.MessagesJSONChars == 0 || report.Payload.ToolSchemasJSONChars == 0 {
		t.Fatalf("expected non-zero payload measurements: %#v", report.Payload)
	}
}

func TestInspectPromptMarksCompactedToolHistory(t *testing.T) {
	workspace := t.TempDir()
	shared := t.TempDir()

	for _, name := range []string{"AGENTS.md", "SOUL.md", "USER.md", "IDENTITY.md", "TOOLS.md"} {
		if err := os.WriteFile(filepath.Join(shared, name), []byte("# "+name+"\n"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	sessionKey := "discord:allen-ernie@compacted"
	sm := session.NewSessionManager(filepath.Join(workspace, "sessions"))
	sess := sm.GetOrCreate(sessionKey)
	sess.Messages = []providers.Message{
		{Role: "user", Content: "earlier user prompt"},
		{Role: "tool", ToolName: "read_file", Content: "Tool output from `read_file` omitted from durable session history for efficiency (1800 chars, 20 lines). Re-run the tool if full content is needed."},
		{Role: "user", Content: "latest user prompt"},
	}
	if err := sm.Save(sessionKey); err != nil {
		t.Fatalf("save session: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Agents.Defaults.SharedWorkspace = shared

	report, err := InspectPrompt(cfg, PromptInspectOptions{
		SessionKey: sessionKey,
		Workspace:  workspace,
	})
	if err != nil {
		t.Fatalf("InspectPrompt: %v", err)
	}

	if len(report.History.ToolMessages) != 1 {
		t.Fatalf("expected one tool message bucket, got %d", len(report.History.ToolMessages))
	}
	toolBucket := report.History.ToolMessages[0]
	if !toolBucket.AlreadyCompacted {
		t.Fatalf("expected tool history to be recognized as already compacted")
	}
	if toolBucket.WouldCompactRawNow {
		t.Fatalf("did not expect already-compacted placeholder to be marked raw-compactable")
	}
}

func TestBuildPromptEnvelopeIncludesLatestUserAndSchemas(t *testing.T) {
	workspace := t.TempDir()
	shared := t.TempDir()

	for _, name := range []string{"AGENTS.md", "SOUL.md", "USER.md", "IDENTITY.md", "TOOLS.md"} {
		if err := os.WriteFile(filepath.Join(shared, name), []byte("# "+name+"\n"), 0644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	sessionKey := "discord:allen-ernie@envelope"
	sm := session.NewSessionManager(filepath.Join(workspace, "sessions"))
	sess := sm.GetOrCreate(sessionKey)
	sess.Messages = []providers.Message{
		{Role: "user", Content: "earlier user prompt"},
		{Role: "assistant", Content: "using tool", ToolCalls: []providers.ToolCall{{ID: "call_1", Name: "read_file", Function: &providers.FunctionCall{Name: "read_file"}}}},
		{Role: "tool", ToolName: "read_file", ToolCallID: "call_1", Content: "file content"},
		{Role: "user", Content: "latest user prompt"},
		{Role: "assistant", Content: "already answered"},
	}
	sess.Summary = "Earlier summary"
	if err := sm.Save(sessionKey); err != nil {
		t.Fatalf("save session: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Workspace = workspace
	cfg.Agents.Defaults.SharedWorkspace = shared

	env, err := BuildPromptEnvelope(cfg, PromptInspectOptions{
		SessionKey: sessionKey,
		Workspace:  workspace,
	})
	if err != nil {
		t.Fatalf("BuildPromptEnvelope: %v", err)
	}
	if env.SessionKey != sessionKey {
		t.Fatalf("unexpected session key: %q", env.SessionKey)
	}
	if env.SystemPrompt == "" {
		t.Fatal("expected system prompt")
	}
	if env.Summary != "Earlier summary" {
		t.Fatalf("unexpected summary: %q", env.Summary)
	}
	if len(env.Messages) != 4 {
		t.Fatalf("expected history plus latest user, got %d", len(env.Messages))
	}
	if got := env.Messages[len(env.Messages)-1].Content; got != "latest user prompt" {
		t.Fatalf("expected latest user in envelope, got %q", got)
	}
	if len(env.ToolSchemas) == 0 {
		t.Fatal("expected tool schemas")
	}
	if env.Options.KeepRecentMessages == 0 {
		t.Fatal("expected non-zero optimize options")
	}
}
