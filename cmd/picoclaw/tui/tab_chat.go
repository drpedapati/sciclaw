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
	exec     Executor
	viewport viewport.Model
	input    textinput.Model
	history  []chatMessage
	waiting  bool
}

func NewChatModel(exec Executor) ChatModel {
	vp := viewport.New(60, 15)

	ti := textinput.New()
	ti.Placeholder = "Type your message here..."
	ti.CharLimit = 500
	ti.Width = 50
	ti.Focus()

	return ChatModel{
		exec:     exec,
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
		return m, sendChatCmd(m.exec, val)

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

	wrapW := m.viewport.Width - 4
	if wrapW < 30 {
		wrapW = 30
	}

	indent := "          " // 10 chars to align under "Agent: " content
	contentW := wrapW - len(indent)

	for _, msg := range m.history {
		switch msg.Role {
		case "user":
			lines = append(lines, "")
			wrapped := wrapText(msg.Content, wrapW-6)
			lines = append(lines, fmt.Sprintf(" %s %s", styleBold.Foreground(colorAccent).Render("You:"), wrapped[0]))
			for _, w := range wrapped[1:] {
				lines = append(lines, "       "+w)
			}
		case "assistant":
			lines = append(lines, "")
			rendered := renderMarkdown(msg.Content, contentW)
			rLines := strings.Split(rendered, "\n")
			lines = append(lines, fmt.Sprintf(" %s %s", styleBold.Foreground(colorSuccess).Render("Agent:"), rLines[0]))
			for _, w := range rLines[1:] {
				lines = append(lines, indent+w)
			}
		case "error":
			lines = append(lines, "")
			wrapped := wrapText(msg.Content, contentW)
			lines = append(lines, fmt.Sprintf(" %s %s", styleErr.Render("Error:"), wrapped[0]))
			for _, w := range wrapped[1:] {
				lines = append(lines, indent+w)
			}
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

// renderMarkdown does lightweight markdown-to-terminal rendering:
// headers, bold, inline code, fenced code blocks, and bullet lists.
func renderMarkdown(text string, width int) string {
	if width < 20 {
		width = 20
	}

	codeBlockStyle := lipgloss.NewStyle().Foreground(colorWarning)
	codeInlineStyle := lipgloss.NewStyle().Foreground(colorWarning)
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(colorTitle)
	boldStyle := lipgloss.NewStyle().Bold(true)

	var out []string
	srcLines := strings.Split(text, "\n")
	inCodeBlock := false

	for _, line := range srcLines {
		// Fenced code block toggle.
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCodeBlock = !inCodeBlock
			if inCodeBlock {
				out = append(out, styleDim.Render("───"))
			} else {
				out = append(out, styleDim.Render("───"))
			}
			continue
		}

		if inCodeBlock {
			out = append(out, codeBlockStyle.Render(line))
			continue
		}

		trimmed := strings.TrimSpace(line)

		// Blank line.
		if trimmed == "" {
			out = append(out, "")
			continue
		}

		// Headers.
		if strings.HasPrefix(trimmed, "### ") {
			out = append(out, headerStyle.Render(strings.TrimPrefix(trimmed, "### ")))
			continue
		}
		if strings.HasPrefix(trimmed, "## ") {
			out = append(out, headerStyle.Render(strings.TrimPrefix(trimmed, "## ")))
			continue
		}
		if strings.HasPrefix(trimmed, "# ") {
			out = append(out, headerStyle.Render(strings.TrimPrefix(trimmed, "# ")))
			continue
		}

		// Bullet lists — preserve the bullet, wrap the rest.
		if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
			body := trimmed[2:]
			body = applyInlineMarkdown(body, codeInlineStyle, boldStyle)
			wrapped := wrapText(body, width-2)
			out = append(out, "- "+wrapped[0])
			for _, w := range wrapped[1:] {
				out = append(out, "  "+w)
			}
			continue
		}

		// Numbered lists.
		if len(trimmed) > 2 && trimmed[0] >= '0' && trimmed[0] <= '9' {
			if dotIdx := strings.Index(trimmed, ". "); dotIdx > 0 && dotIdx < 4 {
				prefix := trimmed[:dotIdx+2]
				body := trimmed[dotIdx+2:]
				body = applyInlineMarkdown(body, codeInlineStyle, boldStyle)
				wrapped := wrapText(body, width-len(prefix))
				out = append(out, prefix+wrapped[0])
				pad := strings.Repeat(" ", len(prefix))
				for _, w := range wrapped[1:] {
					out = append(out, pad+w)
				}
				continue
			}
		}

		// Regular paragraph — apply inline markdown then wrap.
		styled := applyInlineMarkdown(trimmed, codeInlineStyle, boldStyle)
		for _, w := range wrapText(styled, width) {
			out = append(out, w)
		}
	}

	return strings.Join(out, "\n")
}

// applyInlineMarkdown handles **bold** and `inline code` within a line.
func applyInlineMarkdown(s string, codeStyle, boldStyle lipgloss.Style) string {
	// Inline code: `...`
	for {
		start := strings.Index(s, "`")
		if start < 0 {
			break
		}
		end := strings.Index(s[start+1:], "`")
		if end < 0 {
			break
		}
		end += start + 1
		code := s[start+1 : end]
		s = s[:start] + codeStyle.Render(code) + s[end+1:]
	}

	// Bold: **...**
	for {
		start := strings.Index(s, "**")
		if start < 0 {
			break
		}
		end := strings.Index(s[start+2:], "**")
		if end < 0 {
			break
		}
		end += start + 2
		bold := s[start+2 : end]
		s = s[:start] + boldStyle.Render(bold) + s[end+2:]
	}

	return s
}

func sendChatCmd(exec Executor, message string) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw agent -m " + shellEscape(message) + " -s tui:chat 2>/dev/null"
		out, err := exec.ExecShell(120*time.Second, cmd)
		if err != nil {
			return chatResponseMsg{err: fmt.Errorf("agent error: %w", err)}
		}
		return chatResponseMsg{content: out}
	}
}
