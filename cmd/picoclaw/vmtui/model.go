package vmtui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var tabNames = []string{"Home", "Chat", "Messaging Apps", "Users", "Login", "Health Check", "Agent Service", "Your Files"}

// Messages.
type snapshotMsg struct {
	snapshot VMSnapshot
	err      error
}
type tickMsg time.Time
type actionDoneMsg struct{ output string }

// Model is the root TUI model.
type Model struct {
	activeTab int
	width     int
	height    int

	// Shared state
	snapshot    *VMSnapshot
	snapshotErr error
	loading     bool
	spinner     spinner.Model
	lastRefresh time.Time

	// Sub-models for each tab
	home     HomeModel
	chat     ChatModel
	channels ChannelsModel
	users    UsersModel
	login    LoginModel
	doctor   DoctorModel
	agent    AgentModel
	files    FilesModel
}

func NewModel() Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(colorAccent)

	return Model{
		activeTab: 0,
		spinner:   s,
		loading:   true,
		home:      NewHomeModel(),
		chat:      NewChatModel(),
		channels:  NewChannelsModel(),
		users:     NewUsersModel(),
		login:     NewLoginModel(),
		doctor:    NewDoctorModel(),
		agent:     NewAgentModel(),
		files:     NewFilesModel(),
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		fetchSnapshotCmd(),
		tickCmd(),
	)
}

// subTabCapturingInput returns true when a sub-tab is in a mode
// that captures all keyboard input (text entry, confirmation dialogs).
func (m Model) subTabCapturingInput() bool {
	return m.activeTab == 1 || // Chat always captures
		(m.activeTab == 2 && m.channels.mode != modeNormal) ||
		(m.activeTab == 3 && m.users.mode != usersNormal) ||
		(m.activeTab == 7 && m.files.mode != filesNormal)
}

// maybeAutoRunDoctor triggers doctor auto-run when the Health Check tab is first visited.
func (m *Model) maybeAutoRunDoctor() tea.Cmd {
	if m.activeTab == 5 {
		return m.doctor.AutoRun()
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
		return m, nil

	case tea.KeyMsg:
		// When a sub-tab is capturing input, allow ctrl+c and tab navigation,
		// then delegate everything else to the active sub-tab.
		if m.subTabCapturingInput() {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "tab":
				m.activeTab = (m.activeTab + 1) % len(tabNames)
				return m, m.maybeAutoRunDoctor()
			case "shift+tab":
				m.activeTab = (m.activeTab - 1 + len(tabNames)) % len(tabNames)
				return m, m.maybeAutoRunDoctor()
			}
			var cmd tea.Cmd
			switch m.activeTab {
			case 1:
				m.chat, cmd = m.chat.Update(msg, m.snapshot)
			case 2:
				m.channels, cmd = m.channels.Update(msg, m.snapshot)
			case 3:
				m.users, cmd = m.users.Update(msg, m.snapshot)
			case 7:
				m.files, cmd = m.files.Update(msg, m.snapshot)
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
			m.activeTab = (m.activeTab + 1) % len(tabNames)
			return m, m.maybeAutoRunDoctor()
		case "shift+tab":
			m.activeTab = (m.activeTab - 1 + len(tabNames)) % len(tabNames)
			return m, m.maybeAutoRunDoctor()
		}

		// Delegate to active tab.
		var cmd tea.Cmd
		switch m.activeTab {
		case 0:
			m.home, cmd = m.home.Update(msg, m.snapshot)
		case 1:
			m.chat, cmd = m.chat.Update(msg, m.snapshot)
		case 2:
			m.channels, cmd = m.channels.Update(msg, m.snapshot)
		case 3:
			m.users, cmd = m.users.Update(msg, m.snapshot)
		case 4:
			m.login, cmd = m.login.Update(msg, m.snapshot)
		case 5:
			m.doctor, cmd = m.doctor.Update(msg, m.snapshot)
		case 6:
			m.agent, cmd = m.agent.Update(msg, m.snapshot)
		case 7:
			m.files, cmd = m.files.Update(msg, m.snapshot)
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
		} else {
			m.snapshotErr = msg.err
		}
		return m, nil

	case tickMsg:
		if !m.loading && time.Since(m.lastRefresh) > 10*time.Second {
			m.loading = true
			return m, tea.Batch(m.spinner.Tick, fetchSnapshotCmd(), tickCmd())
		}
		return m, tickCmd()

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case actionDoneMsg:
		m.loading = true
		return m, tea.Batch(m.spinner.Tick, fetchSnapshotCmd())

	case chatResponseMsg:
		m.chat.HandleResponse(msg)
		return m, nil

	case doctorDoneMsg:
		m.doctor.HandleResult(msg)
		return m, nil

	case logsMsg:
		m.agent.HandleLogsMsg(msg)
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
	header := lipgloss.NewStyle().Bold(true).Foreground(colorAccent).Render("  sciClaw VM Control Center")
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
		content := fmt.Sprintf("\n  %s Connecting to VM...\n", m.spinner.View())
		b.WriteString(content)
	} else {
		var content string
		switch m.activeTab {
		case 0:
			content = m.home.View(m.snapshot, contentWidth)
		case 1:
			content = m.chat.View(m.snapshot, contentWidth)
		case 2:
			content = m.channels.View(m.snapshot, contentWidth)
		case 3:
			content = m.users.View(m.snapshot, contentWidth)
		case 4:
			content = m.login.View(m.snapshot, contentWidth)
		case 5:
			content = m.doctor.View(m.snapshot, contentWidth)
		case 6:
			content = m.agent.View(m.snapshot, contentWidth)
		case 7:
			content = m.files.View(m.snapshot, contentWidth)
		}
		b.WriteString(content)
	}

	// Pad to push status bar to bottom
	rendered := b.String()
	lines := strings.Count(rendered, "\n") + 1
	for lines < m.height-1 {
		rendered += "\n"
		lines++
	}

	rendered += m.renderStatusBar()

	return rendered
}

func (m Model) renderTabBar() string {
	var tabs []string
	for i, name := range tabNames {
		if i == m.activeTab {
			tabs = append(tabs, styleTabActive.Render(name))
		} else {
			tabs = append(tabs, styleTabInactive.Render(name))
		}
	}
	row := lipgloss.JoinHorizontal(lipgloss.Top, tabs...)
	return styleTabBar.Width(m.width).Render(row)
}

func (m Model) renderStatusBar() string {
	left := ""
	if m.snapshot != nil {
		stateColor := colorSuccess
		switch m.snapshot.State {
		case "Stopped":
			stateColor = colorWarning
		case "NotFound", "":
			stateColor = colorError
		}
		left = fmt.Sprintf(" VM: %s", lipgloss.NewStyle().Foreground(stateColor).Bold(true).Render(m.snapshot.State))
		if m.loading {
			left += fmt.Sprintf("  %s", m.spinner.View())
		}
	} else if m.loading {
		left = fmt.Sprintf(" %s Connecting...", m.spinner.View())
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

func fetchSnapshotCmd() tea.Cmd {
	return func() tea.Msg {
		snap := CollectSnapshot()
		return snapshotMsg{snapshot: snap}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
