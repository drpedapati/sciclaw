package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type modelsMode int

const (
	modelsNormal modelsMode = iota
	modelsSetModel
	modelsSetEffort
)

type modelsStatusMsg struct{ output string }

var effortLevels = []string{"none", "minimal", "low", "medium", "high", "xhigh"}

// ModelsModel handles the Models tab.
type ModelsModel struct {
	exec   Executor
	mode   modelsMode
	loaded bool

	// Parsed status
	modelName       string
	providerName    string
	authMethod      string
	reasoningEffort string

	// Set model text input
	input textinput.Model

	// Effort selection
	effortIdx int
}

func NewModelsModel(exec Executor) ModelsModel {
	ti := textinput.New()
	ti.CharLimit = 64
	ti.Width = 40
	ti.Placeholder = "e.g. gpt-5.2 or claude-opus-4-6"
	return ModelsModel{exec: exec, input: ti}
}

func (m *ModelsModel) AutoRun() tea.Cmd {
	if !m.loaded {
		return fetchModelsStatus(m.exec)
	}
	return nil
}

func (m *ModelsModel) HandleStatus(msg modelsStatusMsg) {
	m.loaded = true
	m.parseStatus(msg.output)
}

func (m *ModelsModel) parseStatus(output string) {
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "Model":
			m.modelName = val
		case "Provider":
			m.providerName = val
		case "Auth":
			m.authMethod = val
		case "Reasoning Effort":
			m.reasoningEffort = val
		}
	}
}

func (m ModelsModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (ModelsModel, tea.Cmd) {
	key := msg.String()

	if m.mode == modelsSetModel {
		switch key {
		case "esc":
			m.mode = modelsNormal
			m.input.Blur()
			return m, nil
		case "enter":
			val := strings.TrimSpace(m.input.Value())
			if val == "" {
				m.mode = modelsNormal
				m.input.Blur()
				return m, nil
			}
			m.mode = modelsNormal
			m.input.Blur()
			return m, setModelCmd(m.exec, val)
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	if m.mode == modelsSetEffort {
		switch key {
		case "esc":
			m.mode = modelsNormal
			return m, nil
		case "left", "h":
			if m.effortIdx > 0 {
				m.effortIdx--
			}
		case "right", "l":
			if m.effortIdx < len(effortLevels)-1 {
				m.effortIdx++
			}
		case "enter":
			level := effortLevels[m.effortIdx]
			m.mode = modelsNormal
			return m, setEffortCmd(m.exec, level)
		}
		return m, nil
	}

	// Normal mode.
	switch key {
	case "s":
		m.mode = modelsSetModel
		m.input.SetValue("")
		m.input.Focus()
		return m, nil
	case "e":
		m.mode = modelsSetEffort
		for i, lvl := range effortLevels {
			if lvl == m.reasoningEffort {
				m.effortIdx = i
				break
			}
		}
		return m, nil
	case "l":
		m.loaded = false
		return m, fetchModelsStatus(m.exec)
	}
	return m, nil
}

func (m ModelsModel) View(snap *VMSnapshot, width int) string {
	panelW := width - 4
	if panelW < 40 {
		panelW = 40
	}

	if !m.loaded {
		return "\n  Loading model status...\n"
	}

	var lines []string

	lines = append(lines, fmt.Sprintf("  %s %s",
		styleLabel.Render("Model:"), styleValue.Render(m.modelName)))
	lines = append(lines, fmt.Sprintf("  %s %s",
		styleLabel.Render("Provider:"), styleValue.Render(m.providerName)))
	lines = append(lines, fmt.Sprintf("  %s %s",
		styleLabel.Render("Auth:"), styleValue.Render(m.authMethod)))
	lines = append(lines, fmt.Sprintf("  %s %s",
		styleLabel.Render("Effort:"), styleValue.Render(m.reasoningEffort)))

	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s Set model   %s Set reasoning effort   %s Refresh",
		styleKey.Render("[s]"),
		styleKey.Render("[e]"),
		styleKey.Render("[l]"),
	))

	if m.mode == modelsSetModel {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  Model name: %s", m.input.View()))
		lines = append(lines, styleDim.Render("    Enter to confirm, Esc to cancel"))
	}

	if m.mode == modelsSetEffort {
		lines = append(lines, "")
		var opts []string
		for i, lvl := range effortLevels {
			if i == m.effortIdx {
				opts = append(opts, styleBold.Render("▸ "+lvl))
			} else {
				opts = append(opts, styleDim.Render("  "+lvl))
			}
		}
		lines = append(lines, "  "+strings.Join(opts, "  "))
		lines = append(lines, styleDim.Render("    ←/→ to pick, Enter to confirm, Esc to cancel"))
	}

	content := strings.Join(lines, "\n")
	panel := stylePanel.Width(panelW).Render(content)
	title := stylePanelTitle.Render("AI Model Configuration")
	return placePanelTitle(panel, title)
}

func fetchModelsStatus(exec Executor) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw models status 2>&1"
		out, _ := exec.ExecShell(10*time.Second, cmd)
		return modelsStatusMsg{output: out}
	}
}

func setModelCmd(exec Executor, model string) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw models set " + shellEscape(model) + " 2>&1"
		_, _ = exec.ExecShell(10*time.Second, cmd)
		return actionDoneMsg{output: "Model set to " + model}
	}
}

func setEffortCmd(exec Executor, level string) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw models effort " + level + " 2>&1"
		_, _ = exec.ExecShell(10*time.Second, cmd)
		return actionDoneMsg{output: "Reasoning effort set to " + level}
	}
}
