package tui

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type emailTestExec struct {
	configRaw     string
	readErr       error
	writtenRaw    string
	writeErr      error
	commandOut    string
	commandErr    error
	commandArgs   []string
}

func (e *emailTestExec) Mode() Mode { return ModeLocal }
func (e *emailTestExec) ExecShell(_ time.Duration, _ string) (string, error) { return "", nil }
func (e *emailTestExec) ExecCommand(_ time.Duration, args ...string) (string, error) {
	e.commandArgs = append([]string(nil), args...)
	if e.commandErr != nil {
		return e.commandOut, e.commandErr
	}
	return e.commandOut, nil
}
func (e *emailTestExec) ReadFile(path string) (string, error) {
	if e.readErr != nil {
		return "", e.readErr
	}
	if path == e.ConfigPath() {
		return e.configRaw, nil
	}
	return "", os.ErrNotExist
}
func (e *emailTestExec) WriteFile(path string, data []byte, _ os.FileMode) error {
	if e.writeErr != nil {
		return e.writeErr
	}
	if path == e.ConfigPath() {
		e.writtenRaw = string(data)
	}
	return nil
}
func (e *emailTestExec) ConfigPath() string { return "/tmp/config.json" }
func (e *emailTestExec) AuthPath() string   { return "/tmp/auth.json" }
func (e *emailTestExec) HomePath() string   { return "/tmp" }
func (e *emailTestExec) BinaryPath() string { return "sciclaw" }
func (e *emailTestExec) AgentVersion() string { return "vtest" }
func (e *emailTestExec) ServiceInstalled() bool { return true }
func (e *emailTestExec) ServiceActive() bool { return true }
func (e *emailTestExec) InteractiveProcess(_ ...string) *exec.Cmd { return exec.Command("true") }

func TestFetchEmailDataParsesConfig(t *testing.T) {
	execStub := &emailTestExec{
		configRaw: `{
  "channels": {
    "email": {
      "enabled": true,
      "provider": "resend",
      "api_key": "secret",
      "address": "support@example.com",
      "display_name": "sciClaw",
      "base_url": "https://resend.example.com/api",
      "allow_from": ["alice@example.com", "@example.com"],
      "receive_enabled": false
    }
  }
}`,
	}
	msg := fetchEmailData(execStub)().(emailDataMsg)
	if msg.err != nil {
		t.Fatalf("fetchEmailData error: %v", msg.err)
	}
	if !msg.enabled || !msg.keyPresent {
		t.Fatalf("enabled=%v keyPresent=%v", msg.enabled, msg.keyPresent)
	}
	if msg.address != "support@example.com" {
		t.Fatalf("address=%q", msg.address)
	}
	if len(msg.allowFrom) != 2 {
		t.Fatalf("allowFrom=%v", msg.allowFrom)
	}
}

func TestEmailModelToggleEnabledWritesConfig(t *testing.T) {
	execStub := &emailTestExec{
		configRaw: `{"channels":{"email":{"enabled":false,"provider":"resend","base_url":"https://api.resend.com"}}}`,
	}
	m := NewEmailModel(execStub)
	m.loaded = true
	m.enabled = false
	msg := m.toggleEnabled()().(emailActionMsg)
	if !msg.ok {
		t.Fatalf("toggle failed: %#v", msg)
	}
	if !strings.Contains(execStub.writtenRaw, `"enabled": true`) {
		t.Fatalf("written config missing enabled=true: %s", execStub.writtenRaw)
	}
}

func TestEmailModelSendTestEmailUsesRecipient(t *testing.T) {
	execStub := &emailTestExec{commandOut: "Test email sent"}
	m := NewEmailModel(execStub)
	m.testRecipient = "person@example.com"

	msg := m.sendTestEmail()().(emailActionMsg)
	if !msg.ok {
		t.Fatalf("expected success: %#v", msg)
	}
	args := strings.Join(execStub.commandArgs, " ")
	if !strings.Contains(args, "channels test email --to person@example.com") {
		t.Fatalf("unexpected command args: %v", execStub.commandArgs)
	}
}
