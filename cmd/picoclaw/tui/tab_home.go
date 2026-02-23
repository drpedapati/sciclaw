package tui

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Wizard step constants.
const (
	wizardWelcome = 0 // Welcome screen
	wizardAuth    = 1 // Authentication
	wizardSmoke   = 2 // Smoke test (optional)
	wizardChannel = 3 // Channel selection
	wizardService = 4 // Gateway service install
	wizardDone    = 5 // Done
)

// HomeModel handles the Home tab.
type HomeModel struct {
	exec         Executor
	selectedItem int // 0 = suggested action

	// Onboard wizard state
	wizardChecked    bool   // whether the first snapshot was checked
	onboardActive    bool   // wizard overlay visible
	onboardStep      int    // current wizard step
	onboardLoading   bool   // async command in progress
	onboardResult    string // result text from last async op
	onboardSmokePass bool   // smoke test passed
}

func NewHomeModel(exec Executor) HomeModel {
	return HomeModel{exec: exec}
}

func (m HomeModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (HomeModel, tea.Cmd) {
	// Wizard captures all input when active.
	if m.onboardActive {
		return m.updateWizard(msg)
	}

	switch msg.String() {
	case "enter":
		if snap != nil {
			_, _, tabIdx := snap.SuggestedStep()
			if tabIdx >= 0 {
				return m, func() tea.Msg { return homeNavigateMsg{tabID: tabIdx} }
			}
		}
	case "t":
		// Smoke test from normal Home view.
		if snap != nil && snap.ConfigExists {
			return m, m.runSmokeTest()
		}
	}
	return m, nil
}

// --- Wizard Update ---

func (m HomeModel) updateWizard(msg tea.KeyMsg) (HomeModel, tea.Cmd) {
	key := msg.String()

	switch m.onboardStep {
	case wizardWelcome:
		if key == "enter" {
			// Create config with defaults + workspace directory.
			return m, m.createDefaultConfig()
		}

	case wizardAuth:
		switch key {
		case "enter":
			c := m.exec.InteractiveProcess("sciclaw", "auth", "login", "--provider", "openai")
			return m, tea.ExecProcess(c, func(err error) tea.Msg {
				return onboardExecDoneMsg{step: wizardAuth}
			})
		case "a":
			c := m.exec.InteractiveProcess("sciclaw", "auth", "login", "--provider", "anthropic")
			return m, tea.ExecProcess(c, func(err error) tea.Msg {
				return onboardExecDoneMsg{step: wizardAuth}
			})
		case "esc":
			m.onboardStep = wizardSmoke
		}

	case wizardSmoke:
		if m.onboardLoading {
			return m, nil // wait for result
		}
		if m.onboardResult != "" {
			// Result shown — Enter to continue.
			if key == "enter" {
				m.onboardResult = ""
				m.onboardStep = wizardChannel
			}
			return m, nil
		}
		switch key {
		case "enter":
			m.onboardLoading = true
			return m, m.runSmokeTest()
		case "esc":
			m.onboardStep = wizardChannel
		}

	case wizardChannel:
		switch key {
		case "t":
			c := m.exec.InteractiveProcess("sciclaw", "channels", "setup", "telegram")
			return m, tea.ExecProcess(c, func(err error) tea.Msg {
				return onboardExecDoneMsg{step: wizardChannel}
			})
		case "d":
			c := m.exec.InteractiveProcess("sciclaw", "channels", "setup", "discord")
			return m, tea.ExecProcess(c, func(err error) tea.Msg {
				return onboardExecDoneMsg{step: wizardChannel}
			})
		case "s", "esc":
			m.onboardStep = wizardService
		}

	case wizardService:
		if m.onboardLoading {
			return m, nil
		}
		if m.onboardResult != "" {
			if key == "enter" {
				m.onboardResult = ""
				m.onboardStep = wizardDone
			}
			return m, nil
		}
		switch key {
		case "enter":
			m.onboardLoading = true
			return m, m.installService()
		case "esc":
			m.onboardStep = wizardDone
		}

	case wizardDone:
		if key == "enter" {
			m.onboardActive = false
		}
	}

	return m, nil
}

// HandleExecDone processes async wizard command results.
func (m *HomeModel) HandleExecDone(msg onboardExecDoneMsg) {
	m.onboardLoading = false

	switch msg.step {
	case wizardWelcome:
		// Config creation result.
		if msg.err != nil {
			m.onboardResult = "Failed to create config: " + msg.err.Error()
		} else {
			m.onboardStep = wizardAuth
		}
	case wizardAuth:
		m.onboardStep = wizardSmoke
	case wizardSmoke:
		if msg.err != nil {
			m.onboardResult = "fail"
			m.onboardSmokePass = false
		} else {
			m.onboardResult = "pass"
			m.onboardSmokePass = true
		}
	case wizardChannel:
		m.onboardStep = wizardService
	case wizardService:
		if msg.err != nil {
			m.onboardResult = "Service install failed: " + msg.err.Error()
		} else {
			m.onboardResult = "Service installed and started."
		}
	}
}

// --- Wizard Commands ---

func (m HomeModel) createDefaultConfig() tea.Cmd {
	exec := m.exec
	return func() tea.Msg {
		// Ensure ~/.picoclaw/ directory exists.
		home := exec.HomePath()
		mkdirCmd := "mkdir -p " + shellEscape(home+"/.picoclaw/workspace")
		_, _ = exec.ExecShell(5*time.Second, mkdirCmd)

		// Create default config.
		cfg := map[string]interface{}{
			"agents": map[string]interface{}{
				"defaults": map[string]interface{}{
					"model":     "gpt-5.2",
					"workspace": "~/.picoclaw/workspace",
				},
			},
			"channels": map[string]interface{}{
				"discord":  map[string]interface{}{"enabled": false},
				"telegram": map[string]interface{}{"enabled": false},
			},
		}
		err := writeConfigMap(exec, cfg)
		return onboardExecDoneMsg{step: wizardWelcome, err: err}
	}
}

func (m HomeModel) runSmokeTest() tea.Cmd {
	exec := m.exec
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw agent -m 'Hello, are you there?' 2>&1"
		out, err := exec.ExecShell(30*time.Second, cmd)
		output := strings.TrimSpace(out)
		if output == "" && err != nil {
			output = err.Error()
		}
		return onboardExecDoneMsg{step: wizardSmoke, output: output, err: err}
	}
}

func (m HomeModel) installService() tea.Cmd {
	exec := m.exec
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw service install 2>&1 && HOME=" + exec.HomePath() + " sciclaw service start 2>&1"
		out, err := exec.ExecShell(20*time.Second, cmd)
		return onboardExecDoneMsg{step: wizardService, output: strings.TrimSpace(out), err: err}
	}
}

// --- View ---

func (m HomeModel) View(snap *VMSnapshot, width int) string {
	if m.onboardActive {
		return m.viewWizard(snap, width)
	}
	return m.viewNormal(snap, width)
}

func (m HomeModel) viewNormal(snap *VMSnapshot, width int) string {
	if snap == nil {
		return "\n  No data available yet.\n"
	}

	panelW := width - 4
	if panelW > 100 {
		panelW = 100
	}
	if panelW < 40 {
		panelW = 40
	}

	var b strings.Builder

	// Info panel — mode-aware
	if snap.State == "Local" {
		b.WriteString(renderSystemInfoPanel(snap, panelW))
	} else {
		b.WriteString(renderVMInfoPanel(snap, panelW))
	}
	b.WriteString("\n")

	// Setup Checklist panel
	b.WriteString(renderChecklistPanel(snap, panelW))
	b.WriteString("\n")

	// Suggested Next Step panel
	b.WriteString(renderSuggestedPanel(snap, panelW))
	b.WriteString("\n")

	// Keybindings
	b.WriteString(fmt.Sprintf("  %s Navigate to suggested step   %s Test connection\n",
		styleKey.Render("[Enter]"),
		styleKey.Render("[t]"),
	))

	return b.String()
}

// --- Wizard View ---

func (m HomeModel) viewWizard(snap *VMSnapshot, width int) string {
	panelW := width - 4
	if panelW > 80 {
		panelW = 80
	}
	if panelW < 40 {
		panelW = 40
	}

	var b strings.Builder

	switch m.onboardStep {
	case wizardWelcome:
		b.WriteString(m.wizardFrame(panelW, "Welcome",
			"\n"+
				"  Welcome to "+styleBold.Render("sciClaw")+" setup.\n"+
				"\n"+
				"  This wizard will walk you through the initial configuration.\n"+
				"  It takes about 2 minutes.\n"+
				"\n"+
				"  "+styleDim.Render("Press Enter to begin.")+"\n",
		))

	case wizardAuth:
		b.WriteString(m.wizardFrame(panelW, "Authentication",
			"\n"+
				"  "+styleOK.Render("✓")+" Configuration file created.\n"+
				"\n"+
				"  Choose your AI provider:\n"+
				"\n"+
				"  "+styleKey.Render("[Enter]")+" Log in with OpenAI (recommended)\n"+
				"  "+styleKey.Render("[a]")+"     Log in with Anthropic\n"+
				"  "+styleKey.Render("[Esc]")+"   Skip for now\n",
		))

	case wizardSmoke:
		var content string
		if m.onboardLoading {
			content = "\n" +
				"  Testing connection...\n" +
				"\n" +
				"  " + styleDim.Render("Sending a test message to your AI provider.") + "\n"
		} else if m.onboardResult != "" {
			icon := styleOK.Render("✓ Pass")
			if !m.onboardSmokePass {
				icon = styleErr.Render("✗ Fail")
			}
			content = "\n" +
				"  Smoke test: " + icon + "\n" +
				"\n" +
				"  " + styleDim.Render("Press Enter to continue.") + "\n"
		} else {
			content = "\n" +
				"  Test your AI connection?\n" +
				"\n" +
				"  " + styleKey.Render("[Enter]") + " Run smoke test\n" +
				"  " + styleKey.Render("[Esc]") + "   Skip\n"
		}
		b.WriteString(m.wizardFrame(panelW, "Smoke Test (optional)", content))

	case wizardChannel:
		b.WriteString(m.wizardFrame(panelW, "Chat Channel",
			"\n"+
				"  Connect a messaging app?\n"+
				"\n"+
				"  "+styleKey.Render("[t]")+" Set up Telegram\n"+
				"  "+styleKey.Render("[d]")+" Set up Discord\n"+
				"  "+styleKey.Render("[s]")+" Skip for now\n",
		))

	case wizardService:
		var content string
		if m.onboardLoading {
			content = "\n" +
				"  Installing gateway service...\n" +
				"\n" +
				"  " + styleDim.Render("This enables the background agent.") + "\n"
		} else if m.onboardResult != "" {
			content = "\n" +
				"  " + styleOK.Render("✓") + " " + m.onboardResult + "\n" +
				"\n" +
				"  " + styleDim.Render("Press Enter to continue.") + "\n"
		} else {
			content = "\n" +
				"  Install the background gateway service?\n" +
				"\n" +
				"  This lets your agent run continuously and respond\n" +
				"  to messages even when the TUI is closed.\n" +
				"\n" +
				"  " + styleKey.Render("[Enter]") + " Install and start service\n" +
				"  " + styleKey.Render("[Esc]") + "   Skip for now\n"
		}
		b.WriteString(m.wizardFrame(panelW, "Gateway Service", content))

	case wizardDone:
		var content string
		content = "\n" +
			"  " + styleOK.Render("✓") + " " + styleBold.Render("Setup complete!") + "\n" +
			"\n"
		if snap != nil {
			content += renderInlineChecklist(snap)
		}
		content += "\n" +
			"  " + styleDim.Render("Press Enter to go to the Home tab.") + "\n"
		b.WriteString(m.wizardFrame(panelW, "All Set", content))
	}

	// Progress indicator
	total := 6
	step := m.onboardStep + 1
	if step > total {
		step = total
	}
	progress := fmt.Sprintf("  Step %d of %d", step, total)
	b.WriteString(styleDim.Render(progress) + "\n")

	return b.String()
}

func (m HomeModel) wizardFrame(w int, title, content string) string {
	panel := stylePanel.Width(w).Render(content)
	titleStyled := stylePanelTitle.Render("Setup: " + title)
	return placePanelTitle(panel, titleStyled) + "\n"
}

// renderInlineChecklist renders a compact checklist for the wizard done screen.
func renderInlineChecklist(snap *VMSnapshot) string {
	type checkItem struct {
		status string
		label  string
	}

	items := []checkItem{
		{boolStatus(snap.ConfigExists), "Configuration file"},
		{boolStatus(snap.WorkspaceExists), "Workspace folder"},
		{boolStatus(snap.AuthStoreExists), "Login credentials"},
		{providerCheckStatus(snap.OpenAI, snap.Anthropic), providerCheckLabel(snap.OpenAI, snap.Anthropic)},
		{channelCheckStatus(snap.Discord.Status, snap.Telegram.Status), channelCheckLabel(snap.Discord, snap.Telegram)},
		{boolStatus(snap.ServiceInstalled), "Gateway service installed"},
		{boolStatus(snap.ServiceRunning), "Gateway service running"},
	}

	var lines []string
	for _, item := range items {
		lines = append(lines, fmt.Sprintf("   %s %s", statusIcon(item.status), item.label))
	}
	return strings.Join(lines, "\n") + "\n"
}

// --- Normal View Helpers ---

func renderSystemInfoPanel(snap *VMSnapshot, w int) string {
	verStr := snap.AgentVersion
	if verStr == "" {
		verStr = "-"
	}

	osStr := runtime.GOOS + "/" + runtime.GOARCH

	backend := "systemd (user)"
	if runtime.GOOS == "darwin" {
		backend = "launchd (user)"
	}

	wsPath := snap.WorkspacePath
	if wsPath == "" {
		wsPath = styleDim.Render("not set")
	}

	content := fmt.Sprintf(
		"%s %s\n%s %s\n%s %s\n%s %s\n%s %s",
		styleLabel.Render("Mode:"), styleOK.Render("Local"),
		styleLabel.Render("System:"), styleValue.Render(osStr),
		styleLabel.Render("Workspace:"), wsPath,
		styleLabel.Render("Service:"), styleValue.Render(backend),
		styleLabel.Render("Agent:"), styleValue.Render(verStr),
	)

	panel := stylePanel.Width(w).Render(content)
	title := stylePanelTitle.Render("System")
	return placePanelTitle(panel, title)
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

	wsPath := snap.WorkspacePath
	if wsPath == "" {
		wsPath = styleDim.Render("not set")
	}

	content := fmt.Sprintf(
		"%s %s    %s %s\n%s %s    %s %s\n%s %s\n%s %s",
		styleLabel.Render("Status:"), stateStyle.Render(snap.State),
		styleLabel.Render("IP:"), styleValue.Render(ipStr),
		styleLabel.Render("CPU Load:"), styleValue.Render(loadStr),
		styleLabel.Render("Memory:"), styleValue.Render(memStr),
		styleLabel.Render("Workspace:"), wsPath,
		styleLabel.Render("Agent:"), styleValue.Render(verStr),
	)

	panel := stylePanel.Width(w).Render(content)
	title := stylePanelTitle.Render("Virtual Machine")
	return placePanelTitle(panel, title)
}

func renderChecklistPanel(snap *VMSnapshot, w int) string {
	type checkItem struct {
		status string
		label  string
	}

	items := []checkItem{
		{boolStatus(snap.ConfigExists), "Configuration file"},
		{boolStatus(snap.WorkspaceExists), "Workspace folder"},
		{boolStatus(snap.AuthStoreExists), "Login credentials"},
		{providerCheckStatus(snap.OpenAI, snap.Anthropic), providerCheckLabel(snap.OpenAI, snap.Anthropic)},
		{channelCheckStatus(snap.Discord.Status, snap.Telegram.Status), channelCheckLabel(snap.Discord, snap.Telegram)},
		{boolStatus(snap.ServiceInstalled), "Gateway service installed"},
		{boolStatus(snap.ServiceRunning), "Gateway service running"},
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
		return fmt.Sprintf("Channel (%s)", strings.Join(parts, ", "))
	}
	return "Channel (not configured)"
}
