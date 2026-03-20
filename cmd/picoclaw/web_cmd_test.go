package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/cmd/picoclaw/tui"
	"github.com/sipeed/picoclaw/pkg/config"
)

func TestNewWebServerServesEmbeddedApp(t *testing.T) {
	srv := newWebServer(nil, "")

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for embedded app, got %d", rec.Code)
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "<!doctype html>") {
		t.Fatalf("expected embedded index.html, got body %q", rec.Body.String())
	}
}

func TestNewWebServerSpaFallbackUsesEmbeddedIndex(t *testing.T) {
	srv := newWebServer(nil, "")

	req := httptest.NewRequest(http.MethodGet, "/routing", nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for SPA fallback, got %d", rec.Code)
	}
	if !strings.Contains(strings.ToLower(rec.Body.String()), "<!doctype html>") {
		t.Fatalf("expected embedded SPA index.html, got body %q", rec.Body.String())
	}
}

type webTestExec struct {
	command string
	output  string
	err     error
	installed bool
	running   bool
}

func (e *webTestExec) Mode() tui.Mode { return tui.ModeLocal }
func (e *webTestExec) ExecShell(_ time.Duration, shellCmd string) (string, error) {
	e.command = shellCmd
	return e.output, e.err
}
func (e *webTestExec) ExecCommand(_ time.Duration, _ ...string) (string, error) { return "", nil }
func (e *webTestExec) ReadFile(_ string) (string, error)                        { return "", os.ErrNotExist }
func (e *webTestExec) WriteFile(_ string, _ []byte, _ os.FileMode) error        { return nil }
func (e *webTestExec) ConfigPath() string                                       { return "/tmp/config.json" }
func (e *webTestExec) AuthPath() string                                         { return "/tmp/auth.json" }
func (e *webTestExec) HomePath() string                                         { return "/Users/tester" }
func (e *webTestExec) BinaryPath() string                                       { return "sciclaw" }
func (e *webTestExec) AgentVersion() string                                     { return "vtest" }
func (e *webTestExec) ServiceInstalled() bool                                   { return e.installed }
func (e *webTestExec) ServiceActive() bool                                      { return e.running }
func (e *webTestExec) InteractiveProcess(_ ...string) *exec.Cmd                 { return exec.Command("true") }

func TestHandleChatSuppressesAgentStderr(t *testing.T) {
	execStub := &webTestExec{output: "hello"}
	srv := newWebServer(execStub, "")

	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"message":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleChat(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(execStub.command, "2>/dev/null") {
		t.Fatalf("expected stderr suppression in command, got %q", execStub.command)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["response"] != "hello" {
		t.Fatalf("unexpected response body: %q", body["response"])
	}
	if body["mode"] != "full" {
		t.Fatalf("expected full mode, got %q", body["mode"])
	}
}

func TestHandleChatUsesLitePathForGreeting(t *testing.T) {
	execStub := &webTestExec{}
	srv := newWebServer(execStub, "")
	liteCalls := 0
	srv.liteChatRunner = func(_ context.Context, message string) (*liteChatResult, error) {
		liteCalls++
		if message != "hello" {
			t.Fatalf("unexpected lite message: %q", message)
		}
		return &liteChatResult{Response: "hi there"}, nil
	}

	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"message":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleChat(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if liteCalls != 1 {
		t.Fatalf("expected lite chat to run once, got %d", liteCalls)
	}
	if execStub.command != "" {
		t.Fatalf("expected full agent path to be skipped, got command %q", execStub.command)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["response"] != "hi there" {
		t.Fatalf("unexpected lite response body: %q", body["response"])
	}
	if body["mode"] != "lite" {
		t.Fatalf("expected lite mode, got %q", body["mode"])
	}
}

func TestHandleChatFallsBackWhenLiteChatFails(t *testing.T) {
	execStub := &webTestExec{output: "fallback"}
	srv := newWebServer(execStub, "")
	srv.liteChatRunner = func(_ context.Context, _ string) (*liteChatResult, error) {
		return nil, os.ErrPermission
	}

	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"message":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleChat(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(execStub.command, " agent ") {
		t.Fatalf("expected fallback full agent command, got %q", execStub.command)
	}

	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["response"] != "fallback" {
		t.Fatalf("unexpected fallback response body: %q", body["response"])
	}
	if body["mode"] != "full" {
		t.Fatalf("expected full mode after lite fallback, got %q", body["mode"])
	}
}

func TestHandleModelsCatalogUsesDiscoverJSON(t *testing.T) {
	execStub := &webTestExec{
		output: `{"provider":"anthropic","source":"endpoint+builtin","models":["claude-sonnet-4.6","gpt-5.4","claude-sonnet-4.6"],"warning":"partial"}`,
	}
	srv := newWebServer(execStub, "")

	req := httptest.NewRequest(http.MethodGet, "/api/models/catalog", nil)
	rec := httptest.NewRecorder()
	srv.handleModelsAction(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(execStub.command, "models discover --json") {
		t.Fatalf("expected discover command, got %q", execStub.command)
	}

	var body struct {
		Provider string `json:"provider"`
		Source   string `json:"source"`
		Warning  string `json:"warning"`
		Models   []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			Provider string `json:"provider"`
			Source   string `json:"source"`
		} `json:"models"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Provider != "anthropic" || body.Source != "endpoint+builtin" || body.Warning != "partial" {
		t.Fatalf("unexpected metadata: %#v", body)
	}
	if len(body.Models) != 2 {
		t.Fatalf("expected deduped models, got %#v", body.Models)
	}
	if body.Models[0].ID != "claude-sonnet-4.6" || body.Models[0].Provider != "anthropic" {
		t.Fatalf("unexpected first model: %#v", body.Models[0])
	}
}

func TestHandleRoutingReadsRealConfigMappings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, ".picoclaw")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	cfg := config.DefaultConfig()
	workspace := filepath.Join(home, "workspace-a")
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		t.Fatalf("mkdir workspace: %v", err)
	}
	cfg.Routing.Enabled = true
	cfg.Routing.UnmappedBehavior = config.RoutingUnmappedBehaviorMentionOnly
	cfg.Routing.Mappings = []config.RoutingMapping{
		{
			Channel:        "discord",
			ChatID:         "12345",
			Workspace:      workspace,
			AllowedSenders: config.FlexibleStringSlice{"u1", "u2"},
			Label:          "Alpha",
		},
	}
	if err := config.SaveConfig(filepath.Join(cfgDir, "config.json"), cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	srv := newWebServer(&webTestExec{}, "")

	req := httptest.NewRequest(http.MethodGet, "/api/routing/status", nil)
	rec := httptest.NewRecorder()
	srv.handleRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status code=%d", rec.Code)
	}
	var status struct {
		Enabled          bool   `json:"enabled"`
		UnmappedBehavior string `json:"unmappedBehavior"`
		TotalMappings    int    `json:"totalMappings"`
		InvalidMappings  int    `json:"invalidMappings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !status.Enabled || status.UnmappedBehavior != config.RoutingUnmappedBehaviorMentionOnly || status.TotalMappings != 1 || status.InvalidMappings != 0 {
		t.Fatalf("unexpected status: %#v", status)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/routing/mappings", nil)
	rec = httptest.NewRecorder()
	srv.handleRouting(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("mappings code=%d", rec.Code)
	}
	var mappings []struct {
		ID             string   `json:"id"`
		Channel        string   `json:"channel"`
		ChatID         string   `json:"chatId"`
		Workspace      string   `json:"workspace"`
		AllowedSenders []string `json:"allowedSenders"`
		Label          string   `json:"label"`
		Mode           string   `json:"mode"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &mappings); err != nil {
		t.Fatalf("decode mappings: %v", err)
	}
	if len(mappings) != 1 {
		t.Fatalf("expected 1 mapping, got %#v", mappings)
	}
	if mappings[0].ID != "discord:12345" || mappings[0].Workspace != workspace || mappings[0].Label != "Alpha" || mappings[0].Mode != "default" {
		t.Fatalf("unexpected mapping: %#v", mappings[0])
	}
}

func TestHandleModelsPutPersistsResolvedProvider(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, ".picoclaw")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	cfg := config.DefaultConfig()
	cfg.Agents.Defaults.Model = "claude-sonnet-4.6"
	cfg.Agents.Defaults.Provider = "anthropic"
	cfg.Providers.OpenAI.AuthMethod = "oauth"
	if err := config.SaveConfig(filepath.Join(cfgDir, "config.json"), cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	srv := newWebServer(&webTestExec{running: true}, "")
	req := httptest.NewRequest(http.MethodPut, "/api/models", strings.NewReader(`{"model":"gpt-5.4"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleModels(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body struct {
		OK              bool   `json:"ok"`
		Model           string `json:"model"`
		Provider        string `json:"provider"`
		RestartRequired bool   `json:"restartRequired"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.OK || body.Model != "gpt-5.4" || body.Provider != "openai" || !body.RestartRequired {
		t.Fatalf("unexpected response: %#v", body)
	}

	reloaded, err := config.LoadConfig(filepath.Join(cfgDir, "config.json"))
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	if reloaded.Agents.Defaults.Model != "gpt-5.4" || reloaded.Agents.Defaults.Provider != "openai" {
		t.Fatalf("unexpected persisted config: model=%q provider=%q", reloaded.Agents.Defaults.Model, reloaded.Agents.Defaults.Provider)
	}
}

func TestShouldUseLightweightWebChat(t *testing.T) {
	tests := []struct {
		message string
		want    bool
	}{
		{message: "hello", want: true},
		{message: "What can you do?", want: true},
		{message: "please search PubMed", want: false},
		{message: "read this file", want: false},
		{message: "https://example.com", want: false},
	}

	for _, tc := range tests {
		if got := shouldUseLightweightWebChat(tc.message); got != tc.want {
			t.Fatalf("shouldUseLightweightWebChat(%q) = %v, want %v", tc.message, got, tc.want)
		}
	}
}
