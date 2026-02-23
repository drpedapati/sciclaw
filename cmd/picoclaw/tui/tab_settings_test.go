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
	home             string
	configRaw        string
	readErr          error
	writtenRaw       string
	writeErr         error
	serviceInstalled bool
	serviceActive    bool
	serviceStatusOut string
	serviceStatusErr error
	shellOut         string
	shellErr         error
	shellCommands    []string
}

func (e *settingsTestExec) Mode() Mode { return ModeLocal }

func (e *settingsTestExec) ExecShell(_ time.Duration, cmd string) (string, error) {
	e.shellCommands = append(e.shellCommands, cmd)
	if strings.Contains(cmd, "sciclaw service status") {
		return e.serviceStatusOut, e.serviceStatusErr
	}
	if e.shellErr != nil {
		return "", e.shellErr
	}
	if strings.TrimSpace(e.shellOut) != "" {
		return e.shellOut, nil
	}
	return "", nil
}

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

func (e *settingsTestExec) ServiceInstalled() bool { return e.serviceInstalled }

func (e *settingsTestExec) ServiceActive() bool { return e.serviceActive }

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

func TestSettingsModel_ServiceAutoStartToggleUsesInstallUninstall(t *testing.T) {
	t.Run("install when disabled", func(t *testing.T) {
		execStub := &settingsTestExec{}
		m := NewSettingsModel(execStub)
		m.loaded = true
		snap := &VMSnapshot{ServiceAutoStart: false}
		m.selectedRow = rowIndexByKey(t, m.buildDisplayRows(snap), "svc_autostart")

		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}, snap)
		if cmd == nil {
			t.Fatalf("expected service install command")
		}
		msg, ok := cmd().(serviceActionMsg)
		if !ok {
			t.Fatalf("expected serviceActionMsg")
		}
		if msg.action != "install" {
			t.Fatalf("expected install action, got %q", msg.action)
		}
		if !containsCommand(execStub.shellCommands, "service install") {
			t.Fatalf("expected install command to run, got %v", execStub.shellCommands)
		}
	})

	t.Run("uninstall when enabled", func(t *testing.T) {
		execStub := &settingsTestExec{}
		m := NewSettingsModel(execStub)
		m.loaded = true
		snap := &VMSnapshot{ServiceAutoStart: true}
		m.selectedRow = rowIndexByKey(t, m.buildDisplayRows(snap), "svc_autostart")

		_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}, snap)
		if cmd == nil {
			t.Fatalf("expected service uninstall command")
		}
		msg, ok := cmd().(serviceActionMsg)
		if !ok {
			t.Fatalf("expected serviceActionMsg")
		}
		if msg.action != "uninstall" {
			t.Fatalf("expected uninstall action, got %q", msg.action)
		}
		if !containsCommand(execStub.shellCommands, "service uninstall") {
			t.Fatalf("expected uninstall command to run, got %v", execStub.shellCommands)
		}
	})
}

func TestSettingsModel_ServiceQuickActions(t *testing.T) {
	execStub := &settingsTestExec{}
	m := NewSettingsModel(execStub)
	m.loaded = true
	snap := &VMSnapshot{}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}}, snap)
	if cmd == nil {
		t.Fatalf("expected start service command")
	}
	startMsg, ok := cmd().(serviceActionMsg)
	if !ok {
		t.Fatalf("expected serviceActionMsg for start")
	}
	if startMsg.action != "start" {
		t.Fatalf("expected start action, got %q", startMsg.action)
	}
	if !containsCommand(execStub.shellCommands, "service start") {
		t.Fatalf("expected start command to run, got %v", execStub.shellCommands)
	}

	_, cmd = next.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'t'}}, snap)
	if cmd == nil {
		t.Fatalf("expected stop service command")
	}
	stopMsg, ok := cmd().(serviceActionMsg)
	if !ok {
		t.Fatalf("expected serviceActionMsg for stop")
	}
	if stopMsg.action != "stop" {
		t.Fatalf("expected stop action, got %q", stopMsg.action)
	}
	if !containsCommand(execStub.shellCommands, "service stop") {
		t.Fatalf("expected stop command to run, got %v", execStub.shellCommands)
	}
}

func TestSettingsModel_RestartIndicatorsForAgentSettings(t *testing.T) {
	execStub := &settingsTestExec{}
	m := NewSettingsModel(execStub)
	m.loaded = true
	m.defaultModel = "gpt-4o"
	m.reasoningEffort = ""

	runningSnap := &VMSnapshot{ServiceRunning: true}
	if rowByKey(t, m.buildDisplayRows(runningSnap), "default_model").restartRequired {
		t.Fatalf("did not expect model restart indicator before change")
	}
	if rowByKey(t, m.buildDisplayRows(runningSnap), "reasoning_effort").restartRequired {
		t.Fatalf("did not expect reasoning restart indicator before change")
	}

	m.editKey = "default_model"
	_ = m.applyTextEdit("gpt-5")
	_ = m.cycleEnum("reasoning_effort", "default", []string{"", "low", "medium", "high"})

	if !rowByKey(t, m.buildDisplayRows(runningSnap), "default_model").restartRequired {
		t.Fatalf("expected model restart indicator after change")
	}
	if !rowByKey(t, m.buildDisplayRows(runningSnap), "reasoning_effort").restartRequired {
		t.Fatalf("expected reasoning restart indicator after change")
	}

	stoppedSnap := &VMSnapshot{ServiceRunning: false}
	if rowByKey(t, m.buildDisplayRows(stoppedSnap), "default_model").restartRequired {
		t.Fatalf("did not expect model restart indicator while service is stopped")
	}
	if rowByKey(t, m.buildDisplayRows(stoppedSnap), "reasoning_effort").restartRequired {
		t.Fatalf("did not expect reasoning restart indicator while service is stopped")
	}

	// Successful restart clears pending indicators.
	m.HandleServiceAction(serviceActionMsg{action: "restart", ok: true})
	if rowByKey(t, m.buildDisplayRows(runningSnap), "default_model").restartRequired {
		t.Fatalf("expected model restart indicator cleared after restart")
	}
	if rowByKey(t, m.buildDisplayRows(runningSnap), "reasoning_effort").restartRequired {
		t.Fatalf("expected reasoning restart indicator cleared after restart")
	}
}

func TestSettingsView_IncludesServiceQuickActionKeybindings(t *testing.T) {
	m := NewSettingsModel(&settingsTestExec{})
	m.loaded = true
	view := m.View(&VMSnapshot{}, 100)
	if !strings.Contains(view, "Start service") {
		t.Fatalf("expected start-service keybinding in view")
	}
	if !strings.Contains(view, "Stop service") {
		t.Fatalf("expected stop-service keybinding in view")
	}
}

func TestCollectServiceState_ParsesEnabled(t *testing.T) {
	execStub := &settingsTestExec{
		serviceStatusOut: "Installed: yes\nRunning: no\nEnabled: yes\n",
	}
	installed, running, autoStart := collectServiceState(execStub)
	if !installed {
		t.Fatalf("expected installed=true")
	}
	if running {
		t.Fatalf("expected running=false")
	}
	if !autoStart {
		t.Fatalf("expected autoStart=true from Enabled: yes")
	}
}

func TestCollectServiceState_FallsBackWhenStatusFails(t *testing.T) {
	execStub := &settingsTestExec{
		serviceStatusErr: os.ErrPermission,
		serviceInstalled: true,
		serviceActive:    false,
	}
	installed, running, autoStart := collectServiceState(execStub)
	if !installed {
		t.Fatalf("expected installed=true from fallback")
	}
	if running {
		t.Fatalf("expected running=false from fallback")
	}
	if !autoStart {
		t.Fatalf("expected autoStart to follow installed fallback")
	}
}

func containsCommand(commands []string, want string) bool {
	for _, cmd := range commands {
		if strings.Contains(cmd, want) {
			return true
		}
	}
	return false
}

func rowIndexByKey(t *testing.T, rows []settingRow, key string) int {
	t.Helper()
	for i, row := range rows {
		if row.key == key {
			return i
		}
	}
	t.Fatalf("missing row %q", key)
	return -1
}

func rowByKey(t *testing.T, rows []settingRow, key string) settingRow {
	t.Helper()
	idx := rowIndexByKey(t, rows, key)
	return rows[idx]
}
