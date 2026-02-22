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
	action string
	ok     bool
	output string
}

// AgentModel handles the Agent Service tab.
type AgentModel struct {
	exec         Executor
	logsViewport viewport.Model
	logsContent  string
	logsLoaded   bool
}

func NewAgentModel(exec Executor) AgentModel {
	vp := viewport.New(60, 10)
	vp.SetContent("Loading logs...")
	return AgentModel{exec: exec, logsViewport: vp}
}

func (m AgentModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (AgentModel, tea.Cmd) {
	switch msg.String() {
	case "s":
		return m, serviceAction(m.exec, "start")
	case "t":
		return m, serviceAction(m.exec, "stop")
	case "r":
		return m, serviceAction(m.exec, "restart")
	case "i":
		return m, serviceAction(m.exec, "install")
	case "u":
		return m, serviceAction(m.exec, "uninstall")
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
	header := fmt.Sprintf("Service %s completed.", msg.action)
	if !msg.ok {
		header = fmt.Sprintf("Service %s failed.", msg.action)
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

	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s Start   %s Stop   %s Restart",
		styleKey.Render("[s]"),
		styleKey.Render("[t]"),
		styleKey.Render("[r]"),
	))
	lines = append(lines, fmt.Sprintf("  %s Install/Reinstall   %s Uninstall",
		styleKey.Render("[i]"),
		styleKey.Render("[u]"),
	))
	lines = append(lines, fmt.Sprintf("  %s Fetch latest logs",
		styleKey.Render("[l]"),
	))

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
		cmd := "HOME=" + exec.HomePath() + " sciclaw service " + action + " 2>&1"
		out, err := exec.ExecShell(15*time.Second, cmd)
		if err != nil {
			if strings.TrimSpace(out) == "" {
				out = err.Error()
			}
			return serviceActionMsg{action: action, ok: false, output: strings.TrimSpace(out)}
		}
		if strings.TrimSpace(out) == "" {
			out = "No output."
		}
		return serviceActionMsg{action: action, ok: true, output: strings.TrimSpace(out)}
	}
}

func fetchLogs(exec Executor) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw service logs --lines 50 2>&1"
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
