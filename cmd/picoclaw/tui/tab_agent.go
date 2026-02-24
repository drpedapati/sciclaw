package tui

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type logsMsg struct{ content string }
type serviceActionMsg struct {
	action      string
	ok          bool
	normalized  bool
	output      string
	duration    time.Duration
	statusKnown bool
	installed   bool
	running     bool
}

// AgentModel handles the Agent Service tab.
type AgentModel struct {
	exec         Executor
	logsViewport viewport.Model
	logsContent  string
	logsLoaded   bool
	actionBusy   bool
	actionName   string
	actionStart  time.Time
	lastAction   serviceActionMsg
	lastActionAt time.Time
	successStreak int
}

func NewAgentModel(exec Executor) AgentModel {
	vp := viewport.New(60, 10)
	vp.SetContent("Loading logs...")
	return AgentModel{exec: exec, logsViewport: vp}
}

func (m AgentModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (AgentModel, tea.Cmd) {
	startAction := func(action string) (AgentModel, tea.Cmd) {
		if m.actionBusy {
			return m, nil
		}
		m.actionBusy = true
		m.actionName = action
		m.actionStart = time.Now()
		return m, serviceAction(m.exec, action)
	}

	switch msg.String() {
	case "s":
		return startAction("start")
	case "t":
		return startAction("stop")
	case "r":
		return startAction("restart")
	case "i":
		return startAction("install")
	case "f":
		return startAction("refresh")
	case "u":
		return startAction("uninstall")
	case "l":
		return m, fetchLogs(m.exec)
	}

	// Forward to viewport for scrolling.
	var cmd tea.Cmd
	m.logsViewport, cmd = m.logsViewport.Update(msg)
	return m, cmd
}

func (m *AgentModel) HandleLogsMsg(msg logsMsg) {
	m.logsContent = msg.content
	m.logsLoaded = true
	m.logsViewport.SetContent(msg.content)
}

func (m *AgentModel) HandleServiceAction(msg serviceActionMsg) {
	m.actionBusy = false
	m.actionName = ""
	m.actionStart = time.Time{}

	now := time.Now()
	if msg.ok {
		if !m.lastActionAt.IsZero() && now.Sub(m.lastActionAt) <= 90*time.Second {
			m.successStreak++
			if m.successStreak < 1 {
				m.successStreak = 1
			}
		} else {
			m.successStreak = 1
		}
	} else {
		m.successStreak = 0
	}
	m.lastAction = msg
	m.lastActionAt = now

	header := fmt.Sprintf("Service %s completed.", msg.action)
	if !msg.ok {
		header = fmt.Sprintf("Service %s failed.", msg.action)
	} else if msg.normalized {
		header = fmt.Sprintf("Service %s completed (state verified).", msg.action)
	}
	content := header
	if strings.TrimSpace(msg.output) != "" {
		content += "\n\n" + strings.TrimSpace(msg.output)
	}
	m.logsContent = content
	m.logsLoaded = true
	m.logsViewport.SetContent(content)
}

func (m *AgentModel) HandleResize(width, height int) {
	w := width - 8
	if w < 40 {
		w = 40
	}
	h := height / 3
	if h < 5 {
		h = 5
	}
	m.logsViewport.Width = w
	m.logsViewport.Height = h
}

func (m AgentModel) View(snap *VMSnapshot, width int) string {
	if snap == nil {
		return "\n  No data available yet.\n"
	}

	panelW := width - 4
	if panelW < 40 {
		panelW = 40
	}

	var b strings.Builder

	// Service status panel
	b.WriteString(m.renderServicePanel(snap, panelW))
	b.WriteString("\n")

	// Logs panel
	b.WriteString(m.renderLogsPanel(panelW))

	return b.String()
}

func (m AgentModel) renderServicePanel(snap *VMSnapshot, w int) string {
	var lines []string

	lines = append(lines, fmt.Sprintf(" %s %s %s",
		styleLabel.Render("Installed:"),
		statusIcon(boolStatus(snap.ServiceInstalled)),
		yesNo(snap.ServiceInstalled),
	))
	lines = append(lines, fmt.Sprintf(" %s %s %s",
		styleLabel.Render("Running:"),
		statusIcon(boolStatus(snap.ServiceRunning)),
		yesNo(snap.ServiceRunning),
	))
	lines = append(lines, fmt.Sprintf(" %s %s",
		styleLabel.Render("Backend:"),
		styleDim.Render(serviceBackendLabel(m.exec.Mode())),
	))

	if m.actionBusy {
		lines = append(lines, fmt.Sprintf(" %s %s %s",
			styleLabel.Render("Action:"),
			styleWarn.Render(strings.ToUpper(m.actionName)),
			styleDim.Render(renderServiceActionProgress(m.actionName, m.actionStart)),
		))
	}
	if !m.lastActionAt.IsZero() {
		lastStyle := styleOK
		if !m.lastAction.ok {
			lastStyle = styleErr
		} else if m.lastAction.normalized {
			lastStyle = styleWarn
		}
		lines = append(lines, fmt.Sprintf(" %s %s (%s)",
			styleLabel.Render("Last:"),
			lastStyle.Render(fmt.Sprintf("%s %s", strings.ToUpper(m.lastAction.action), map[bool]string{true: "OK", false: "FAIL"}[m.lastAction.ok])),
			styleDim.Render(formatDurationCompact(m.lastAction.duration)),
		))
		if m.successStreak > 1 {
			lines = append(lines, fmt.Sprintf(" %s %s",
				styleLabel.Render("Momentum:"),
				styleValue.Render(fmt.Sprintf("x%d %s", m.successStreak, serviceStreakTitle(m.successStreak))),
			))
		}
	}

	if !m.actionBusy {
		lines = append(lines, "")
		if snap.ServiceRunning {
			lines = append(lines, fmt.Sprintf("  %s Stop   %s Restart   %s Fetch latest logs",
				styleKey.Render("[t]"),
				styleKey.Render("[r]"),
				styleKey.Render("[l]"),
			))
		} else if snap.ServiceInstalled {
			lines = append(lines, fmt.Sprintf("  %s Start   %s Reinstall   %s Uninstall   %s Fetch latest logs",
				styleKey.Render("[s]"),
				styleKey.Render("[i]"),
				styleKey.Render("[u]"),
				styleKey.Render("[l]"),
			))
		} else {
			lines = append(lines, fmt.Sprintf("  %s Install   %s Fetch latest logs",
				styleKey.Render("[i]"),
				styleKey.Render("[l]"),
			))
		}
	}

	content := strings.Join(lines, "\n")
	panel := stylePanel.Width(w).Render(content)
	title := stylePanelTitle.Render("Gateway Service")
	return placePanelTitle(panel, title)
}

func (m AgentModel) renderLogsPanel(w int) string {
	content := m.logsViewport.View()
	if !m.logsLoaded {
		content = styleDim.Render("  Press [l] to load recent logs.")
	}
	panel := stylePanel.Width(w).Render(content)
	title := stylePanelTitle.Render("Recent Activity")
	return placePanelTitle(panel, title)
}

func yesNo(v bool) string {
	if v {
		return styleOK.Render("Yes")
	}
	return styleErr.Render("No")
}

func serviceBackendLabel(mode Mode) string {
	if mode == ModeVM {
		return "systemd (user)"
	}
	if runtime.GOOS == "darwin" {
		return "launchd (user)"
	}
	return "systemd (user)"
}

func serviceAction(exec Executor, action string) tea.Cmd {
	return func() tea.Msg {
		started := time.Now()
		cmd := "HOME=" + exec.HomePath() + " " + shellEscape(exec.BinaryPath()) + " service " + action + " 2>&1"
		out, err := exec.ExecShell(20*time.Second, cmd)
		if strings.TrimSpace(out) == "" {
			out = "No output."
		}

		// Give the service process time to settle before checking status.
		// start/restart/refresh all kill and relaunch the process; checking
		// too quickly catches the brief window where it is dead.
		switch strings.TrimSpace(action) {
		case "start", "restart", "refresh":
			time.Sleep(2 * time.Second)
		}

		installed, running, statusKnown, statusOut := serviceStatusSnapshot(exec)
		normalized := false
		ok := err == nil
		if !ok && statusKnown && inferServiceActionSuccess(action, installed, running) {
			ok = true
			normalized = true
		}

		statusLine := ""
		if statusKnown {
			statusLine = fmt.Sprintf("Observed status: installed=%s running=%s", yesNoPlain(installed), yesNoPlain(running))
		} else if strings.TrimSpace(statusOut) != "" {
			statusLine = "Observed status:\n" + strings.TrimSpace(statusOut)
		}
		if statusLine != "" {
			out = strings.TrimSpace(out) + "\n\n" + statusLine
		}

		return serviceActionMsg{
			action:      action,
			ok:          ok,
			normalized:  normalized,
			output:      strings.TrimSpace(out),
			duration:    time.Since(started),
			statusKnown: statusKnown,
			installed:   installed,
			running:     running,
		}
	}
}

func serviceStatusSnapshot(exec Executor) (installed, running, statusKnown bool, raw string) {
	cmd := "HOME=" + exec.HomePath() + " " + shellEscape(exec.BinaryPath()) + " service status 2>&1"
	out, err := exec.ExecShell(8*time.Second, cmd)
	if err != nil {
		return false, false, false, out
	}
	installed, okInstalled := parseServiceStatusFlagFromOutput(out, "installed")
	running, okRunning := parseServiceStatusFlagFromOutput(out, "running")
	return installed, running, okInstalled && okRunning, out
}

func parseServiceStatusFlagFromOutput(out, key string) (bool, bool) {
	prefix := strings.ToLower(strings.TrimSpace(key)) + ":"
	for _, line := range strings.Split(out, "\n") {
		l := strings.ToLower(strings.TrimSpace(line))
		if !strings.HasPrefix(l, prefix) {
			continue
		}
		val := strings.TrimSpace(strings.TrimPrefix(l, prefix))
		switch {
		case strings.HasPrefix(val, "yes"):
			return true, true
		case strings.HasPrefix(val, "no"):
			return false, true
		}
		return false, false
	}
	return false, false
}

func inferServiceActionSuccess(action string, installed, running bool) bool {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "start", "restart":
		return running
	case "stop":
		return !running
	case "install":
		return installed
	case "uninstall":
		return !installed
	case "refresh":
		return installed && running
	default:
		return false
	}
}

func renderServiceActionProgress(action string, started time.Time) string {
	if started.IsZero() {
		return "queued"
	}
	elapsed := time.Since(started)
	target := 8 * time.Second
	switch action {
	case "stop":
		target = 5 * time.Second
	case "restart":
		target = 10 * time.Second
	case "install", "uninstall", "refresh":
		target = 12 * time.Second
	}
	pct := float64(elapsed) / float64(target)
	if pct > 1 {
		pct = 1
	}
	width := 12
	filled := int(pct * float64(width))
	if filled > width {
		filled = width
	}
	bar := "[" + strings.Repeat("=", filled) + strings.Repeat(".", width-filled) + "]"
	phase := "processing"
	switch action {
	case "start":
		phase = "booting"
	case "stop":
		phase = "draining"
	case "restart":
		phase = "cycling"
	case "install":
		phase = "provisioning"
	case "uninstall":
		phase = "tearing-down"
	case "refresh":
		phase = "syncing"
	}
	return fmt.Sprintf("%s %s %s", bar, phase, formatDurationCompact(elapsed))
}

func formatDurationCompact(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func serviceStreakTitle(streak int) string {
	switch {
	case streak >= 6:
		return "(SRE mode)"
	case streak >= 4:
		return "(operator)"
	default:
		return "(warming up)"
	}
}

func yesNoPlain(v bool) string {
	if v {
		return "yes"
	}
	return "no"
}

func fetchLogs(exec Executor) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " " + shellEscape(exec.BinaryPath()) + " service logs --lines 50 2>&1"
		out, err := exec.ExecShell(10*time.Second, cmd)
		if err != nil {
			return logsMsg{content: fmt.Sprintf("Error fetching logs: %v", err)}
		}
		if strings.TrimSpace(out) == "" {
			return logsMsg{content: "No logs available."}
		}
		return logsMsg{content: out}
	}
}
