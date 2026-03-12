package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type emailMode int

const (
	emailNormal emailMode = iota
	emailEditing
)

type emailDataMsg struct {
	enabled        bool
	provider       string
	address        string
	displayName    string
	baseURL        string
	keyPresent     bool
	allowFrom      []string
	receiveEnabled bool
	err            error
}

type emailActionMsg struct {
	action string
	output string
	ok     bool
}

type emailField int

const (
	emailFieldEnabled emailField = iota
	emailFieldAddress
	emailFieldDisplayName
	emailFieldAPIKey
	emailFieldBaseURL
	emailFieldAllowFrom
	emailFieldTestRecipient
)

type EmailModel struct {
	exec          Executor
	mode          emailMode
	loaded        bool
	selectedRow   int
	enabled       bool
	provider      string
	address       string
	displayName   string
	baseURL       string
	keyPresent    bool
	allowFrom     []string
	receiveEnabled bool
	testRecipient string
	lastOut       string
	flashMsg      string
	flashUntil    time.Time

	input   textinput.Model
	editKey emailField
}

func NewEmailModel(exec Executor) EmailModel {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 48
	ti.Placeholder = "value"
	return EmailModel{
		exec:  exec,
		input: ti,
	}
}

func (m *EmailModel) AutoRun() tea.Cmd {
	if !m.loaded {
		return fetchEmailData(m.exec)
	}
	return nil
}

func (m *EmailModel) HandleData(msg emailDataMsg) {
	m.loaded = true
	if msg.err != nil {
		m.lastOut = msg.err.Error()
		return
	}
	m.enabled = msg.enabled
	m.provider = msg.provider
	m.address = msg.address
	m.displayName = msg.displayName
	m.baseURL = msg.baseURL
	m.keyPresent = msg.keyPresent
	m.allowFrom = append([]string(nil), msg.allowFrom...)
	m.receiveEnabled = msg.receiveEnabled
	if strings.TrimSpace(m.testRecipient) == "" {
		m.testRecipient = msg.address
	}
}

func (m *EmailModel) HandleAction(msg emailActionMsg) {
	if trimmed := strings.TrimSpace(msg.output); trimmed != "" {
		m.lastOut = shortenOutput(trimmed, 500)
	}
	if msg.ok {
		switch msg.action {
		case "toggle", "edit":
			m.flashMsg = "Email settings saved"
		case "test":
			m.flashMsg = "Test email sent"
		}
	} else {
		m.flashMsg = "Email action failed"
	}
	m.flashUntil = time.Now().Add(4 * time.Second)
}

func (m EmailModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (EmailModel, tea.Cmd) {
	if m.mode == emailEditing {
		switch msg.String() {
		case "esc":
			m.mode = emailNormal
			m.input.Blur()
			m.input.EchoMode = textinput.EchoNormal
			return m, nil
		case "enter":
			value := strings.TrimSpace(m.input.Value())
			editKey := m.editKey
			m.mode = emailNormal
			m.input.Blur()
			m.input.EchoMode = textinput.EchoNormal
			if editKey == emailFieldTestRecipient {
				m.testRecipient = value
				return m, nil
			}
			return m, tea.Batch(m.applyTextEdit(editKey, value), fetchEmailData(m.exec))
		default:
			var cmd tea.Cmd
			m.input, cmd = m.input.Update(msg)
			return m, cmd
		}
	}

	switch msg.String() {
	case "up", "k":
		if m.selectedRow > 0 {
			m.selectedRow--
		}
	case "down", "j":
		if m.selectedRow < 6 {
			m.selectedRow++
		}
	case " ", "enter":
		switch emailField(m.selectedRow) {
		case emailFieldEnabled:
			return m, tea.Batch(m.toggleEnabled(), fetchEmailData(m.exec))
		case emailFieldAddress:
			return m.startEdit(emailFieldAddress, m.address, false)
		case emailFieldDisplayName:
			return m.startEdit(emailFieldDisplayName, m.displayName, false)
		case emailFieldAPIKey:
			return m.startEdit(emailFieldAPIKey, "", true)
		case emailFieldBaseURL:
			return m.startEdit(emailFieldBaseURL, m.baseURL, false)
		case emailFieldAllowFrom:
			return m.startEdit(emailFieldAllowFrom, strings.Join(m.allowFrom, ", "), false)
		case emailFieldTestRecipient:
			return m.startEdit(emailFieldTestRecipient, m.testRecipient, false)
		}
	case "t":
		return m, m.sendTestEmail()
	case "r":
		return m, fetchEmailData(m.exec)
	}

	return m, nil
}

func (m EmailModel) startEdit(field emailField, current string, secret bool) (EmailModel, tea.Cmd) {
	m.mode = emailEditing
	m.editKey = field
	m.input.SetValue(current)
	m.input.CursorEnd()
	m.input.Focus()
	if secret {
		m.input.EchoMode = textinput.EchoPassword
		m.input.EchoCharacter = '•'
	} else {
		m.input.EchoMode = textinput.EchoNormal
	}
	return m, nil
}

func (m EmailModel) toggleEnabled() tea.Cmd {
	return func() tea.Msg {
		err := updateConfigMap(m.exec, func(cfg map[string]interface{}) error {
			channels := ensureMap(cfg, "channels")
			email := ensureMap(channels, "email")
			email["enabled"] = !m.enabled
			if strings.TrimSpace(emailStringValue(email["provider"])) == "" {
				email["provider"] = "resend"
			}
			if strings.TrimSpace(emailStringValue(email["base_url"])) == "" {
				email["base_url"] = "https://api.resend.com"
			}
			if strings.TrimSpace(emailStringValue(email["display_name"])) == "" {
				email["display_name"] = "sciClaw"
			}
			email["receive_enabled"] = false
			email["receive_mode"] = "poll"
			email["poll_interval_seconds"] = 30
			return nil
		})
		if err != nil {
			return emailActionMsg{action: "toggle", ok: false, output: err.Error()}
		}
		return emailActionMsg{action: "toggle", ok: true, output: "Email enabled state updated"}
	}
}

func (m EmailModel) applyTextEdit(field emailField, value string) tea.Cmd {
	return func() tea.Msg {
		err := updateConfigMap(m.exec, func(cfg map[string]interface{}) error {
			channels := ensureMap(cfg, "channels")
			email := ensureMap(channels, "email")
			email["provider"] = "resend"
			email["receive_enabled"] = false
			email["receive_mode"] = "poll"
			if _, ok := email["poll_interval_seconds"]; !ok {
				email["poll_interval_seconds"] = 30
			}
			switch field {
			case emailFieldAddress:
				email["address"] = value
			case emailFieldDisplayName:
				email["display_name"] = value
			case emailFieldAPIKey:
				email["api_key"] = value
			case emailFieldBaseURL:
				if strings.TrimSpace(value) == "" {
					value = "https://api.resend.com"
				}
				email["base_url"] = value
			case emailFieldAllowFrom:
				email["allow_from"] = parseCSVEmail(value)
			}
			return nil
		})
		if err != nil {
			return emailActionMsg{action: "edit", ok: false, output: err.Error()}
		}
		return emailActionMsg{action: "edit", ok: true, output: "Email settings updated"}
	}
}

func (m EmailModel) sendTestEmail() tea.Cmd {
	recipient := strings.TrimSpace(m.testRecipient)
	if recipient == "" {
		recipient = strings.TrimSpace(m.address)
	}
	return func() tea.Msg {
		if recipient == "" {
			return emailActionMsg{action: "test", ok: false, output: "test recipient is empty"}
		}
		out, err := m.exec.ExecCommand(60*time.Second, m.exec.BinaryPath(), "channels", "test", "email", "--to", recipient, "--subject", "sciClaw Email tab test")
		if err != nil {
			if strings.TrimSpace(out) == "" {
				out = err.Error()
			}
			return emailActionMsg{action: "test", ok: false, output: out}
		}
		return emailActionMsg{action: "test", ok: true, output: out}
	}
}

func (m EmailModel) View(snap *VMSnapshot, width int) string {
	panelW := width - 4
	if panelW > 100 {
		panelW = 100
	}
	if panelW < 50 {
		panelW = 50
	}

	rows := []string{
		fmt.Sprintf("  %-22s %s", "Email:", boolEnabledLabel(m.enabled)),
		fmt.Sprintf("  %-22s %s", "Provider:", fallbackString(m.provider, "resend")),
		fmt.Sprintf("  %-22s %s", "Address:", fallbackDash(m.address)),
		fmt.Sprintf("  %-22s %s", "Display name:", fallbackDash(m.displayName)),
		fmt.Sprintf("  %-22s %s", "Resend URL:", fallbackDash(m.baseURL)),
		fmt.Sprintf("  %-22s %s", "API key:", presentLabel(m.keyPresent)),
		fmt.Sprintf("  %-22s %s", "Receive:", "Coming soon (send-only in this build)"),
		fmt.Sprintf("  %-22s %s", "Sender allowlist:", allowlistLabel(m.allowFrom)),
		fmt.Sprintf("  %-22s %s", "Test recipient:", fallbackDash(m.testRecipient)),
	}

	selectable := []int{0, 2, 3, 5, 4, 7, 8}
	for idx, lineIdx := range selectable {
		if idx == m.selectedRow {
			rows[lineIdx] = lipgloss.NewStyle().Background(lipgloss.Color("#2A2A4A")).Bold(true).Render(rows[lineIdx])
		}
	}

	var b strings.Builder
	b.WriteString("\n")
	b.WriteString(stylePanel.Width(panelW).Render(strings.Join(rows, "\n")))
	b.WriteString("\n\n")
	b.WriteString(styleHint.Render("  Enter/Space: edit   [t] Send test email   [r] Refresh"))
	b.WriteString("\n")
	b.WriteString(styleDim.Render("  Use the sender allowlist now so inbound email can be enabled safely later."))

	if m.mode == emailEditing {
		b.WriteString("\n\n")
		b.WriteString(stylePanel.Width(panelW).Render(renderEmailEditOverlay(m)))
	}

	if strings.TrimSpace(m.lastOut) != "" {
		b.WriteString("\n\n")
		b.WriteString(stylePanel.Width(panelW).Render("  Last Output\n\n" + indentBlock(m.lastOut, "    ")))
	}

	if !m.flashUntil.IsZero() && time.Now().Before(m.flashUntil) && strings.TrimSpace(m.flashMsg) != "" {
		b.WriteString("\n\n")
		b.WriteString(styleHint.Render("  " + m.flashMsg))
	}

	return b.String()
}

func renderEmailEditOverlay(m EmailModel) string {
	label := "Edit value"
	switch m.editKey {
	case emailFieldAddress:
		label = "Edit from address"
	case emailFieldDisplayName:
		label = "Edit display name"
	case emailFieldAPIKey:
		label = "Edit Resend API key"
	case emailFieldBaseURL:
		label = "Edit Resend API URL"
	case emailFieldAllowFrom:
		label = "Edit sender allowlist"
	case emailFieldTestRecipient:
		label = "Edit test recipient"
	}
	lines := []string{
		"  " + label,
		"",
		"  " + m.input.View(),
		"",
		"  Enter to save, Esc to cancel",
	}
	return strings.Join(lines, "\n")
}

func fetchEmailData(exec Executor) tea.Cmd {
	return func() tea.Msg {
		raw, err := exec.ReadFile(exec.ConfigPath())
		if err != nil {
			if os.IsNotExist(err) {
				return emailDataMsg{}
			}
			return emailDataMsg{err: err}
		}
		var parsed struct {
			Channels struct {
				Email struct {
					Enabled        bool           `json:"enabled"`
					Provider       string         `json:"provider"`
					APIKey         string         `json:"api_key"`
					Address        string         `json:"address"`
					DisplayName    string         `json:"display_name"`
					BaseURL        string         `json:"base_url"`
					AllowFrom      flexStringSlice `json:"allow_from"`
					ReceiveEnabled bool           `json:"receive_enabled"`
				} `json:"email"`
			} `json:"channels"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			return emailDataMsg{err: err}
		}
		return emailDataMsg{
			enabled:        parsed.Channels.Email.Enabled,
			provider:       parsed.Channels.Email.Provider,
			address:        parsed.Channels.Email.Address,
			displayName:    parsed.Channels.Email.DisplayName,
			baseURL:        parsed.Channels.Email.BaseURL,
			keyPresent:     strings.TrimSpace(parsed.Channels.Email.APIKey) != "",
			allowFrom:      []string(parsed.Channels.Email.AllowFrom),
			receiveEnabled: parsed.Channels.Email.ReceiveEnabled,
		}
	}
}

func emailStringValue(v interface{}) string {
	s, _ := v.(string)
	return s
}

func parseCSVEmail(s string) []string {
	if strings.TrimSpace(s) == "" {
		return []string{}
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func boolEnabledLabel(v bool) string {
	if v {
		return "Enabled"
	}
	return "Disabled"
}

func fallbackDash(v string) string {
	if strings.TrimSpace(v) == "" {
		return "—"
	}
	return v
}

func fallbackString(v, fallback string) string {
	if strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func presentLabel(v bool) string {
	if v {
		return "configured"
	}
	return "missing"
}

func allowlistLabel(items []string) string {
	if len(items) == 0 {
		return "empty"
	}
	return fmt.Sprintf("%d rule(s)", len(items))
}

func indentBlock(s, prefix string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}
