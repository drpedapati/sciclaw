package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type phiUIMode int

const (
	phiNormal phiUIMode = iota
	phiEditModel
)

type phiDataMsg struct {
	mode           string
	cloudModel     string
	cloudProvider  string
	localBackend   string
	localModel     string
	localPreset    string
	backendRunning string
	backendInstall string
	backendVersion string
	modelReady     string
	hardware       string
	note           string
	err            string
}

type phiActionMsg struct {
	action string
	output string
	ok     bool
}

type phiEvalProbe struct {
	Name       string `json:"name"`
	Passed     bool   `json:"passed"`
	DurationMS int64  `json:"duration_ms"`
}

type phiEvalSummary struct {
	Label   string
	Detail  string
	Timings string
}

// PhiModel handles global PHI/local runtime management.
type PhiModel struct {
	exec   Executor
	mode   phiUIMode
	loaded bool

	globalMode string

	cloudModel    string
	cloudProvider string

	localBackend string
	localModel   string
	localPreset  string

	backendRunning string
	backendInstall string
	backendVersion string
	modelReady     string
	hardware       string

	note       string
	err        string
	lastOut    string
	eval       *phiEvalSummary
	flashMsg   string
	flashUntil time.Time
	opInFlight bool
	opName     string

	input textinput.Model
}

func NewPhiModel(exec Executor) PhiModel {
	ti := textinput.New()
	ti.CharLimit = 80
	ti.Width = 42
	ti.Placeholder = "qwen3.5:4b"
	return PhiModel{
		exec:           exec,
		globalMode:     "cloud",
		backendRunning: "unknown",
		backendInstall: "unknown",
		modelReady:     "unknown",
		input:          ti,
	}
}

func (m *PhiModel) AutoRun() tea.Cmd {
	if !m.loaded {
		return fetchPhiData(m.exec)
	}
	return nil
}

func (m *PhiModel) HandleData(msg phiDataMsg) {
	m.loaded = true
	m.globalMode = normalizePhiMode(msg.mode)
	m.cloudModel = strings.TrimSpace(msg.cloudModel)
	m.cloudProvider = strings.TrimSpace(msg.cloudProvider)
	m.localBackend = strings.TrimSpace(strings.ToLower(msg.localBackend))
	m.localModel = strings.TrimSpace(msg.localModel)
	m.localPreset = strings.TrimSpace(strings.ToLower(msg.localPreset))
	m.backendRunning = strings.TrimSpace(strings.ToLower(msg.backendRunning))
	m.backendInstall = strings.TrimSpace(strings.ToLower(msg.backendInstall))
	m.backendVersion = strings.TrimSpace(msg.backendVersion)
	m.modelReady = strings.TrimSpace(strings.ToLower(msg.modelReady))
	m.hardware = strings.TrimSpace(msg.hardware)
	m.note = strings.TrimSpace(msg.note)
	m.err = strings.TrimSpace(msg.err)
}

func (m *PhiModel) HandleAction(msg phiActionMsg) {
	if msg.action == "setup" || msg.action == "pull" || msg.action == "install" || msg.action == "service-start" || msg.action == "service-stop" || msg.action == "eval" {
		m.opInFlight = false
		m.opName = ""
	}

	out := strings.TrimSpace(msg.output)
	if out != "" {
		m.lastOut = shortenOutput(out, 800)
	}
	if msg.action == "eval" && msg.ok {
		if eval, ok := parsePhiEvalSummary(out); ok {
			m.eval = eval
		}
	}

	label := "PHI action complete"
	switch msg.action {
	case "setup":
		label = "PHI setup complete"
	case "mode-phi":
		label = "Global mode set to PHI"
	case "mode-cloud":
		label = "Global mode set to Cloud"
	case "set-local":
		label = "Local runtime defaults updated"
	case "pull":
		label = "Local model pull complete"
	case "install":
		label = "Ollama install complete"
	case "service-start":
		label = "Ollama service started"
	case "service-stop":
		label = "Ollama service stopped"
	case "eval":
		label = "PHI local eval complete"
	case "refresh":
		label = "PHI status refreshed"
	}
	if !msg.ok {
		label = "PHI action failed"
		if out != "" {
			label += ": " + shortenOutput(out, 180)
		}
		m.flashMsg = styleErr.Render("✗") + " " + label
	} else {
		if out != "" {
			label += ": " + shortenOutput(out, 180)
		}
		m.flashMsg = styleOK.Render("✓") + " " + label
	}
	m.flashUntil = time.Now().Add(6 * time.Second)
}

func (m PhiModel) Update(msg tea.KeyMsg, _ *VMSnapshot) (PhiModel, tea.Cmd) {
	key := msg.String()

	if m.mode == phiEditModel {
		switch key {
		case "esc":
			m.mode = phiNormal
			m.input.Blur()
			return m, nil
		case "enter":
			model := strings.TrimSpace(m.input.Value())
			m.mode = phiNormal
			m.input.Blur()
			if model == "" {
				return m, nil
			}
			modelCopy := model
			return m, phiSetLocalDefaultsCmd(m.exec, nil, &modelCopy, nil)
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	if m.opInFlight && (key == "p" || key == "d" || key == "e" || key == "i" || key == "o" || key == "x") {
		op := m.opName
		if strings.TrimSpace(op) == "" {
			op = "operation"
		}
		m.flashMsg = styleErr.Render("✗") + " " + op + " already running. Please wait for completion."
		m.flashUntil = time.Now().Add(4 * time.Second)
		return m, nil
	}

	switch key {
	case "r", "l":
		return m, fetchPhiData(m.exec)
	case "p":
		m.opInFlight = true
		m.opName = "PHI setup"
		return m, phiSetupCmd(m.exec)
	case "g":
		return m, phiSetModeCmd(m.exec, "phi")
	case "c":
		return m, phiSetModeCmd(m.exec, "cloud")
	case "2":
		model := "qwen3.5:2b"
		preset := "speed"
		return m, phiSetLocalDefaultsCmd(m.exec, nil, &model, &preset)
	case "4":
		model := "qwen3.5:4b"
		preset := "balanced"
		return m, phiSetLocalDefaultsCmd(m.exec, nil, &model, &preset)
	case "9":
		model := "qwen3.5:9b"
		preset := "quality"
		return m, phiSetLocalDefaultsCmd(m.exec, nil, &model, &preset)
	case "m":
		m.mode = phiEditModel
		m.input.SetValue(m.localModel)
		m.input.Focus()
		return m, nil
	case "b":
		next := "ollama"
		if strings.EqualFold(strings.TrimSpace(m.localBackend), "ollama") {
			next = "mlx"
		}
		return m, phiSetLocalDefaultsCmd(m.exec, &next, nil, nil)
	case "s":
		nextPreset := nextPhiPreset(m.localPreset)
		return m, phiSetLocalDefaultsCmd(m.exec, nil, nil, &nextPreset)
	case "d":
		backend := strings.TrimSpace(strings.ToLower(m.localBackend))
		model := strings.TrimSpace(m.localModel)
		if backend == "" {
			backend = "ollama"
		}
		if model == "" {
			m.flashMsg = styleErr.Render("✗") + " Set a local model first ([2]/[4]/[9] or [m])."
			m.flashUntil = time.Now().Add(4 * time.Second)
			return m, nil
		}
		m.opInFlight = true
		m.opName = "Model pull"
		return m, phiPullModelCmd(m.exec, backend, model)
	case "e":
		m.opInFlight = true
		m.opName = "PHI local eval"
		return m, phiEvalCmd(m.exec)
	case "i":
		m.opInFlight = true
		m.opName = "Ollama install"
		return m, phiInstallOllamaCmd(m.exec)
	case "o":
		m.opInFlight = true
		m.opName = "Start Ollama service"
		return m, phiOllamaServiceCmd(m.exec, "start")
	case "x":
		m.opInFlight = true
		m.opName = "Stop Ollama service"
		return m, phiOllamaServiceCmd(m.exec, "stop")
	}

	return m, nil
}

func (m PhiModel) View(_ *VMSnapshot, width int) string {
	panelW := width - 4
	if panelW < 50 {
		panelW = 50
	}

	if !m.loaded {
		return "\n  Loading PHI mode status...\n"
	}

	label := lipgloss.NewStyle().Foreground(colorMuted).Width(16)
	modeDisplay := strings.ToUpper(m.globalMode)
	if m.globalMode == "cloud" {
		modeDisplay = "CLOUD"
	}
	if m.globalMode == "phi" {
		modeDisplay = "PHI (LOCAL)"
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("  %s  %s", label.Render("Global mode:"), styleValue.Render(modeDisplay)))
	lines = append(lines, fmt.Sprintf("  %s  %s", label.Render("Cloud model:"), styleValue.Render(orUnknown(m.cloudModel))))
	lines = append(lines, fmt.Sprintf("  %s  %s", label.Render("Provider:"), styleValue.Render(orUnknown(m.cloudProvider))))
	lines = append(lines, "")
	lines = append(lines, "  "+styleBold.Render("Local Runtime Defaults"))
	lines = append(lines, fmt.Sprintf("  %s  %s", label.Render("Backend:"), styleValue.Render(orUnknown(m.localBackend))))
	lines = append(lines, fmt.Sprintf("  %s  %s", label.Render("Model:"), styleValue.Render(orUnknown(m.localModel))))
	lines = append(lines, fmt.Sprintf("  %s  %s", label.Render("Preset:"), styleValue.Render(orUnknown(m.localPreset))))
	lines = append(lines, "")
	lines = append(lines, "  "+styleBold.Render("Backend Health"))
	lines = append(lines, fmt.Sprintf("  %s  %s", label.Render("Installed:"), phiHealthValue(m.backendInstall)))
	lines = append(lines, fmt.Sprintf("  %s  %s", label.Render("Running:"), phiHealthValue(m.backendRunning)))
	lines = append(lines, fmt.Sprintf("  %s  %s", label.Render("Model ready:"), phiHealthValue(m.modelReady)))
	if m.eval != nil {
		lines = append(lines, fmt.Sprintf("  %s  %s", label.Render("Suitability:"), renderPhiEvalLabel(m.eval.Label)))
		lines = append(lines, fmt.Sprintf("  %s  %s", label.Render("Eval timings:"), styleValue.Render(m.eval.Timings)))
		lines = append(lines, fmt.Sprintf("  %s  %s", label.Render("Eval note:"), styleHint.Render(m.eval.Detail)))
	} else {
		lines = append(lines, fmt.Sprintf("  %s  %s", label.Render("Suitability:"), styleHint.Render("Run [e] once on this machine.")))
	}
	if strings.TrimSpace(m.backendVersion) != "" {
		lines = append(lines, fmt.Sprintf("  %s  %s", label.Render("Version:"), styleValue.Render(m.backendVersion)))
	}
	if strings.TrimSpace(m.hardware) != "" {
		lines = append(lines, fmt.Sprintf("  %s  %s", label.Render("Hardware:"), m.hardware))
	}
	if strings.TrimSpace(m.note) != "" {
		lines = append(lines, "")
		lines = append(lines, "  "+styleHint.Render(m.note))
	}
	if strings.TrimSpace(m.err) != "" {
		lines = append(lines, "")
		lines = append(lines, "  "+styleErr.Render(m.err))
	}

	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s Setup   %s Use PHI   %s Use Cloud   %s/%s/%s Qwen size",
		styleKey.Render("[p]"),
		styleKey.Render("[g]"),
		styleKey.Render("[c]"),
		styleKey.Render("[2]"),
		styleKey.Render("[4]"),
		styleKey.Render("[9]"),
	))
	lines = append(lines, fmt.Sprintf("  %s Custom model   %s Cycle preset   %s Toggle backend   %s Pull model",
		styleKey.Render("[m]"),
		styleKey.Render("[s]"),
		styleKey.Render("[b]"),
		styleKey.Render("[d]"),
	))
	lines = append(lines, fmt.Sprintf("  %s Eval local quality   %s Install Ollama   %s Start Ollama   %s Stop Ollama   %s Refresh",
		styleKey.Render("[e]"),
		styleKey.Render("[i]"),
		styleKey.Render("[o]"),
		styleKey.Render("[x]"),
		styleKey.Render("[r]"),
	))
	if m.opInFlight {
		op := m.opName
		if strings.TrimSpace(op) == "" {
			op = "Operation"
		}
		lines = append(lines, "  "+styleHint.Render(op+" in progress..."))
	}

	if m.mode == phiEditModel {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  Local model tag: %s", m.input.View()))
		lines = append(lines, styleDim.Render("    Enter to save, Esc to cancel"))
	}

	if strings.TrimSpace(m.lastOut) != "" {
		lines = append(lines, "")
		lines = append(lines, "  "+styleBold.Render("Last Output"))
		for _, line := range strings.Split(shortenOutput(m.lastOut, 400), "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			lines = append(lines, "    "+line)
		}
	}

	if !m.flashUntil.IsZero() && time.Now().Before(m.flashUntil) {
		lines = append(lines, "")
		lines = append(lines, "  "+m.flashMsg)
	}

	content := strings.Join(lines, "\n")
	panel := stylePanel.Width(panelW).Render(content)
	title := stylePanelTitle.Render("PHI Mode (Local Qwen)")
	return placePanelTitle(panel, title)
}

func fetchPhiData(exec Executor) tea.Cmd {
	return func() tea.Msg {
		msg := phiDataMsg{
			mode:           "cloud",
			backendRunning: "unknown",
			backendInstall: "unknown",
			modelReady:     "unknown",
		}

		if cfg, err := readConfigMap(exec); err == nil {
			agents := ensureMap(cfg, "agents")
			defaults := ensureMap(agents, "defaults")
			msg.localBackend = asString(defaults["local_backend"])
			msg.localModel = asString(defaults["local_model"])
			msg.localPreset = asString(defaults["local_preset"])
			if mode := normalizePhiMode(asString(defaults["mode"])); mode != "" {
				msg.mode = mode
			}
			msg.cloudModel = asString(defaults["model"])
			msg.cloudProvider = asString(defaults["provider"])
		} else if !isConfigNotFoundError(err) {
			msg.err = fmt.Sprintf("config read failed: %v", err)
		}

		statusCmd := phiHomeEnv(exec) + " " + shellEscape(exec.BinaryPath()) + " modes status 2>&1"
		statusOut, statusErr := exec.ExecShell(15*time.Second, statusCmd)
		if strings.TrimSpace(statusOut) != "" {
			parseModesStatusOutput(statusOut, &msg)
		}
		if statusErr != nil && strings.TrimSpace(statusOut) == "" && msg.err == "" {
			msg.err = statusErr.Error()
		}

		phiStatusCmd := phiHomeEnv(exec) + " " + shellEscape(exec.BinaryPath()) + " modes phi-status 2>&1"
		phiStatusOut, _ := exec.ExecShell(15*time.Second, phiStatusCmd)
		if strings.TrimSpace(phiStatusOut) != "" {
			parsePhiStatusOutput(phiStatusOut, &msg)
		}

		// Surface backend health even when global mode is cloud.
		probeCmd := phiHomeEnv(exec) + " bash -lc " + shellEscape(phiOllamaProbeScript()) + " 2>&1"
		probeOut, _ := exec.ExecShell(10*time.Second, probeCmd)
		if strings.TrimSpace(probeOut) != "" {
			parsePhiOllamaProbeOutput(probeOut, &msg)
		}

		if msg.localBackend == "" {
			msg.localBackend = "ollama"
		}
		if msg.localPreset == "" {
			msg.localPreset = "balanced"
		}

		return msg
	}
}

func parseModesStatusOutput(output string, msg *phiDataMsg) {
	if msg == nil {
		return
	}

	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "Mode":
			msg.mode = normalizePhiMode(val)
		case "Model":
			if msg.mode == "phi" {
				msg.localModel = val
			} else {
				msg.cloudModel = val
			}
		case "Provider":
			msg.cloudProvider = val
		case "Backend":
			msg.localBackend = strings.ToLower(val)
		case "Preset":
			msg.localPreset = strings.ToLower(val)
		case "Hardware":
			msg.hardware = val
		case "Status":
			lower := strings.ToLower(val)
			if strings.Contains(lower, "running") {
				msg.backendInstall = "yes"
				msg.backendRunning = "yes"
				if open := strings.Index(val, "("); open >= 0 {
					if close := strings.LastIndex(val, ")"); close > open+1 {
						msg.backendVersion = strings.TrimSpace(val[open+1 : close])
					}
				}
			} else if strings.Contains(lower, "installed but not running") {
				msg.backendInstall = "yes"
				msg.backendRunning = "no"
			} else if strings.Contains(lower, "not installed") {
				msg.backendInstall = "no"
				msg.backendRunning = "no"
			}
		}
	}
}

func parsePhiStatusOutput(output string, msg *phiDataMsg) {
	if msg == nil {
		return
	}

	lowerOut := strings.ToLower(strings.TrimSpace(output))
	if strings.Contains(lowerOut, "not in phi mode") {
		msg.note = "Local backend health appears after PHI mode is enabled globally."
		return
	}

	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "Backend":
			msg.localBackend = strings.ToLower(val)
		case "Model":
			msg.localModel = val
		case "Installed":
			msg.backendInstall = phiBoolToken(val)
		case "Running":
			msg.backendRunning = phiBoolToken(val)
		case "Version":
			msg.backendVersion = val
		case "Model ready":
			msg.modelReady = phiBoolToken(val)
		case "Hardware":
			msg.hardware = val
		}
	}
}

func phiSetModeCmd(exec Executor, mode string) tea.Cmd {
	return func() tea.Msg {
		cmd := phiHomeEnv(exec) + " " + shellEscape(exec.BinaryPath()) + " modes set " + shellEscape(mode) + " 2>&1"
		out, err := exec.ExecShell(60*time.Second, cmd)
		out = strings.TrimSpace(out)
		if err != nil {
			if out == "" {
				out = err.Error()
			}
			return phiActionMsg{action: "mode-" + normalizePhiMode(mode), output: out, ok: false}
		}
		return phiActionMsg{action: "mode-" + normalizePhiMode(mode), output: out, ok: true}
	}
}

func phiSetupCmd(exec Executor) tea.Cmd {
	return func() tea.Msg {
		cmd := phiHomeEnv(exec) + " " + shellEscape(exec.BinaryPath()) + " modes phi-setup 2>&1"
		out, err := exec.ExecShell(20*time.Minute, cmd)
		out = strings.TrimSpace(out)
		if err != nil {
			if out == "" {
				out = err.Error()
			}
			return phiActionMsg{action: "setup", output: out, ok: false}
		}
		return phiActionMsg{action: "setup", output: out, ok: true}
	}
}

func phiEvalCmd(exec Executor) tea.Cmd {
	return func() tea.Msg {
		cmd := phiHomeEnv(exec) + " " + shellEscape(exec.BinaryPath()) + " modes phi-eval --json 2>&1"
		out, err := exec.ExecShell(3*time.Minute, cmd)
		if err != nil {
			if strings.TrimSpace(out) == "" {
				out = err.Error()
			}
			return phiActionMsg{action: "eval", output: out, ok: false}
		}
		return phiActionMsg{action: "eval", output: out, ok: true}
	}
}

func parsePhiEvalSummary(raw string) (*phiEvalSummary, bool) {
	var payload struct {
		Results []phiEvalProbe `json:"results"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &payload); err != nil {
		return nil, false
	}
	if len(payload.Results) == 0 {
		return nil, false
	}

	var textMS, jsonMS, toolMS int64
	allPassed := true
	for _, result := range payload.Results {
		if !result.Passed {
			allPassed = false
		}
		switch strings.TrimSpace(strings.ToLower(result.Name)) {
		case "text":
			textMS = result.DurationMS
		case "json":
			jsonMS = result.DurationMS
		case "tool":
			toolMS = result.DurationMS
		}
	}

	summary := &phiEvalSummary{
		Timings: fmt.Sprintf("text %.1fs, json %.1fs, tool %.1fs", millisToSeconds(textMS), millisToSeconds(jsonMS), millisToSeconds(toolMS)),
	}
	switch {
	case !allPassed:
		summary.Label = "needs attention"
		summary.Detail = "One or more local probes failed."
	case textMS > 15000 || toolMS > 8000:
		summary.Label = "fallback only"
		summary.Detail = "Local mode works, but this machine is too slow for normal interactive turns."
	case textMS > 6000 || toolMS > 4000:
		summary.Label = "usable, slower"
		summary.Detail = "Local mode is working, but users should expect noticeably slower turns."
	default:
		summary.Label = "good interactive"
		summary.Detail = "Local mode looks healthy for everyday interactive use."
	}
	return summary, true
}

func millisToSeconds(ms int64) float64 {
	return float64(ms) / 1000.0
}

func renderPhiEvalLabel(label string) string {
	switch strings.TrimSpace(strings.ToLower(label)) {
	case "good interactive":
		return styleOK.Render(label)
	case "usable, slower":
		return styleHint.Render(label)
	case "fallback only", "needs attention":
		return styleErr.Render(label)
	default:
		return styleValue.Render(label)
	}
}

func phiPullModelCmd(exec Executor, backend, model string) tea.Cmd {
	return func() tea.Msg {
		backend = strings.TrimSpace(strings.ToLower(backend))
		model = strings.TrimSpace(model)
		if backend != "ollama" {
			return phiActionMsg{
				action: "pull",
				output: fmt.Sprintf("Model pull UI currently supports ollama only (current backend: %s).", backend),
				ok:     false,
			}
		}
		script := phiBrewLookupScript() + `
` + phiOllamaLookupScript() + `
if [ -z "$OLLAMA_BIN" ]; then
  echo "Ollama binary not found. Run [i] Setup or install from https://ollama.com"
  exit 1
fi
"$OLLAMA_BIN" pull ` + shellEscape(model) + `
`
		cmd := phiHomeEnv(exec) + " bash -lc " + shellEscape(script) + " 2>&1"
		out, err := exec.ExecShell(20*time.Minute, cmd)
		out = strings.TrimSpace(out)
		if err != nil {
			if out == "" {
				out = err.Error()
			}
			return phiActionMsg{action: "pull", output: out, ok: false}
		}
		return phiActionMsg{action: "pull", output: out, ok: true}
	}
}

func phiInstallOllamaCmd(exec Executor) tea.Cmd {
	return func() tea.Msg {
		script := phiOllamaInstallScript()
		cmd := phiHomeEnv(exec) + " bash -lc " + shellEscape(script) + " 2>&1"
		out, err := exec.ExecShell(20*time.Minute, cmd)
		out = strings.TrimSpace(out)
		if err != nil {
			if out == "" {
				out = err.Error()
			}
			return phiActionMsg{action: "install", output: out, ok: false}
		}
		return phiActionMsg{action: "install", output: out, ok: true}
	}
}

func phiOllamaServiceCmd(exec Executor, op string) tea.Cmd {
	return func() tea.Msg {
		op = strings.TrimSpace(strings.ToLower(op))
		if op != "start" && op != "stop" {
			return phiActionMsg{action: "service-" + op, output: "unsupported service operation", ok: false}
		}
		script := phiBrewLookupScript() + `
set -e
OP=` + shellEscape(op) + `
MANAGED=0

if command -v systemctl >/dev/null 2>&1; then
  if systemctl list-unit-files 2>/dev/null | grep -q '^ollama\.service'; then
    if [ "$(id -u)" -eq 0 ]; then
      systemctl "$OP" ollama
      MANAGED=1
    elif command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then
      sudo -n systemctl "$OP" ollama
      MANAGED=1
    elif systemctl --user list-unit-files 2>/dev/null | grep -q '^ollama\.service'; then
      systemctl --user "$OP" ollama
      MANAGED=1
    else
      echo "Found systemd ollama.service, but current user cannot control it without sudo."
    fi
  fi
fi

if [ "$MANAGED" -eq 0 ] && [ -n "$BREW_BIN" ]; then
  "$BREW_BIN" services "$OP" ollama
  MANAGED=1
fi

if [ "$MANAGED" -eq 0 ]; then
  echo "No managed Ollama service detected. Start it manually and press [r] refresh."
  exit 1
fi

echo "Ollama service $OP requested."
`
		cmd := phiHomeEnv(exec) + " bash -lc " + shellEscape(script) + " 2>&1"
		out, err := exec.ExecShell(2*time.Minute, cmd)
		out = strings.TrimSpace(out)
		if err != nil {
			if out == "" {
				out = err.Error()
			}
			return phiActionMsg{action: "service-" + op, output: out, ok: false}
		}
		return phiActionMsg{action: "service-" + op, output: out, ok: true}
	}
}

func phiSetLocalDefaultsCmd(exec Executor, backend, model, preset *string) tea.Cmd {
	return func() tea.Msg {
		updated := make([]string, 0, 3)
		if err := updateConfigMap(exec, func(cfg map[string]interface{}) error {
			agents := ensureMap(cfg, "agents")
			defaults := ensureMap(agents, "defaults")
			if backend != nil {
				val := strings.TrimSpace(strings.ToLower(*backend))
				defaults["local_backend"] = val
				updated = append(updated, "backend="+val)
			}
			if model != nil {
				val := strings.TrimSpace(*model)
				defaults["local_model"] = val
				updated = append(updated, "model="+val)
				if strings.TrimSpace(asString(defaults["local_backend"])) == "" {
					defaults["local_backend"] = "ollama"
				}
			}
			if preset != nil {
				val := strings.TrimSpace(strings.ToLower(*preset))
				defaults["local_preset"] = val
				updated = append(updated, "preset="+val)
			}
			return nil
		}); err != nil {
			return phiActionMsg{action: "set-local", output: fmt.Sprintf("Failed to save config: %v", err), ok: false}
		}

		// Apply runtime update live where possible.
		reloadCmd := phiHomeEnv(exec) + " " + shellEscape(exec.BinaryPath()) + " routing reload 2>&1"
		_, _ = exec.ExecShell(10*time.Second, reloadCmd)
		if len(updated) == 0 {
			return phiActionMsg{action: "set-local", output: "No local runtime changes.", ok: true}
		}
		return phiActionMsg{action: "set-local", output: "Updated " + strings.Join(updated, ", "), ok: true}
	}
}

func phiHomeEnv(exec Executor) string {
	return "HOME=" + shellEscape(exec.HomePath())
}

func phiBrewLookupScript() string {
	return `
BREW_BIN=""
if command -v brew >/dev/null 2>&1; then
  BREW_BIN="$(command -v brew)"
elif [ -x /usr/local/bin/brew ]; then
  BREW_BIN=/usr/local/bin/brew
elif [ -x /opt/homebrew/bin/brew ]; then
  BREW_BIN=/opt/homebrew/bin/brew
elif [ -x /home/linuxbrew/.linuxbrew/bin/brew ]; then
  BREW_BIN=/home/linuxbrew/.linuxbrew/bin/brew
fi
`
}

func phiOllamaLookupScript() string {
	return `
OLLAMA_BIN=""
if command -v ollama >/dev/null 2>&1; then
  OLLAMA_BIN="$(command -v ollama)"
elif [ -x /usr/local/bin/ollama ]; then
  OLLAMA_BIN=/usr/local/bin/ollama
elif [ -x /opt/homebrew/bin/ollama ]; then
  OLLAMA_BIN=/opt/homebrew/bin/ollama
elif [ -x /home/linuxbrew/.linuxbrew/bin/ollama ]; then
  OLLAMA_BIN=/home/linuxbrew/.linuxbrew/bin/ollama
elif [ -x /home/linuxbrew/.linuxbrew/opt/ollama/bin/ollama ]; then
  OLLAMA_BIN=/home/linuxbrew/.linuxbrew/opt/ollama/bin/ollama
elif [ -n "$BREW_BIN" ]; then
  BREW_PREFIX="$("$BREW_BIN" --prefix 2>/dev/null || true)"
  if [ -n "$BREW_PREFIX" ] && [ -x "$BREW_PREFIX/bin/ollama" ]; then
    OLLAMA_BIN="$BREW_PREFIX/bin/ollama"
  fi
fi
`
}

func phiOllamaInstallScript() string {
	return `
set -e
` + phiBrewLookupScript() + `
` + phiOllamaLookupScript() + `
if [ -n "$OLLAMA_BIN" ]; then
  echo "ollama is already installed at $OLLAMA_BIN."
else
  OS_NAME="$(uname -s)"
  USED_INSTALLER=""

  if [ "$OS_NAME" = "Linux" ] && (command -v nvidia-smi >/dev/null 2>&1 || [ -c /dev/nvidiactl ]); then
    if [ "$(id -u)" -eq 0 ] || (command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1); then
      if command -v curl >/dev/null 2>&1; then
        echo "Installing Ollama via official Linux installer (best GPU support)..."
        curl -fsSL https://ollama.com/install.sh | sh
        USED_INSTALLER="official"
      fi
    fi
  fi

  if [ -z "$USED_INSTALLER" ] && [ -n "$BREW_BIN" ]; then
    echo "Installing Ollama via Homebrew..."
    "$BREW_BIN" install ollama
    USED_INSTALLER="brew"
  fi

  if [ -z "$USED_INSTALLER" ] && command -v curl >/dev/null 2>&1; then
    echo "Installing Ollama via official installer..."
    curl -fsSL https://ollama.com/install.sh | sh
    USED_INSTALLER="official"
  fi

  if [ -z "$USED_INSTALLER" ]; then
    echo "Unable to auto-install Ollama on this system."
    echo "Install manually from: https://ollama.com"
    exit 1
  fi

  ` + phiOllamaLookupScript() + `
fi

if [ -z "$OLLAMA_BIN" ]; then
  echo "Ollama install completed, but executable was not found in expected paths."
  echo "Try a new terminal session, then rerun PHI setup."
  exit 1
fi

SERVICE_STARTED=0
if command -v systemctl >/dev/null 2>&1; then
  if systemctl list-unit-files 2>/dev/null | grep -q '^ollama\.service'; then
    if [ "$(id -u)" -eq 0 ]; then
      systemctl enable --now ollama || systemctl start ollama || true
      SERVICE_STARTED=1
    elif command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then
      sudo -n systemctl enable --now ollama || sudo -n systemctl start ollama || true
      SERVICE_STARTED=1
    elif systemctl --user list-unit-files 2>/dev/null | grep -q '^ollama\.service'; then
      systemctl --user start ollama || true
      SERVICE_STARTED=1
    fi
  fi
fi

if [ "$SERVICE_STARTED" -eq 0 ] && [ -n "$BREW_BIN" ]; then
  "$BREW_BIN" services start ollama || true
fi

"$OLLAMA_BIN" --version || true
echo "Ollama install step complete."
`
}

func phiOllamaProbeScript() string {
	return `
` + phiBrewLookupScript() + `
` + phiOllamaLookupScript() + `

if [ -z "$OLLAMA_BIN" ]; then
  echo "installed:no"
  echo "running:no"
  exit 0
fi

echo "installed:yes"
VER="$("$OLLAMA_BIN" --version 2>/dev/null | head -n 1)"
if [ -n "$VER" ]; then
  echo "version:$VER"
fi
if "$OLLAMA_BIN" ps >/dev/null 2>&1; then
  echo "running:yes"
else
  RUNNING="no"
  if command -v systemctl >/dev/null 2>&1 && systemctl is-active --quiet ollama 2>/dev/null; then
    RUNNING="yes"
  elif command -v systemctl >/dev/null 2>&1 && systemctl --user is-active --quiet ollama 2>/dev/null; then
    RUNNING="yes"
  elif [ -n "$BREW_BIN" ] && "$BREW_BIN" services list 2>/dev/null | grep -E '^ollama[[:space:]]+started' >/dev/null; then
    RUNNING="yes"
  fi
  echo "running:$RUNNING"
fi
`
}

func parsePhiOllamaProbeOutput(output string, msg *phiDataMsg) {
	if msg == nil {
		return
	}
	for _, raw := range strings.Split(output, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(strings.ToLower(parts[0]))
		val := strings.TrimSpace(parts[1])
		switch key {
		case "installed":
			msg.backendInstall = phiBoolToken(val)
		case "running":
			msg.backendRunning = phiBoolToken(val)
		case "version":
			if strings.TrimSpace(msg.backendVersion) == "" {
				msg.backendVersion = val
			}
		}
	}
}

func normalizePhiMode(raw string) string {
	val := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.HasPrefix(val, "phi"):
		return "phi"
	case strings.HasPrefix(val, "vm"):
		return "vm"
	case strings.HasPrefix(val, "cloud"), val == "":
		return "cloud"
	default:
		return val
	}
}

func nextPhiPreset(current string) string {
	switch strings.ToLower(strings.TrimSpace(current)) {
	case "speed":
		return "balanced"
	case "balanced":
		return "quality"
	case "quality":
		return "speed"
	default:
		return "balanced"
	}
}

func phiBoolToken(raw string) string {
	val := strings.ToLower(strings.TrimSpace(raw))
	switch val {
	case "true", "yes", "ready", "running", "ok":
		return "yes"
	case "false", "no", "not ready":
		return "no"
	default:
		return "unknown"
	}
}

func phiHealthValue(raw string) string {
	val := strings.ToLower(strings.TrimSpace(raw))
	switch val {
	case "yes":
		return styleOK.Render("yes")
	case "no":
		return styleErr.Render("no")
	default:
		return styleDim.Render("unknown")
	}
}

func orUnknown(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}
	return v
}

func shortenOutput(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	if len(s) <= max || max < 5 {
		return s
	}
	keep := max - 1
	if keep < 1 {
		keep = 1
	}
	return s[:keep] + "…"
}
