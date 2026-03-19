package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/cmd/picoclaw/tui"
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
func (e *webTestExec) ServiceInstalled() bool                                   { return false }
func (e *webTestExec) ServiceActive() bool                                      { return false }
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
}
