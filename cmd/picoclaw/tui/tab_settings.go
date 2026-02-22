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

type settingsMode int

const (
	settingsNormal  settingsMode = iota
	settingsEditing              // text input active
)

// settingsDataMsg carries parsed config values.
type settingsDataMsg struct {
	discordEnabled   bool
	telegramEnabled  bool
	routingEnabled   bool
	unmappedBehavior string
	defaultModel     string
	reasoningEffort  string
	err              error
}

type settingKind int

const (
	settingReadonly settingKind = iota
	settingBool
	settingEnum
	settingText
)

type settingRow struct {
	key     string
	label   string
	value   string
	kind    settingKind
	options []string // valid values for enums
	section string   // section header (set on first row of each section)
}

// SettingsModel handles the Settings tab.
type SettingsModel struct {
	exec        Executor
	mode        settingsMode
	loaded      bool
	selectedRow int

	// Editable config values
	discordEnabled   bool
	telegramEnabled  bool
	routingEnabled   bool
	unmappedBehavior string
	defaultModel     string
	reasoningEffort  string

	vp     viewport.Model
	width  int
	height int

	// Text editing
	input   textinput.Model
	editKey string

	// Flash feedback
	flashMsg   string
	flashUntil time.Time
}

func NewSettingsModel(exec Executor) SettingsModel {
	ti := textinput.New()
	ti.CharLimit = 100
	ti.Width = 40

	vp := viewport.New(60, 10)
	vp.KeyMap = viewport.KeyMap{}

	return SettingsModel{
		exec:  exec,
		input: ti,
		vp:    vp,
	}
}

func (m *SettingsModel) AutoRun() tea.Cmd {
	if !m.loaded {
		return fetchSettingsData(m.exec)
	}
	return nil
}

func (m *SettingsModel) HandleData(msg settingsDataMsg) {
	m.loaded = true
	if msg.err != nil {
		return
	}
	m.discordEnabled = msg.discordEnabled
	m.telegramEnabled = msg.telegramEnabled
	m.routingEnabled = msg.routingEnabled
	m.unmappedBehavior = msg.unmappedBehavior
	m.defaultModel = msg.defaultModel
	m.reasoningEffort = msg.reasoningEffort
}

func (m *SettingsModel) HandleResize(width, height int) {
	m.width = width
	m.height = height
	w := width - 8
	if w > 96 {
		w = 96
	}
	if w < 40 {
		w = 40
	}
	avail := height - 10
	if avail < 6 {
		avail = 6
	}
	m.vp.Width = w
	m.vp.Height = avail
}

func (m SettingsModel) buildDisplayRows(snap *VMSnapshot) []settingRow {
	effortDisplay := m.reasoningEffort
	if effortDisplay == "" {
		effortDisplay = "default"
	}

	rows := []settingRow{
		{key: "discord_enabled", label: "Discord", value: boolYesNo(m.discordEnabled), kind: settingBool, section: "Channels"},
		{key: "telegram_enabled", label: "Telegram", value: boolYesNo(m.telegramEnabled), kind: settingBool},
		{key: "routing_enabled", label: "Routing", value: boolYesNo(m.routingEnabled), kind: settingBool, section: "Routing"},
		{key: "unmapped_behavior", label: "Unmapped behavior", value: m.unmappedBehavior, kind: settingEnum, options: []string{"block", "default"}},
		{key: "default_model", label: "Default model", value: m.defaultModel, kind: settingText, section: "Agent"},
		{key: "reasoning_effort", label: "Reasoning effort", value: effortDisplay, kind: settingEnum, options: []string{"", "low", "medium", "high"}},
	}
	if snap != nil {
		rows = append(rows,
			settingRow{key: "svc_installed", label: "Installed", value: boolYesNo(snap.ServiceInstalled), kind: settingReadonly, section: "Service"},
			settingRow{key: "svc_running", label: "Running", value: boolYesNo(snap.ServiceRunning), kind: settingReadonly},
			settingRow{key: "workspace", label: "Workspace", value: snap.WorkspacePath, kind: settingReadonly, section: "General"},
		)
	}
	return rows
}

func boolYesNo(b bool) string {
	if b {
		return "Yes"
	}
	return "No"
}

// --- Update ---

func (m SettingsModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (SettingsModel, tea.Cmd) {
	key := msg.String()
	rows := m.buildDisplayRows(snap)

	if m.mode == settingsEditing {
		switch key {
		case "esc":
			m.mode = settingsNormal
			m.input.Blur()
			return m, nil
		case "enter":
			val := strings.TrimSpace(m.input.Value())
			m.mode = settingsNormal
			m.input.Blur()
			if val != "" {
				return m, m.applyTextEdit(val)
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}

	switch key {
	case "up", "k":
		if m.selectedRow > 0 {
			m.selectedRow--
		}
	case "down", "j":
		if m.selectedRow < len(rows)-1 {
			m.selectedRow++
		}
	case "enter", " ":
		if m.selectedRow < len(rows) {
			row := rows[m.selectedRow]
			switch row.kind {
			case settingBool:
				return m, m.toggleBool(row.key)
			case settingEnum:
				return m, m.cycleEnum(row.key, row.value, row.options)
			case settingText:
				m.mode = settingsEditing
				m.editKey = row.key
				m.input.SetValue(row.value)
				m.input.Focus()
				return m, nil
			}
		}
	case "l":
		m.loaded = false
		return m, fetchSettingsData(m.exec)
	}
	return m, nil
}

func (m *SettingsModel) toggleBool(key string) tea.Cmd {
	switch key {
	case "discord_enabled":
		m.discordEnabled = !m.discordEnabled
		m.setFlash("Discord", m.discordEnabled)
		return settingsToggleChannel(m.exec, "discord", m.discordEnabled)
	case "telegram_enabled":
		m.telegramEnabled = !m.telegramEnabled
		m.setFlash("Telegram", m.telegramEnabled)
		return settingsToggleChannel(m.exec, "telegram", m.telegramEnabled)
	case "routing_enabled":
		m.routingEnabled = !m.routingEnabled
		m.setFlash("Routing", m.routingEnabled)
		return routingToggleCmd(m.exec, m.routingEnabled)
	}
	return nil
}

func (m *SettingsModel) cycleEnum(key, current string, options []string) tea.Cmd {
	if len(options) == 0 {
		return nil
	}
	idx := 0
	for i, opt := range options {
		display := opt
		if opt == "" {
			display = "default"
		}
		if display == current {
			idx = i
			break
		}
	}
	next := options[(idx+1)%len(options)]

	switch key {
	case "unmapped_behavior":
		m.unmappedBehavior = next
		m.flashMsg = styleOK.Render("✓") + " Unmapped behavior: " + next
		m.flashUntil = time.Now().Add(3 * time.Second)
		return settingsSetConfig(m.exec, []string{"routing", "unmapped_behavior"}, next)
	case "reasoning_effort":
		m.reasoningEffort = next
		display := next
		if display == "" {
			display = "default"
		}
		m.flashMsg = styleOK.Render("✓") + " Reasoning effort: " + display
		m.flashUntil = time.Now().Add(3 * time.Second)
		return settingsSetConfig(m.exec, []string{"agents", "defaults", "reasoning_effort"}, next)
	}
	return nil
}

func (m *SettingsModel) applyTextEdit(value string) tea.Cmd {
	switch m.editKey {
	case "default_model":
		m.defaultModel = value
		m.flashMsg = styleOK.Render("✓") + " Model: " + value
		m.flashUntil = time.Now().Add(3 * time.Second)
		return settingsSetConfig(m.exec, []string{"agents", "defaults", "model"}, value)
	}
	return nil
}

func (m *SettingsModel) setFlash(name string, enabled bool) {
	action := "disabled"
	if enabled {
		action = "enabled"
	}
	m.flashMsg = styleOK.Render("✓") + " " + name + " " + action
	m.flashUntil = time.Now().Add(3 * time.Second)
}

// --- View ---

func (m SettingsModel) View(snap *VMSnapshot, width int) string {
	panelW := width - 4
	if panelW > 100 {
		panelW = 100
	}
	if panelW < 40 {
		panelW = 40
	}

	if !m.loaded {
		return "\n  Loading settings...\n"
	}

	rows := m.buildDisplayRows(snap)
	var lines []string
	lastSection := ""

	for i, row := range rows {
		if row.section != "" && row.section != lastSection {
			if lastSection != "" {
				lines = append(lines, "")
			}
			lines = append(lines, fmt.Sprintf("  %s", styleBold.Foreground(colorTitle).Render(row.section)))
			lastSection = row.section
		}

		indicator := "  "
		if i == m.selectedRow && m.mode == settingsNormal {
			indicator = styleBold.Foreground(colorAccent).Render("▸ ")
		}

		valStr := row.value
		switch row.kind {
		case settingBool:
			if row.value == "Yes" {
				valStr = styleOK.Render("Yes")
			} else {
				valStr = styleErr.Render("No")
			}
		case settingReadonly:
			if row.value == "Yes" {
				valStr = styleOK.Render("Yes")
			} else if row.value == "No" {
				valStr = styleErr.Render("No")
			} else if row.value == "" {
				valStr = styleDim.Render("not set")
			} else {
				valStr = styleDim.Render(row.value)
			}
		case settingEnum, settingText:
			if row.value == "" {
				valStr = styleDim.Render("not set")
			} else {
				valStr = styleValue.Render(row.value)
			}
		}

		labelStyle := lipgloss.NewStyle().Foreground(colorMuted).Width(20)
		line := fmt.Sprintf("  %s%s  %s", indicator, labelStyle.Render(row.label), valStr)

		if i == m.selectedRow && m.mode == settingsNormal {
			line = lipgloss.NewStyle().
				Background(lipgloss.Color("#2A2A4A")).
				Width(panelW - 4).
				Render(line)
		}

		lines = append(lines, line)
	}

	content := strings.Join(lines, "\n")
	m.vp.SetContent(content)

	var b strings.Builder
	panel := stylePanel.Width(panelW).Render(m.vp.View())
	title := stylePanelTitle.Render("Settings")
	b.WriteString(placePanelTitle(panel, title))

	// Keybindings
	b.WriteString(fmt.Sprintf("  %s Toggle / Edit   %s Refresh\n",
		styleKey.Render("[Enter]"),
		styleKey.Render("[l]"),
	))

	// Text editing overlay
	if m.mode == settingsEditing {
		b.WriteString("\n")
		editLabel := m.editKey
		if m.editKey == "default_model" {
			editLabel = "Model"
		}
		b.WriteString(fmt.Sprintf("  %s: %s\n", styleBold.Render(editLabel), m.input.View()))
		b.WriteString(styleDim.Render("    Enter to save, Esc to cancel") + "\n")
	}

	// Flash message
	if !m.flashUntil.IsZero() && time.Now().Before(m.flashUntil) {
		b.WriteString("  " + m.flashMsg + "\n")
	}

	return b.String()
}

// --- Commands ---

func fetchSettingsData(exec Executor) tea.Cmd {
	return func() tea.Msg {
		msg := settingsDataMsg{}
		cfg, err := readConfigMap(exec)
		if err != nil {
			msg.err = err
			return msg
		}

		if channels, ok := cfg["channels"].(map[string]interface{}); ok {
			if discord, ok := channels["discord"].(map[string]interface{}); ok {
				msg.discordEnabled, _ = discord["enabled"].(bool)
			}
			if telegram, ok := channels["telegram"].(map[string]interface{}); ok {
				msg.telegramEnabled, _ = telegram["enabled"].(bool)
			}
		}

		if agents, ok := cfg["agents"].(map[string]interface{}); ok {
			if defaults, ok := agents["defaults"].(map[string]interface{}); ok {
				msg.defaultModel, _ = defaults["model"].(string)
				msg.reasoningEffort, _ = defaults["reasoning_effort"].(string)
			}
		}

		if routing, ok := cfg["routing"].(map[string]interface{}); ok {
			msg.routingEnabled, _ = routing["enabled"].(bool)
			msg.unmappedBehavior, _ = routing["unmapped_behavior"].(string)
		}
		if msg.unmappedBehavior == "" {
			msg.unmappedBehavior = "block"
		}

		return msg
	}
}

func settingsToggleChannel(exec Executor, channel string, enabled bool) tea.Cmd {
	return func() tea.Msg {
		cfg, err := readConfigMap(exec)
		if err != nil {
			cfg = map[string]interface{}{}
		}
		channels := ensureMap(cfg, "channels")
		ch := ensureMap(channels, channel)
		ch["enabled"] = enabled
		_ = writeConfigMap(exec, cfg)
		return actionDoneMsg{output: channel + " toggled"}
	}
}

func settingsSetConfig(exec Executor, path []string, value interface{}) tea.Cmd {
	return func() tea.Msg {
		cfg, err := readConfigMap(exec)
		if err != nil {
			cfg = map[string]interface{}{}
		}
		current := cfg
		for _, key := range path[:len(path)-1] {
			current = ensureMap(current, key)
		}
		current[path[len(path)-1]] = value
		_ = writeConfigMap(exec, cfg)
		return actionDoneMsg{output: "Updated " + path[len(path)-1]}
	}
}
