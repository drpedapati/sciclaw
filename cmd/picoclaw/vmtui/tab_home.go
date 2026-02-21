package vmtui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HomeModel handles the Home tab.
type HomeModel struct {
	selectedItem int // 0 = suggested action
}

func NewHomeModel() HomeModel {
	return HomeModel{}
}

func (m HomeModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (HomeModel, tea.Cmd) {
	switch msg.String() {
	case "enter":
		if snap != nil {
			_, _, tabIdx := snap.SuggestedStep()
			if tabIdx >= 0 {
				// Return a message to switch tabs — handled by parent
				return m, nil
			}
		}
	}
	return m, nil
}

func (m HomeModel) View(snap *VMSnapshot, width int) string {
	if snap == nil {
		return "\n  No data available yet.\n"
	}

	panelW := width - 4
	if panelW < 40 {
		panelW = 40
	}

	var b strings.Builder

	// VM Info panel
	b.WriteString(renderVMInfoPanel(snap, panelW))
	b.WriteString("\n")

	// Setup Checklist panel
	b.WriteString(renderChecklistPanel(snap, panelW))
	b.WriteString("\n")

	// Suggested Next Step panel
	b.WriteString(renderSuggestedPanel(snap, panelW))

	return b.String()
}

func renderVMInfoPanel(snap *VMSnapshot, w int) string {
	stateStyle := styleOK
	switch snap.State {
	case "Running":
		stateStyle = styleOK
	case "Stopped":
		stateStyle = styleWarn
	default:
		stateStyle = styleErr
	}

	ipStr := snap.IPv4
	if ipStr == "" {
		ipStr = "-"
	}
	loadStr := snap.Load
	if loadStr == "" {
		loadStr = "-"
	}
	memStr := snap.Memory
	if memStr == "" {
		memStr = "-"
	}
	verStr := snap.AgentVersion
	if verStr == "" {
		verStr = "-"
	}

	content := fmt.Sprintf(
		"%s %s    %s %s\n%s %s    %s %s\n%s %s",
		styleLabel.Render("Status:"), stateStyle.Render(snap.State),
		styleLabel.Render("IP:"), styleValue.Render(ipStr),
		styleLabel.Render("CPU Load:"), styleValue.Render(loadStr),
		styleLabel.Render("Memory:"), styleValue.Render(memStr),
		styleLabel.Render("Agent:"), styleValue.Render(verStr),
	)

	panel := stylePanel.Width(w).Render(content)
	title := stylePanelTitle.Render("Virtual Machine")
	return placePanelTitle(panel, title)
}

func renderChecklistPanel(snap *VMSnapshot, w int) string {
	items := []struct {
		status string
		label  string
	}{
		{boolStatus(snap.ConfigExists), "Configuration file"},
		{boolStatus(snap.WorkspaceExists), "Workspace folder"},
		{boolStatus(snap.AuthStoreExists), "Login credentials"},
		{providerCheckStatus(snap.OpenAI, snap.Anthropic), providerCheckLabel(snap.OpenAI, snap.Anthropic)},
		{channelCheckStatus(snap.Discord.Status, snap.Telegram.Status), channelCheckLabel(snap.Discord, snap.Telegram)},
		{boolStatus(snap.ServiceInstalled), "Agent service installed"},
		{boolStatus(snap.ServiceRunning), "Agent service running"},
	}

	var lines []string
	for _, item := range items {
		lines = append(lines, fmt.Sprintf(" %s %s", statusIcon(item.status), item.label))
	}

	content := strings.Join(lines, "\n")
	panel := stylePanel.Width(w).Render(content)
	title := stylePanelTitle.Render("Setup Checklist")
	return placePanelTitle(panel, title)
}

func renderSuggestedPanel(snap *VMSnapshot, w int) string {
	msg, detail, _ := snap.SuggestedStep()

	arrow := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("▸")
	msgStyled := styleBold.Render(msg)
	detailStyled := styleDim.Render("  " + detail)
	hint := styleDim.Render("  Press Enter to do this now.")

	content := fmt.Sprintf("\n %s %s\n%s\n%s\n", arrow, msgStyled, detailStyled, hint)
	panel := styleSuggestedPanel.Width(w).Render(content)
	title := stylePanelTitle.Render("Suggested Next Step")
	return placePanelTitle(panel, title)
}

func placePanelTitle(panel, title string) string {
	// Place title above the panel — reliable with ANSI-styled borders.
	return " " + title + "\n" + panel
}

func boolStatus(ok bool) string {
	if ok {
		return "ready"
	}
	return "missing"
}

func providerCheckStatus(openai, anthropic string) string {
	if openai == "ready" || anthropic == "ready" {
		return "ready"
	}
	return "missing"
}

func providerCheckLabel(openai, anthropic string) string {
	parts := []string{}
	if openai == "ready" {
		parts = append(parts, "OpenAI: ready")
	}
	if anthropic == "ready" {
		parts = append(parts, "Anthropic: ready")
	}
	if len(parts) > 0 {
		return fmt.Sprintf("AI provider (%s)", strings.Join(parts, ", "))
	}
	return "AI provider (not configured)"
}

func channelCheckStatus(discord, telegram string) string {
	if discord == "ready" || telegram == "ready" {
		return "ready"
	}
	if discord == "open" || telegram == "open" {
		return "open"
	}
	return "missing"
}

func channelCheckLabel(discord, telegram ChannelSnapshot) string {
	parts := []string{}
	describeChannel := func(name string, ch ChannelSnapshot) string {
		switch ch.Status {
		case "ready":
			return fmt.Sprintf("%s: ready", name)
		case "open":
			return fmt.Sprintf("%s: no approved users", name)
		case "broken":
			return fmt.Sprintf("%s: missing token", name)
		default:
			return ""
		}
	}
	if d := describeChannel("Discord", discord); d != "" {
		parts = append(parts, d)
	}
	if t := describeChannel("Telegram", telegram); t != "" {
		parts = append(parts, t)
	}
	if len(parts) > 0 {
		return fmt.Sprintf("Messaging app (%s)", strings.Join(parts, ", "))
	}
	return "Messaging app (not configured)"
}
