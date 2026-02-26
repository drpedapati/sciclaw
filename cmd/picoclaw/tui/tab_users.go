package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type usersMode int

const (
	usersNormal usersMode = iota
	usersAdd
	usersConfirmRemove
	usersEdit
)

// userRow represents one entry in the unified users table.
type userRow struct {
	Channel string       // "discord" or "telegram"
	User    ApprovedUser // from allowlist.go
	OrigIdx int          // index in that channel's allow_from
}

// UsersModel handles the Users tab.
type UsersModel struct {
	exec        Executor
	mode        usersMode
	selectedRow int

	// Add wizard state
	addInput   textinput.Model
	addStep    int // 0=channel pick, 1=ID, 2=optional name
	addChannel string
	pendingID  string

	// Remove confirmation state
	removeRow userRow

	// Edit state
	editRow   userRow
	editInput textinput.Model
}

func NewUsersModel(exec Executor) UsersModel {
	ti := textinput.New()
	ti.CharLimit = 64
	ti.Width = 40
	ei := textinput.New()
	ei.CharLimit = 64
	ei.Width = 40
	return UsersModel{exec: exec, addInput: ti, editInput: ei}
}

func buildRows(snap *VMSnapshot) []userRow {
	if snap == nil {
		return nil
	}
	var rows []userRow
	for i, u := range snap.Discord.ApprovedUsers {
		rows = append(rows, userRow{Channel: "discord", User: u, OrigIdx: i})
	}
	for i, u := range snap.Telegram.ApprovedUsers {
		rows = append(rows, userRow{Channel: "telegram", User: u, OrigIdx: i})
	}
	return rows
}

func (m UsersModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (UsersModel, tea.Cmd) {
	if snap == nil {
		return m, nil
	}

	key := msg.String()
	rows := buildRows(snap)

	// Add wizard mode.
	if m.mode == usersAdd {
		switch key {
		case "esc":
			m.mode = usersNormal
			m.addInput.Blur()
			return m, nil
		}

		if m.addStep == 0 {
			// Channel pick — single keypress.
			switch key {
			case "d":
				m.addChannel = "discord"
				m.addStep = 1
				m.addInput.SetValue("")
				m.addInput.Placeholder = "e.g. 123456789012345678"
				m.addInput.Focus()
			case "t":
				m.addChannel = "telegram"
				m.addStep = 1
				m.addInput.SetValue("")
				m.addInput.Placeholder = "e.g. 123456789"
				m.addInput.Focus()
			}
			return m, nil
		}

		// Steps 1-2: text input.
		if key == "enter" {
			return m.handleAddSubmit()
		}
		var cmd tea.Cmd
		m.addInput, cmd = m.addInput.Update(msg)
		return m, cmd
	}

	// Remove confirmation mode.
	if m.mode == usersConfirmRemove {
		switch key {
		case "y", "Y":
			m.mode = usersNormal
			return m, removeUserFromConfig(m.exec, m.removeRow.Channel, m.removeRow.OrigIdx)
		case "n", "N", "esc":
			m.mode = usersNormal
		}
		return m, nil
	}

	// Edit mode.
	if m.mode == usersEdit {
		switch key {
		case "esc":
			m.mode = usersNormal
			m.editInput.Blur()
			return m, nil
		case "enter":
			name := strings.TrimSpace(m.editInput.Value())
			current := m.editRow.User
			entry := ""
			if current.UserID != "" {
				entry = FormatEntry(current.UserID, name)
			} else if name != "" {
				entry = name
			} else {
				entry = strings.TrimSpace(current.Raw)
			}
			if strings.TrimSpace(entry) == "" {
				m.mode = usersNormal
				m.editInput.Blur()
				return m, nil
			}
			m.mode = usersNormal
			m.editInput.Blur()
			return m, updateUserInConfig(m.exec, m.editRow.Channel, m.editRow.OrigIdx, entry)
		}
		var cmd tea.Cmd
		m.editInput, cmd = m.editInput.Update(msg)
		return m, cmd
	}

	// Normal mode.
	switch key {
	case "up", "k":
		if m.selectedRow > 0 {
			m.selectedRow--
		}
	case "down", "j":
		if m.selectedRow < len(rows)-1 {
			m.selectedRow++
		}
	case "a":
		m.mode = usersAdd
		m.addStep = 0
		m.addChannel = ""
		m.pendingID = ""
		m.addInput.Blur()
		return m, nil
	case "d", "backspace", "delete":
		if m.selectedRow < len(rows) {
			m.removeRow = rows[m.selectedRow]
			m.mode = usersConfirmRemove
		}
	case "e":
		if m.selectedRow < len(rows) {
			m.editRow = rows[m.selectedRow]
			m.mode = usersEdit
			m.editInput.SetValue(strings.TrimSpace(m.editRow.User.Username))
			m.editInput.Placeholder = "(optional display name)"
			m.editInput.Focus()
		}
	}
	return m, nil
}

func (m UsersModel) handleAddSubmit() (UsersModel, tea.Cmd) {
	val := strings.TrimSpace(m.addInput.Value())

	if m.addStep == 1 {
		// User ID submitted.
		if val == "" {
			m.mode = usersNormal
			m.addInput.Blur()
			return m, nil
		}
		m.pendingID = val
		m.addStep = 2
		m.addInput.SetValue("")
		m.addInput.Placeholder = "(optional display name)"
		return m, nil
	}

	// Step 2: optional name submitted.
	entry := FormatEntry(m.pendingID, val)
	m.mode = usersNormal
	m.addInput.Blur()
	return m, addUserToConfig(m.exec, m.addChannel, entry)
}

func (m UsersModel) View(snap *VMSnapshot, width int) string {
	if snap == nil {
		return "\n  No data available yet.\n"
	}

	panelW := width - 4
	if panelW < 40 {
		panelW = 40
	}

	rows := buildRows(snap)
	var lines []string

	if len(rows) == 0 {
		lines = append(lines, "")
		lines = append(lines, "  No approved users yet.")
		lines = append(lines, "")
		lines = append(lines, styleDim.Render("  Approved users control who can message your bot."))
		lines = append(lines, styleDim.Render("  Without any, anyone who finds your bot can use it."))
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %s Add a user", styleKey.Render("[a]")))
	} else {
		// Table header.
		lines = append(lines, fmt.Sprintf("  %s  %-10s  %-22s  %s",
			styleDim.Render(" # "),
			styleDim.Render("Channel"),
			styleDim.Render("User ID"),
			styleDim.Render("Name"),
		))
		lines = append(lines, styleDim.Render("  "+strings.Repeat("─", 55)))

		for i, row := range rows {
			chName := capitalizeFirst(row.Channel)
			id := row.User.DisplayID()
			name := row.User.DisplayName()
			num := fmt.Sprintf(" %d ", i+1)

			line := fmt.Sprintf("  %s  %-10s  %-22s  %s", num, chName, id, name)
			if i == m.selectedRow && m.mode == usersNormal {
				line = lipgloss.NewStyle().
					Background(lipgloss.Color("#2A2A4A")).
					Bold(true).
					Render(line)
			}
			lines = append(lines, line)
		}

		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %s Add user   %s Remove selected",
			styleKey.Render("[a]"),
			styleKey.Render("[d]"),
		))
		lines = append(lines, fmt.Sprintf("  %s Edit selected label",
			styleKey.Render("[e]"),
		))
	}

	// Overlay for add/remove modes.
	if m.mode == usersAdd {
		lines = append(lines, "")
		lines = append(lines, m.renderAddOverlay())
	}
	if m.mode == usersConfirmRemove {
		lines = append(lines, "")
		chName := capitalizeFirst(m.removeRow.Channel)
		lines = append(lines, fmt.Sprintf("  Remove %s from %s? %s / %s",
			styleBold.Render(m.removeRow.User.DisplayName()),
			styleBold.Render(chName),
			styleKey.Render("[y]es"),
			styleKey.Render("[n]o"),
		))
	}
	if m.mode == usersEdit {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  Edit display name for %s (%s): %s",
			styleBold.Render(m.editRow.User.DisplayName()),
			styleBold.Render(capitalizeFirst(m.editRow.Channel)),
			m.editInput.View(),
		))
		lines = append(lines, styleHint.Render("    Leave blank to clear label and keep only the ID"))
		lines = append(lines, styleDim.Render("    Enter to save, Esc to cancel"))
	}

	content := strings.Join(lines, "\n")
	panel := stylePanel.Width(panelW).Render(content)
	title := stylePanelTitle.Render("Approved Users")
	return placePanelTitle(panel, title)
}

func (m UsersModel) renderAddOverlay() string {
	var lines []string

	switch m.addStep {
	case 0:
		lines = append(lines, styleBold.Render("  Add user to which channel?"))
		lines = append(lines, fmt.Sprintf("  %s Discord   %s Telegram",
			styleKey.Render("[d]"),
			styleKey.Render("[t]"),
		))
		lines = append(lines, styleDim.Render("    Esc to cancel"))
	case 1:
		chName := capitalizeFirst(m.addChannel)
		lines = append(lines, fmt.Sprintf("  Enter %s User ID: %s", chName, m.addInput.View()))
		if m.addChannel == "discord" {
			lines = append(lines, styleHint.Render("    Discord Settings → Advanced → Developer Mode → Right-click avatar → Copy User ID"))
		} else {
			lines = append(lines, styleHint.Render("    Tip: Have them search @userinfobot in Telegram and send it a message"))
		}
		lines = append(lines, styleDim.Render("    Esc to cancel"))
	case 2:
		lines = append(lines, fmt.Sprintf("  Display name (optional): %s", m.addInput.View()))
		lines = append(lines, styleHint.Render("    Press Enter to skip"))
		lines = append(lines, styleDim.Render("    Esc to cancel"))
	}

	return strings.Join(lines, "\n")
}

func capitalizeFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
