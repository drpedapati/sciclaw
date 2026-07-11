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
	readFiles     map[string]string
	readErr       error
	writtenRaw    string
	writtenFiles  map[string]string
	writeErr      error
	shellOut      string
	shellErr      error
	shellMatchOut map[string]string
	shellCommands []string
	shellTimeouts []time.Duration
}

func (e *phiTestExec) Mode() Mode { return ModeLocal }

func (e *phiTestExec) ExecShell(timeout time.Duration, cmd string) (string, error) {
	e.shellTimeouts = append(e.shellTimeouts, timeout)
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
	if out, ok := e.readFiles[path]; ok {
		return out, nil
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
	if e.writtenFiles == nil {
		e.writtenFiles = map[string]string{}
	}
	e.writtenFiles[path] = string(data)
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
Model:    gpt-5.6-sol
Provider: openai
`, &msg)

	if msg.mode != "cloud" {
		t.Fatalf("mode=%q want cloud", msg.mode)
	}
	if msg.cloudModel != "gpt-5.6-sol" {
		t.Fatalf("cloudModel=%q want gpt-5.6-sol", msg.cloudModel)
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
      "model": "gpt-5.6-sol"
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

func TestPhiSetLocalDefaultsCmd_RejectsUnsupportedBackend(t *testing.T) {
	execStub := &phiTestExec{
		configRaw: `{"agents":{"defaults":{"mode":"","model":"gpt-5.6-sol"}}}`,
	}
	backend := "mlx"
	msg := phiSetLocalDefaultsCmd(execStub, &backend, nil, nil)().(phiActionMsg)
	if msg.ok {
		t.Fatalf("expected failure, got %#v", msg)
	}
	if !strings.Contains(strings.ToLower(msg.output), "use ollama") {
		t.Fatalf("unexpected output: %q", msg.output)
	}
}

func TestPhiPullModelCmd_BuildsOllamaPull(t *testing.T) {
	execStub := &phiTestExec{shellOut: "pulled"}
	msg := phiPullModelCmd(execStub, "ollama", "qwen3.5:4b")().(phiActionMsg)
	if !msg.ok {
		t.Fatalf("expected success, got %#v", msg)
	}
	if len(execStub.shellCommands) == 0 || !strings.Contains(execStub.shellCommands[0], "pull") {
		t.Fatalf("unexpected pull command: %#v", execStub.shellCommands)
	}
	if !strings.Contains(execStub.shellCommands[0], "qwen3.5:4b") {
		t.Fatalf("expected model name in pull command, got: %s", execStub.shellCommands[0])
	}
	if !strings.Contains(execStub.shellCommands[0], "OLLAMA_BIN") {
		t.Fatalf("expected ollama path resolution script, got: %s", execStub.shellCommands[0])
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
	if !strings.Contains(execStub.shellCommands[0], "OP=") || !strings.Contains(execStub.shellCommands[0], "start") {
		t.Fatalf("expected start command, got: %s", execStub.shellCommands[0])
	}
	if !strings.Contains(execStub.shellCommands[1], "OP=") || !strings.Contains(execStub.shellCommands[1], "stop") {
		t.Fatalf("expected stop command, got: %s", execStub.shellCommands[1])
	}
}

func TestPhiEvalCmd_BuildsEvalCommand(t *testing.T) {
	execStub := &phiTestExec{
		home:     "/Users/tester/My Home",
		shellOut: "pass",
	}
	msg := phiEvalCmd(execStub)().(phiActionMsg)
	if !msg.ok {
		t.Fatalf("expected success, got %#v", msg)
	}
	if len(execStub.shellCommands) == 0 {
		t.Fatal("expected eval shell command")
	}
	cmd := execStub.shellCommands[0]
	if !strings.Contains(cmd, "HOME='/Users/tester/My Home'") {
		t.Fatalf("expected escaped HOME in eval command, got: %s", cmd)
	}
	if !strings.Contains(cmd, "modes phi-eval --json") {
		t.Fatalf("expected phi-eval command, got: %s", cmd)
	}
	if len(execStub.shellTimeouts) == 0 || execStub.shellTimeouts[0] != 11*time.Minute {
		t.Fatalf("expected 11 minute eval timeout, got %#v", execStub.shellTimeouts)
	}
}

func TestParsePhiEvalSummary_GoodInteractive(t *testing.T) {
	summary, ok := parsePhiEvalSummary(`{
  "backend": "ollama",
  "model": "qwen3.5:9b",
  "evaluated_at": "2026-03-09T15:04:05Z",
  "results": [
    {"name":"text","passed":true,"duration_ms":3018},
    {"name":"json","passed":true,"duration_ms":371},
    {"name":"extract","passed":true,"duration_ms":622,"fallback_used":true},
    {"name":"tool","passed":true,"duration_ms":1278}
  ]
}`)
	if !ok || summary == nil {
		t.Fatal("expected eval summary")
	}
	if summary.Label != "good interactive" {
		t.Fatalf("label=%q", summary.Label)
	}
	if !strings.Contains(summary.Timings, "text 3.0s") {
		t.Fatalf("timings=%q", summary.Timings)
	}
	if summary.Backend != "ollama" || summary.Model != "qwen3.5:9b" {
		t.Fatalf("backend/model=%q/%q", summary.Backend, summary.Model)
	}
	if !strings.Contains(summary.ProbeStatus, "extract ok") {
		t.Fatalf("probeStatus=%q", summary.ProbeStatus)
	}
	if summary.Recovery != "extract" {
		t.Fatalf("recovery=%q want extract", summary.Recovery)
	}
	if summary.LastEval == "" {
		t.Fatal("expected formatted last eval time")
	}
}

func TestParsePhiEvalSummary_FallbackOnly(t *testing.T) {
	summary, ok := parsePhiEvalSummary(`{
  "results": [
    {"name":"text","passed":true,"duration_ms":26930},
    {"name":"json","passed":true,"duration_ms":1586},
    {"name":"extract","passed":true,"duration_ms":2430},
    {"name":"tool","passed":true,"duration_ms":8658}
  ]
}`)
	if !ok || summary == nil {
		t.Fatal("expected eval summary")
	}
	if summary.Label != "fallback only" {
		t.Fatalf("label=%q", summary.Label)
	}
}

func TestParsePhiEvalSummary_ExtractLatencyAffectsLabel(t *testing.T) {
	summary, ok := parsePhiEvalSummary(`{
  "results": [
    {"name":"text","passed":true,"duration_ms":3018},
    {"name":"json","passed":true,"duration_ms":371},
    {"name":"extract","passed":true,"duration_ms":4100},
    {"name":"tool","passed":true,"duration_ms":1278}
  ]
}`)
	if !ok || summary == nil {
		t.Fatal("expected eval summary")
	}
	if summary.Label != "usable, slower" {
		t.Fatalf("label=%q", summary.Label)
	}
}

func TestParsePhiEvalSummary_IncompleteOutputNeedsAttention(t *testing.T) {
	summary, ok := parsePhiEvalSummary(`{
  "results": [
    {"name":"json","passed":true,"duration_ms":371},
    {"name":"tool","passed":true,"duration_ms":1278}
  ]
}`)
	if !ok || summary == nil {
		t.Fatal("expected eval summary")
	}
	if summary.Label != "needs attention" {
		t.Fatalf("label=%q", summary.Label)
	}
}

func TestPhiModelHandleAction_EvalStoresSummary(t *testing.T) {
	execStub := &phiTestExec{}
	m := NewPhiModel(execStub)
	m.HandleAction(phiActionMsg{
		action: "eval",
		ok:     true,
		output: `{"backend":"ollama","model":"qwen3.5:9b","evaluated_at":"2026-03-09T15:04:05Z","results":[{"name":"text","passed":true,"duration_ms":3018},{"name":"json","passed":true,"duration_ms":371},{"name":"extract","passed":true,"duration_ms":622,"fallback_used":true},{"name":"tool","passed":true,"duration_ms":1278}]}`,
	})
	if m.eval == nil {
		t.Fatal("expected eval summary to be stored")
	}
	if m.eval.Label != "good interactive" {
		t.Fatalf("label=%q", m.eval.Label)
	}
	if got := execStub.writtenFiles[phiEvalStatePath(execStub)]; !strings.Contains(got, `"backend": "ollama"`) {
		t.Fatalf("expected persisted eval file, got %q", got)
	}
}

func TestPhiModelHandleAction_EvalParseFailureBecomesFailureState(t *testing.T) {
	execStub := &phiTestExec{}
	m := NewPhiModel(execStub)
	m.localBackend = "ollama"
	m.localModel = "qwen3.5:4b"
	m.localPreset = "balanced"
	m.HandleAction(phiActionMsg{
		action: "eval",
		ok:     true,
		output: "not-json",
	})
	if m.eval == nil {
		t.Fatal("expected failure summary")
	}
	if m.eval.Label != "needs attention" {
		t.Fatalf("label=%q", m.eval.Label)
	}
	if !strings.Contains(strings.ToLower(m.flashMsg), "failed") {
		t.Fatalf("expected failure flash, got %q", m.flashMsg)
	}
	if got := execStub.writtenFiles[phiEvalStatePath(execStub)]; !strings.Contains(got, "invalid eval output") {
		t.Fatalf("expected persisted invalid-output failure, got %q", got)
	}
}

func TestPhiModelHandleAction_EvalFailurePersistsFailureSummary(t *testing.T) {
	execStub := &phiTestExec{}
	m := NewPhiModel(execStub)
	m.localBackend = "ollama"
	m.localModel = "qwen3.5:4b"
	m.HandleAction(phiActionMsg{
		action: "eval",
		ok:     false,
		output: "tool round-trip timed out",
	})
	if m.eval == nil {
		t.Fatal("expected eval summary")
	}
	if m.eval.Label != "needs attention" {
		t.Fatalf("label=%q", m.eval.Label)
	}
	if !strings.Contains(strings.ToLower(m.eval.LastError), "timed out") {
		t.Fatalf("lastError=%q", m.eval.LastError)
	}
	if got := execStub.writtenFiles[phiEvalStatePath(execStub)]; !strings.Contains(got, `"error": "tool round-trip timed out"`) {
		t.Fatalf("expected persisted failure eval, got %q", got)
	}
}

func TestFetchPhiData_LoadsPersistedEvalSummary(t *testing.T) {
	execStub := &phiTestExec{
		configRaw: `{"agents":{"defaults":{"mode":"phi","local_backend":"ollama","local_model":"qwen3.5:9b","local_preset":"quality"}}}`,
		readFiles: map[string]string{
			phiEvalStatePath(&phiTestExec{}): `{"backend":"ollama","model":"qwen3.5:9b","preset":"quality","evaluated_at":"2026-03-09T15:04:05Z","results":[{"name":"text","passed":true,"duration_ms":3018},{"name":"json","passed":true,"duration_ms":371},{"name":"extract","passed":true,"duration_ms":622},{"name":"tool","passed":true,"duration_ms":1278}]}`,
		},
	}
	msg := fetchPhiData(execStub)().(phiDataMsg)
	if msg.eval == nil {
		t.Fatal("expected cached eval summary")
	}
	if msg.eval.Model != "qwen3.5:9b" {
		t.Fatalf("model=%q", msg.eval.Model)
	}
	if msg.eval.Label != "good interactive" {
		t.Fatalf("label=%q", msg.eval.Label)
	}
}

func TestFetchPhiData_StalePersistedEvalNeedsAttention(t *testing.T) {
	execStub := &phiTestExec{
		configRaw: `{"agents":{"defaults":{"mode":"phi","local_backend":"ollama","local_model":"qwen3.5:9b","local_preset":"quality"}}}`,
		readFiles: map[string]string{
			phiEvalStatePath(&phiTestExec{}): `{"backend":"ollama","model":"qwen3.5:4b","preset":"balanced","evaluated_at":"2026-03-09T15:04:05Z","results":[{"name":"text","passed":true,"duration_ms":3018},{"name":"json","passed":true,"duration_ms":371},{"name":"extract","passed":true,"duration_ms":622},{"name":"tool","passed":true,"duration_ms":1278}]}`,
		},
	}
	msg := fetchPhiData(execStub)().(phiDataMsg)
	if msg.eval == nil {
		t.Fatal("expected cached eval summary")
	}
	if msg.eval.Label != "needs attention" {
		t.Fatalf("label=%q", msg.eval.Label)
	}
	if !strings.Contains(strings.ToLower(msg.eval.Detail), "different local setup") {
		t.Fatalf("detail=%q", msg.eval.Detail)
	}
}

func TestFetchPhiData_DefaultPresetDoesNotMarkMatchingEvalStale(t *testing.T) {
	execStub := &phiTestExec{
		configRaw: `{"agents":{"defaults":{"mode":"phi","local_model":"qwen3.5:4b"}}}`,
		readFiles: map[string]string{
			phiEvalStatePath(&phiTestExec{}): `{"backend":"ollama","model":"qwen3.5:4b","preset":"balanced","evaluated_at":"2026-03-09T15:04:05Z","results":[{"name":"text","passed":true,"duration_ms":3018},{"name":"json","passed":true,"duration_ms":371},{"name":"extract","passed":true,"duration_ms":622},{"name":"tool","passed":true,"duration_ms":1278}]}`,
		},
	}
	msg := fetchPhiData(execStub)().(phiDataMsg)
	if msg.eval == nil {
		t.Fatal("expected cached eval summary")
	}
	if msg.eval.Label == "needs attention" && strings.Contains(strings.ToLower(msg.eval.ProbeStatus), "stale") {
		t.Fatalf("expected matching defaulted runtime, got stale summary: %+v", msg.eval)
	}
}

func TestHandleData_ClearsEvalWhenNoCachedSummaryPresent(t *testing.T) {
	m := NewPhiModel(&phiTestExec{})
	m.eval = &phiEvalSummary{Label: "good interactive"}
	m.HandleData(phiDataMsg{})
	if m.eval != nil {
		t.Fatalf("expected eval to be cleared, got %+v", m.eval)
	}
}

func TestPhiModelHandleAction_SetLocalClearsCachedEval(t *testing.T) {
	execStub := &phiTestExec{}
	m := NewPhiModel(execStub)
	m.eval = &phiEvalSummary{Label: "good interactive"}
	m.HandleAction(phiActionMsg{action: "set-local", ok: true, output: "updated"})
	if m.eval != nil {
		t.Fatalf("expected eval cleared, got %+v", m.eval)
	}
	if got := execStub.writtenFiles[phiEvalStatePath(execStub)]; strings.TrimSpace(got) != "{}" {
		t.Fatalf("expected cleared eval cache, got %q", got)
	}
}

func TestPhiModelHandleAction_SetLocalFailureKeepsCachedEval(t *testing.T) {
	execStub := &phiTestExec{}
	m := NewPhiModel(execStub)
	m.eval = &phiEvalSummary{Label: "good interactive"}
	m.HandleAction(phiActionMsg{action: "set-local", ok: false, output: "save failed"})
	if m.eval == nil || m.eval.Label != "good interactive" {
		t.Fatalf("expected eval to remain, got %+v", m.eval)
	}
	if _, ok := execStub.writtenFiles[phiEvalStatePath(execStub)]; ok {
		t.Fatalf("expected eval cache to remain untouched on failure")
	}
}

func TestPhiPerformanceExpectation_CPUOnlyFallbackMessaging(t *testing.T) {
	note := phiPerformanceExpectation("linux amd64, 16GB RAM, GPU: none", &phiEvalSummary{Label: "fallback only"})
	if !strings.Contains(strings.ToLower(note), "cpu-only") {
		t.Fatalf("expected cpu-only wording, got %q", note)
	}
	if !strings.Contains(strings.ToLower(note), "shorter private tasks") {
		t.Fatalf("expected fallback-lane guidance, got %q", note)
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

	m4 := NewPhiModel(execStub)
	m4.loaded = true
	nextEval, cmdEval := m4.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}, nil)
	if cmdEval == nil || !nextEval.opInFlight || nextEval.opName != "PHI local eval" {
		t.Fatalf("expected eval op in flight, got opInFlight=%v opName=%q", nextEval.opInFlight, nextEval.opName)
	}
}

func TestPhiModel_UpdateBackendKeyWarnsWhenAlreadyOnOllama(t *testing.T) {
	execStub := &phiTestExec{}
	m := NewPhiModel(execStub)
	m.loaded = true
	m.localBackend = "ollama"

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}}, nil)
	if cmd != nil {
		t.Fatal("expected no command when backend toggle is disabled")
	}
	if len(execStub.shellCommands) != 0 {
		t.Fatalf("expected no shell commands, got %#v", execStub.shellCommands)
	}
	if !strings.Contains(strings.ToLower(next.flashMsg), "ollama is the supported local backend") {
		t.Fatalf("unexpected flash message: %q", next.flashMsg)
	}
}

func TestPhiPerformanceExpectation_CPUOnlyWithoutEval(t *testing.T) {
	note := phiPerformanceExpectation("linux amd64, 16GB RAM, GPU: none", nil)
	if !strings.Contains(strings.ToLower(note), "cpu-only machine") {
		t.Fatalf("note=%q", note)
	}
	if !strings.Contains(note, "Run [e]") {
		t.Fatalf("note=%q", note)
	}
}

func TestPhiPerformanceExpectation_CPUOnlyFallbackOnly(t *testing.T) {
	note := phiPerformanceExpectation("linux amd64, 16GB RAM, GPU: none", &phiEvalSummary{Label: "fallback only"})
	if !strings.Contains(strings.ToLower(note), "shorter private tasks") {
		t.Fatalf("note=%q", note)
	}
	if !strings.Contains(strings.ToLower(note), "slow") {
		t.Fatalf("note=%q", note)
	}
}

func TestPhiModelView_ShowsCPUOnlyPerformanceLine(t *testing.T) {
	m := NewPhiModel(&phiTestExec{})
	m.loaded = true
	m.localBackend = "ollama"
	m.localModel = "qwen3.5:4b"
	m.localPreset = "balanced"
	m.backendInstall = "yes"
	m.backendRunning = "yes"
	m.modelReady = "yes"
	m.hardware = "linux amd64, 16GB RAM, GPU: none"

	view := m.View(nil, 100)
	if !strings.Contains(view, "Performance:") {
		t.Fatalf("expected performance line:\n%s", view)
	}
	if !strings.Contains(strings.ToLower(view), "cpu-only machine") {
		t.Fatalf("expected cpu-only guidance:\n%s", view)
	}
}
