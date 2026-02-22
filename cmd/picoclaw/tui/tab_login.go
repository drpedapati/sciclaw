package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type loginSelection int

const (
	loginOpenAI loginSelection = iota
	loginAnthropic
)

type loginMode int

const (
	loginNormal loginMode = iota
	loginAPIKey
)

// LoginModel handles the Login tab.
type LoginModel struct {
	exec     Executor
	selected loginSelection
	mode     loginMode
	input    textinput.Model

	// Flash feedback
	flashMsg   string
	flashUntil time.Time
}

func NewLoginModel(exec Executor) LoginModel {
	ti := textinput.New()
	ti.CharLimit = 128
	ti.Width = 50
	ti.EchoMode = textinput.EchoPassword
	return LoginModel{exec: exec, input: ti}
}

func (m LoginModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (LoginModel, tea.Cmd) {
	// Handle API key entry mode.
	if m.mode == loginAPIKey {
		switch msg.String() {
		case "esc":
			m.mode = loginNormal
			m.input.Blur()
			return m, nil
		case "enter":
			key := strings.TrimSpace(m.input.Value())
			if key == "" {
				m.mode = loginNormal
				m.input.Blur()
				return m, nil
			}
			provider := "openai"
			if m.selected == loginAnthropic {
				provider = "anthropic"
			}
			if err := saveAPIKey(m.exec, provider, key); err != nil {
				m.flashMsg = styleErr.Render("  Error: " + err.Error())
				m.flashUntil = time.Now().Add(4 * time.Second)
				m.mode = loginNormal
				m.input.Blur()
				return m, nil
			}
			m.flashMsg = styleOK.Render("  API key saved for " + provider + ".")
			m.flashUntil = time.Now().Add(4 * time.Second)
			m.mode = loginNormal
			m.input.Blur()
			m.input.SetValue("")
			return m, func() tea.Msg { return actionDoneMsg{output: "API key saved."} }
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}

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
	case "k":
		m.mode = loginAPIKey
		m.input.SetValue("")
		m.input.Focus()
		return m, m.input.Cursor.BlinkCmd()
	}
	return m, nil
}

func (m LoginModel) View(snap *VMSnapshot, width int) string {
	if snap == nil {
		return "\n  No data available yet.\n"
	}

	panelW := width - 4
	if panelW > 100 {
		panelW = 100
	}
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
	lines = append(lines, fmt.Sprintf("  %s Set API key", styleKey.Render("[k]")))
	lines = append(lines, "")
	lines = append(lines, styleHint.Render("  Use arrow keys to select a provider, then press Enter to log in."))

	content := strings.Join(lines, "\n")
	panel := stylePanel.Width(panelW).Render(content)
	title := stylePanelTitle.Render("AI Provider Credentials")

	var b strings.Builder
	b.WriteString(placePanelTitle(panel, title))

	// API key entry overlay
	if m.mode == loginAPIKey {
		provider := "OpenAI"
		if m.selected == loginAnthropic {
			provider = "Anthropic"
		}
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  %s API key: %s\n", styleBold.Render(provider), m.input.View()))
		b.WriteString(styleDim.Render("    Enter to save, Esc to cancel") + "\n")
	}

	// Flash message
	if !m.flashUntil.IsZero() && time.Now().Before(m.flashUntil) {
		b.WriteString(m.flashMsg + "\n")
	}

	return b.String()
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

// saveAPIKey writes the API key into config.json under providers.<provider>.api_key.
func saveAPIKey(exec Executor, provider, key string) error {
	cfg, err := readConfigMap(exec)
	if err != nil {
		cfg = map[string]interface{}{}
	}
	providers := ensureMap(cfg, "providers")
	p := ensureMap(providers, provider)
	p["api_key"] = key
	return writeConfigMap(exec, cfg)
}

func logoutCmd(exec Executor, provider string) tea.Cmd {
	return func() tea.Msg {
		cmd := fmt.Sprintf("HOME=%s sciclaw auth logout --provider %s", exec.HomePath(), provider)
		_, _ = exec.ExecShell(5*time.Second, cmd)
		return actionDoneMsg{output: "Logged out from " + provider + "."}
	}
}
