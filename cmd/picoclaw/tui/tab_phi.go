package tui

import (
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
	flashMsg   string
	flashUntil time.Time

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
	out := strings.TrimSpace(msg.output)
	if out != "" {
		m.lastOut = shortenOutput(out, 800)
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

	switch key {
	case "r", "l":
		return m, fetchPhiData(m.exec)
	case "p":
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
		return m, phiPullModelCmd(m.exec, backend, model)
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
	lines = append(lines, fmt.Sprintf("  %s Custom model   %s Cycle preset   %s Toggle backend   %s Pull model   %s Refresh",
		styleKey.Render("[m]"),
		styleKey.Render("[s]"),
		styleKey.Render("[b]"),
		styleKey.Render("[d]"),
		styleKey.Render("[r]"),
	))

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

		statusCmd := "HOME=" + exec.HomePath() + " " + shellEscape(exec.BinaryPath()) + " modes status 2>&1"
		statusOut, statusErr := exec.ExecShell(15*time.Second, statusCmd)
		if strings.TrimSpace(statusOut) != "" {
			parseModesStatusOutput(statusOut, &msg)
		}
		if statusErr != nil && strings.TrimSpace(statusOut) == "" && msg.err == "" {
			msg.err = statusErr.Error()
		}

		phiStatusCmd := "HOME=" + exec.HomePath() + " " + shellEscape(exec.BinaryPath()) + " modes phi-status 2>&1"
		phiStatusOut, _ := exec.ExecShell(15*time.Second, phiStatusCmd)
		if strings.TrimSpace(phiStatusOut) != "" {
			parsePhiStatusOutput(phiStatusOut, &msg)
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
		cmd := "HOME=" + exec.HomePath() + " " + shellEscape(exec.BinaryPath()) + " modes set " + shellEscape(mode) + " 2>&1"
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
		cmd := "HOME=" + exec.HomePath() + " " + shellEscape(exec.BinaryPath()) + " modes phi-setup 2>&1"
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
		cmd := "HOME=" + exec.HomePath() + " ollama pull " + shellEscape(model) + " 2>&1"
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

func phiSetLocalDefaultsCmd(exec Executor, backend, model, preset *string) tea.Cmd {
	return func() tea.Msg {
		cfg, err := readConfigMap(exec)
		if err != nil {
			return phiActionMsg{action: "set-local", output: fmt.Sprintf("Failed to load config: %v", err), ok: false}
		}

		agents := ensureMap(cfg, "agents")
		defaults := ensureMap(agents, "defaults")
		updated := make([]string, 0, 3)
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

		if err := writeConfigMap(exec, cfg); err != nil {
			return phiActionMsg{action: "set-local", output: fmt.Sprintf("Failed to save config: %v", err), ok: false}
		}

		// Apply runtime update live where possible.
		reloadCmd := "HOME=" + exec.HomePath() + " " + shellEscape(exec.BinaryPath()) + " routing reload 2>&1"
		_, _ = exec.ExecShell(10*time.Second, reloadCmd)
		if len(updated) == 0 {
			return phiActionMsg{action: "set-local", output: "No local runtime changes.", ok: true}
		}
		return phiActionMsg{action: "set-local", output: "Updated " + strings.Join(updated, ", "), ok: true}
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
