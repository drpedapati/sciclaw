package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
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

// SkillsModel handles the Skills tab with a master-detail layout.
type SkillsModel struct {
	exec        Executor
	mode        skillsMode
	loaded      bool
	selectedRow int
	skills      []skillRow

	// Viewports for master list and detail pane.
	listVP   viewport.Model
	detailVP viewport.Model
	width    int
	height   int

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

	listVP := viewport.New(60, 8)
	listVP.KeyMap = viewport.KeyMap{} // disable built-in keys
	detailVP := viewport.New(60, 8)
	detailVP.KeyMap = viewport.KeyMap{} // disable built-in keys

	return SkillsModel{
		exec:     exec,
		input:    ti,
		listVP:   listVP,
		detailVP: detailVP,
	}
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
	if m.selectedRow >= len(m.skills) {
		m.selectedRow = max(0, len(m.skills)-1)
	}
	m.rebuildListContent()
	m.rebuildDetailContent()
}

func (m *SkillsModel) HandleResize(width, height int) {
	m.width = width
	m.height = height
	w := width - 8
	if w > 96 {
		w = 96
	}
	if w < 40 {
		w = 40
	}
	avail := height - 14 // room for header, tab bar, keybindings, status bar
	listH := avail * 2 / 5
	if listH < 4 {
		listH = 4
	}
	detailH := avail - listH
	if detailH < 4 {
		detailH = 4
	}
	m.listVP.Width = w
	m.listVP.Height = listH
	m.detailVP.Width = w
	m.detailVP.Height = detailH
	m.rebuildListContent()
	m.rebuildDetailContent()
}

func (m *SkillsModel) rebuildListContent() {
	if len(m.skills) == 0 {
		m.listVP.SetContent(styleDim.Render("  No skills installed."))
		return
	}

	var lines []string
	for i, sk := range m.skills {
		indicator := "  "
		if i == m.selectedRow {
			indicator = styleBold.Foreground(colorAccent).Render("▸ ")
		}

		name := sk.Name
		if i == m.selectedRow {
			name = styleBold.Render(name)
		}

		source := ""
		if sk.Source != "" {
			source = styleDim.Render(" (" + sk.Source + ")")
		}

		line := fmt.Sprintf("  %s%s%s", indicator, name, source)
		if i == m.selectedRow {
			line = lipgloss.NewStyle().
				Background(lipgloss.Color("#2A2A4A")).
				Width(m.listVP.Width - 2).
				Render(line)
		}
		lines = append(lines, line)
	}
	m.listVP.SetContent(strings.Join(lines, "\n"))
}

func (m *SkillsModel) rebuildDetailContent() {
	if len(m.skills) == 0 || m.selectedRow >= len(m.skills) {
		m.detailVP.SetContent(styleDim.Render("  Select a skill to view details."))
		return
	}

	sk := m.skills[m.selectedRow]
	var lines []string
	lines = append(lines, fmt.Sprintf("  %s  %s", styleLabel.Render("Name:"), styleValue.Render(sk.Name)))
	if sk.Source != "" {
		lines = append(lines, fmt.Sprintf("  %s  %s", styleLabel.Render("Source:"), sourceBadge(sk.Source)))
	}
	if sk.Description != "" {
		lines = append(lines, "")
		// Word-wrap description to panel width.
		descW := m.detailVP.Width - 6
		if descW < 30 {
			descW = 30
		}
		for _, wrapped := range wrapText(sk.Description, descW) {
			lines = append(lines, "  "+wrapped)
		}
	}
	m.detailVP.SetContent(strings.Join(lines, "\n"))
	m.detailVP.GotoTop()
}

func sourceBadge(source string) string {
	switch source {
	case "workspace":
		return lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render(source)
	case "builtin":
		return lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(source)
	case "global":
		return lipgloss.NewStyle().Foreground(colorWarning).Bold(true).Render(source)
	default:
		return styleDim.Render(source)
	}
}

// wrapText wraps a string to the given width, breaking on spaces.
func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{text}
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return nil
	}
	var lines []string
	current := words[0]
	for _, w := range words[1:] {
		if len(current)+1+len(w) > width {
			lines = append(lines, current)
			current = w
		} else {
			current += " " + w
		}
	}
	lines = append(lines, current)
	return lines
}

// syncListScroll ensures the selected row is visible in the list viewport.
func (m *SkillsModel) syncListScroll() {
	if len(m.skills) == 0 {
		return
	}
	// Each row is one line in the viewport content.
	topVisible := m.listVP.YOffset
	bottomVisible := topVisible + m.listVP.Height - 1
	if m.selectedRow < topVisible {
		m.listVP.SetYOffset(m.selectedRow)
	} else if m.selectedRow > bottomVisible {
		m.listVP.SetYOffset(m.selectedRow - m.listVP.Height + 1)
	}
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
			m.rebuildListContent()
			m.rebuildDetailContent()
			m.syncListScroll()
		}
	case "down", "j":
		if m.selectedRow < len(m.skills)-1 {
			m.selectedRow++
			m.rebuildListContent()
			m.rebuildDetailContent()
			m.syncListScroll()
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
	if panelW > 100 {
		panelW = 100
	}
	if panelW < 40 {
		panelW = 40
	}

	if !m.loaded {
		return "\n  Loading skills...\n"
	}

	var b strings.Builder

	// Master list panel.
	listContent := m.listVP.View()
	listPanel := stylePanel.Width(panelW).Render(listContent)
	listTitle := stylePanelTitle.Render("Skills")
	b.WriteString(placePanelTitle(listPanel, listTitle))

	// Detail panel.
	detailContent := m.detailVP.View()
	detailPanel := stylePanel.Width(panelW).Render(detailContent)
	detailTitle := stylePanelTitle.Render("Detail")
	b.WriteString(placePanelTitle(detailPanel, detailTitle))

	// Keybindings.
	if len(m.skills) > 0 {
		b.WriteString(fmt.Sprintf("  %s Install   %s Remove   %s Refresh\n",
			styleKey.Render("[i]"),
			styleKey.Render("[r]"),
			styleKey.Render("[l]"),
		))
	} else {
		b.WriteString(fmt.Sprintf("  %s Install a skill   %s Refresh\n",
			styleKey.Render("[i]"),
			styleKey.Render("[l]"),
		))
	}

	// Overlays.
	if m.mode == skillsInstall {
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  GitHub repo: %s\n", m.input.View()))
		b.WriteString(styleHint.Render("    e.g. drpedapati/sciclaw-skills/weather") + "\n")
		b.WriteString(styleDim.Render("    Enter to install, Esc to cancel") + "\n")
	}

	if m.mode == skillsConfirmRemove {
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  Remove skill %s? %s / %s\n",
			styleBold.Render(m.removeName),
			styleKey.Render("[y]es"),
			styleKey.Render("[n]o"),
		))
	}

	return b.String()
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
