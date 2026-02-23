package tui

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type settingsTestExec struct {
	home       string
	configRaw  string
	readErr    error
	writtenRaw string
	writeErr   error
}

func (e *settingsTestExec) Mode() Mode { return ModeLocal }

func (e *settingsTestExec) ExecShell(_ time.Duration, _ string) (string, error) { return "", nil }

func (e *settingsTestExec) ExecCommand(_ time.Duration, _ ...string) (string, error) { return "", nil }

func (e *settingsTestExec) ReadFile(path string) (string, error) {
	if e.readErr != nil {
		return "", e.readErr
	}
	if path == e.ConfigPath() {
		return e.configRaw, nil
	}
	return "", os.ErrNotExist
}

func (e *settingsTestExec) WriteFile(path string, data []byte, _ os.FileMode) error {
	if e.writeErr != nil {
		return e.writeErr
	}
	if path == e.ConfigPath() {
		e.writtenRaw = string(data)
	}
	return nil
}

func (e *settingsTestExec) ConfigPath() string { return "/tmp/config.json" }

func (e *settingsTestExec) AuthPath() string { return "/tmp/auth.json" }

func (e *settingsTestExec) HomePath() string {
	if strings.TrimSpace(e.home) == "" {
		return "/Users/tester"
	}
	return e.home
}

func (e *settingsTestExec) AgentVersion() string { return "vtest" }

func (e *settingsTestExec) ServiceInstalled() bool { return false }

func (e *settingsTestExec) ServiceActive() bool { return false }

func (e *settingsTestExec) InteractiveProcess(_ ...string) *exec.Cmd { return exec.Command("true") }

func TestSettingsToggleChannel_ReadFailureDoesNotWrite(t *testing.T) {
	execStub := &settingsTestExec{readErr: os.ErrNotExist}
	msg := settingsToggleChannel(execStub, "discord", false)().(actionDoneMsg)
	if !strings.Contains(strings.ToLower(msg.output), "failed to load config") {
		t.Fatalf("expected load-config failure message, got %q", msg.output)
	}
	if execStub.writtenRaw != "" {
		t.Fatalf("expected no write on read failure, wrote %q", execStub.writtenRaw)
	}
}

func TestSettingsToggleChannel_PreservesExistingFields(t *testing.T) {
	execStub := &settingsTestExec{
		configRaw: `{
  "channels": {
    "discord": {
      "enabled": true,
      "token": "abc123",
      "allow_from": ["u1"]
    }
  }
}`,
	}
	msg := settingsToggleChannel(execStub, "discord", false)().(actionDoneMsg)
	if !strings.Contains(strings.ToLower(msg.output), "discord disabled") {
		t.Fatalf("expected disable confirmation message, got %q", msg.output)
	}
	if !strings.Contains(execStub.writtenRaw, `"token": "abc123"`) {
		t.Fatalf("expected token to be preserved, wrote %q", execStub.writtenRaw)
	}
	if !strings.Contains(execStub.writtenRaw, `"enabled": false`) {
		t.Fatalf("expected enabled=false in written config, wrote %q", execStub.writtenRaw)
	}
}

func TestSettingsModel_DisableChannelRequiresConfirmation(t *testing.T) {
	execStub := &settingsTestExec{
		configRaw: `{"channels":{"discord":{"enabled":true,"token":"abc"}}}`,
	}
	m := NewSettingsModel(execStub)
	m.loaded = true
	m.discordEnabled = true

	nextAny, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}, nil)
	if cmd != nil {
		t.Fatalf("expected no command before disable confirmation")
	}
	next := nextAny
	if next.mode != settingsConfirmDisable {
		t.Fatalf("expected confirm-disable mode, got %v", next.mode)
	}
	if next.pendingToggleKey != "discord_enabled" {
		t.Fatalf("expected pending discord toggle, got %q", next.pendingToggleKey)
	}

	nextAny, cmd = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}, nil)
	if cmd == nil {
		t.Fatalf("expected toggle command after confirming disable")
	}
	next = nextAny
	if next.mode != settingsNormal {
		t.Fatalf("expected normal mode after confirmation, got %v", next.mode)
	}
	if next.discordEnabled {
		t.Fatalf("expected local discordEnabled state to be false after confirmation")
	}
}
