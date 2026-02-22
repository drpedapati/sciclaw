package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type loginSelection int

const (
	loginOpenAI loginSelection = iota
	loginAnthropic
)

// LoginModel handles the Login tab.
type LoginModel struct {
	exec     Executor
	selected loginSelection
}

func NewLoginModel(exec Executor) LoginModel {
	return LoginModel{exec: exec}
}

func (m LoginModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (LoginModel, tea.Cmd) {
	switch msg.String() {
	case "up":
		if m.selected > loginOpenAI {
			m.selected--
		}
	case "down":
		if m.selected < loginAnthropic {
			m.selected++
		}
	case "l", "enter":
		provider := "openai"
		if m.selected == loginAnthropic {
			provider = "anthropic"
		}
		c := m.exec.InteractiveProcess("sciclaw", "auth", "login", "--provider", provider)
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return actionDoneMsg{output: "Login flow completed."}
		})
	case "o":
		provider := "openai"
		if m.selected == loginAnthropic {
			provider = "anthropic"
		}
		return m, logoutCmd(m.exec, provider)
	}
	return m, nil
}

func (m LoginModel) View(snap *VMSnapshot, width int) string {
	if snap == nil {
		return "\n  No data available yet.\n"
	}

	panelW := width - 4
	if panelW < 40 {
		panelW = 40
	}

	var lines []string

	// Table header
	lines = append(lines, fmt.Sprintf("  %-14s  %-12s  %s",
		styleDim.Render("Provider"),
		styleDim.Render("Status"),
		styleDim.Render("Details"),
	))
	lines = append(lines, styleDim.Render("  "+strings.Repeat("─", 50)))

	// OpenAI row
	openaiRow := renderProviderRow("OpenAI", snap.OpenAI)
	if m.selected == loginOpenAI {
		openaiRow = lipgloss.NewStyle().Background(lipgloss.Color("#2A2A4A")).Bold(true).Render(openaiRow)
	}
	lines = append(lines, openaiRow)

	// Anthropic row
	anthropicRow := renderProviderRow("Anthropic", snap.Anthropic)
	if m.selected == loginAnthropic {
		anthropicRow = lipgloss.NewStyle().Background(lipgloss.Color("#2A2A4A")).Bold(true).Render(anthropicRow)
	}
	lines = append(lines, anthropicRow)

	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s Log in (opens browser/device code flow)", styleKey.Render("[Enter]")))
	lines = append(lines, fmt.Sprintf("  %s Log out of selected provider", styleKey.Render("[o]")))
	lines = append(lines, "")
	lines = append(lines, styleHint.Render("  Use arrow keys to select a provider, then press Enter to log in."))

	content := strings.Join(lines, "\n")
	panel := stylePanel.Width(panelW).Render(content)
	title := stylePanelTitle.Render("AI Provider Credentials")
	return placePanelTitle(panel, title)
}

func renderProviderRow(name, state string) string {
	icon := statusIcon(state)
	statusText := styleBold.Render(state)
	if state == "ready" {
		statusText = styleOK.Render("Active")
	} else {
		statusText = styleErr.Render("Not set")
	}
	return fmt.Sprintf("  %-14s  %s %-12s", name, icon, statusText)
}

func logoutCmd(exec Executor, provider string) tea.Cmd {
	return func() tea.Msg {
		cmd := fmt.Sprintf("HOME=%s sciclaw auth logout --provider %s", exec.HomePath(), provider)
		_, _ = exec.ExecShell(5*time.Second, cmd)
		return actionDoneMsg{output: "Logged out from " + provider + "."}
	}
}
