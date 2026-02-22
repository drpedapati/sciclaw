package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type routingMode int

const (
	routingNormal routingMode = iota
	routingConfirmRemove
)

// Messages for async routing operations.
type routingStatusMsg struct{ output string }
type routingListMsg struct{ output string }
type routingValidateMsg struct{ output string }
type routingReloadMsg struct{ output string }

type routingStatusInfo struct {
	Enabled          bool
	UnmappedBehavior string
	MappingCount     int
	InvalidCount     int
	ValidationOK     bool
	ValidationMsg    string
}

type routingRow struct {
	Channel        string
	ChatID         string
	Workspace      string
	AllowedSenders string
	Label          string
}

// RoutingModel handles the Routing tab with status + master-detail layout.
type RoutingModel struct {
	exec        Executor
	mode        routingMode
	loaded      bool
	selectedRow int
	status      routingStatusInfo
	mappings    []routingRow

	listVP   viewport.Model
	detailVP viewport.Model
	width    int
	height   int

	// Remove confirmation
	removeMapping routingRow

	// Inline feedback
	flashMsg   string
	flashUntil time.Time
}

func NewRoutingModel(exec Executor) RoutingModel {
	listVP := viewport.New(60, 6)
	listVP.KeyMap = viewport.KeyMap{}
	detailVP := viewport.New(60, 6)
	detailVP.KeyMap = viewport.KeyMap{}

	return RoutingModel{
		exec:     exec,
		listVP:   listVP,
		detailVP: detailVP,
	}
}

func (m *RoutingModel) AutoRun() tea.Cmd {
	if !m.loaded {
		return tea.Batch(fetchRoutingStatus(m.exec), fetchRoutingListCmd(m.exec))
	}
	return nil
}

func (m *RoutingModel) HandleStatus(msg routingStatusMsg) {
	m.status = parseRoutingStatus(msg.output)
}

func (m *RoutingModel) HandleList(msg routingListMsg) {
	m.loaded = true
	m.mappings = parseRoutingList(msg.output)
	if m.selectedRow >= len(m.mappings) {
		m.selectedRow = max(0, len(m.mappings)-1)
	}
	m.rebuildListContent()
	m.rebuildDetailContent()
}

func (m *RoutingModel) HandleValidate(msg routingValidateMsg) {
	out := strings.TrimSpace(msg.output)
	if strings.Contains(out, "valid") && !strings.Contains(out, "invalid") {
		m.flashMsg = styleOK.Render("✓") + " Validation passed"
	} else {
		m.flashMsg = styleErr.Render("✗") + " " + out
	}
	m.flashUntil = time.Now().Add(5 * time.Second)
}

func (m *RoutingModel) HandleReload(msg routingReloadMsg) {
	out := strings.TrimSpace(msg.output)
	if strings.Contains(out, "reload requested") || strings.Contains(out, "Routing reload") {
		m.flashMsg = styleOK.Render("✓") + " Reload requested"
	} else {
		m.flashMsg = styleErr.Render("✗") + " " + out
	}
	m.flashUntil = time.Now().Add(5 * time.Second)
}

func (m *RoutingModel) HandleResize(width, height int) {
	m.width = width
	m.height = height
	w := width - 8
	if w < 40 {
		w = 40
	}
	// Status panel is fixed (~5 lines rendered separately).
	// Remaining space split between list and detail viewports.
	avail := height - 20 // header, tab bar, status panel, keybindings, status bar
	listH := avail * 2 / 5
	if listH < 3 {
		listH = 3
	}
	detailH := avail - listH
	if detailH < 3 {
		detailH = 3
	}
	m.listVP.Width = w
	m.listVP.Height = listH
	m.detailVP.Width = w
	m.detailVP.Height = detailH
	m.rebuildListContent()
	m.rebuildDetailContent()
}

func (m *RoutingModel) rebuildListContent() {
	if len(m.mappings) == 0 {
		m.listVP.SetContent(styleDim.Render("  No routing mappings configured."))
		return
	}

	var lines []string
	for i, r := range m.mappings {
		indicator := "  "
		if i == m.selectedRow {
			indicator = styleBold.Foreground(colorAccent).Render("▸ ")
		}

		chatID := r.ChatID
		if len(chatID) > 14 {
			chatID = chatID[:12] + "…"
		}

		label := r.Label
		if label == "" || label == "-" {
			label = styleDim.Render("—")
		}

		channel := r.Channel
		if i == m.selectedRow {
			channel = styleBold.Render(channel)
		}

		line := fmt.Sprintf("  %s%-10s %-14s  %s", indicator, channel, chatID, label)
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

func (m *RoutingModel) rebuildDetailContent() {
	if len(m.mappings) == 0 || m.selectedRow >= len(m.mappings) {
		m.detailVP.SetContent(styleDim.Render("  Select a mapping to view details."))
		return
	}

	r := m.mappings[m.selectedRow]
	var lines []string
	lines = append(lines, fmt.Sprintf("  %s  %s", styleLabel.Render("Channel:"), styleValue.Render(r.Channel)))
	lines = append(lines, fmt.Sprintf("  %s  %s", styleLabel.Render("Chat ID:"), styleValue.Render(r.ChatID)))
	if r.Label != "" && r.Label != "-" {
		lines = append(lines, fmt.Sprintf("  %s  %s", styleLabel.Render("Label:"), styleValue.Render(r.Label)))
	}
	lines = append(lines, fmt.Sprintf("  %s  %s", styleLabel.Render("Workspace:"), r.Workspace))
	if r.AllowedSenders != "" {
		lines = append(lines, fmt.Sprintf("  %s  %s", styleLabel.Render("Senders:"), r.AllowedSenders))
	}
	m.detailVP.SetContent(strings.Join(lines, "\n"))
	m.detailVP.GotoTop()
}

func (m *RoutingModel) syncListScroll() {
	if len(m.mappings) == 0 {
		return
	}
	topVisible := m.listVP.YOffset
	bottomVisible := topVisible + m.listVP.Height - 1
	if m.selectedRow < topVisible {
		m.listVP.SetYOffset(m.selectedRow)
	} else if m.selectedRow > bottomVisible {
		m.listVP.SetYOffset(m.selectedRow - m.listVP.Height + 1)
	}
}

// --- Parsers ---

func parseRoutingStatus(output string) routingStatusInfo {
	info := routingStatusInfo{}
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		switch key {
		case "Routing enabled":
			info.Enabled = val == "true"
		case "Unmapped behavior":
			info.UnmappedBehavior = val
		case "Mappings":
			info.MappingCount, _ = strconv.Atoi(val)
		case "Invalid mappings":
			info.InvalidCount, _ = strconv.Atoi(val)
		case "Validation":
			info.ValidationMsg = val
			info.ValidationOK = strings.HasPrefix(val, "ok")
		}
	}
	return info
}

func parseRoutingList(output string) []routingRow {
	var mappings []routingRow
	lines := strings.Split(output, "\n")

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		// Mapping header: "- channel chat_id"
		if !strings.HasPrefix(line, "- ") {
			continue
		}
		rest := strings.TrimPrefix(line, "- ")
		fields := strings.Fields(rest)
		if len(fields) < 2 {
			continue
		}

		row := routingRow{
			Channel: fields[0],
			ChatID:  fields[1],
		}

		// Parse indented detail lines.
		for i+1 < len(lines) {
			next := lines[i+1]
			trimmed := strings.TrimSpace(next)
			if trimmed == "" || strings.HasPrefix(trimmed, "- ") {
				break
			}
			parts := strings.SplitN(trimmed, ":", 2)
			if len(parts) != 2 {
				break
			}
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			switch key {
			case "workspace":
				row.Workspace = val
			case "allowed_senders":
				row.AllowedSenders = val
			case "label":
				row.Label = val
			}
			i++
		}

		mappings = append(mappings, row)
	}
	return mappings
}

// --- Update ---

func (m RoutingModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (RoutingModel, tea.Cmd) {
	key := msg.String()

	if m.mode == routingConfirmRemove {
		switch key {
		case "y", "Y":
			m.mode = routingNormal
			return m, routingRemoveCmd(m.exec, m.removeMapping.Channel, m.removeMapping.ChatID)
		case "n", "N", "esc":
			m.mode = routingNormal
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
		if m.selectedRow < len(m.mappings)-1 {
			m.selectedRow++
			m.rebuildListContent()
			m.rebuildDetailContent()
			m.syncListScroll()
		}
	case "t":
		return m, routingToggleCmd(m.exec, !m.status.Enabled)
	case "d", "backspace", "delete":
		if m.selectedRow < len(m.mappings) {
			m.removeMapping = m.mappings[m.selectedRow]
			m.mode = routingConfirmRemove
		}
	case "v":
		return m, routingValidateCmd(m.exec)
	case "R":
		return m, routingReloadCmd(m.exec)
	case "l":
		m.loaded = false
		return m, tea.Batch(fetchRoutingStatus(m.exec), fetchRoutingListCmd(m.exec))
	}
	return m, nil
}

// --- View ---

func (m RoutingModel) View(snap *VMSnapshot, width int) string {
	panelW := width - 4
	if panelW < 40 {
		panelW = 40
	}

	if !m.loaded {
		return "\n  Loading routing configuration...\n"
	}

	var b strings.Builder

	// Status panel.
	b.WriteString(m.renderStatusPanel(panelW))

	// Mappings list panel.
	listContent := m.listVP.View()
	listPanel := stylePanel.Width(panelW).Render(listContent)
	listTitle := stylePanelTitle.Render("Mappings")
	b.WriteString(placePanelTitle(listPanel, listTitle))

	// Detail panel.
	detailContent := m.detailVP.View()
	detailPanel := stylePanel.Width(panelW).Render(detailContent)
	detailTitle := stylePanelTitle.Render("Detail")
	b.WriteString(placePanelTitle(detailPanel, detailTitle))

	// Keybindings.
	b.WriteString(fmt.Sprintf("  %s Toggle   %s Remove   %s Validate   %s Reload   %s Refresh\n",
		styleKey.Render("[t]"),
		styleKey.Render("[d]"),
		styleKey.Render("[v]"),
		styleKey.Render("[R]"),
		styleKey.Render("[l]"),
	))

	// Flash message.
	if !m.flashUntil.IsZero() && time.Now().Before(m.flashUntil) {
		b.WriteString("  " + m.flashMsg + "\n")
	}

	// Overlay: remove confirmation.
	if m.mode == routingConfirmRemove {
		b.WriteString("\n")
		b.WriteString(fmt.Sprintf("  Remove mapping %s? %s / %s\n",
			styleBold.Render(m.removeMapping.Channel+":"+m.removeMapping.ChatID),
			styleKey.Render("[y]es"),
			styleKey.Render("[n]o"),
		))
	}

	return b.String()
}

func (m RoutingModel) renderStatusPanel(panelW int) string {
	enabledIcon := styleOK.Render("✓")
	enabledText := styleOK.Render("Yes")
	if !m.status.Enabled {
		enabledIcon = styleErr.Render("✗")
		enabledText = styleErr.Render("No")
	}

	invalidText := fmt.Sprintf("%d", m.status.InvalidCount)
	if m.status.InvalidCount > 0 {
		invalidText = styleErr.Render(invalidText)
	}

	validationIcon := styleOK.Render("✓")
	validationText := m.status.ValidationMsg
	if !m.status.ValidationOK {
		validationIcon = styleErr.Render("✗")
		validationText = styleErr.Render(validationText)
	}

	var lines []string
	lines = append(lines, fmt.Sprintf("  Enabled: %s %s    Mappings: %s",
		enabledIcon, enabledText, styleBold.Render(fmt.Sprintf("%d", m.status.MappingCount))))
	lines = append(lines, fmt.Sprintf("  Unmapped: %s   Invalid: %s",
		styleBold.Render(m.status.UnmappedBehavior), invalidText))
	lines = append(lines, fmt.Sprintf("  Validation: %s %s", validationIcon, validationText))

	content := strings.Join(lines, "\n")
	panel := stylePanel.Width(panelW).Render(content)
	title := stylePanelTitle.Render("Routing Status")
	return placePanelTitle(panel, title)
}

// --- Commands ---

func fetchRoutingStatus(exec Executor) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw routing status 2>&1"
		out, _ := exec.ExecShell(10*time.Second, cmd)
		return routingStatusMsg{output: out}
	}
}

func fetchRoutingListCmd(exec Executor) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw routing list 2>&1"
		out, _ := exec.ExecShell(10*time.Second, cmd)
		return routingListMsg{output: out}
	}
}

func routingToggleCmd(exec Executor, enable bool) tea.Cmd {
	return func() tea.Msg {
		action := "disable"
		if enable {
			action = "enable"
		}
		cmd := "HOME=" + exec.HomePath() + " sciclaw routing " + action + " 2>&1"
		_, _ = exec.ExecShell(10*time.Second, cmd)
		return actionDoneMsg{output: "Routing " + action + "d"}
	}
}

func routingRemoveCmd(exec Executor, channel, chatID string) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw routing remove --channel " +
			shellEscape(channel) + " --chat-id " + shellEscape(chatID) + " 2>&1"
		_, _ = exec.ExecShell(10*time.Second, cmd)
		return actionDoneMsg{output: "Removed mapping " + channel + ":" + chatID}
	}
}

func routingValidateCmd(exec Executor) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw routing validate 2>&1"
		out, _ := exec.ExecShell(10*time.Second, cmd)
		return routingValidateMsg{output: out}
	}
}

func routingReloadCmd(exec Executor) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw routing reload 2>&1"
		out, _ := exec.ExecShell(10*time.Second, cmd)
		return routingReloadMsg{output: out}
	}
}
