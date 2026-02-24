package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

type modelsMode int

const (
	modelsNormal modelsMode = iota
	modelsSelectModel
	modelsSetModel
	modelsSetEffort
)

type modelsStatusMsg struct{ output string }
type modelsCatalogMsg struct {
	provider string
	source   string
	models   []string
	warning  string
	err      string
}

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

	// Set model text input (manual mode)
	input textinput.Model

	// Discovered model options
	modelOptions    []string
	modelOptionsIdx int
	modelsProvider  string
	modelsSource    string
	modelsWarning   string
	modelsLoading   bool
	modelsErr       string

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
		return tea.Batch(fetchModelsStatus(m.exec), fetchModelsCatalog(m.exec))
	}
	return nil
}

func (m *ModelsModel) HandleStatus(msg modelsStatusMsg) {
	m.loaded = true
	m.parseStatus(msg.output)
}

func (m *ModelsModel) HandleCatalog(msg modelsCatalogMsg) {
	m.modelsLoading = false
	m.modelsProvider = msg.provider
	m.modelsSource = msg.source
	m.modelsWarning = msg.warning
	m.modelsErr = msg.err
	m.modelOptions = append([]string(nil), msg.models...)
	if m.modelOptionsIdx >= len(m.modelOptions) {
		m.modelOptionsIdx = 0
	}
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

	if m.mode == modelsSelectModel {
		switch key {
		case "esc":
			m.mode = modelsNormal
			return m, nil
		case "r":
			m.modelsLoading = true
			return m, fetchModelsCatalog(m.exec)
		case "m":
			m.mode = modelsSetModel
			m.input.SetValue("")
			m.input.Focus()
			return m, nil
		case "up", "k":
			if !m.modelsLoading && m.modelOptionsIdx > 0 {
				m.modelOptionsIdx--
			}
			return m, nil
		case "down", "j":
			if !m.modelsLoading && m.modelOptionsIdx < len(m.modelOptions)-1 {
				m.modelOptionsIdx++
			}
			return m, nil
		case "enter":
			if m.modelsLoading || len(m.modelOptions) == 0 {
				return m, nil
			}
			selected := m.modelOptions[m.modelOptionsIdx]
			m.mode = modelsNormal
			return m, setModelCmd(m.exec, selected)
		}
		return m, nil
	}

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
		m.mode = modelsSelectModel
		if len(m.modelOptions) == 0 && !m.modelsLoading {
			m.modelsLoading = true
			return m, fetchModelsCatalog(m.exec)
		}
		return m, nil
	case "m":
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
		m.modelsLoading = true
		return m, tea.Batch(fetchModelsStatus(m.exec), fetchModelsCatalog(m.exec))
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
	lines = append(lines, fmt.Sprintf("  %s Select model   %s Manual model   %s Set reasoning effort   %s Refresh",
		styleKey.Render("[s]"),
		styleKey.Render("[m]"),
		styleKey.Render("[e]"),
		styleKey.Render("[l]"),
	))

	if m.mode == modelsSelectModel {
		lines = append(lines, "")
		lines = append(lines, styleBold.Render("  Pick a model:"))
		if m.modelsProvider != "" {
			source := m.modelsSource
			if source == "" {
				source = "unknown"
			}
			lines = append(lines, fmt.Sprintf("  %s %s (%s)",
				styleLabel.Render("Catalog:"),
				styleValue.Render(m.modelsProvider),
				source,
			))
		}

		if m.modelsLoading {
			lines = append(lines, "  "+styleDim.Render("Loading model catalog..."))
		} else if m.modelsErr != "" {
			lines = append(lines, "  "+styleErr.Render(m.modelsErr))
		}

		if !m.modelsLoading && len(m.modelOptions) > 0 {
			maxVisible := 10
			start := 0
			if m.modelOptionsIdx > maxVisible-3 {
				start = m.modelOptionsIdx - maxVisible + 3
			}
			end := start + maxVisible
			if end > len(m.modelOptions) {
				end = len(m.modelOptions)
			}

			for i := start; i < end; i++ {
				prefix := "  "
				if i == m.modelOptionsIdx {
					prefix = styleBold.Render("▸ ")
				}
				lines = append(lines, fmt.Sprintf("    %s%s", prefix, m.modelOptions[i]))
			}
		}

		if m.modelsWarning != "" {
			lines = append(lines, "  "+styleDim.Render(m.modelsWarning))
		}
		lines = append(lines, styleDim.Render("    ↑/↓ to pick, Enter to apply, [r] refresh, [m] manual, Esc cancel"))
	}

	if m.mode == modelsSetModel {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  Model id: %s", m.input.View()))
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
		cmd := "HOME=" + exec.HomePath() + " " + shellEscape(exec.BinaryPath()) + " models status 2>&1"
		out, _ := exec.ExecShell(10*time.Second, cmd)
		return modelsStatusMsg{output: out}
	}
}

func fetchModelsCatalog(exec Executor) tea.Cmd {
	type discoverPayload struct {
		Provider string   `json:"provider"`
		Source   string   `json:"source"`
		Models   []string `json:"models"`
		Warning  string   `json:"warning"`
	}

	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " " + shellEscape(exec.BinaryPath()) + " models discover --json 2>&1"
		out, err := exec.ExecShell(20*time.Second, cmd)
		trimmed := strings.TrimSpace(out)

		var payload discoverPayload
		if json.Unmarshal([]byte(trimmed), &payload) == nil {
			return modelsCatalogMsg{
				provider: payload.Provider,
				source:   payload.Source,
				models:   dedupeModelIDs(payload.Models),
				warning:  payload.Warning,
			}
		}

		// Fallback parser for plain text output.
		models := parseModelListOutput(trimmed)
		if len(models) > 0 {
			return modelsCatalogMsg{
				models: dedupeModelIDs(models),
				source: "plain",
			}
		}

		msg := firstNonEmptyLine(trimmed)
		if msg == "" && err != nil {
			msg = err.Error()
		}
		if msg == "" {
			msg = "No model catalog returned"
		}
		return modelsCatalogMsg{err: msg}
	}
}

func setModelCmd(exec Executor, model string) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " " + shellEscape(exec.BinaryPath()) + " models set " + shellEscape(model) + " 2>&1"
		_, _ = exec.ExecShell(10*time.Second, cmd)
		return actionDoneMsg{output: "Model set to " + model}
	}
}

func setEffortCmd(exec Executor, level string) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " " + shellEscape(exec.BinaryPath()) + " models effort " + level + " 2>&1"
		_, _ = exec.ExecShell(10*time.Second, cmd)
		return actionDoneMsg{output: "Reasoning effort set to " + level}
	}
}

func parseModelListOutput(output string) []string {
	var models []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "- ") {
			models = append(models, strings.TrimSpace(strings.TrimPrefix(line, "- ")))
		}
	}
	return models
}

func dedupeModelIDs(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func firstNonEmptyLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
