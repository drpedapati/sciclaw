package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type skillsMode int

const (
	skillsNormal skillsMode = iota
	skillsInstall
	skillsConfirmRemove
)

type skillsListMsg struct{ output string }

type skillRow struct {
	Name        string
	Source      string // workspace, global, builtin
	Description string
}

// SkillsModel handles the Skills tab.
type SkillsModel struct {
	exec        Executor
	mode        skillsMode
	loaded      bool
	selectedRow int
	skills      []skillRow

	// Install text input
	input textinput.Model

	// Remove confirmation
	removeName string
}

func NewSkillsModel(exec Executor) SkillsModel {
	ti := textinput.New()
	ti.CharLimit = 128
	ti.Width = 50
	ti.Placeholder = "e.g. drpedapati/sciclaw-skills/weather"
	return SkillsModel{exec: exec, input: ti}
}

func (m *SkillsModel) AutoRun() tea.Cmd {
	if !m.loaded {
		return fetchSkillsList(m.exec)
	}
	return nil
}

func (m *SkillsModel) HandleList(msg skillsListMsg) {
	m.loaded = true
	m.skills = parseSkillsList(msg.output)
}

func parseSkillsList(output string) []skillRow {
	var skills []skillRow
	lines := strings.Split(output, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		// Look for lines like: ✓ skill-name (source)
		if !strings.Contains(line, "✓") {
			continue
		}
		// Parse: ✓ name (source)
		idx := strings.Index(line, "✓")
		rest := strings.TrimSpace(line[idx+len("✓"):])

		name := rest
		source := ""
		if pIdx := strings.LastIndex(rest, "("); pIdx >= 0 {
			name = strings.TrimSpace(rest[:pIdx])
			source = strings.Trim(rest[pIdx:], "()")
		}

		desc := ""
		if i+1 < len(lines) {
			desc = strings.TrimSpace(lines[i+1])
		}

		skills = append(skills, skillRow{Name: name, Source: source, Description: desc})
		i++ // skip description line
	}
	return skills
}

func (m SkillsModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (SkillsModel, tea.Cmd) {
	key := msg.String()

	if m.mode == skillsInstall {
		switch key {
		case "esc":
			m.mode = skillsNormal
			m.input.Blur()
			return m, nil
		case "enter":
			val := strings.TrimSpace(m.input.Value())
			if val == "" {
				m.mode = skillsNormal
				m.input.Blur()
				return m, nil
			}
			m.mode = skillsNormal
			m.input.Blur()
			return m, installSkillCmd(m.exec, val)
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	if m.mode == skillsConfirmRemove {
		switch key {
		case "y", "Y":
			m.mode = skillsNormal
			return m, removeSkillCmd(m.exec, m.removeName)
		case "n", "N", "esc":
			m.mode = skillsNormal
		}
		return m, nil
	}

	// Normal mode.
	switch key {
	case "up", "k":
		if m.selectedRow > 0 {
			m.selectedRow--
		}
	case "down", "j":
		if m.selectedRow < len(m.skills)-1 {
			m.selectedRow++
		}
	case "i":
		m.mode = skillsInstall
		m.input.SetValue("")
		m.input.Focus()
		return m, nil
	case "r", "backspace", "delete":
		if m.selectedRow < len(m.skills) {
			m.removeName = m.skills[m.selectedRow].Name
			m.mode = skillsConfirmRemove
		}
	case "l":
		m.loaded = false
		return m, fetchSkillsList(m.exec)
	}
	return m, nil
}

func (m SkillsModel) View(snap *VMSnapshot, width int) string {
	panelW := width - 4
	if panelW < 40 {
		panelW = 40
	}

	if !m.loaded {
		return "\n  Loading skills...\n"
	}

	var lines []string

	if len(m.skills) == 0 {
		lines = append(lines, "")
		lines = append(lines, "  No skills installed.")
		lines = append(lines, "")
		lines = append(lines, styleDim.Render("  Skills extend your agent's capabilities."))
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %s Install a skill   %s Refresh",
			styleKey.Render("[i]"),
			styleKey.Render("[l]"),
		))
	} else {
		// Table header.
		lines = append(lines, fmt.Sprintf("  %-24s  %-12s  %s",
			styleDim.Render("Name"),
			styleDim.Render("Source"),
			styleDim.Render("Description"),
		))
		lines = append(lines, styleDim.Render("  "+strings.Repeat("─", 60)))

		for i, sk := range m.skills {
			line := fmt.Sprintf("  %-24s  %-12s  %s", sk.Name, sk.Source, sk.Description)
			if i == m.selectedRow && m.mode == skillsNormal {
				line = lipgloss.NewStyle().
					Background(lipgloss.Color("#2A2A4A")).
					Bold(true).
					Render(line)
			}
			lines = append(lines, line)
		}

		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %s Install   %s Remove selected   %s Refresh",
			styleKey.Render("[i]"),
			styleKey.Render("[r]"),
			styleKey.Render("[l]"),
		))
	}

	// Overlays.
	if m.mode == skillsInstall {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  GitHub repo: %s", m.input.View()))
		lines = append(lines, styleHint.Render("    e.g. drpedapati/sciclaw-skills/weather"))
		lines = append(lines, styleDim.Render("    Enter to install, Esc to cancel"))
	}

	if m.mode == skillsConfirmRemove {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  Remove skill %s? %s / %s",
			styleBold.Render(m.removeName),
			styleKey.Render("[y]es"),
			styleKey.Render("[n]o"),
		))
	}

	content := strings.Join(lines, "\n")
	panel := stylePanel.Width(panelW).Render(content)
	title := stylePanelTitle.Render("Skills")
	return placePanelTitle(panel, title)
}

func fetchSkillsList(exec Executor) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw skills list 2>&1"
		out, _ := exec.ExecShell(10*time.Second, cmd)
		return skillsListMsg{output: out}
	}
}

func installSkillCmd(exec Executor, repo string) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw skills install " + shellEscape(repo) + " 2>&1"
		out, _ := exec.ExecShell(30*time.Second, cmd)
		return actionDoneMsg{output: "Skill install: " + strings.TrimSpace(out)}
	}
}

func removeSkillCmd(exec Executor, name string) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw skills remove " + shellEscape(name) + " 2>&1"
		_, _ = exec.ExecShell(10*time.Second, cmd)
		return actionDoneMsg{output: "Removed skill " + name}
	}
}
