package tui

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type phiTestExec struct {
	home          string
	configRaw     string
	readErr       error
	writtenRaw    string
	writeErr      error
	shellOut      string
	shellErr      error
	shellMatchOut map[string]string
	shellCommands []string
}

func (e *phiTestExec) Mode() Mode { return ModeLocal }

func (e *phiTestExec) ExecShell(_ time.Duration, cmd string) (string, error) {
	e.shellCommands = append(e.shellCommands, cmd)
	for needle, out := range e.shellMatchOut {
		if strings.Contains(cmd, needle) {
			return out, nil
		}
	}
	if e.shellErr != nil {
		return "", e.shellErr
	}
	return e.shellOut, nil
}

func (e *phiTestExec) ExecCommand(_ time.Duration, _ ...string) (string, error) { return "", nil }

func (e *phiTestExec) ReadFile(path string) (string, error) {
	if e.readErr != nil {
		return "", e.readErr
	}
	if path == e.ConfigPath() {
		return e.configRaw, nil
	}
	return "", os.ErrNotExist
}

func (e *phiTestExec) WriteFile(path string, data []byte, _ os.FileMode) error {
	if e.writeErr != nil {
		return e.writeErr
	}
	if path == e.ConfigPath() {
		e.writtenRaw = string(data)
	}
	return nil
}

func (e *phiTestExec) ConfigPath() string { return "/tmp/config.json" }
func (e *phiTestExec) AuthPath() string   { return "/tmp/auth.json" }

func (e *phiTestExec) HomePath() string {
	if strings.TrimSpace(e.home) == "" {
		return "/Users/tester"
	}
	return e.home
}

func (e *phiTestExec) BinaryPath() string { return "sciclaw" }

func (e *phiTestExec) AgentVersion() string { return "vtest" }

func (e *phiTestExec) ServiceInstalled() bool { return false }
func (e *phiTestExec) ServiceActive() bool    { return false }

func (e *phiTestExec) InteractiveProcess(_ ...string) *exec.Cmd { return exec.Command("true") }

func TestParseModesStatusOutput_Cloud(t *testing.T) {
	msg := phiDataMsg{}
	parseModesStatusOutput(`Mode:     Cloud
Model:    gpt-5.2
Provider: openai
`, &msg)

	if msg.mode != "cloud" {
		t.Fatalf("mode=%q want cloud", msg.mode)
	}
	if msg.cloudModel != "gpt-5.2" {
		t.Fatalf("cloudModel=%q want gpt-5.2", msg.cloudModel)
	}
	if msg.cloudProvider != "openai" {
		t.Fatalf("cloudProvider=%q want openai", msg.cloudProvider)
	}
}

func TestParseModesStatusOutput_Phi(t *testing.T) {
	msg := phiDataMsg{}
	parseModesStatusOutput(`Mode:     PHI (local inference)
Backend:  ollama
Model:    qwen3.5:4b
Preset:   balanced
Hardware: darwin arm64, 16GB RAM, GPU: apple
Status:   running (0.6.0)
`, &msg)

	if msg.mode != "phi" {
		t.Fatalf("mode=%q want phi", msg.mode)
	}
	if msg.localBackend != "ollama" {
		t.Fatalf("localBackend=%q want ollama", msg.localBackend)
	}
	if msg.localModel != "qwen3.5:4b" {
		t.Fatalf("localModel=%q want qwen3.5:4b", msg.localModel)
	}
	if msg.localPreset != "balanced" {
		t.Fatalf("localPreset=%q want balanced", msg.localPreset)
	}
	if msg.backendInstall != "yes" || msg.backendRunning != "yes" {
		t.Fatalf("backend health install=%q running=%q want yes/yes", msg.backendInstall, msg.backendRunning)
	}
	if msg.backendVersion != "0.6.0" {
		t.Fatalf("backendVersion=%q want 0.6.0", msg.backendVersion)
	}
}

func TestParsePhiStatusOutput(t *testing.T) {
	msg := phiDataMsg{}
	parsePhiStatusOutput(`Backend: ollama
Model:   qwen3.5:4b
Installed: true
Running:   false
Version:   0.6.0
Model ready: true
Hardware: linux amd64, 32GB RAM, GPU: nvidia
`, &msg)

	if msg.localBackend != "ollama" {
		t.Fatalf("localBackend=%q want ollama", msg.localBackend)
	}
	if msg.localModel != "qwen3.5:4b" {
		t.Fatalf("localModel=%q want qwen3.5:4b", msg.localModel)
	}
	if msg.backendInstall != "yes" {
		t.Fatalf("backendInstall=%q want yes", msg.backendInstall)
	}
	if msg.backendRunning != "no" {
		t.Fatalf("backendRunning=%q want no", msg.backendRunning)
	}
	if msg.modelReady != "yes" {
		t.Fatalf("modelReady=%q want yes", msg.modelReady)
	}
}

func TestPhiSetLocalDefaultsCmd_WritesConfigAndReloads(t *testing.T) {
	execStub := &phiTestExec{
		configRaw: `{
  "agents": {
    "defaults": {
      "mode": "",
      "model": "gpt-5.2"
    }
  }
}`,
	}
	backend := "ollama"
	model := "qwen3.5:9b"
	preset := "quality"

	msg := phiSetLocalDefaultsCmd(execStub, &backend, &model, &preset)().(phiActionMsg)
	if !msg.ok {
		t.Fatalf("expected success, got %#v", msg)
	}
	for _, want := range []string{
		`"local_backend": "ollama"`,
		`"local_model": "qwen3.5:9b"`,
		`"local_preset": "quality"`,
	} {
		if !strings.Contains(execStub.writtenRaw, want) {
			t.Fatalf("written config missing %q:\n%s", want, execStub.writtenRaw)
		}
	}
	foundReload := false
	for _, cmd := range execStub.shellCommands {
		if strings.Contains(cmd, "routing reload") {
			foundReload = true
			break
		}
	}
	if !foundReload {
		t.Fatalf("expected routing reload command after local defaults update")
	}
}

func TestPhiPullModelCmd_BuildsOllamaPull(t *testing.T) {
	execStub := &phiTestExec{shellOut: "pulled"}
	msg := phiPullModelCmd(execStub, "ollama", "qwen3.5:4b")().(phiActionMsg)
	if !msg.ok {
		t.Fatalf("expected success, got %#v", msg)
	}
	if len(execStub.shellCommands) == 0 || !strings.Contains(execStub.shellCommands[0], "ollama pull 'qwen3.5:4b'") {
		t.Fatalf("unexpected pull command: %#v", execStub.shellCommands)
	}
}

func TestPhiPullModelCmd_RejectsUnsupportedBackend(t *testing.T) {
	execStub := &phiTestExec{}
	msg := phiPullModelCmd(execStub, "mlx", "qwen3.5:4b")().(phiActionMsg)
	if msg.ok {
		t.Fatalf("expected failure for mlx pull, got %#v", msg)
	}
	if !strings.Contains(strings.ToLower(msg.output), "ollama only") {
		t.Fatalf("unexpected output: %q", msg.output)
	}
}

func TestPhiSetupCmd_EscapesHomePath(t *testing.T) {
	execStub := &phiTestExec{
		home:     "/Users/tester/My Home",
		shellOut: "ok",
	}
	msg := phiSetupCmd(execStub)().(phiActionMsg)
	if !msg.ok {
		t.Fatalf("expected success, got %#v", msg)
	}
	if len(execStub.shellCommands) == 0 {
		t.Fatal("expected setup shell command")
	}
	if !strings.Contains(execStub.shellCommands[0], "HOME='/Users/tester/My Home'") {
		t.Fatalf("expected escaped HOME in command, got: %s", execStub.shellCommands[0])
	}
}

func TestPhiInstallOllamaCmd_BuildsBrewInstallScript(t *testing.T) {
	execStub := &phiTestExec{
		home:     "/Users/tester/My Home",
		shellOut: "installed",
	}
	msg := phiInstallOllamaCmd(execStub)().(phiActionMsg)
	if !msg.ok {
		t.Fatalf("expected success, got %#v", msg)
	}
	if len(execStub.shellCommands) == 0 {
		t.Fatal("expected install shell command")
	}
	cmd := execStub.shellCommands[0]
	if !strings.Contains(cmd, "HOME='/Users/tester/My Home'") {
		t.Fatalf("expected escaped HOME in install command, got: %s", cmd)
	}
	if !strings.Contains(cmd, "install ollama") {
		t.Fatalf("expected brew install ollama in command, got: %s", cmd)
	}
	if !strings.Contains(cmd, "/home/linuxbrew/.linuxbrew/bin/brew") {
		t.Fatalf("expected linuxbrew fallback in command, got: %s", cmd)
	}
}

func TestPhiOllamaServiceCmd_StartAndStop(t *testing.T) {
	execStub := &phiTestExec{shellOut: "ok"}
	start := phiOllamaServiceCmd(execStub, "start")().(phiActionMsg)
	if !start.ok {
		t.Fatalf("expected start success, got %#v", start)
	}
	stop := phiOllamaServiceCmd(execStub, "stop")().(phiActionMsg)
	if !stop.ok {
		t.Fatalf("expected stop success, got %#v", stop)
	}
	if len(execStub.shellCommands) < 2 {
		t.Fatalf("expected at least two service commands, got %#v", execStub.shellCommands)
	}
	if !strings.Contains(execStub.shellCommands[0], "services start ollama") {
		t.Fatalf("expected start command, got: %s", execStub.shellCommands[0])
	}
	if !strings.Contains(execStub.shellCommands[1], "services stop ollama") {
		t.Fatalf("expected stop command, got: %s", execStub.shellCommands[1])
	}
}

func TestParsePhiOllamaProbeOutput(t *testing.T) {
	msg := phiDataMsg{}
	parsePhiOllamaProbeOutput("installed:yes\nrunning:no\nversion:ollama version is 0.11.2\n", &msg)
	if msg.backendInstall != "yes" {
		t.Fatalf("backendInstall=%q want yes", msg.backendInstall)
	}
	if msg.backendRunning != "no" {
		t.Fatalf("backendRunning=%q want no", msg.backendRunning)
	}
	if msg.backendVersion != "ollama version is 0.11.2" {
		t.Fatalf("backendVersion=%q", msg.backendVersion)
	}
}

func TestPhiModel_UpdateBlocksConcurrentLongRunningActions(t *testing.T) {
	execStub := &phiTestExec{}
	m := NewPhiModel(execStub)
	m.loaded = true
	m.opInFlight = true
	m.opName = "PHI setup"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}}, nil)
	if cmd != nil {
		t.Fatal("expected nil command while operation is in flight")
	}
	if !next.opInFlight {
		t.Fatal("expected operation state to remain in-flight")
	}
	if !strings.Contains(strings.ToLower(next.flashMsg), "already running") {
		t.Fatalf("expected busy warning, got %q", next.flashMsg)
	}
}

func TestPhiModel_UpdateStartStopInstallSetInFlight(t *testing.T) {
	execStub := &phiTestExec{}
	m := NewPhiModel(execStub)
	m.loaded = true

	nextInstall, cmdInstall := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}}, nil)
	if cmdInstall == nil || !nextInstall.opInFlight || nextInstall.opName != "Ollama install" {
		t.Fatalf("expected install op in flight, got opInFlight=%v opName=%q", nextInstall.opInFlight, nextInstall.opName)
	}

	m2 := NewPhiModel(execStub)
	m2.loaded = true
	nextStart, cmdStart := m2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}}, nil)
	if cmdStart == nil || !nextStart.opInFlight || nextStart.opName != "Start Ollama service" {
		t.Fatalf("expected start op in flight, got opInFlight=%v opName=%q", nextStart.opInFlight, nextStart.opName)
	}

	m3 := NewPhiModel(execStub)
	m3.loaded = true
	nextStop, cmdStop := m3.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}}, nil)
	if cmdStop == nil || !nextStop.opInFlight || nextStop.opName != "Stop Ollama service" {
		t.Fatalf("expected stop op in flight, got opInFlight=%v opName=%q", nextStop.opInFlight, nextStop.opName)
	}
}
