package tui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type routingMode int

const (
	routingNormal routingMode = iota
	routingConfirmRemove
	routingAddWizard
	routingEditUsers
	routingEditWorkspace
	routingBrowseFolder
	routingPickRoom
	routingPairTelegram
)

type routingBrowseTarget int

const (
	browseTargetAddWizard routingBrowseTarget = iota
	browseTargetEditWorkspace
)

// Wizard steps for adding a mapping.
const (
	addStepChannel      = 0
	addStepChatID       = 1 // Discord: auto-picker; Telegram: choice screen
	addStepWorkspace    = 2
	addStepAllow        = 3
	addStepLabel        = 4
	addStepConfirm      = 5
	addStepChatIDManual = 6 // manual text input fallback (both channels)
)

// Messages for async routing operations.
type routingStatusMsg struct{ output string }
type routingListMsg struct{ output string }
type routingValidateMsg struct{ output string }
type routingReloadMsg struct{ output string }
type routingActionMsg struct {
	action  string
	output  string
	ok      bool
	channel string
	chatID  string
	sender  string
}
type routingDirListMsg struct {
	path string
	dirs []string
	err  string
}
type routingDiscordRoomsMsg struct {
	rooms []discordRoom
	err   string
}
type routingTelegramPairMsg struct {
	chatID   string
	chatType string
	username string
	err      string
}

type discordRoom struct {
	ChannelID   string
	GuildName   string
	ChannelName string
}

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

type routingExplainInfo struct {
	Sender       string
	Event        string
	Allowed      string
	Workspace    string
	SessionKey   string
	Reason       string
	MappingLabel string
	Raw          string
}

func (i routingExplainInfo) hasStructuredData() bool {
	return i.Event != "" || i.Allowed != "" || i.Workspace != "" || i.SessionKey != "" || i.Reason != ""
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

	// Add-mapping wizard state
	wizardStep          int
	wizardChannel       string
	wizardChatID        string
	wizardPath          string
	wizardAllow         string
	wizardLabel         string
	wizardInput         textinput.Model
	wizardAllowCursor   int
	wizardAllowSelected map[string]bool
	wizardAllowManual   bool

	// Edit-users/workspace state
	editUsersInput     textinput.Model
	editWorkspaceInput textinput.Model
	editUsersCursor    int
	editUsersSelected  map[string]bool
	editUsersExtras    []string
	editUsersManual    bool

	// Folder browser state
	browserPath    string
	browserEntries []string
	browserCursor  int
	browserLoading bool
	browserErr     string
	browserTarget  routingBrowseTarget

	// Discord room picker state
	discordRooms []discordRoom
	roomCursor   int
	roomsLoading bool
	roomsErr     string

	// Telegram pairing state
	pairLoading bool
	pairErr     string

	// Inline feedback
	flashMsg   string
	flashUntil time.Time

	// Last explain output pinned to a specific mapping.
	explainForKey string
	explainInfo   routingExplainInfo
}

func NewRoutingModel(exec Executor) RoutingModel {
	listVP := viewport.New(60, 6)
	listVP.KeyMap = viewport.KeyMap{}
	detailVP := viewport.New(60, 6)
	detailVP.KeyMap = viewport.KeyMap{}

	wi := textinput.New()
	wi.CharLimit = 200
	wi.Width = 50

	ei := textinput.New()
	ei.CharLimit = 200
	ei.Width = 50
	ew := textinput.New()
	ew.CharLimit = 300
	ew.Width = 50

	return RoutingModel{
		exec:               exec,
		listVP:             listVP,
		detailVP:           detailVP,
		wizardInput:        wi,
		editUsersInput:     ei,
		editWorkspaceInput: ew,
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
	if m.explainForKey != "" && !m.hasMappingKey(m.explainForKey) {
		m.explainForKey = ""
		m.explainInfo = routingExplainInfo{}
	}
	m.rebuildListContent()
	m.syncListScroll()
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

func (m *RoutingModel) HandleAction(msg routingActionMsg) {
	if msg.action == "explain" {
		out := strings.TrimSpace(msg.output)
		m.explainInfo = parseRoutingExplainOutput(out)
		m.explainInfo.Sender = strings.TrimSpace(msg.sender)
		if strings.TrimSpace(msg.channel) != "" && strings.TrimSpace(msg.chatID) != "" {
			m.explainForKey = routingRowKey(msg.channel, msg.chatID)
		} else if m.selectedRow < len(m.mappings) {
			row := m.mappings[m.selectedRow]
			m.explainForKey = routingRowKey(row.Channel, row.ChatID)
		}
		if msg.ok && m.explainInfo.hasStructuredData() {
			m.flashMsg = styleOK.Render("✓") + " Explain updated"
		} else {
			if out == "" {
				out = "Explain failed"
			}
			m.flashMsg = styleErr.Render("✗") + " " + out
		}
		m.flashUntil = time.Now().Add(6 * time.Second)
		m.rebuildDetailContent()
		return
	}

	defaultLabel := "Routing action"
	switch msg.action {
	case "add":
		defaultLabel = "Mapping saved"
	case "remove":
		defaultLabel = "Mapping detached"
	case "set-users":
		defaultLabel = "Allowed users updated"
	case "enable":
		defaultLabel = "Routing enabled"
	case "disable":
		defaultLabel = "Routing disabled"
	}

	out := strings.TrimSpace(msg.output)
	if msg.ok {
		if out == "" {
			out = defaultLabel
		}
		m.flashMsg = styleOK.Render("✓") + " " + out
	} else {
		if out == "" {
			out = defaultLabel + " failed"
		}
		m.flashMsg = styleErr.Render("✗") + " " + out
	}
	m.flashUntil = time.Now().Add(6 * time.Second)
}

func (m *RoutingModel) HandleDirList(msg routingDirListMsg) {
	m.browserLoading = false
	if msg.path != m.browserPath {
		return // stale response
	}
	if msg.err != "" {
		m.browserErr = msg.err
		m.browserEntries = nil
	} else {
		m.browserErr = ""
		m.browserEntries = msg.dirs
		m.browserCursor = 0
	}
}

func (m *RoutingModel) HandleResize(width, height int) {
	m.width = width
	m.height = height
	w := width - 8
	if w > 96 {
		w = 96
	}
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
	m.syncListScroll()
	m.rebuildDetailContent()
}

func (m *RoutingModel) rebuildListContent() {
	if len(m.mappings) == 0 {
		m.listVP.SetContent(styleDim.Render("  No routing mappings configured."))
		return
	}

	var lines []string
	labelW := m.listVP.Width/2 - 6
	if labelW < 12 {
		labelW = 12
	}
	if labelW > 28 {
		labelW = 28
	}
	for i, r := range m.mappings {
		indicator := "  "
		if i == m.selectedRow {
			indicator = styleBold.Foreground(colorAccent).Render("▸ ")
		}

		label := r.Label
		if label == "" || label == "-" {
			// Fallback: use last segment of workspace path.
			label = filepath.Base(r.Workspace)
			if label == "." || label == "/" || label == "" {
				label = "untitled"
			}
		}

		if i == m.selectedRow {
			label = styleBold.Render(label)
		}

		meta := fmt.Sprintf("%s \u2022 %s", r.Channel, truncateMiddle(r.ChatID, 16))
		line := fmt.Sprintf("  %s%-*s %s", indicator, labelW, label, styleDim.Render(meta))
		if i == m.selectedRow {
			lineW := m.listVP.Width - 2
			if lineW < 0 {
				lineW = 0
			}
			line = lipgloss.NewStyle().
				Background(lipgloss.Color("#2A2A4A")).
				Width(lineW).
				Render(line)
		}
		lines = append(lines, line)
	}
	m.listVP.SetContent(strings.Join(lines, "\n"))
}

// truncatePathComponents returns the last n path components, prefixed with ".../" if truncated.
func truncatePathComponents(p string, n int) string {
	if p == "" {
		return p
	}
	// Replace home dir prefix with ~.
	cleaned := filepath.Clean(p)
	parts := strings.Split(cleaned, string(filepath.Separator))
	// Remove empty leading element from absolute paths.
	var nonEmpty []string
	for _, s := range parts {
		if s != "" {
			nonEmpty = append(nonEmpty, s)
		}
	}
	if len(nonEmpty) <= n {
		return p
	}
	return "\u2026/" + strings.Join(nonEmpty[len(nonEmpty)-n:], "/")
}

func truncateMiddle(s string, maxLen int) string {
	if len(s) <= maxLen || maxLen < 5 {
		return s
	}
	head := (maxLen - 1) / 2
	tail := maxLen - head - 1
	return s[:head] + "\u2026" + s[len(s)-tail:]
}

func truncateValue(s string, maxLen int) string {
	if len(s) <= maxLen || maxLen < 2 {
		return s
	}
	return s[:maxLen-1] + "\u2026"
}

func routingRowKey(channel, chatID string) string {
	return strings.ToLower(strings.TrimSpace(channel)) + ":" + strings.TrimSpace(chatID)
}

func (m *RoutingModel) hasMappingKey(key string) bool {
	for _, row := range m.mappings {
		if routingRowKey(row.Channel, row.ChatID) == key {
			return true
		}
	}
	return false
}

func (m *RoutingModel) rebuildDetailContent() {
	if len(m.mappings) == 0 || m.selectedRow >= len(m.mappings) {
		m.detailVP.SetContent(styleDim.Render("  Select a mapping to view details."))
		return
	}

	r := m.mappings[m.selectedRow]
	maxValW := m.detailVP.Width - 18
	if maxValW < 20 {
		maxValW = 20
	}

	detailLabel := lipgloss.NewStyle().Foreground(colorMuted).Width(16)

	var lines []string
	lines = append(lines, fmt.Sprintf("  %s  %s", detailLabel.Render("Channel:"), styleValue.Render(r.Channel)))
	lines = append(lines, fmt.Sprintf("  %s  %s", detailLabel.Render("Chat room:"), styleValue.Render(r.ChatID)))
	if r.Label != "" && r.Label != "-" {
		lines = append(lines, fmt.Sprintf("  %s  %s", detailLabel.Render("Label:"), styleValue.Render(r.Label)))
	}
	ws := r.Workspace
	if len(ws) > maxValW {
		ws = "\u2026" + ws[len(ws)-maxValW+1:]
	}
	lines = append(lines, fmt.Sprintf("  %s  %s", detailLabel.Render("Folder:"), ws))
	if r.AllowedSenders != "" {
		senders := r.AllowedSenders
		if len(senders) > maxValW {
			senders = senders[:maxValW-1] + "\u2026"
		}
		lines = append(lines, fmt.Sprintf("  %s  %s", detailLabel.Render("Allowed users:"), senders))
	}
	lines = append(lines, "")
	modifyCmd := fmt.Sprintf("sciclaw routing set-users --channel %s --chat-id %s --allow <ids>", r.Channel, r.ChatID)
	lines = append(lines, "  "+styleDim.Render("Modify: ")+styleValue.Render(modifyCmd))
	explainSender := firstAllowedSender(r.AllowedSenders)
	if explainSender == "" {
		lines = append(lines, "  "+styleDim.Render("Explain: ")+styleHint.Render("add at least one allowed sender, then press [e]"))
	} else {
		explainCmd := fmt.Sprintf("sciclaw routing explain --channel %s --chat-id %s --sender %s", r.Channel, r.ChatID, explainSender)
		lines = append(lines, "  "+styleDim.Render("Explain: ")+styleValue.Render(explainCmd))
	}
	if m.explainForKey == routingRowKey(r.Channel, r.ChatID) && strings.TrimSpace(m.explainInfo.Raw) != "" {
		lines = append(lines, "")
		lines = append(lines, "  "+styleBold.Render("Explain output"))
		if m.explainInfo.hasStructuredData() {
			if m.explainInfo.Sender != "" {
				lines = append(lines, fmt.Sprintf("  %s  %s", detailLabel.Render("Sender:"), styleValue.Render(truncateValue(m.explainInfo.Sender, maxValW))))
			}
			if m.explainInfo.Event != "" {
				lines = append(lines, fmt.Sprintf("  %s  %s", detailLabel.Render("Event:"), styleValue.Render(truncateValue(m.explainInfo.Event, maxValW))))
			}
			if m.explainInfo.Allowed != "" {
				lines = append(lines, fmt.Sprintf("  %s  %s", detailLabel.Render("Allowed:"), styleValue.Render(truncateValue(m.explainInfo.Allowed, maxValW))))
			}
			if m.explainInfo.Workspace != "" {
				lines = append(lines, fmt.Sprintf("  %s  %s", detailLabel.Render("Workspace:"), styleValue.Render(truncateValue(m.explainInfo.Workspace, maxValW))))
			}
			if m.explainInfo.SessionKey != "" {
				lines = append(lines, fmt.Sprintf("  %s  %s", detailLabel.Render("Session key:"), styleValue.Render(truncateValue(m.explainInfo.SessionKey, maxValW))))
			}
			if m.explainInfo.Reason != "" {
				lines = append(lines, fmt.Sprintf("  %s  %s", detailLabel.Render("Reason:"), styleValue.Render(truncateValue(m.explainInfo.Reason, maxValW))))
			}
			if m.explainInfo.MappingLabel != "" {
				lines = append(lines, fmt.Sprintf("  %s  %s", detailLabel.Render("Mapping label:"), styleValue.Render(truncateValue(m.explainInfo.MappingLabel, maxValW))))
			}
		} else {
			lines = append(lines, "  "+styleErr.Render(truncateValue(m.explainInfo.Raw, maxValW*3)))
		}
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

func channelApprovedUsers(channel string, snap *VMSnapshot) []ApprovedUser {
	if snap == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(channel)) {
	case "telegram":
		return snap.Telegram.ApprovedUsers
	default:
		return snap.Discord.ApprovedUsers
	}
}

func parseAllowCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		v := strings.TrimSpace(p)
		if v == "" {
			continue
		}
		out = append(out, v)
	}
	return out
}

func firstAllowedSender(allowCSV string) string {
	for _, raw := range parseAllowCSV(allowCSV) {
		token := canonicalSenderToken(raw)
		if token != "" {
			return token
		}
	}
	return ""
}

func parseRoutingExplainOutput(output string) routingExplainInfo {
	info := routingExplainInfo{Raw: strings.TrimSpace(output)}
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])
		switch key {
		case "event":
			info.Event = val
		case "allowed":
			info.Allowed = val
		case "workspace":
			info.Workspace = val
		case "session_key":
			info.SessionKey = val
		case "reason":
			info.Reason = val
		case "mapping_label":
			info.MappingLabel = val
		}
	}
	return info
}

func canonicalSenderToken(raw string) string {
	parsed := ParseApprovedUser(raw)
	if strings.TrimSpace(parsed.UserID) != "" {
		return strings.TrimSpace(parsed.UserID)
	}
	if strings.TrimSpace(parsed.Raw) != "" {
		return strings.TrimSpace(parsed.Raw)
	}
	return strings.TrimSpace(raw)
}

func approvedUserToken(u ApprovedUser) string {
	if strings.TrimSpace(u.UserID) != "" {
		return strings.TrimSpace(u.UserID)
	}
	return canonicalSenderToken(u.Raw)
}

func initAllowSelection(users []ApprovedUser, allowCSV string) (map[string]bool, []string) {
	selected := map[string]bool{}
	extras := []string{}

	available := map[string]struct{}{}
	for _, u := range users {
		token := approvedUserToken(u)
		if token != "" {
			available[token] = struct{}{}
		}
	}

	seenExtras := map[string]struct{}{}
	for _, raw := range parseAllowCSV(allowCSV) {
		canonical := canonicalSenderToken(raw)
		if canonical == "" {
			continue
		}
		if _, ok := available[canonical]; ok {
			selected[canonical] = true
			continue
		}
		if _, seen := seenExtras[canonical]; seen {
			continue
		}
		seenExtras[canonical] = struct{}{}
		extras = append(extras, strings.TrimSpace(raw))
	}

	return selected, extras
}

func buildAllowCSV(users []ApprovedUser, selected map[string]bool, extras []string) string {
	if selected == nil {
		selected = map[string]bool{}
	}
	out := make([]string, 0, len(users)+len(extras))
	seen := map[string]struct{}{}

	for _, u := range users {
		token := approvedUserToken(u)
		if token == "" || !selected[token] {
			continue
		}
		if _, ok := seen[token]; ok {
			continue
		}
		seen[token] = struct{}{}
		out = append(out, token)
	}

	for _, raw := range extras {
		token := strings.TrimSpace(raw)
		if token == "" {
			continue
		}
		canonical := canonicalSenderToken(token)
		if canonical == "" {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, token)
	}

	return strings.Join(out, ",")
}

func allUsersSelected(users []ApprovedUser, selected map[string]bool) bool {
	if len(users) == 0 {
		return false
	}
	for _, u := range users {
		token := approvedUserToken(u)
		if token == "" {
			continue
		}
		if !selected[token] {
			return false
		}
	}
	return true
}

// --- Update ---

func (m RoutingModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (RoutingModel, tea.Cmd) {
	switch m.mode {
	case routingConfirmRemove:
		return m.updateConfirmRemove(msg)
	case routingAddWizard:
		return m.updateAddWizard(msg, snap)
	case routingEditUsers:
		return m.updateEditUsers(msg, snap)
	case routingEditWorkspace:
		return m.updateEditWorkspace(msg, snap)
	case routingBrowseFolder:
		return m.updateBrowseFolder(msg, snap)
	case routingPickRoom:
		return m.updatePickRoom(msg, snap)
	case routingPairTelegram:
		return m.updatePairTelegram(msg)
	default:
		return m.updateNormal(msg, snap)
	}
}

func (m RoutingModel) updateConfirmRemove(msg tea.KeyMsg) (RoutingModel, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.mode = routingNormal
		return m, routingRemoveCmd(m.exec, m.removeMapping.Channel, m.removeMapping.ChatID)
	case "n", "N", "esc":
		m.mode = routingNormal
	}
	return m, nil
}

func (m RoutingModel) updateNormal(msg tea.KeyMsg, snap *VMSnapshot) (RoutingModel, tea.Cmd) {
	switch msg.String() {
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
	case "d", "backspace", "delete", "x":
		if m.selectedRow < len(m.mappings) {
			m.removeMapping = m.mappings[m.selectedRow]
			m.mode = routingConfirmRemove
		}
	case "e":
		return m.startExplain()
	case "f":
		return m.startEditWorkspace()
	case "v":
		return m, routingValidateCmd(m.exec)
	case "R":
		return m, routingReloadCmd(m.exec)
	case "l":
		m.loaded = false
		return m, tea.Batch(fetchRoutingStatus(m.exec), fetchRoutingListCmd(m.exec))
	case "a":
		return m.startAddWizard(snap)
	case "u":
		return m.startEditUsers(snap)
	}
	return m, nil
}

func (m RoutingModel) startExplain() (RoutingModel, tea.Cmd) {
	if m.selectedRow >= len(m.mappings) {
		return m, nil
	}
	row := m.mappings[m.selectedRow]
	sender := firstAllowedSender(row.AllowedSenders)
	if sender == "" {
		m.flashMsg = styleErr.Render("✗") + " Add at least one allowed sender before running explain"
		m.flashUntil = time.Now().Add(5 * time.Second)
		m.rebuildDetailContent()
		return m, nil
	}
	return m, routingExplainCmd(m.exec, row.Channel, row.ChatID, sender)
}

// --- Add Wizard ---

func (m RoutingModel) startAddWizard(snap *VMSnapshot) (RoutingModel, tea.Cmd) {
	m.mode = routingAddWizard
	m.wizardStep = addStepChannel
	m.wizardChannel = "discord"
	m.wizardChatID = ""
	m.wizardPath = ""
	m.wizardAllow = ""
	m.wizardLabel = ""
	m.wizardAllowCursor = 0
	m.wizardAllowSelected = nil
	m.wizardAllowManual = false
	m.wizardInput.SetValue("")
	m.wizardInput.Blur()
	return m, nil
}

func (m *RoutingModel) initWizardAllowStep(snap *VMSnapshot) {
	users := channelApprovedUsers(m.wizardChannel, snap)
	m.wizardAllowCursor = 0
	m.wizardAllowSelected = map[string]bool{}
	m.wizardAllowManual = len(users) == 0

	if len(users) == 0 {
		m.wizardInput.SetValue("")
		m.wizardInput.Placeholder = "sender_id1,sender_id2"
		m.wizardInput.Focus()
		return
	}

	// Default to all known approved users selected for new mappings.
	for _, u := range users {
		token := approvedUserToken(u)
		if token == "" {
			continue
		}
		m.wizardAllowSelected[token] = true
	}
	m.wizardInput.Blur()
}

func (m RoutingModel) updateAddWizard(msg tea.KeyMsg, snap *VMSnapshot) (RoutingModel, tea.Cmd) {
	key := msg.String()

	if key == "esc" {
		m.mode = routingNormal
		m.wizardInput.Blur()
		return m, nil
	}

	switch m.wizardStep {
	case addStepChannel:
		switch key {
		case "left", "right", " ":
			if m.wizardChannel == "discord" {
				m.wizardChannel = "telegram"
			} else {
				m.wizardChannel = "discord"
			}
		case "enter":
			m.wizardStep = addStepChatID
			if m.wizardChannel == "discord" {
				// Auto-transition to Discord room picker
				return m.startDiscordPicker()
			}
			// Telegram: show choice screen (p/m)
		}
		return m, nil

	case addStepChatID:
		// Telegram choice screen: [p] pair or [m] manual
		switch key {
		case "p":
			return m.startTelegramPair()
		case "m":
			m.wizardStep = addStepChatIDManual
			m.wizardInput.SetValue("")
			m.wizardInput.Placeholder = "e.g. -1001234567890"
			m.wizardInput.Focus()
		}
		return m, nil

	case addStepChatIDManual:
		if key == "enter" {
			val := strings.TrimSpace(m.wizardInput.Value())
			if val == "" {
				return m, nil
			}
			m.wizardChatID = val
			m.wizardStep = addStepWorkspace
			m.wizardInput.SetValue("")
			m.wizardInput.Placeholder = "/absolute/path/to/workspace"
			if snap != nil && snap.WorkspacePath != "" {
				m.wizardInput.SetValue(expandHomeForExecPath(snap.WorkspacePath, m.exec.HomePath()))
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.wizardInput, cmd = m.wizardInput.Update(msg)
		return m, cmd

	case addStepWorkspace:
		switch key {
		case "enter":
			val := strings.TrimSpace(m.wizardInput.Value())
			if val == "" {
				return m, nil
			}
			m.wizardPath = expandHomeForExecPath(val, m.exec.HomePath())
			m.wizardStep = addStepAllow
			m.initWizardAllowStep(snap)
			return m, nil
		case "ctrl+b":
			return m, m.startBrowse(snap)
		}
		var cmd tea.Cmd
		m.wizardInput, cmd = m.wizardInput.Update(msg)
		return m, cmd

	case addStepAllow:
		users := channelApprovedUsers(m.wizardChannel, snap)
		if len(users) == 0 || m.wizardAllowManual {
			switch key {
			case "enter":
				val := strings.TrimSpace(m.wizardInput.Value())
				if val == "" {
					return m, nil
				}
				m.wizardAllow = val
				m.wizardStep = addStepLabel
				m.wizardInput.SetValue("")
				m.wizardInput.Placeholder = "(optional label)"
				return m, nil
			case "p":
				if len(users) > 0 {
					m.wizardAllowManual = false
					m.wizardInput.Blur()
					if m.wizardAllowSelected == nil {
						m.wizardAllowSelected, _ = initAllowSelection(users, m.wizardInput.Value())
					}
				}
				return m, nil
			}
			var cmd tea.Cmd
			m.wizardInput, cmd = m.wizardInput.Update(msg)
			return m, cmd
		}

		if m.wizardAllowSelected == nil {
			m.wizardAllowSelected = map[string]bool{}
		}

		switch key {
		case "up", "k":
			if m.wizardAllowCursor > 0 {
				m.wizardAllowCursor--
			}
			return m, nil
		case "down", "j":
			if m.wizardAllowCursor < len(users)-1 {
				m.wizardAllowCursor++
			}
			return m, nil
		case " ":
			if m.wizardAllowCursor >= 0 && m.wizardAllowCursor < len(users) {
				token := approvedUserToken(users[m.wizardAllowCursor])
				if token != "" {
					m.wizardAllowSelected[token] = !m.wizardAllowSelected[token]
				}
			}
			return m, nil
		case "a":
			selectAll := !allUsersSelected(users, m.wizardAllowSelected)
			for _, u := range users {
				token := approvedUserToken(u)
				if token == "" {
					continue
				}
				m.wizardAllowSelected[token] = selectAll
			}
			return m, nil
		case "m":
			m.wizardAllowManual = true
			m.wizardInput.SetValue(buildAllowCSV(users, m.wizardAllowSelected, nil))
			m.wizardInput.Placeholder = "sender_id1,sender_id2"
			m.wizardInput.Focus()
			return m, nil
		case "enter":
			val := buildAllowCSV(users, m.wizardAllowSelected, nil)
			if strings.TrimSpace(val) == "" {
				return m, nil
			}
			m.wizardAllow = val
			m.wizardStep = addStepLabel
			m.wizardInput.SetValue("")
			m.wizardInput.Placeholder = "(optional label)"
			return m, nil
		}
		return m, nil

	case addStepLabel:
		if key == "enter" {
			m.wizardLabel = strings.TrimSpace(m.wizardInput.Value())
			m.wizardStep = addStepConfirm
			m.wizardInput.Blur()
			return m, nil
		}
		var cmd tea.Cmd
		m.wizardInput, cmd = m.wizardInput.Update(msg)
		return m, cmd

	case addStepConfirm:
		if key == "enter" {
			m.mode = routingNormal
			return m, routingAddMappingCmd(m.exec, m.wizardChannel, m.wizardChatID,
				m.wizardPath, m.wizardAllow, m.wizardLabel)
		}
		return m, nil
	}

	return m, nil
}

// --- Folder Browser ---

func (m *RoutingModel) startBrowse(snap *VMSnapshot) tea.Cmd {
	return m.startBrowseFromInput(strings.TrimSpace(m.wizardInput.Value()), snap, browseTargetAddWizard)
}

func (m *RoutingModel) startBrowseFromInput(rawPath string, snap *VMSnapshot, target routingBrowseTarget) tea.Cmd {
	m.mode = routingBrowseFolder
	m.browserTarget = target
	startPath := expandHomeForExecPath(rawPath, m.exec.HomePath())
	if startPath == "" || !filepath.IsAbs(startPath) {
		if target == browseTargetEditWorkspace && m.selectedRow < len(m.mappings) {
			startPath = expandHomeForExecPath(m.mappings[m.selectedRow].Workspace, m.exec.HomePath())
		}
		if (startPath == "" || !filepath.IsAbs(startPath)) && snap != nil && snap.WorkspacePath != "" {
			startPath = expandHomeForExecPath(snap.WorkspacePath, m.exec.HomePath())
		}
		if startPath == "" || !filepath.IsAbs(startPath) {
			startPath = m.exec.HomePath()
		}
	}
	m.browserPath = startPath
	m.browserCursor = 0
	m.browserLoading = true
	m.browserErr = ""
	m.browserEntries = nil
	if target == browseTargetEditWorkspace {
		m.editWorkspaceInput.Blur()
	} else {
		m.wizardInput.Blur()
	}
	return fetchDirListCmd(m.exec, startPath)
}

func (m RoutingModel) updateBrowseFolder(msg tea.KeyMsg, snap *VMSnapshot) (RoutingModel, tea.Cmd) {
	key := msg.String()

	restoreFromBrowse := func(path string) (RoutingModel, tea.Cmd) {
		switch m.browserTarget {
		case browseTargetEditWorkspace:
			m.mode = routingEditWorkspace
			m.editWorkspaceInput.SetValue(path)
			m.editWorkspaceInput.Focus()
			return m, nil
		default:
			m.mode = routingAddWizard
			m.wizardStep = addStepWorkspace
			m.wizardInput.SetValue(path)
			m.wizardInput.Focus()
			return m, nil
		}
	}

	switch key {
	case "esc":
		return restoreFromBrowse(m.browserPath)

	case "up", "k":
		if m.browserCursor > 0 {
			m.browserCursor--
		}
		return m, nil

	case "down", "j":
		maxIdx := len(m.browserEntries) + 1 // ".." + entries + "[Select]"
		if m.browserCursor < maxIdx {
			m.browserCursor++
		}
		return m, nil

	case "enter":
		selectIdx := len(m.browserEntries) + 1
		if m.browserCursor == 0 {
			// Go up to parent
			parent := filepath.Dir(m.browserPath)
			if parent == m.browserPath {
				return m, nil
			}
			m.browserPath = parent
			m.browserCursor = 0
			m.browserLoading = true
			return m, fetchDirListCmd(m.exec, parent)
		} else if m.browserCursor == selectIdx {
			// Select current folder
			return restoreFromBrowse(m.browserPath)
		} else {
			// Descend into directory
			dirName := m.browserEntries[m.browserCursor-1]
			newPath := filepath.Join(m.browserPath, dirName)
			m.browserPath = newPath
			m.browserCursor = 0
			m.browserLoading = true
			return m, fetchDirListCmd(m.exec, newPath)
		}

	case " ":
		// Space selects current folder
		return restoreFromBrowse(m.browserPath)
	}

	return m, nil
}

// --- Edit Users ---

func (m RoutingModel) startEditUsers(snap *VMSnapshot) (RoutingModel, tea.Cmd) {
	if m.selectedRow >= len(m.mappings) {
		return m, nil
	}
	m.mode = routingEditUsers
	row := m.mappings[m.selectedRow]
	m.editUsersInput.SetValue(row.AllowedSenders)
	m.editUsersInput.Placeholder = "sender_id1,sender_id2"
	m.editUsersCursor = 0
	users := channelApprovedUsers(row.Channel, snap)
	m.editUsersSelected, m.editUsersExtras = initAllowSelection(users, row.AllowedSenders)
	m.editUsersManual = len(users) == 0
	if m.editUsersManual {
		m.editUsersInput.Focus()
	} else {
		m.editUsersInput.Blur()
	}
	return m, nil
}

func (m RoutingModel) updateEditUsers(msg tea.KeyMsg, snap *VMSnapshot) (RoutingModel, tea.Cmd) {
	key := msg.String()
	if key == "esc" {
		m.mode = routingNormal
		m.editUsersInput.Blur()
		return m, nil
	}

	if m.selectedRow >= len(m.mappings) {
		m.mode = routingNormal
		m.editUsersInput.Blur()
		return m, nil
	}
	row := m.mappings[m.selectedRow]
	users := channelApprovedUsers(row.Channel, snap)

	if len(users) == 0 || m.editUsersManual {
		switch key {
		case "enter":
			val := strings.TrimSpace(m.editUsersInput.Value())
			if val == "" {
				return m, nil
			}
			m.mode = routingNormal
			m.editUsersInput.Blur()
			return m, routingSetUsersCmd(m.exec, row.Channel, row.ChatID, val)
		case "p":
			if len(users) > 0 {
				m.editUsersManual = false
				m.editUsersInput.Blur()
				if m.editUsersSelected == nil {
					m.editUsersSelected, m.editUsersExtras = initAllowSelection(users, m.editUsersInput.Value())
				}
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.editUsersInput, cmd = m.editUsersInput.Update(msg)
		return m, cmd
	}

	if m.editUsersSelected == nil {
		m.editUsersSelected = map[string]bool{}
	}

	switch key {
	case "up", "k":
		if m.editUsersCursor > 0 {
			m.editUsersCursor--
		}
		return m, nil
	case "down", "j":
		if m.editUsersCursor < len(users)-1 {
			m.editUsersCursor++
		}
		return m, nil
	case " ":
		if m.editUsersCursor >= 0 && m.editUsersCursor < len(users) {
			token := approvedUserToken(users[m.editUsersCursor])
			if token != "" {
				m.editUsersSelected[token] = !m.editUsersSelected[token]
			}
		}
		return m, nil
	case "a":
		selectAll := !allUsersSelected(users, m.editUsersSelected)
		for _, u := range users {
			token := approvedUserToken(u)
			if token == "" {
				continue
			}
			m.editUsersSelected[token] = selectAll
		}
		return m, nil
	case "m":
		m.editUsersManual = true
		m.editUsersInput.SetValue(buildAllowCSV(users, m.editUsersSelected, m.editUsersExtras))
		m.editUsersInput.Placeholder = "sender_id1,sender_id2"
		m.editUsersInput.Focus()
		return m, nil
	case "enter":
		val := buildAllowCSV(users, m.editUsersSelected, m.editUsersExtras)
		if strings.TrimSpace(val) == "" {
			m.flashMsg = styleErr.Render("✗") + " Select at least one allowed sender"
			m.flashUntil = time.Now().Add(4 * time.Second)
			return m, nil
		}
		m.mode = routingNormal
		m.editUsersInput.Blur()
		return m, routingSetUsersCmd(m.exec, row.Channel, row.ChatID, val)
	}

	return m, nil
}

func (m RoutingModel) startEditWorkspace() (RoutingModel, tea.Cmd) {
	if m.selectedRow >= len(m.mappings) {
		return m, nil
	}
	row := m.mappings[m.selectedRow]
	m.mode = routingEditWorkspace
	m.editWorkspaceInput.SetValue(row.Workspace)
	m.editWorkspaceInput.Placeholder = "/absolute/path/to/workspace"
	m.editWorkspaceInput.Focus()
	return m, nil
}

func (m RoutingModel) updateEditWorkspace(msg tea.KeyMsg, snap *VMSnapshot) (RoutingModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = routingNormal
		m.editWorkspaceInput.Blur()
		return m, nil
	case "ctrl+b":
		return m, m.startBrowseFromInput(strings.TrimSpace(m.editWorkspaceInput.Value()), snap, browseTargetEditWorkspace)
	case "enter":
		if m.selectedRow >= len(m.mappings) {
			m.mode = routingNormal
			m.editWorkspaceInput.Blur()
			return m, nil
		}
		val := strings.TrimSpace(m.editWorkspaceInput.Value())
		if val == "" {
			return m, nil
		}
		row := m.mappings[m.selectedRow]
		workspace := expandHomeForExecPath(val, m.exec.HomePath())
		m.mode = routingNormal
		m.editWorkspaceInput.Blur()
		return m, routingAddMappingCmd(m.exec, row.Channel, row.ChatID, workspace, row.AllowedSenders, row.Label)
	}
	var cmd tea.Cmd
	m.editWorkspaceInput, cmd = m.editWorkspaceInput.Update(msg)
	return m, cmd
}

// --- Discord Room Picker ---

func (m RoutingModel) startDiscordPicker() (RoutingModel, tea.Cmd) {
	m.mode = routingPickRoom
	m.discordRooms = nil
	m.roomCursor = 0
	m.roomsLoading = true
	m.roomsErr = ""
	return m, fetchDiscordRoomsCmd(m.exec)
}

func (m *RoutingModel) HandleDiscordRooms(msg routingDiscordRoomsMsg) {
	m.roomsLoading = false
	if msg.err != "" {
		m.roomsErr = msg.err
		m.discordRooms = nil
	} else {
		m.roomsErr = ""
		m.discordRooms = msg.rooms
		m.roomCursor = 0
	}
}

func (m RoutingModel) updatePickRoom(msg tea.KeyMsg, snap *VMSnapshot) (RoutingModel, tea.Cmd) {
	key := msg.String()

	switch key {
	case "esc":
		m.mode = routingNormal
		m.wizardInput.Blur()
		return m, nil

	case "up", "k":
		if m.roomCursor > 0 {
			m.roomCursor--
		}
		return m, nil

	case "down", "j":
		if m.roomCursor < len(m.discordRooms)-1 {
			m.roomCursor++
		}
		return m, nil

	case "enter":
		if len(m.discordRooms) > 0 && m.roomCursor < len(m.discordRooms) {
			room := m.discordRooms[m.roomCursor]
			m.wizardChatID = room.ChannelID
			if m.wizardLabel == "" {
				m.wizardLabel = room.GuildName + "/" + room.ChannelName
			}
			m.mode = routingAddWizard
			m.wizardStep = addStepWorkspace
			m.wizardInput.SetValue("")
			m.wizardInput.Placeholder = "/absolute/path/to/workspace"
			if snap != nil && snap.WorkspacePath != "" {
				m.wizardInput.SetValue(expandHomeForExecPath(snap.WorkspacePath, m.exec.HomePath()))
			}
			m.wizardInput.Focus()
		}
		return m, nil

	case "m":
		// Switch to manual entry
		m.mode = routingAddWizard
		m.wizardStep = addStepChatIDManual
		m.wizardInput.SetValue("")
		m.wizardInput.Placeholder = "e.g. 1234567890123"
		m.wizardInput.Focus()
		return m, nil
	}

	return m, nil
}

// --- Telegram Pairing ---

func (m RoutingModel) startTelegramPair() (RoutingModel, tea.Cmd) {
	m.mode = routingPairTelegram
	m.pairLoading = true
	m.pairErr = ""
	return m, startTelegramPairCmd(m.exec)
}

func (m *RoutingModel) HandleTelegramPair(msg routingTelegramPairMsg) {
	m.pairLoading = false
	if msg.err != "" {
		m.pairErr = msg.err
		// Return to wizard choice screen
		m.mode = routingAddWizard
		m.wizardStep = addStepChatID
	} else {
		m.pairErr = ""
		m.wizardChatID = msg.chatID
		if msg.username != "" && m.wizardLabel == "" {
			m.wizardLabel = msg.username
		}
		// Advance to workspace step
		m.mode = routingAddWizard
		m.wizardStep = addStepWorkspace
		m.wizardInput.SetValue("")
		m.wizardInput.Placeholder = "/absolute/path/to/workspace"
		m.wizardInput.Focus()
		m.flashMsg = styleOK.Render("Detected chat: " + msg.chatID)
		m.flashUntil = time.Now().Add(5 * time.Second)
	}
}

func (m RoutingModel) updatePairTelegram(msg tea.KeyMsg) (RoutingModel, tea.Cmd) {
	if msg.String() == "esc" {
		m.mode = routingAddWizard
		m.wizardStep = addStepChatID
		return m, nil
	}
	return m, nil
}

// --- View ---

func (m RoutingModel) View(snap *VMSnapshot, width int) string {
	panelW := width - 4
	if panelW > 100 {
		panelW = 100
	}
	if panelW < 40 {
		panelW = 40
	}

	if !m.loaded {
		return "\n  Loading routing configuration...\n"
	}

	var b strings.Builder

	// Empty state: show onboarding guidance instead of empty panels.
	if len(m.mappings) == 0 {
		var guide []string
		guide = append(guide, "")
		guide = append(guide, styleBold.Render("  No routing mappings yet."))
		guide = append(guide, styleDim.Render("  Route each chat room to a dedicated workspace folder with per-room sender rules."))
		guide = append(guide, "")
		guide = append(guide, styleDim.Render("  Quick start:"))
		guide = append(guide, styleDim.Render(fmt.Sprintf("    1) Press %s and walk through Add Mapping", styleKey.Render("[a]"))))
		guide = append(guide, styleDim.Render("    2) Enable routing in Settings tab or run:"))
		guide = append(guide, "       "+styleValue.Render("sciclaw routing enable"))
		guide = append(guide, styleDim.Render("    3) Validate + reload after changes"))
		guide = append(guide, "       "+styleValue.Render("sciclaw routing validate"))
		guide = append(guide, "       "+styleValue.Render("sciclaw routing reload"))
		guide = append(guide, "")
		guide = append(guide, styleDim.Render("  Example command block:"))
		guide = append(guide, "       "+styleValue.Render("sciclaw routing add ..."))
		guide = append(guide, "         "+styleValue.Render("--channel discord \\"))
		guide = append(guide, "         "+styleValue.Render("--chat-id <room_id> \\"))
		guide = append(guide, "         "+styleValue.Render("--workspace /absolute/path/to/project \\"))
		guide = append(guide, "         "+styleValue.Render("--allow <sender_id1,sender_id2> \\"))
		guide = append(guide, "         "+styleValue.Render("--label <friendly_name>"))
		if !m.status.Enabled {
			guide = append(guide, "")
			guide = append(guide, styleHint.Render("  Routing is currently disabled. Enable it in Settings tab or with:"))
			guide = append(guide, "       "+styleValue.Render("sciclaw routing enable"))
		}

		content := strings.Join(guide, "\n")
		panel := stylePanel.Width(panelW).Render(content)
		title := stylePanelTitle.Render("Channel Routing")
		b.WriteString(placePanelTitle(panel, title))

		// Flash message.
		if !m.flashUntil.IsZero() && time.Now().Before(m.flashUntil) {
			b.WriteString("  " + m.flashMsg + "\n")
		}

		// Wizard overlay (can be triggered from empty state).
		if m.mode == routingAddWizard {
			b.WriteString("\n")
			b.WriteString(m.renderAddWizardOverlay(snap))
		}
		if m.mode == routingBrowseFolder {
			b.WriteString("\n")
			b.WriteString(m.renderFolderBrowser())
		}
		if m.mode == routingPickRoom {
			b.WriteString("\n")
			b.WriteString(m.renderDiscordPicker())
		}
		if m.mode == routingPairTelegram {
			b.WriteString("\n")
			b.WriteString(m.renderTelegramPairing())
		}

		return b.String()
	}

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
	b.WriteString(fmt.Sprintf("  %s Add   %s Explain   %s Edit Folder   %s Edit Users   %s Enable/Disable   %s Detach   %s Check config   %s Apply changes   %s Refresh\n",
		styleKey.Render("[a]"),
		styleKey.Render("[e]"),
		styleKey.Render("[f]"),
		styleKey.Render("[u]"),
		styleKey.Render("[t]"),
		styleKey.Render("[x]"),
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
		b.WriteString(fmt.Sprintf("  Detach mapping %s? %s / %s\n",
			styleBold.Render(m.removeMapping.Channel+":"+m.removeMapping.ChatID),
			styleKey.Render("[y]es"),
			styleKey.Render("[n]o"),
		))
	}

	// Overlay: add wizard.
	if m.mode == routingAddWizard {
		b.WriteString("\n")
		b.WriteString(m.renderAddWizardOverlay(snap))
	}

	// Overlay: edit users.
	if m.mode == routingEditUsers {
		b.WriteString("\n")
		b.WriteString(m.renderEditUsersOverlay(snap))
	}

	// Overlay: edit workspace.
	if m.mode == routingEditWorkspace {
		b.WriteString("\n")
		b.WriteString(m.renderEditWorkspaceOverlay())
	}

	// Overlay: folder browser.
	if m.mode == routingBrowseFolder {
		b.WriteString("\n")
		b.WriteString(m.renderFolderBrowser())
	}

	// Overlay: Discord room picker.
	if m.mode == routingPickRoom {
		b.WriteString("\n")
		b.WriteString(m.renderDiscordPicker())
	}

	// Overlay: Telegram pairing.
	if m.mode == routingPairTelegram {
		b.WriteString("\n")
		b.WriteString(m.renderTelegramPairing())
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

	unmappedDisplay := m.status.UnmappedBehavior
	switch unmappedDisplay {
	case "":
		unmappedDisplay = "blocked until mapped"
	case "block":
		unmappedDisplay = "blocked until mapped"
	case "default":
		unmappedDisplay = "fallback to default workspace"
	}

	statusLabel := lipgloss.NewStyle().Foreground(colorMuted).Width(16)

	var lines []string
	lines = append(lines, fmt.Sprintf("  %s  %s %s", statusLabel.Render("Enabled:"), enabledIcon, enabledText))
	lines = append(lines, fmt.Sprintf("  %s  %s", statusLabel.Render("Mappings:"), styleBold.Render(fmt.Sprintf("%d", m.status.MappingCount))))
	lines = append(lines, fmt.Sprintf("  %s  %s", statusLabel.Render("Unrouted rooms:"), styleBold.Render(unmappedDisplay)))
	if !m.status.Enabled {
		lines = append(lines, "")
		lines = append(lines, styleHint.Render("  Routing is disabled. Enable it in Settings tab or run:"))
		lines = append(lines, "    "+styleValue.Render("sciclaw routing enable"))
	}

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
		out, err := exec.ExecShell(10*time.Second, cmd)
		out = strings.TrimSpace(out)
		if err != nil {
			if out == "" {
				out = err.Error()
			}
			return routingActionMsg{action: action, output: out, ok: false}
		}
		if out == "" {
			out = "Routing " + action + "d"
		}
		return routingActionMsg{action: action, output: out, ok: true}
	}
}

func routingRemoveCmd(exec Executor, channel, chatID string) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw routing remove --channel " +
			shellEscape(channel) + " --chat-id " + shellEscape(chatID) + " 2>&1"
		out, err := exec.ExecShell(10*time.Second, cmd)
		out = strings.TrimSpace(out)
		if err != nil {
			if out == "" {
				out = err.Error()
			}
			return routingActionMsg{action: "remove", output: out, ok: false}
		}
		if out == "" {
			out = "Removed mapping " + channel + ":" + chatID
		}
		return routingActionMsg{action: "remove", output: out, ok: true}
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

func routingAddMappingCmd(exec Executor, channel, chatID, workspace, allowCSV, label string) tea.Cmd {
	return func() tea.Msg {
		workspace = expandHomeForExecPath(workspace, exec.HomePath())
		cmd := fmt.Sprintf("HOME=%s sciclaw routing add --channel %s --chat-id %s --workspace %s --allow %s",
			exec.HomePath(),
			shellEscape(channel),
			shellEscape(chatID),
			shellEscape(workspace),
			shellEscape(allowCSV),
		)
		if strings.TrimSpace(label) != "" {
			cmd += " --label " + shellEscape(label)
		}
		cmd += " 2>&1"
		out, err := exec.ExecShell(10*time.Second, cmd)
		out = strings.TrimSpace(out)
		if err != nil {
			if out == "" {
				out = err.Error()
			}
			return routingActionMsg{action: "add", output: out, ok: false}
		}
		return routingActionMsg{action: "add", output: out, ok: true}
	}
}

func routingSetUsersCmd(exec Executor, channel, chatID, allowCSV string) tea.Cmd {
	return func() tea.Msg {
		cmd := fmt.Sprintf("HOME=%s sciclaw routing set-users --channel %s --chat-id %s --allow %s 2>&1",
			exec.HomePath(),
			shellEscape(channel),
			shellEscape(chatID),
			shellEscape(allowCSV),
		)
		out, err := exec.ExecShell(10*time.Second, cmd)
		out = strings.TrimSpace(out)
		if err != nil {
			if out == "" {
				out = err.Error()
			}
			return routingActionMsg{action: "set-users", output: out, ok: false}
		}
		return routingActionMsg{action: "set-users", output: out, ok: true}
	}
}

func routingExplainCmd(exec Executor, channel, chatID, sender string) tea.Cmd {
	return func() tea.Msg {
		cmd := fmt.Sprintf("HOME=%s sciclaw routing explain --channel %s --chat-id %s --sender %s 2>&1",
			exec.HomePath(),
			shellEscape(channel),
			shellEscape(chatID),
			shellEscape(sender),
		)
		out, err := exec.ExecShell(10*time.Second, cmd)
		out = strings.TrimSpace(out)
		if err != nil {
			if out == "" {
				out = err.Error()
			}
			return routingActionMsg{
				action:  "explain",
				output:  out,
				ok:      false,
				channel: channel,
				chatID:  chatID,
				sender:  sender,
			}
		}
		return routingActionMsg{
			action:  "explain",
			output:  out,
			ok:      true,
			channel: channel,
			chatID:  chatID,
			sender:  sender,
		}
	}
}

func fetchDirListCmd(exec Executor, dirPath string) tea.Cmd {
	return func() tea.Msg {
		resolvedPath := expandHomeForExecPath(dirPath, exec.HomePath())
		cmd := fmt.Sprintf("ls -1pF %s 2>/dev/null", shellEscape(resolvedPath))
		out, err := exec.ExecShell(5*time.Second, cmd)
		if err != nil {
			return routingDirListMsg{path: resolvedPath, err: "Cannot read directory"}
		}
		var dirs []string
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if strings.HasSuffix(line, "/") {
				dirs = append(dirs, strings.TrimSuffix(line, "/"))
			}
		}
		return routingDirListMsg{path: resolvedPath, dirs: dirs}
	}
}

func expandHomeForExecPath(path, home string) string {
	path = strings.TrimSpace(path)
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	return path
}

func fetchDiscordRoomsCmd(exec Executor) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw channels list-rooms --channel discord 2>&1"
		out, err := exec.ExecShell(15*time.Second, cmd)
		if err != nil || strings.TrimSpace(out) == "" {
			errMsg := "Failed to list Discord channels"
			if out != "" {
				errMsg = strings.TrimSpace(out)
			}
			return routingDiscordRoomsMsg{err: errMsg}
		}
		var rooms []discordRoom
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			parts := strings.SplitN(line, "|", 3)
			if len(parts) != 3 {
				continue
			}
			rooms = append(rooms, discordRoom{
				ChannelID:   parts[0],
				GuildName:   parts[1],
				ChannelName: parts[2],
			})
		}
		if len(rooms) == 0 {
			return routingDiscordRoomsMsg{err: "No text channels found"}
		}
		return routingDiscordRoomsMsg{rooms: rooms}
	}
}

func startTelegramPairCmd(exec Executor) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw channels pair-telegram --timeout 15 2>&1"
		out, err := exec.ExecShell(20*time.Second, cmd)
		if err != nil || strings.TrimSpace(out) == "" {
			errMsg := "Pairing timed out — no message received"
			if out != "" {
				errMsg = strings.TrimSpace(out)
			}
			return routingTelegramPairMsg{err: errMsg}
		}
		parts := strings.SplitN(strings.TrimSpace(out), "|", 3)
		if len(parts) < 2 {
			return routingTelegramPairMsg{err: "Unexpected response: " + out}
		}
		username := ""
		if len(parts) >= 3 {
			username = parts[2]
		}
		return routingTelegramPairMsg{
			chatID:   parts[0],
			chatType: parts[1],
			username: username,
		}
	}
}

// --- Render overlays ---

func (m RoutingModel) renderAddWizardOverlay(snap *VMSnapshot) string {
	var lines []string
	displayStep := m.wizardStep + 1
	if m.wizardStep == addStepChatIDManual {
		displayStep = addStepChatID + 1 // show as step 2
	}
	lines = append(lines, styleBold.Render(fmt.Sprintf("  Add Routing Mapping (step %d/6)", displayStep)))

	switch m.wizardStep {
	case addStepChannel:
		disco := styleDim.Render("  discord  ")
		tele := styleDim.Render("  telegram  ")
		if m.wizardChannel == "discord" {
			disco = styleBold.Foreground(colorAccent).Render("[ discord ]")
		} else {
			tele = styleBold.Foreground(colorAccent).Render("[ telegram ]")
		}
		lines = append(lines, "  Channel: "+disco+"  "+tele)
		lines = append(lines, styleHint.Render("    Left/Right to switch, Enter to continue"))

	case addStepChatID:
		// Telegram choice screen
		lines = append(lines, "  How to identify the chat room:")
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("    %s  Pair — send a message from the chat to detect it", styleKey.Render("[p]")))
		lines = append(lines, fmt.Sprintf("    %s  Enter the chat ID manually", styleKey.Render("[m]")))
		if m.pairErr != "" {
			lines = append(lines, "")
			lines = append(lines, "  "+styleErr.Render(m.pairErr))
		}

	case addStepChatIDManual:
		lines = append(lines, fmt.Sprintf("  Chat room ID: %s", m.wizardInput.View()))
		lines = append(lines, styleHint.Render("    The numeric chat/channel ID from "+m.wizardChannel))

	case addStepWorkspace:
		lines = append(lines, fmt.Sprintf("  Workspace path: %s", m.wizardInput.View()))
		lines = append(lines, styleHint.Render("    Absolute path to the project folder"))
		lines = append(lines, fmt.Sprintf("    %s to browse folders", styleKey.Render("Ctrl+B")))

	case addStepAllow:
		users := channelApprovedUsers(m.wizardChannel, snap)
		if len(users) == 0 || m.wizardAllowManual {
			lines = append(lines, fmt.Sprintf("  Allowed senders: %s", m.wizardInput.View()))
			lines = append(lines, styleHint.Render("    Comma-separated user IDs (e.g. 123456,789012)"))
			if len(users) > 0 {
				lines = append(lines, styleHint.Render(fmt.Sprintf("    %s switch back to Users picker", styleKey.Render("p"))))
			}
		} else {
			lines = append(lines, "  Allowed senders (from Users tab):")
			for i, user := range users {
				mark := "[ ]"
				token := approvedUserToken(user)
				if m.wizardAllowSelected != nil && m.wizardAllowSelected[token] {
					mark = "[x]"
				}
				prefix := "  "
				if i == m.wizardAllowCursor {
					prefix = styleBold.Foreground(colorAccent).Render("> ")
				}

				label := user.DisplayID()
				if user.Username != "" {
					label = fmt.Sprintf("%s (%s)", user.Username, user.DisplayID())
				}
				line := fmt.Sprintf("    %s%s %s", prefix, mark, label)
				if i == m.wizardAllowCursor {
					line = lipgloss.NewStyle().
						Background(lipgloss.Color("#2A2A4A")).
						Render(line)
				}
				lines = append(lines, line)
			}
			lines = append(lines, styleHint.Render(fmt.Sprintf("    %s move  %s toggle  %s all/none  %s manual CSV",
				styleKey.Render("j/k"),
				styleKey.Render("Space"),
				styleKey.Render("a"),
				styleKey.Render("m"),
			)))
		}

	case addStepLabel:
		lines = append(lines, fmt.Sprintf("  Label (optional): %s", m.wizardInput.View()))
		lines = append(lines, styleHint.Render("    A friendly name for this mapping. Press Enter to skip."))

	case addStepConfirm:
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %s", styleDim.Render("Review:")))
		lines = append(lines, fmt.Sprintf("    Channel:  %s", styleValue.Render(m.wizardChannel)))
		lines = append(lines, fmt.Sprintf("    Chat ID:  %s", styleValue.Render(m.wizardChatID)))
		lines = append(lines, fmt.Sprintf("    Folder:   %s", styleValue.Render(m.wizardPath)))
		lines = append(lines, fmt.Sprintf("    Allow:    %s", styleValue.Render(m.wizardAllow)))
		if m.wizardLabel != "" {
			lines = append(lines, fmt.Sprintf("    Label:    %s", styleValue.Render(m.wizardLabel)))
		}
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  Press %s to save, %s to cancel",
			styleKey.Render("Enter"), styleKey.Render("Esc")))
	}

	if m.wizardStep < addStepConfirm {
		lines = append(lines, styleDim.Render("    Esc to cancel"))
	}
	return strings.Join(lines, "\n")
}

func (m RoutingModel) renderEditUsersOverlay(snap *VMSnapshot) string {
	row := m.mappings[m.selectedRow]
	users := channelApprovedUsers(row.Channel, snap)
	var lines []string
	lines = append(lines, styleBold.Render(fmt.Sprintf("  Edit allowed users for %s:%s", row.Channel, row.ChatID)))
	if len(users) == 0 || m.editUsersManual {
		lines = append(lines, fmt.Sprintf("  Allowed senders: %s", m.editUsersInput.View()))
		lines = append(lines, styleHint.Render("    Comma-separated user IDs. This replaces the current list."))
		if len(users) > 0 {
			lines = append(lines, styleHint.Render(fmt.Sprintf("    %s switch back to Users picker", styleKey.Render("p"))))
		}
		lines = append(lines, styleDim.Render("    Enter to save, Esc to cancel"))
		return strings.Join(lines, "\n")
	}

	lines = append(lines, "  Toggle users from the central approved-users list:")
	for i, user := range users {
		mark := "[ ]"
		token := approvedUserToken(user)
		if m.editUsersSelected != nil && m.editUsersSelected[token] {
			mark = "[x]"
		}
		prefix := "  "
		if i == m.editUsersCursor {
			prefix = styleBold.Foreground(colorAccent).Render("> ")
		}

		label := user.DisplayID()
		if user.Username != "" {
			label = fmt.Sprintf("%s (%s)", user.Username, user.DisplayID())
		}
		line := fmt.Sprintf("    %s%s %s", prefix, mark, label)
		if i == m.editUsersCursor {
			line = lipgloss.NewStyle().
				Background(lipgloss.Color("#2A2A4A")).
				Render(line)
		}
		lines = append(lines, line)
	}
	if len(m.editUsersExtras) > 0 {
		lines = append(lines, styleHint.Render(fmt.Sprintf("    Preserving %d custom sender(s) not in Users list", len(m.editUsersExtras))))
	}
	lines = append(lines, styleHint.Render(fmt.Sprintf("    %s move  %s toggle  %s all/none  %s manual CSV",
		styleKey.Render("j/k"),
		styleKey.Render("Space"),
		styleKey.Render("a"),
		styleKey.Render("m"),
	)))
	lines = append(lines, styleDim.Render("    Enter to save, Esc to cancel"))
	return strings.Join(lines, "\n")
}

func (m RoutingModel) renderEditWorkspaceOverlay() string {
	row := m.mappings[m.selectedRow]
	var lines []string
	lines = append(lines, styleBold.Render(fmt.Sprintf("  Edit folder for %s:%s", row.Channel, row.ChatID)))
	lines = append(lines, fmt.Sprintf("  Workspace path: %s", m.editWorkspaceInput.View()))
	lines = append(lines, styleHint.Render("    Enter a new project folder for this room mapping."))
	lines = append(lines, fmt.Sprintf("    %s to browse folders", styleKey.Render("Ctrl+B")))
	lines = append(lines, styleDim.Render("    Enter to save, Esc to cancel"))
	return strings.Join(lines, "\n")
}

func (m RoutingModel) renderFolderBrowser() string {
	var lines []string
	lines = append(lines, styleBold.Render("  Browse Folders"))
	lines = append(lines, fmt.Sprintf("  %s %s", styleDim.Render("Location:"), styleValue.Render(m.browserPath)))
	lines = append(lines, "")

	if m.browserLoading {
		lines = append(lines, styleDim.Render("  Loading..."))
	} else if m.browserErr != "" {
		lines = append(lines, styleErr.Render("  "+m.browserErr))
	} else {
		// Build selectable list: [0] = "..", [1..N] = dirs, [N+1] = "[Select this folder]"
		items := []string{".."}
		items = append(items, m.browserEntries...)
		items = append(items, "[Select this folder]")

		maxVisible := 12
		start := 0
		if m.browserCursor > maxVisible-3 {
			start = m.browserCursor - maxVisible + 3
		}
		end := start + maxVisible
		if end > len(items) {
			end = len(items)
		}

		for i := start; i < end; i++ {
			indicator := "  "
			if i == m.browserCursor {
				indicator = styleBold.Foreground(colorAccent).Render("> ")
			}
			name := items[i]
			if i > 0 && i < len(items)-1 {
				name += "/"
			}
			line := fmt.Sprintf("    %s%s", indicator, name)
			if i == m.browserCursor {
				line = lipgloss.NewStyle().
					Background(lipgloss.Color("#2A2A4A")).
					Render(line)
			}
			lines = append(lines, line)
		}
		if end < len(items) {
			lines = append(lines, styleDim.Render(fmt.Sprintf("    ... %d more", len(items)-end)))
		}
	}

	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s Navigate   %s Enter/Select   %s Select current   %s Back",
		styleKey.Render("j/k"),
		styleKey.Render("Enter"),
		styleKey.Render("Space"),
		styleKey.Render("Esc"),
	))

	return strings.Join(lines, "\n")
}

func (m RoutingModel) renderDiscordPicker() string {
	var lines []string
	lines = append(lines, styleBold.Render("  Select Discord Channel"))
	lines = append(lines, "")

	if m.roomsLoading {
		lines = append(lines, styleDim.Render("  Fetching servers and channels..."))
	} else if m.roomsErr != "" {
		lines = append(lines, styleErr.Render("  "+m.roomsErr))
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %s Enter ID manually   %s Cancel",
			styleKey.Render("[m]"), styleKey.Render("Esc")))
	} else {
		maxVisible := 12
		start := 0
		if m.roomCursor > maxVisible-3 {
			start = m.roomCursor - maxVisible + 3
		}
		end := start + maxVisible
		if end > len(m.discordRooms) {
			end = len(m.discordRooms)
		}

		lastGuild := ""
		for i := start; i < end; i++ {
			room := m.discordRooms[i]
			if room.GuildName != lastGuild {
				if lastGuild != "" {
					lines = append(lines, "")
				}
				lines = append(lines, "  "+styleBold.Render(room.GuildName))
				lastGuild = room.GuildName
			}
			indicator := "  "
			if i == m.roomCursor {
				indicator = styleBold.Foreground(colorAccent).Render("> ")
			}
			line := fmt.Sprintf("    %s%s", indicator, room.ChannelName)
			if i == m.roomCursor {
				line = lipgloss.NewStyle().
					Background(lipgloss.Color("#2A2A4A")).
					Render(line)
			}
			lines = append(lines, line)
		}
		if end < len(m.discordRooms) {
			lines = append(lines, styleDim.Render(fmt.Sprintf("    ... %d more", len(m.discordRooms)-end)))
		}

		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %s Navigate   %s Select   %s Enter ID manually   %s Cancel",
			styleKey.Render("j/k"),
			styleKey.Render("Enter"),
			styleKey.Render("[m]"),
			styleKey.Render("Esc"),
		))
	}

	return strings.Join(lines, "\n")
}

func (m RoutingModel) renderTelegramPairing() string {
	var lines []string
	lines = append(lines, styleBold.Render("  Telegram Pairing"))
	lines = append(lines, "")
	lines = append(lines, "  Send a message from the Telegram chat you want to route.")
	lines = append(lines, styleDim.Render("  Listening for messages... (15 second timeout)"))
	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s Cancel", styleKey.Render("Esc")))
	return strings.Join(lines, "\n")
}
