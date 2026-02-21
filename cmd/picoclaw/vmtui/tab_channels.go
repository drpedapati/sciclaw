package vmtui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type channelFocus int

const (
	focusDiscord channelFocus = iota
	focusTelegram
)

type channelMode int

const (
	modeNormal channelMode = iota
	modeAddUser
	modeConfirmRemove
	modeSetup // inline channel setup wizard
)

// ChannelsModel handles the Messaging Apps tab.
type ChannelsModel struct {
	focus       channelFocus
	mode        channelMode
	selectedRow int // selected row in the focused channel's user table

	// Add user flow
	addInput    textinput.Model
	addChannel  string // "discord" or "telegram"
	addStep     int    // 0 = ID, 1 = optional name

	// Remove user confirmation
	removeUser ApprovedUser
	removeIdx  int

	// Temporary add state
	pendingID string

	// Setup wizard state
	setupChannel string // "discord" or "telegram"
	setupStep    int    // 0=token, 1=userID, 2=userName(optional), 3=confirm
	setupToken   string
	setupUserID  string
}

func NewChannelsModel() ChannelsModel {
	ti := textinput.New()
	ti.CharLimit = 64
	ti.Width = 40

	return ChannelsModel{
		addInput: ti,
	}
}

func (m ChannelsModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (ChannelsModel, tea.Cmd) {
	if snap == nil {
		return m, nil
	}

	key := msg.String()

	// Handle text input mode.
	if m.mode == modeAddUser {
		switch key {
		case "esc":
			m.mode = modeNormal
			m.addInput.Blur()
			return m, nil
		case "enter":
			return m.handleAddSubmit(snap)
		default:
			var cmd tea.Cmd
			m.addInput, cmd = m.addInput.Update(msg)
			return m, cmd
		}
	}

	// Handle setup wizard mode.
	if m.mode == modeSetup {
		switch key {
		case "esc":
			m.mode = modeNormal
			m.addInput.Blur()
			m.addInput.EchoMode = textinput.EchoNormal
			return m, nil
		case "enter":
			return m.handleSetupSubmit(snap)
		default:
			if m.setupStep < 3 {
				var cmd tea.Cmd
				m.addInput, cmd = m.addInput.Update(msg)
				return m, cmd
			}
		}
		return m, nil
	}

	// Handle remove confirmation.
	if m.mode == modeConfirmRemove {
		switch key {
		case "y", "Y":
			return m.executeRemove(snap)
		case "n", "N", "esc":
			m.mode = modeNormal
			return m, nil
		}
		return m, nil
	}

	// Normal mode key handling.
	switch key {
	case "up", "k":
		if m.selectedRow > 0 {
			m.selectedRow--
		}
	case "down", "j":
		users := m.focusedUsers(snap)
		if m.selectedRow < len(users)-1 {
			m.selectedRow++
		}
	case "left", "right":
		if m.focus == focusDiscord {
			m.focus = focusTelegram
		} else {
			m.focus = focusDiscord
		}
		m.selectedRow = 0
	case "a":
		return m.startAddUser()
	case "d", "backspace", "delete":
		users := m.focusedUsers(snap)
		if m.selectedRow < len(users) {
			m.removeUser = users[m.selectedRow]
			m.removeIdx = m.selectedRow
			m.mode = modeConfirmRemove
		}
	case "s":
		return m.startSetup(snap)
	}
	return m, nil
}

func (m ChannelsModel) View(snap *VMSnapshot, width int) string {
	if snap == nil {
		return "\n  No data available yet.\n"
	}

	panelW := width - 4
	if panelW < 40 {
		panelW = 40
	}

	var b strings.Builder

	// Discord panel
	b.WriteString(m.renderChannelPanel("Discord", snap.Discord, m.focus == focusDiscord, panelW, snap))
	b.WriteString("\n")

	// Telegram panel
	b.WriteString(m.renderChannelPanel("Telegram", snap.Telegram, m.focus == focusTelegram, panelW, snap))
	b.WriteString("\n")
	b.WriteString(styleHint.Render("  Arrow keys: navigate   Left/Right: switch between Discord and Telegram"))

	return b.String()
}

func (m ChannelsModel) renderChannelPanel(name string, ch ChannelSnapshot, focused bool, w int, snap *VMSnapshot) string {
	var lines []string

	// Status line
	var badge string
	switch ch.Status {
	case "ready":
		badge = badgeReady()
	case "open":
		badge = badgeWarning()
	case "off":
		badge = styleDim.Render("[Not Configured]")
	default:
		badge = badgeNotReady()
	}

	statusText := channelStatusText(ch)
	lines = append(lines, fmt.Sprintf(" %s %s  %s", styleLabel.Render("Status:"), statusText, badge))

	if ch.Status == "off" {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %s Set up %s", styleKey.Render("[s]"), name))
	} else {
		// Approved users table
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf(" %s", styleBold.Render("Approved Users (who can talk to your bot):")))

		if len(ch.ApprovedUsers) == 0 {
			lines = append(lines, styleWarn.Render("   No users approved yet. Anyone could message the bot."))
		} else {
			// Table header
			lines = append(lines, fmt.Sprintf("  %s  %-20s  %s",
				styleDim.Render(" # "),
				styleDim.Render("User ID"),
				styleDim.Render("Username"),
			))
			lines = append(lines, styleDim.Render("  "+strings.Repeat("─", 50)))

			isFocused := (name == "Discord" && m.focus == focusDiscord) || (name == "Telegram" && m.focus == focusTelegram)
			for i, user := range ch.ApprovedUsers {
				num := fmt.Sprintf(" %d ", i+1)
				id := user.DisplayID()
				uname := user.DisplayName()

				row := fmt.Sprintf("  %s  %-20s  %s", num, id, uname)
				if isFocused && i == m.selectedRow && m.mode == modeNormal {
					row = lipgloss.NewStyle().
						Background(lipgloss.Color("#2A2A4A")).
						Bold(true).
						Render(row)
				}
				lines = append(lines, row)
			}
		}

		// Actions
		lines = append(lines, "")
		actions := fmt.Sprintf("  %s Add a user   %s Remove selected   %s Reconfigure",
			styleKey.Render("[a]"),
			styleKey.Render("[d]"),
			styleKey.Render("[s]"),
		)
		lines = append(lines, actions)
	}

	// Overlay modes.
	isFocused := (name == "Discord" && m.focus == focusDiscord) || (name == "Telegram" && m.focus == focusTelegram)
	if isFocused && m.mode == modeAddUser {
		lines = append(lines, "")
		lines = append(lines, renderAddUserOverlay(m, name))
	}
	if isFocused && m.mode == modeConfirmRemove {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  Remove %s? %s / %s",
			styleBold.Render(m.removeUser.DisplayName()),
			styleKey.Render("[y]es"),
			styleKey.Render("[n]o"),
		))
	}
	if isFocused && m.mode == modeSetup {
		lines = append(lines, "")
		lines = append(lines, m.renderSetupOverlay(name))
	}

	content := strings.Join(lines, "\n")
	borderStyle := stylePanel
	if focused {
		borderStyle = borderStyle.BorderForeground(colorAccent)
	}
	panel := borderStyle.Width(w).Render(content)
	title := stylePanelTitle.Render(name)
	return placePanelTitle(panel, title)
}

func renderAddUserOverlay(m ChannelsModel, channelName string) string {
	var lines []string
	if m.addStep == 0 {
		if channelName == "Discord" {
			lines = append(lines, fmt.Sprintf("  Enter their Discord User ID: %s", m.addInput.View()))
			lines = append(lines, styleHint.Render("    To find it: Discord Settings → Advanced → Developer Mode → Right-click avatar → Copy User ID"))
		} else {
			lines = append(lines, fmt.Sprintf("  Enter their Telegram User ID: %s", m.addInput.View()))
			lines = append(lines, styleHint.Render("    Tip: Ask them to message @userinfobot on Telegram"))
		}
	} else {
		lines = append(lines, fmt.Sprintf("  Add a display name (optional): %s", m.addInput.View()))
		lines = append(lines, styleHint.Render("    Press Enter to skip, or type a name and press Enter"))
	}
	return strings.Join(lines, "\n")
}

func channelStatusText(ch ChannelSnapshot) string {
	switch ch.Status {
	case "ready":
		return styleOK.Render("Connected")
	case "open":
		return styleWarn.Render("Connected, no approved users")
	case "broken":
		return styleErr.Render("Missing bot token")
	default:
		return styleDim.Render("Not configured")
	}
}

func (m ChannelsModel) focusedUsers(snap *VMSnapshot) []ApprovedUser {
	if snap == nil {
		return nil
	}
	if m.focus == focusDiscord {
		return snap.Discord.ApprovedUsers
	}
	return snap.Telegram.ApprovedUsers
}

func (m ChannelsModel) startAddUser() (ChannelsModel, tea.Cmd) {
	m.mode = modeAddUser
	m.addStep = 0
	m.pendingID = ""
	if m.focus == focusDiscord {
		m.addChannel = "discord"
	} else {
		m.addChannel = "telegram"
	}
	m.addInput.SetValue("")
	m.addInput.Placeholder = "e.g. 123456789012345678"
	m.addInput.Focus()
	return m, nil
}

func (m ChannelsModel) handleAddSubmit(snap *VMSnapshot) (ChannelsModel, tea.Cmd) {
	val := strings.TrimSpace(m.addInput.Value())

	if m.addStep == 0 {
		// Step 0: user ID submitted.
		if val == "" {
			m.mode = modeNormal
			m.addInput.Blur()
			return m, nil
		}
		m.pendingID = val
		m.addStep = 1
		m.addInput.SetValue("")
		m.addInput.Placeholder = "(optional display name)"
		return m, nil
	}

	// Step 1: optional name submitted.
	entry := FormatEntry(m.pendingID, val)
	m.mode = modeNormal
	m.addInput.Blur()

	// Save to config inside the VM.
	return m, addUserToVMConfig(m.addChannel, entry)
}

func (m ChannelsModel) executeRemove(snap *VMSnapshot) (ChannelsModel, tea.Cmd) {
	m.mode = modeNormal
	ch := "discord"
	if m.focus == focusTelegram {
		ch = "telegram"
	}
	return m, removeUserFromVMConfig(ch, m.removeIdx)
}

func (m ChannelsModel) startSetup(snap *VMSnapshot) (ChannelsModel, tea.Cmd) {
	m.mode = modeSetup
	m.setupStep = 0
	m.setupToken = ""
	m.setupUserID = ""
	if m.focus == focusDiscord {
		m.setupChannel = "discord"
	} else {
		m.setupChannel = "telegram"
	}
	m.addInput.SetValue("")
	m.addInput.Placeholder = "paste bot token here"
	m.addInput.CharLimit = 256
	m.addInput.EchoMode = textinput.EchoPassword
	m.addInput.Focus()
	return m, nil
}

func (m ChannelsModel) handleSetupSubmit(snap *VMSnapshot) (ChannelsModel, tea.Cmd) {
	val := strings.TrimSpace(m.addInput.Value())

	switch m.setupStep {
	case 0: // Token submitted
		if val == "" {
			m.mode = modeNormal
			m.addInput.Blur()
			m.addInput.EchoMode = textinput.EchoNormal
			return m, nil
		}
		m.setupToken = val

		// Check if channel already has approved users — skip user ID step if so.
		ch := snap.Discord
		if m.setupChannel == "telegram" {
			ch = snap.Telegram
		}
		if len(ch.ApprovedUsers) > 0 {
			// Skip to confirm step.
			m.setupStep = 3
			m.addInput.Blur()
			m.addInput.EchoMode = textinput.EchoNormal
			return m, nil
		}

		m.setupStep = 1
		m.addInput.SetValue("")
		m.addInput.Placeholder = "e.g. 123456789012345678"
		m.addInput.EchoMode = textinput.EchoNormal
		m.addInput.CharLimit = 64
		return m, nil

	case 1: // User ID submitted
		if val == "" {
			m.mode = modeNormal
			m.addInput.Blur()
			m.addInput.EchoMode = textinput.EchoNormal
			return m, nil
		}
		m.setupUserID = val
		m.setupStep = 2
		m.addInput.SetValue("")
		m.addInput.Placeholder = "(optional display name)"
		return m, nil

	case 2: // Optional display name submitted
		if m.setupUserID != "" && val != "" {
			m.setupUserID = FormatEntry(m.setupUserID, val)
		}
		m.setupStep = 3
		m.addInput.Blur()
		return m, nil

	case 3: // Confirm — Enter means yes
		m.mode = modeNormal
		m.addInput.EchoMode = textinput.EchoNormal
		m.addInput.CharLimit = 64
		return m, saveChannelSetup(m.setupChannel, m.setupToken, m.setupUserID)
	}

	return m, nil
}

func (m ChannelsModel) renderSetupOverlay(channelName string) string {
	var lines []string
	header := styleBold.Render(fmt.Sprintf("  Set up %s", channelName))
	lines = append(lines, header)

	switch m.setupStep {
	case 0:
		lines = append(lines, fmt.Sprintf("  Paste your %s bot token: %s", channelName, m.addInput.View()))
		if channelName == "Discord" {
			lines = append(lines, styleHint.Render("    Get this from Discord Developer Portal → Bot → Token"))
		} else {
			lines = append(lines, styleHint.Render("    Get this from @BotFather on Telegram"))
		}
	case 1:
		lines = append(lines, fmt.Sprintf("  Enter your %s User ID: %s", channelName, m.addInput.View()))
		if channelName == "Discord" {
			lines = append(lines, styleHint.Render("    Discord Settings → Advanced → Developer Mode → Right-click avatar → Copy User ID"))
		} else {
			lines = append(lines, styleHint.Render("    Message @userinfobot on Telegram to get your ID"))
		}
	case 2:
		lines = append(lines, fmt.Sprintf("  Add a display name (optional): %s", m.addInput.View()))
		lines = append(lines, styleHint.Render("    Press Enter to skip"))
	case 3:
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %s  Enabled: %s", styleDim.Render("Review:"), styleOK.Render("true")))
		lines = append(lines, fmt.Sprintf("           Token: %s", styleOK.Render("set")))
		if m.setupUserID != "" {
			lines = append(lines, fmt.Sprintf("           User:  %s", styleValue.Render(m.setupUserID)))
		}
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  Save these settings? Press %s to save, %s to cancel",
			styleKey.Render("Enter"), styleKey.Render("Esc")))
	}
	lines = append(lines, styleDim.Render("    Esc to cancel"))
	return strings.Join(lines, "\n")
}

func saveChannelSetup(channel, token, userEntry string) tea.Cmd {
	return func() tea.Msg {
		tokenJSON, _ := json.Marshal(token)
		userJSON, _ := json.Marshal(userEntry)
		script := fmt.Sprintf(`
import json
with open('/home/ubuntu/.picoclaw/config.json', 'r') as f:
    cfg = json.load(f)
ch = cfg.setdefault('channels', {}).setdefault('%s', {})
ch['enabled'] = True
ch['token'] = json.loads(%s)
user = json.loads(%s)
if user:
    af = ch.setdefault('allow_from', [])
    if user not in af:
        af.append(user)
with open('/home/ubuntu/.picoclaw/config.json', 'w') as f:
    json.dump(cfg, f, indent=2)
print('ok')
`, channel, string(tokenJSON), string(userJSON))
		_, _ = VMExecShell(5*time.Second, "python3 -c "+shellEscape(script))
		return actionDoneMsg{output: channel + " setup saved."}
	}
}

// addUserToVMConfig appends a user to the channel's allow_from in the VM config.
func addUserToVMConfig(channel, entry string) tea.Cmd {
	return func() tea.Msg {
		// JSON-encode the entry value for safe embedding in Python.
		entryJSON, _ := json.Marshal(entry)
		script := fmt.Sprintf(`
import json
with open('/home/ubuntu/.picoclaw/config.json', 'r') as f:
    cfg = json.load(f)
ch = cfg.setdefault('channels', {}).setdefault('%s', {})
af = ch.setdefault('allow_from', [])
entry = json.loads(%s)
if entry not in af:
    af.append(entry)
with open('/home/ubuntu/.picoclaw/config.json', 'w') as f:
    json.dump(cfg, f, indent=2)
print('ok')
`, channel, string(entryJSON))
		_, _ = VMExecShell(5*time.Second, "python3 -c "+shellEscape(script))
		return actionDoneMsg{output: "User added."}
	}
}

// removeUserFromVMConfig removes a user by index from the channel's allow_from.
func removeUserFromVMConfig(channel string, idx int) tea.Cmd {
	return func() tea.Msg {
		script := fmt.Sprintf(`
import json
with open('/home/ubuntu/.picoclaw/config.json', 'r') as f:
    cfg = json.load(f)
af = cfg.get('channels', {}).get('%s', {}).get('allow_from', [])
if %d < len(af):
    af.pop(%d)
with open('/home/ubuntu/.picoclaw/config.json', 'w') as f:
    json.dump(cfg, f, indent=2)
print('ok')
`, channel, idx, idx)
		_, _ = VMExecShell(5*time.Second, "python3 -c "+shellEscape(script))
		return actionDoneMsg{output: "User removed."}
	}
}
