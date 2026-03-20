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
	"github.com/sipeed/picoclaw/pkg/bus"
	"github.com/sipeed/picoclaw/pkg/config"
	"github.com/sipeed/picoclaw/pkg/routing"
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
	configRaw   string
	writtenRaw  string
	installed bool
	running   bool
}

func (e *webTestExec) Mode() tui.Mode { return tui.ModeLocal }
func (e *webTestExec) ExecShell(_ time.Duration, shellCmd string) (string, error) {
	e.command = shellCmd
	return e.output, e.err
}
func (e *webTestExec) ExecCommand(_ time.Duration, _ ...string) (string, error) { return "", nil }
func (e *webTestExec) ReadFile(path string) (string, error) {
	if path == e.ConfigPath() && e.configRaw != "" {
		return e.configRaw, nil
	}
	return "", os.ErrNotExist
}
func (e *webTestExec) WriteFile(path string, data []byte, _ os.FileMode) error {
	if path == e.ConfigPath() {
		e.writtenRaw = string(data)
	}
	return nil
}
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

func TestHandleEmailPersistsManagedSettings(t *testing.T) {
	execStub := &webTestExec{
		configRaw: `{"channels":{"email":{"enabled":false,"provider":"","api_key":"","address":"","display_name":"","base_url":"","allow_from":[],"receive_enabled":true,"receive_mode":"","poll_interval_seconds":0}}}`,
	}
	srv := newWebServer(execStub, "")
	req := httptest.NewRequest(http.MethodPut, "/api/email", strings.NewReader(`{
	  "enabled": true,
	  "address": "support@example.com",
	  "displayName": "",
	  "apiKey": "secret",
	  "baseUrl": "",
	  "allowFrom": ["alice@example.com", "@example.org"]
	}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleEmail(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var reloaded struct {
		Channels struct {
			Email struct {
				Enabled             bool     `json:"enabled"`
				Provider            string   `json:"provider"`
				APIKey              string   `json:"api_key"`
				Address             string   `json:"address"`
				DisplayName         string   `json:"display_name"`
				BaseURL             string   `json:"base_url"`
				AllowFrom           []string `json:"allow_from"`
				ReceiveEnabled      bool     `json:"receive_enabled"`
				ReceiveMode         string   `json:"receive_mode"`
				PollIntervalSeconds int      `json:"poll_interval_seconds"`
			} `json:"email"`
		} `json:"channels"`
	}
	if err := json.Unmarshal([]byte(execStub.writtenRaw), &reloaded); err != nil {
		t.Fatalf("decode written config: %v", err)
	}
	if !reloaded.Channels.Email.Enabled {
		t.Fatalf("expected enabled email config")
	}
	if reloaded.Channels.Email.Provider != "resend" {
		t.Fatalf("provider=%q", reloaded.Channels.Email.Provider)
	}
	if reloaded.Channels.Email.DisplayName != "sciClaw" {
		t.Fatalf("displayName=%q", reloaded.Channels.Email.DisplayName)
	}
	if reloaded.Channels.Email.BaseURL != "https://api.resend.com" {
		t.Fatalf("baseURL=%q", reloaded.Channels.Email.BaseURL)
	}
	if reloaded.Channels.Email.APIKey != "secret" {
		t.Fatalf("apiKey=%q", reloaded.Channels.Email.APIKey)
	}
	if got := reloaded.Channels.Email.AllowFrom; len(got) != 2 || got[0] != "alice@example.com" || got[1] != "@example.org" {
		t.Fatalf("allowFrom=%v", got)
	}
	if reloaded.Channels.Email.ReceiveEnabled {
		t.Fatalf("expected receive to remain disabled")
	}
	if reloaded.Channels.Email.ReceiveMode != "poll" {
		t.Fatalf("receiveMode=%q", reloaded.Channels.Email.ReceiveMode)
	}
	if reloaded.Channels.Email.PollIntervalSeconds != 30 {
		t.Fatalf("pollIntervalSeconds=%d", reloaded.Channels.Email.PollIntervalSeconds)
	}

	execStub.configRaw = execStub.writtenRaw
	req = httptest.NewRequest(http.MethodGet, "/api/email", nil)
	rec = httptest.NewRecorder()
	srv.handleEmail(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 on get, got %d", rec.Code)
	}
	var body struct {
		Enabled             bool     `json:"enabled"`
		Provider            string   `json:"provider"`
		Address             string   `json:"address"`
		DisplayName         string   `json:"displayName"`
		HasAPIKey           bool     `json:"hasApiKey"`
		BaseURL             string   `json:"baseUrl"`
		AllowFrom           []string `json:"allowFrom"`
		ReceiveEnabled      bool     `json:"receiveEnabled"`
		ReceiveMode         string   `json:"receiveMode"`
		PollIntervalSeconds int      `json:"pollIntervalSeconds"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !body.Enabled || body.Provider != "resend" || !body.HasAPIKey || body.ReceiveEnabled || body.ReceiveMode != "poll" || body.PollIntervalSeconds != 30 {
		t.Fatalf("unexpected get response: %#v", body)
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

func TestHandleJobsReadsPersistedLedger(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, ".picoclaw")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	workspaceA := filepath.Join(home, "workspace-a")
	workspaceB := filepath.Join(home, "workspace-b")
	if err := os.MkdirAll(workspaceA, 0o755); err != nil {
		t.Fatalf("mkdir workspace a: %v", err)
	}
	if err := os.MkdirAll(workspaceB, 0o755); err != nil {
		t.Fatalf("mkdir workspace b: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Routing.Enabled = true
	cfg.Routing.Mappings = []config.RoutingMapping{{
		Channel:        "discord",
		ChatID:         "room-1",
		Workspace:      workspaceA,
		AllowedSenders: config.FlexibleStringSlice{"u-1"},
		Label:          "Alpha room",
	}}
	if err := config.SaveConfig(filepath.Join(cfgDir, "config.json"), cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	payload := struct {
		Jobs []routing.JobRecord `json:"jobs"`
	}{
		Jobs: []routing.JobRecord{
			{
				ID:         "job-1",
				ShortID:    "0001A",
				Channel:    "discord",
				ChatID:     "room-1",
				Workspace:  workspaceA,
				RuntimeKey: "cloud",
				TargetKey:  workspaceA + "\x00cloud",
				Class:      routing.JobClassWrite,
				State:      routing.JobStateRunning,
				Phase:      "using_tools",
				Detail:     "Using tool: exec",
				AskSummary: "draft the poster abstract",
				Message: bus.InboundMessage{
					Channel:    "discord",
					SenderID:   "u-1",
					ChatID:     "room-1",
					Content:    "draft the poster abstract",
					SessionKey: "discord:room-1@abc",
					Metadata: map[string]string{
						"display_name": "Ernie",
						"message_id":   "m-1",
					},
				},
				StartedAt: time.Now().Add(-2 * time.Minute).UnixMilli(),
				UpdatedAt: time.Now().Add(-1 * time.Minute).UnixMilli(),
			},
			{
				ID:         "job-2",
				ShortID:    "0001B",
				Channel:    "discord",
				ChatID:     "room-2",
				Workspace:  workspaceB,
				RuntimeKey: "cloud",
				TargetKey:  workspaceB + "\x00cloud",
				Class:      routing.JobClassBTW,
				State:      routing.JobStateFailed,
				Phase:      "failed",
				Detail:     "Job failed",
				LastError:  "anthropic 429",
				AskSummary: "find five RCTs",
				StartedAt:  time.Now().Add(-3 * time.Hour).UnixMilli(),
				UpdatedAt:  time.Now().Add(-2 * time.Hour).UnixMilli(),
			},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal jobs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "jobs.json"), data, 0o644); err != nil {
		t.Fatalf("write jobs: %v", err)
	}

	srv := newWebServer(&webTestExec{}, "")
	req := httptest.NewRequest(http.MethodGet, "/api/jobs", nil)
	rec := httptest.NewRecorder()
	srv.handleJobs(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body struct {
		Summary struct {
			Total    int `json:"total"`
			Active   int `json:"active"`
			Running  int `json:"running"`
			Failed   int `json:"failed"`
			Channels int `json:"distinctChannels"`
			Users    int `json:"distinctUsers"`
		} `json:"summary"`
		Jobs []struct {
			ID         string `json:"id"`
			RouteLabel string `json:"routeLabel"`
			UserName   string `json:"userName"`
			Lane       string `json:"lane"`
			State      string `json:"state"`
		} `json:"jobs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Summary.Total != 2 || body.Summary.Active != 1 || body.Summary.Running != 1 || body.Summary.Failed != 1 {
		t.Fatalf("unexpected summary: %#v", body.Summary)
	}
	if len(body.Jobs) != 2 {
		t.Fatalf("expected 2 jobs, got %#v", body.Jobs)
	}
	if body.Jobs[0].ID != "job-1" || body.Jobs[0].RouteLabel != "Alpha room" || body.Jobs[0].UserName != "Ernie" || body.Jobs[0].Lane != "main" {
		t.Fatalf("unexpected first job: %#v", body.Jobs[0])
	}
	if body.Jobs[1].Lane != "btw" || body.Jobs[1].State != "failed" {
		t.Fatalf("unexpected second job: %#v", body.Jobs[1])
	}
}

func TestHandleJobsRejectsMutationEndpoints(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfgDir := filepath.Join(home, ".picoclaw")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	payload := struct {
		Jobs []routing.JobRecord `json:"jobs"`
	}{
		Jobs: []routing.JobRecord{
			{
				ID:        "done-old",
				State:     routing.JobStateDone,
				UpdatedAt: time.Now().Add(-48 * time.Hour).UnixMilli(),
			},
			{
				ID:        "failed-new",
				State:     routing.JobStateFailed,
				UpdatedAt: time.Now().Add(-2 * time.Hour).UnixMilli(),
			},
			{
				ID:        "running-now",
				State:     routing.JobStateRunning,
				UpdatedAt: time.Now().Add(-5 * time.Minute).UnixMilli(),
			},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal jobs: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "jobs.json"), data, 0o644); err != nil {
		t.Fatalf("write jobs: %v", err)
	}

	srv := newWebServer(&webTestExec{}, "")
	req := httptest.NewRequest(http.MethodPost, "/api/jobs/prune", strings.NewReader(`{"olderThanHours":24}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.handleJobs(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}

	remaining, err := loadJobRecords()
	if err != nil {
		t.Fatalf("reload jobs: %v", err)
	}
	if len(remaining) != 3 {
		t.Fatalf("expected ledger to remain at 3 jobs, got %#v", remaining)
	}
	for i, job := range remaining {
		if job.ID != payload.Jobs[i].ID {
			t.Fatalf("expected ledger to remain unchanged, got %#v", remaining)
		}
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
