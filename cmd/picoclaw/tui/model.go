package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Tab logical IDs — stable regardless of which tabs are visible.
const (
	tabHome     = 0
	tabChat     = 1
	tabChannels = 2
	tabUsers    = 3
	tabLogin    = 4
	tabDoctor   = 5
	tabAgent    = 6
	tabFiles    = 7 // VM-only
	tabModels   = 8
	tabSkills   = 9
	tabCron     = 10
	tabRouting  = 11
	tabSettings = 12
)

// tabEntry maps a visible tab position to its logical ID and display name.
type tabEntry struct {
	name string
	id   int
}

// Messages.
type snapshotMsg struct {
	snapshot VMSnapshot
	err      error
}
type tickMsg time.Time
type actionDoneMsg struct{ output string }

// homeNavigateMsg requests a tab switch from the Home tab.
type homeNavigateMsg struct{ tabID int }

// onboardExecDoneMsg reports the result of an async wizard command.
type onboardExecDoneMsg struct {
	step   int
	output string
	err    error
}

// Model is the root TUI model.
type Model struct {
	exec      Executor
	tabs      []tabEntry
	activeTab int
	width     int
	height    int

	// Shared state
	snapshot     *VMSnapshot
	snapshotErr  error
	loading      bool
	spinner      spinner.Model
	lastRefresh  time.Time
	lastAction   string
	lastActionAt time.Time

	// Sub-models for each tab
	home     HomeModel
	chat     ChatModel
	channels ChannelsModel
	users    UsersModel
	login    LoginModel
	doctor   DoctorModel
	agent    AgentModel
	files    FilesModel
	models   ModelsModel
	skills   SkillsModel
	cron     CronModel
	routing  RoutingModel
	settings SettingsModel
}

func buildTabs(mode Mode) []tabEntry {
	tabs := []tabEntry{
		{"Home", tabHome},
		{"Chat", tabChat},
		{"Channels", tabChannels},
		{"Routing", tabRouting},
		{"Users", tabUsers},
		{"Models", tabModels},
		{"Skills", tabSkills},
		{"Schedule", tabCron},
	}
	if mode == ModeVM {
		tabs = append(tabs, tabEntry{"Files", tabFiles})
	}
	tabs = append(tabs,
		tabEntry{"Gateway", tabAgent},
		tabEntry{"Settings", tabSettings},
		tabEntry{"Login", tabLogin},
		tabEntry{"Health", tabDoctor},
	)
	return tabs
}

func NewModel(exec Executor) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(colorAccent)

	return Model{
		exec:      exec,
		tabs:      buildTabs(exec.Mode()),
		activeTab: 0,
		spinner:   s,
		loading:   true,
		home:      NewHomeModel(exec),
		chat:      NewChatModel(exec),
		channels:  NewChannelsModel(exec),
		users:     NewUsersModel(exec),
		login:     NewLoginModel(exec),
		doctor:    NewDoctorModel(exec),
		agent:     NewAgentModel(exec),
		files:     NewFilesModel(),
		models:    NewModelsModel(exec),
		skills:    NewSkillsModel(exec),
		cron:      NewCronModel(exec),
		routing:   NewRoutingModel(exec),
		settings:  NewSettingsModel(exec),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		fetchSnapshotCmd(m.exec),
		tickCmd(),
	)
}

// tabIndex returns the logical tab ID for the currently visible tab position.
func (m Model) tabIndex() int {
	if m.activeTab < len(m.tabs) {
		return m.tabs[m.activeTab].id
	}
	return tabHome
}

// subTabCapturingInput returns true when a sub-tab is in a mode
// that captures all keyboard input (text entry, confirmation dialogs).
func (m Model) subTabCapturingInput() bool {
	idx := m.tabIndex()
	return idx == tabChat || // Chat always captures
		(idx == tabHome && m.home.onboardActive) ||
		(idx == tabChannels && m.channels.mode != modeNormal) ||
		(idx == tabUsers && m.users.mode != usersNormal) ||
		(idx == tabLogin && m.login.mode != loginNormal) ||
		(idx == tabFiles && m.files.mode != filesNormal) ||
		(idx == tabModels && m.models.mode != modelsNormal) ||
		(idx == tabSkills && m.skills.mode != skillsNormal) ||
		(idx == tabRouting && m.routing.mode != routingNormal) ||
		(idx == tabCron && m.cron.mode != cronNormal) ||
		(idx == tabSettings && m.settings.mode != settingsNormal)
}

// maybeAutoRun triggers auto-fetch when certain tabs are first visited.
func (m *Model) maybeAutoRun() tea.Cmd {
	switch m.tabIndex() {
	case tabDoctor:
		return m.doctor.AutoRun()
	case tabModels:
		return m.models.AutoRun()
	case tabSkills:
		return m.skills.AutoRun()
	case tabCron:
		return m.cron.AutoRun()
	case tabRouting:
		return m.routing.AutoRun()
	case tabSettings:
		return m.settings.AutoRun()
	}
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.chat.HandleResize(m.width, m.height)
		m.doctor.HandleResize(m.width, m.height)
		m.agent.HandleResize(m.width, m.height)
		m.skills.HandleResize(m.width, m.height)
		m.routing.HandleResize(m.width, m.height)
		m.settings.HandleResize(m.width, m.height)
		return m, nil

	case tea.KeyMsg:
		idx := m.tabIndex()

		// When a sub-tab is capturing input, allow ctrl+c and tab navigation,
		// then delegate everything else to the active sub-tab.
		if m.subTabCapturingInput() {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "q":
				// Keep the global quit shortcut available during onboarding.
				if idx == tabHome && m.home.onboardActive {
					return m, tea.Quit
				}
			case "tab":
				m.activeTab = (m.activeTab + 1) % len(m.tabs)
				return m, m.maybeAutoRun()
			case "shift+tab":
				m.activeTab = (m.activeTab - 1 + len(m.tabs)) % len(m.tabs)
				return m, m.maybeAutoRun()
			}
			var cmd tea.Cmd
			switch idx {
			case tabHome:
				m.home, cmd = m.home.Update(msg, m.snapshot)
			case tabChat:
				m.chat, cmd = m.chat.Update(msg, m.snapshot)
			case tabChannels:
				m.channels, cmd = m.channels.Update(msg, m.snapshot)
			case tabUsers:
				m.users, cmd = m.users.Update(msg, m.snapshot)
			case tabLogin:
				m.login, cmd = m.login.Update(msg, m.snapshot)
			case tabFiles:
				m.files, cmd = m.files.Update(msg, m.snapshot)
			case tabModels:
				m.models, cmd = m.models.Update(msg, m.snapshot)
			case tabSkills:
				m.skills, cmd = m.skills.Update(msg, m.snapshot)
			case tabRouting:
				m.routing, cmd = m.routing.Update(msg, m.snapshot)
			case tabCron:
				m.cron, cmd = m.cron.Update(msg, m.snapshot)
			case tabSettings:
				m.settings, cmd = m.settings.Update(msg, m.snapshot)
			}
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

		// Global keys.
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "tab":
			m.activeTab = (m.activeTab + 1) % len(m.tabs)
			return m, m.maybeAutoRun()
		case "shift+tab":
			m.activeTab = (m.activeTab - 1 + len(m.tabs)) % len(m.tabs)
			return m, m.maybeAutoRun()
		}

		// Delegate to active tab.
		var cmd tea.Cmd
		switch idx {
		case tabHome:
			m.home, cmd = m.home.Update(msg, m.snapshot)
		case tabChat:
			m.chat, cmd = m.chat.Update(msg, m.snapshot)
		case tabChannels:
			m.channels, cmd = m.channels.Update(msg, m.snapshot)
		case tabUsers:
			m.users, cmd = m.users.Update(msg, m.snapshot)
		case tabLogin:
			m.login, cmd = m.login.Update(msg, m.snapshot)
		case tabDoctor:
			m.doctor, cmd = m.doctor.Update(msg, m.snapshot)
		case tabAgent:
			m.agent, cmd = m.agent.Update(msg, m.snapshot)
		case tabFiles:
			m.files, cmd = m.files.Update(msg, m.snapshot)
		case tabModels:
			m.models, cmd = m.models.Update(msg, m.snapshot)
		case tabSkills:
			m.skills, cmd = m.skills.Update(msg, m.snapshot)
		case tabCron:
			m.cron, cmd = m.cron.Update(msg, m.snapshot)
		case tabRouting:
			m.routing, cmd = m.routing.Update(msg, m.snapshot)
		case tabSettings:
			m.settings, cmd = m.settings.Update(msg, m.snapshot)
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}

	case snapshotMsg:
		m.loading = false
		m.lastRefresh = time.Now()
		if msg.err == nil {
			m.snapshot = &msg.snapshot
			m.snapshotErr = nil
			// Activate onboard wizard on first snapshot when config is missing.
			if !m.home.wizardChecked {
				m.home.wizardChecked = true
				if !msg.snapshot.ConfigExists {
					m.home.onboardActive = true
					m.home.onboardStep = 0
				}
			}
		} else {
			m.snapshotErr = msg.err
		}
		return m, nil

	case homeNavigateMsg:
		for i, t := range m.tabs {
			if t.id == msg.tabID {
				m.activeTab = i
				return m, m.maybeAutoRun()
			}
		}
		return m, nil

	case onboardExecDoneMsg:
		m.home.HandleExecDone(msg)
		// Refresh snapshot after wizard actions.
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, fetchSnapshotCmd(m.exec))

	case tickMsg:
		if !m.loading && time.Since(m.lastRefresh) > 10*time.Second {
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, fetchSnapshotCmd(m.exec), tickCmd())
		}
		return m, tickCmd()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case routingActionMsg:
		m.routing.HandleAction(msg)
		m.loading = true
		return m, tea.Batch(
			m.spinner.Tick,
			fetchSnapshotCmd(m.exec),
			fetchSettingsData(m.exec),
			fetchRoutingStatus(m.exec),
			fetchRoutingListCmd(m.exec),
		)

	case actionDoneMsg:
		if trimmed := strings.TrimSpace(msg.output); trimmed != "" {
			m.lastAction = trimmed
			m.lastActionAt = time.Now()
		}
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, fetchSnapshotCmd(m.exec), fetchSettingsData(m.exec))

	case chatResponseMsg:
		m.chat.HandleResponse(msg)
		return m, nil

	case doctorDoneMsg:
		m.doctor.HandleResult(msg)
		return m, nil

	case logsMsg:
		m.agent.HandleLogsMsg(msg)
		return m, nil

	case serviceActionMsg:
		m.agent.HandleServiceAction(msg)
		m.settings.HandleServiceAction(msg)
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, fetchSnapshotCmd(m.exec))

	case modelsStatusMsg:
		m.models.HandleStatus(msg)
		return m, nil

	case modelsCatalogMsg:
		m.models.HandleCatalog(msg)
		return m, nil

	case skillsListMsg:
		m.skills.HandleList(msg)
		return m, nil

	case cronListMsg:
		m.cron.HandleList(msg)
		return m, nil

	case routingStatusMsg:
		m.routing.HandleStatus(msg)
		return m, nil

	case routingListMsg:
		m.routing.HandleList(msg)
		return m, nil

	case routingValidateMsg:
		m.routing.HandleValidate(msg)
		return m, nil

	case routingReloadMsg:
		m.routing.HandleReload(msg)
		return m, nil

	case routingDirListMsg:
		m.routing.HandleDirList(msg)
		return m, nil

	case routingDiscordRoomsMsg:
		m.routing.HandleDiscordRooms(msg)
		return m, nil

	case routingTelegramPairMsg:
		m.routing.HandleTelegramPair(msg)
		return m, nil

	case settingsDataMsg:
		m.settings.HandleData(msg)
		return m, nil
	}

	return m, tea.Batch(cmds...)
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Header
	title := "🦞🧪 sciClaw Control Center"
	if m.exec.Mode() == ModeVM {
		title = "🦞🧪 sciClaw VM Control Center"
	}
	header := lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render(title)
	b.WriteString(header)
	b.WriteString("\n")

	// Tab bar
	b.WriteString(m.renderTabBar())
	b.WriteString("\n")

	contentWidth := m.width - 2
	if contentWidth < 40 {
		contentWidth = 40
	}

	// Loading indicator
	if m.loading && m.snapshot == nil {
		loadText := "Loading..."
		if m.exec.Mode() == ModeVM {
			loadText = "Connecting to VM..."
		}
		content := fmt.Sprintf("\n  %s %s\n", m.spinner.View(), loadText)
		b.WriteString(content)
	} else {
		idx := m.tabIndex()
		var content string
		switch idx {
		case tabHome:
			content = m.home.View(m.snapshot, contentWidth)
		case tabChat:
			content = m.chat.View(m.snapshot, contentWidth)
		case tabChannels:
			content = m.channels.View(m.snapshot, contentWidth)
		case tabUsers:
			content = m.users.View(m.snapshot, contentWidth)
		case tabLogin:
			content = m.login.View(m.snapshot, contentWidth)
		case tabDoctor:
			content = m.doctor.View(m.snapshot, contentWidth)
		case tabAgent:
			content = m.agent.View(m.snapshot, contentWidth)
		case tabFiles:
			content = m.files.View(m.snapshot, contentWidth)
		case tabModels:
			content = m.models.View(m.snapshot, contentWidth)
		case tabSkills:
			content = m.skills.View(m.snapshot, contentWidth)
		case tabCron:
			content = m.cron.View(m.snapshot, contentWidth)
		case tabRouting:
			content = m.routing.View(m.snapshot, contentWidth)
		case tabSettings:
			content = m.settings.View(m.snapshot, contentWidth)
		}
		b.WriteString(content)
	}

	// Pad to push status bar to bottom
	rendered := b.String()
	rendered = addDebugRowNumbers(rendered)
	lines := strings.Count(rendered, "\n") + 1
	for lines < m.height-1 {
		rendered += "\n"
		lines++
	}

	rendered += m.renderStatusBar()

	return rendered
}

func addDebugRowNumbers(input string) string {
	lines := strings.Split(input, "\n")
	var out strings.Builder
	for i, line := range lines {
		num := fmt.Sprintf("%3d ", i+1)
		out.WriteString(styleRowNumber.Render(num))
		out.WriteString(line)
		if i < len(lines)-1 {
			out.WriteString("\n")
		}
	}
	return out.String()
}

func (m Model) renderTabBar() string {
	// Split tabs into two rows to fit narrow terminals.
	// Row 1: primary workflow tabs, Row 2: admin/config tabs.
	mid := len(m.tabs) / 2
	if mid < 1 {
		mid = len(m.tabs)
	}

	renderRow := func(entries []tabEntry, startIdx int) string {
		var cells []string
		for i, t := range entries {
			if startIdx+i == m.activeTab {
				cells = append(cells, styleTabActive.Render(t.name))
			} else {
				cells = append(cells, styleTabInactive.Render(t.name))
			}
		}
		return lipgloss.JoinHorizontal(lipgloss.Top, cells...)
	}

	row1 := renderRow(m.tabs[:mid], 0)
	row2 := renderRow(m.tabs[mid:], mid)
	both := lipgloss.JoinVertical(lipgloss.Left, row1, row2)
	return styleTabBar.Width(m.width).Render(both)
}

func (m Model) renderStatusBar() string {
	left := ""
	if m.snapshot != nil {
		if m.exec.Mode() == ModeVM {
			stateColor := colorSuccess
			switch m.snapshot.State {
			case "Stopped":
				stateColor = colorWarning
			case "NotFound", "":
				stateColor = colorError
			}
			left = fmt.Sprintf(" VM: %s", lipgloss.NewStyle().Foreground(stateColor).Bold(true).Render(m.snapshot.State))
		} else {
			left = fmt.Sprintf(" Mode: %s", lipgloss.NewStyle().Foreground(colorSuccess).Bold(true).Render("Local"))
		}
		if m.snapshot.ActiveModel != "" {
			left += fmt.Sprintf("  Model: %s", styleDim.Render(m.snapshot.ActiveModel))
		}
		if m.loading {
			left += fmt.Sprintf("  %s", m.spinner.View())
		}
	} else if m.loading {
		left = fmt.Sprintf(" %s Connecting...", m.spinner.View())
	}

	if !m.lastActionAt.IsZero() && time.Since(m.lastActionAt) <= 8*time.Second {
		msgStyle := styleOK
		lower := strings.ToLower(m.lastAction)
		if strings.Contains(lower, "fail") || strings.Contains(lower, "error") {
			msgStyle = styleErr
		}
		if strings.TrimSpace(left) == "" {
			left = " " + msgStyle.Render(m.lastAction)
		} else {
			left += "  " + msgStyle.Render(m.lastAction)
		}
	}

	right := "Tab: switch section  Enter: select  q: quit"
	if !m.lastRefresh.IsZero() {
		ago := time.Since(m.lastRefresh).Truncate(time.Second)
		right = fmt.Sprintf("Updated %s ago | %s", ago, right)
	}

	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right) - 2
	if gap < 1 {
		gap = 1
	}

	return styleStatusBar.Width(m.width).Render(left + strings.Repeat(" ", gap) + right)
}

func fetchSnapshotCmd(exec Executor) tea.Cmd {
	return func() tea.Msg {
		snap := CollectSnapshot(exec)
		return snapshotMsg{snapshot: snap}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
