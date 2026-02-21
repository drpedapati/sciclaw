package vmtui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

type chatMessage struct {
	Role    string // "user", "assistant", "error"
	Content string
}

type chatResponseMsg struct {
	content string
	err     error
}

// ChatModel handles the Chat tab.
type ChatModel struct {
	viewport viewport.Model
	input    textinput.Model
	history  []chatMessage
	waiting  bool
}

func NewChatModel() ChatModel {
	vp := viewport.New(60, 15)

	ti := textinput.New()
	ti.Placeholder = "Type your message here..."
	ti.CharLimit = 500
	ti.Width = 50
	ti.Focus()

	return ChatModel{
		viewport: vp,
		input:    ti,
	}
}

func (m ChatModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (ChatModel, tea.Cmd) {
	key := msg.String()

	switch key {
	case "enter":
		val := strings.TrimSpace(m.input.Value())
		if val == "" || m.waiting {
			return m, nil
		}
		m.history = append(m.history, chatMessage{Role: "user", Content: val})
		m.waiting = true
		m.input.SetValue("")
		m.refreshViewport()
		return m, sendChatCmd(val)

	case "up", "down", "pgup", "pgdown":
		// Forward scroll keys to viewport.
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd

	default:
		if m.waiting {
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
}

func (m *ChatModel) HandleResponse(msg chatResponseMsg) {
	m.waiting = false
	if msg.err != nil {
		m.history = append(m.history, chatMessage{
			Role:    "error",
			Content: msg.err.Error(),
		})
	} else {
		response := strings.TrimSpace(msg.content)
		// Strip the 🔬 logo prefix if present.
		response = strings.TrimPrefix(response, "🔬 ")
		response = strings.TrimPrefix(response, "🔬")
		response = strings.TrimSpace(response)
		if response == "" {
			response = "(no response)"
		}
		m.history = append(m.history, chatMessage{
			Role:    "assistant",
			Content: response,
		})
	}
	m.refreshViewport()
	m.viewport.GotoBottom()
}

func (m *ChatModel) HandleResize(width, height int) {
	w := width - 8
	if w < 40 {
		w = 40
	}
	h := height - 12 // room for header, tab bar, input, status bar
	if h < 5 {
		h = 5
	}
	m.viewport.Width = w
	m.viewport.Height = h
	m.input.Width = w - 4
	m.refreshViewport()
}

func (m *ChatModel) refreshViewport() {
	m.viewport.SetContent(m.renderHistory())
}

func (m ChatModel) View(snap *VMSnapshot, width int) string {
	panelW := width - 4
	if panelW < 40 {
		panelW = 40
	}

	var b strings.Builder

	if len(m.history) == 0 && !m.waiting {
		// Welcome state.
		var lines []string
		lines = append(lines, "")
		lines = append(lines, "  Chat with your AI agent directly from here.")
		lines = append(lines, "  Type a message and press Enter to start.")
		lines = append(lines, "")
		lines = append(lines, styleDim.Render("  The agent can answer questions, run tools,"))
		lines = append(lines, styleDim.Render("  and work with files in your workspace."))
		lines = append(lines, "")

		content := strings.Join(lines, "\n")
		panel := stylePanel.Width(panelW).Render(content)
		title := stylePanelTitle.Render("Chat with Agent")
		b.WriteString(placePanelTitle(panel, title))
	} else {
		// Conversation viewport.
		panel := stylePanel.Width(panelW).Render(m.viewport.View())
		title := stylePanelTitle.Render("Chat with Agent")
		b.WriteString(placePanelTitle(panel, title))
	}

	// Input area.
	b.WriteString("\n")
	if m.waiting {
		b.WriteString(styleDim.Render("  Agent is thinking... please wait"))
	} else {
		b.WriteString(fmt.Sprintf("  %s %s", styleKey.Render(">"), m.input.View()))
	}
	b.WriteString("\n")
	b.WriteString(styleHint.Render("  Enter: send   Arrow keys: scroll"))

	return b.String()
}

func (m ChatModel) renderHistory() string {
	var lines []string

	for _, msg := range m.history {
		switch msg.Role {
		case "user":
			lines = append(lines, "")
			lines = append(lines, fmt.Sprintf(" %s %s", styleBold.Foreground(colorAccent).Render("You:"), msg.Content))
		case "assistant":
			lines = append(lines, "")
			lines = append(lines, fmt.Sprintf(" %s %s", styleBold.Foreground(colorSuccess).Render("Agent:"), msg.Content))
		case "error":
			lines = append(lines, "")
			lines = append(lines, fmt.Sprintf(" %s %s", styleErr.Render("Error:"), msg.Content))
		}
	}

	if m.waiting {
		lines = append(lines, "")
		lines = append(lines, styleDim.Render(" Agent is thinking..."))
	}

	if len(lines) > 0 {
		lines = append(lines, "")
	}

	return strings.Join(lines, "\n")
}

func sendChatCmd(message string) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=/home/ubuntu sciclaw agent -m " + shellEscape(message) + " -s tui:chat 2>/dev/null"
		out, err := VMExecShell(120*time.Second, cmd)
		if err != nil {
			return chatResponseMsg{err: fmt.Errorf("agent error: %w", err)}
		}
		return chatResponseMsg{content: out}
	}
}
