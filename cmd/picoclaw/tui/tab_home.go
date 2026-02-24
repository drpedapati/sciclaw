package tui

import (
	"fmt"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
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
	exec           Executor
	selectedItem   int // 0 = suggested action
	anthropicMode  homeAuthMode
	anthropicInput textinput.Model

	// Onboard wizard state
	wizardChecked    bool   // whether the first snapshot was checked
	onboardActive    bool   // wizard overlay visible
	onboardStep      int    // current wizard step
	onboardLoading   bool   // async command in progress
	onboardResult    string // result text from last async op
	onboardSmokePass   bool   // smoke test passed
	onboardTesting     bool   // connection test running after auth
	onboardSmokeOutput string // AI response from connection test

	// Inline channel setup state (wizard channel step)
	chSetup   bool   // inline setup active
	chChannel string // "discord" or "telegram"
	chStep    int    // 0=token, 1=userID, 2=name, 3=confirm
	chToken   string
	chUserID  string
	chInput   textinput.Model
}

type homeAuthMode int

const (
	homeAuthNormal homeAuthMode = iota
	homeAuthAnthropicAPIKey
)

func NewHomeModel(exec Executor) HomeModel {
	ti := textinput.New()
	ti.CharLimit = 2048
	ti.Width = 54
	ti.EchoMode = textinput.EchoPassword

	chi := textinput.New()
	chi.CharLimit = 256
	chi.Width = 50

	return HomeModel{
		exec:           exec,
		anthropicInput: ti,
		anthropicMode:  homeAuthNormal,
		chInput:        chi,
	}
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
			m.onboardLoading = true
			m.onboardResult = ""
			return m, m.runSmokeTest()
		}
	}
	return m, nil
}

// --- Wizard Update ---

func (m HomeModel) updateWizard(msg tea.KeyMsg) (HomeModel, tea.Cmd) {
	key := msg.String()

	if m.anthropicMode == homeAuthAnthropicAPIKey {
		switch key {
		case "esc":
			m.anthropicMode = homeAuthNormal
			m.anthropicInput.Blur()
			m.anthropicInput.SetValue("")
			return m, nil
		case "enter":
			keyText := strings.TrimSpace(m.anthropicInput.Value())
			m.anthropicMode = homeAuthNormal
			m.anthropicInput.Blur()
			m.anthropicInput.SetValue("")
			if keyText == "" {
				return m, nil
			}
			if err := saveAPIKey(m.exec, "anthropic", keyText); err != nil {
				m.onboardResult = "Failed to save key: " + err.Error()
				return m, nil
			}
			// Key saved — immediately test the connection.
			m.onboardLoading = true
			m.onboardTesting = true
			m.onboardResult = ""
			return m, m.runSmokeTest()

		default:
			var cmd tea.Cmd
			m.anthropicInput, cmd = m.anthropicInput.Update(msg)
			return m, cmd
		}
	}

	switch m.onboardStep {
	case wizardWelcome:
		if key == "enter" {
			// Create config with defaults + workspace directory.
			return m, m.createDefaultConfig()
		}

	case wizardAuth:
		// Block input while connection test is running.
		if m.onboardTesting {
			return m, nil
		}
		// Handle connection test results.
		if !m.onboardLoading && (m.onboardResult == "connected" || strings.Contains(m.onboardResult, "Connection test")) {
			switch key {
			case "enter":
				if m.onboardSmokePass {
					m.onboardResult = ""
					m.onboardSmokeOutput = ""
					m.onboardStep = wizardChannel
					return m, nil
				}
				// Retry failed test.
				m.onboardLoading = true
				m.onboardTesting = true
				m.onboardResult = ""
				m.onboardSmokeOutput = ""
				return m, m.runSmokeTest()
			case "esc":
				m.onboardResult = ""
				m.onboardSmokeOutput = ""
				m.onboardStep = wizardChannel
			}
			return m, nil
		}
		switch key {
		case "enter":
			m.anthropicMode = homeAuthNormal
			m.onboardLoading = true
			m.onboardResult = ""
			c := m.exec.InteractiveProcess(m.exec.BinaryPath(), "auth", "login", "--provider", "openai")
			return m, tea.ExecProcess(c, onboardExecCallback(wizardAuth))
		case "a":
			m.onboardResult = ""
			m.anthropicMode = homeAuthAnthropicAPIKey
			m.anthropicInput.SetValue("")
			m.anthropicInput.Focus()
			return m, m.anthropicInput.Cursor.BlinkCmd()
		case "esc":
			m.anthropicMode = homeAuthNormal
			m.onboardStep = wizardChannel
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
		// Inline channel setup wizard.
		if m.chSetup {
			switch key {
			case "esc":
				m.chSetup = false
				m.chInput.Blur()
				m.chInput.EchoMode = textinput.EchoNormal
				return m, nil
			case "enter":
				return m.handleChSetupSubmit()
			default:
				if m.chStep < 3 {
					var cmd tea.Cmd
					m.chInput, cmd = m.chInput.Update(msg)
					return m, cmd
				}
			}
			return m, nil
		}
		switch key {
		case "t":
			return m.startChSetup("telegram")
		case "d":
			return m.startChSetup("discord")
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

func onboardExecCallback(step int) func(error) tea.Msg {
	return func(err error) tea.Msg {
		return onboardExecDoneMsg{step: step, err: err}
	}
}

// HandleExecDone processes async wizard command results.
// Returns an optional tea.Cmd to chain (e.g. run connection test after auth).
func (m *HomeModel) HandleExecDone(msg onboardExecDoneMsg) tea.Cmd {
	m.onboardLoading = false
	m.anthropicMode = homeAuthNormal
	m.onboardResult = ""

	switch msg.step {
	case wizardWelcome:
		// Config creation result.
		if msg.err != nil {
			m.onboardResult = "Failed to create config: " + msg.err.Error()
		} else {
			m.onboardStep = wizardAuth
		}
	case wizardAuth:
		if msg.err != nil {
			m.onboardResult = "Login was not completed. Retry or press Esc to skip."
			return nil
		}
		// Login succeeded — immediately test the connection.
		m.onboardLoading = true
		m.onboardTesting = true
		m.onboardResult = ""
		return m.runSmokeTest()
	case wizardSmoke:
		m.onboardTesting = false
		m.onboardSmokeOutput = strings.TrimSpace(msg.output)
		if msg.err != nil {
			m.onboardSmokePass = false
		} else {
			m.onboardSmokePass = true
		}
		// If auto-triggered from auth screen, show result inline (don't auto-advance).
		if m.onboardStep == wizardAuth {
			if m.onboardSmokePass {
				m.onboardResult = "connected"
			} else {
				m.onboardResult = "Connection test failed."
			}
			return nil
		}
		// Standalone test screen.
		if m.onboardSmokePass {
			m.onboardResult = "pass"
		} else {
			m.onboardResult = "fail"
		}
	case wizardChannel:
		m.chSetup = false
		m.chInput.Blur()
		m.chInput.EchoMode = textinput.EchoNormal
		if msg.err != nil {
			m.onboardResult = "Channel setup failed. Try again or press [s] to skip."
			return nil
		}
		m.onboardResult = ""
		m.onboardStep = wizardService
	case wizardService:
		if msg.err != nil {
			detail := strings.TrimSpace(msg.output)
			if detail == "" {
				detail = msg.err.Error()
			}
			// The CLI output may already contain "Service install failed:".
			if strings.Contains(strings.ToLower(detail), "service install failed") {
				m.onboardResult = "err:" + detail
			} else {
				m.onboardResult = "err:Service install failed: " + detail
			}
		} else {
			m.onboardResult = "Service installed and started."
		}
	}
	return nil
}

// --- Wizard Commands ---

func (m HomeModel) createDefaultConfig() tea.Cmd {
	exec := m.exec
	return func() tea.Msg {
		// Run the full onboard pipeline (idempotent, non-interactive).
		// This creates config.json, workspace dirs, TOOLS.md, IDENTITY.md,
		// baseline skills, and everything else the doctor check expects.
		cmd := "HOME=" + exec.HomePath() + " " + shellEscape(exec.BinaryPath()) + " onboard -y 2>&1"
		_, err := exec.ExecShell(30*time.Second, cmd)
		return onboardExecDoneMsg{step: wizardWelcome, err: err}
	}
}

func (m HomeModel) runSmokeTest() tea.Cmd {
	exec := m.exec
	return func() tea.Msg {
		modelFlag := ""
		if smokeModel, err := resolveSmokeTestModel(exec); err == nil && smokeModel != "" {
			modelFlag = " --model " + shellEscape(smokeModel)
		}
		cmd := "HOME=" + exec.HomePath() + " " + shellEscape(exec.BinaryPath()) + " agent -m 'Hello, are you there?'" + modelFlag + " 2>/dev/null"
		out, err := exec.ExecShell(30*time.Second, cmd)
		output := strings.TrimSpace(out)
		if output == "" && err != nil {
			output = err.Error()
		}
		return onboardExecDoneMsg{step: wizardSmoke, output: output, err: err}
	}
}

func resolveSmokeTestModel(exec Executor) (string, error) {
	cfg, err := readConfigMap(exec)
	if err != nil {
		return "", err
	}

	agents := mapValue(cfg, "agents")
	defaults := mapValue(agents, "defaults")
	provider := strings.ToLower(asString(defaults["provider"]))
	model := strings.ToLower(asString(defaults["model"]))

	anthropicConfigured := hasAnthropicCredentials(cfg)
	openAIConfigured := hasOpenAICredentials(cfg)

	if strings.Contains(model, "claude") || strings.Contains(model, "anthropic/") {
		return asString(defaults["model"]), nil
	}

	if provider == "anthropic" && anthropicConfigured {
		return anthropicDefaultModel, nil
	}

	if (strings.HasPrefix(model, "gpt") || strings.Contains(model, "openai/")) && !openAIConfigured && anthropicConfigured {
		return anthropicDefaultModel, nil
	}

	return "", nil
}

func hasOpenAICredentials(cfg map[string]interface{}) bool {
	providers := mapValue(cfg, "providers")
	openai := mapValue(providers, "openai")
	authMethod := strings.ToLower(asString(openai["auth_method"]))
	apiKey := strings.TrimSpace(asString(openai["api_key"]))
	return authMethod == "oauth" || authMethod == "token" || apiKey != ""
}

func hasAnthropicCredentials(cfg map[string]interface{}) bool {
	providers := mapValue(cfg, "providers")
	anthropic := mapValue(providers, "anthropic")
	authMethod := strings.ToLower(asString(anthropic["auth_method"]))
	apiKey := strings.TrimSpace(asString(anthropic["api_key"]))
	return authMethod == "oauth" || authMethod == "token" || apiKey != ""
}

func mapValue(m map[string]interface{}, key string) map[string]interface{} {
	v, ok := m[key]
	if !ok {
		return map[string]interface{}{}
	}
	casted, ok := v.(map[string]interface{})
	if !ok {
		return map[string]interface{}{}
	}
	return casted
}

func (m HomeModel) installService() tea.Cmd {
	exec := m.exec
	return func() tea.Msg {
		bin := shellEscape(exec.BinaryPath())
		cmd := "HOME=" + exec.HomePath() + " " + bin + " service install 2>&1 && HOME=" + exec.HomePath() + " " + bin + " service start 2>&1"
		out, err := exec.ExecShell(20*time.Second, cmd)
		return onboardExecDoneMsg{step: wizardService, output: strings.TrimSpace(out), err: err}
	}
}

// --- Inline channel setup (mirrors Channels tab pattern) ---

func (m HomeModel) startChSetup(channel string) (HomeModel, tea.Cmd) {
	m.chSetup = true
	m.chChannel = channel
	m.chStep = 0
	m.chToken = ""
	m.chUserID = ""
	m.chInput.SetValue("")
	m.chInput.Placeholder = "paste bot token here"
	m.chInput.CharLimit = 256
	m.chInput.EchoMode = textinput.EchoPassword
	m.chInput.Focus()
	m.onboardResult = ""
	return m, m.chInput.Cursor.BlinkCmd()
}

func (m HomeModel) handleChSetupSubmit() (HomeModel, tea.Cmd) {
	val := strings.TrimSpace(m.chInput.Value())

	switch m.chStep {
	case 0: // Token
		if val == "" {
			m.chSetup = false
			m.chInput.Blur()
			m.chInput.EchoMode = textinput.EchoNormal
			return m, nil
		}
		m.chToken = val
		m.chStep = 1
		m.chInput.SetValue("")
		m.chInput.Placeholder = "e.g. 123456789012345678"
		m.chInput.EchoMode = textinput.EchoNormal
		m.chInput.CharLimit = 64
		return m, nil

	case 1: // User ID
		if val == "" {
			m.chSetup = false
			m.chInput.Blur()
			return m, nil
		}
		m.chUserID = val
		m.chStep = 2
		m.chInput.SetValue("")
		m.chInput.Placeholder = "(optional display name)"
		return m, nil

	case 2: // Display name (optional)
		if m.chUserID != "" && val != "" {
			m.chUserID = FormatEntry(m.chUserID, val)
		}
		m.chStep = 3
		m.chInput.Blur()
		return m, nil

	case 3: // Confirm → save
		m.chSetup = false
		m.chInput.EchoMode = textinput.EchoNormal
		m.chInput.CharLimit = 256
		m.onboardLoading = true
		return m, m.wizardSaveChannel()
	}
	return m, nil
}

func (m HomeModel) wizardSaveChannel() tea.Cmd {
	exec := m.exec
	channel := m.chChannel
	token := m.chToken
	userEntry := m.chUserID
	return func() tea.Msg {
		if err := saveChannelSetupConfig(exec, channel, token, userEntry); err != nil {
			return onboardExecDoneMsg{step: wizardChannel, err: err}
		}
		return onboardExecDoneMsg{step: wizardChannel}
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
	b.WriteString(fmt.Sprintf("  %s Navigate to suggested step   %s\n",
		styleKey.Render("[Enter]"),
		renderTestConnectionHint(m),
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
		content := m.renderWizardAuthContent()
		b.WriteString(m.wizardFrame(panelW, "Authentication", content))

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
				"  Connection test: " + icon + "\n" +
				"\n" +
				"  " + styleDim.Render("Press Enter to continue.") + "\n"
		} else {
			content = "\n" +
				"  Test your AI connection?\n" +
				"\n" +
				"  " + styleKey.Render("[Enter]") + " Test connection\n" +
				"  " + styleKey.Render("[Esc]") + "   Skip\n"
		}
		b.WriteString(m.wizardFrame(panelW, "Test Connection (optional)", content))

	case wizardChannel:
		var content string
		if m.chSetup {
			content = m.renderChSetupView()
		} else {
			content = "\n"
			if m.onboardSmokePass {
				content += "  " + styleOK.Render("✓") + " AI provider connected!\n\n"
			}
			content +=
				"  Connect a messaging app?\n" +
				"\n" +
				"  " + styleKey.Render("[t]") + " Set up Telegram\n" +
				"  " + styleKey.Render("[d]") + " Set up Discord\n" +
				"  " + styleKey.Render("[s]") + " Skip for now\n"
			if m.onboardLoading {
				content += "\n  " + styleDim.Render("Saving channel settings...") + "\n"
			} else if m.onboardResult != "" {
				content += "\n  " + styleWarn.Render("! "+m.onboardResult) + "\n"
			}
		}
		b.WriteString(m.wizardFrame(panelW, "Chat Channel", content))

	case wizardService:
		var content string
		if m.onboardLoading {
			content = "\n" +
				"  Installing gateway service...\n" +
				"\n" +
				"  " + styleDim.Render("This enables the background agent.") + "\n"
		} else if m.onboardResult != "" {
			icon := styleOK.Render("✓")
			resultText := m.onboardResult
			if strings.HasPrefix(m.onboardResult, "err:") {
				icon = styleErr.Render("✗")
				resultText = m.onboardResult[4:]
			}
			content = "\n" +
				"  " + icon + " " + resultText + "\n" +
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

	// Progress indicator — auth and connection test are combined.
	displayStep := map[int]int{
		wizardWelcome: 1,
		wizardAuth:    2,
		wizardSmoke:   2,
		wizardChannel: 3,
		wizardService: 4,
		wizardDone:    5,
	}
	step := displayStep[m.onboardStep]
	if step == 0 {
		step = 1
	}
	progress := fmt.Sprintf("  Step %d of %d", step, 5)
	b.WriteString(styleDim.Render(progress) + "\n")

	return b.String()
}

func renderTestConnectionHint(m HomeModel) string {
	frames := []string{"◐", "◓", "◑", "◒"}
	if m.onboardLoading {
		frame := frames[int(time.Now().UnixMilli()/120)%len(frames)]
		return fmt.Sprintf("%s %s %s", styleKey.Render("[t]"), frame, styleDim.Render("Testing connection"))
	}
	if m.onboardResult == "" {
		return fmt.Sprintf("%s %s", styleKey.Render("[t]"), styleDim.Render("Test connection"))
	}
	if m.onboardSmokePass {
		return styleOK.Render("✅ [t]") + " " + styleDim.Render("Connection: alive")
	}
	return styleErr.Render("❌ [t]") + " " + styleDim.Render("Connection: fail")
}

func (m HomeModel) renderWizardAuthContent() string {
	// Connection test running — animated spinner.
	if m.onboardTesting {
		frames := []string{"◐", "◓", "◑", "◒"}
		frame := frames[int(time.Now().UnixMilli()/200)%len(frames)]
		return "\n" +
			"  " + styleOK.Render("✓") + " Configuration file created.\n" +
			"\n" +
			"  " + styleWarn.Render(frame) + " " + styleBold.Render("Testing connection...") + "\n" +
			"\n" +
			"  " + styleDim.Render("Sending a quick hello to your AI provider.") + "\n"
	}

	// Connection test passed — show the AI response.
	if m.onboardResult == "connected" {
		s := "\n" +
			"  " + styleOK.Render("✓") + " Configuration file created.\n" +
			"  " + styleOK.Render("✓") + " " + styleBold.Render("Connected!") + "\n"
		if preview := responsePreview(m.onboardSmokeOutput); preview != "" {
			s += "\n" +
				"  " + styleDim.Render("Your AI responded:") + "\n" +
				"  " + styleDim.Render("│") + " " + preview + "\n"
		}
		s += "\n" +
			"  " + styleDim.Render("Press Enter to continue.") + "\n"
		return s
	}

	// Connection test failed — show error with retry.
	if strings.Contains(m.onboardResult, "Connection test") {
		s := "\n" +
			"  " + styleErr.Render("✗") + " " + styleBold.Render("Connection test failed.") + "\n"
		if preview := responsePreview(m.onboardSmokeOutput); preview != "" {
			s += "\n  " + styleDim.Render(preview) + "\n"
		}
		s += "\n" +
			"  " + styleKey.Render("[Enter]") + " Retry test\n" +
			"  " + styleKey.Render("[Esc]") + "   Skip to next step\n"
		return s
	}

	// Anthropic API key entry.
	if m.anthropicMode == homeAuthAnthropicAPIKey {
		return "\n" +
			"  " + styleOK.Render("✓") + " Configuration file created.\n" +
			"\n" +
			"  Enter your Anthropic API key.\n" +
			"  " + styleDim.Render("Get one at console.anthropic.com, or run:") + "\n" +
			"  " + styleDim.Render("  claude setup-token") + "\n" +
			"\n" +
			"  " + styleBold.Render("API key:") + " " + m.anthropicInput.View() + "\n" +
			"  " + styleDim.Render("Enter to save, Esc to cancel") + "\n"
	}

	// Waiting for interactive login to return.
	if m.onboardLoading {
		return "\n" +
			"  " + styleDim.Render("Waiting for login flow to complete...") + "\n"
	}

	// Normal provider selection.
	s := "\n" +
		"  " + styleOK.Render("✓") + " Configuration file created.\n" +
		"\n" +
		"  Choose your AI provider:\n" +
		"\n" +
		"  " + styleKey.Render("[Enter]") + " Log in with OpenAI (recommended)\n" +
		"  " + styleKey.Render("[a]") + "     Use Anthropic API key\n" +
		"  " + styleKey.Render("[Esc]") + "   Skip for now\n"
	if m.onboardResult != "" {
		s += "\n  " + styleWarn.Render("! "+m.onboardResult) + "\n"
	}
	return s
}

// responsePreview returns a trimmed, single-line preview of AI output.
func responsePreview(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	// Take first non-empty line.
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			if len(line) > 100 {
				line = line[:97] + "..."
			}
			return line
		}
	}
	return ""
}

func (m HomeModel) renderChSetupView() string {
	name := "Discord"
	if m.chChannel == "telegram" {
		name = "Telegram"
	}

	var lines []string
	lines = append(lines, "")
	if m.onboardSmokePass {
		lines = append(lines, "  "+styleOK.Render("✓")+" AI provider connected!")
	}
	lines = append(lines, "  "+styleBold.Render("Set up "+name))
	lines = append(lines, "")

	switch m.chStep {
	case 0:
		lines = append(lines, fmt.Sprintf("  Paste your %s bot token: %s", name, m.chInput.View()))
		if name == "Discord" {
			lines = append(lines, styleHint.Render("    Get this from Discord Developer Portal → Bot → Token"))
		} else {
			lines = append(lines, styleHint.Render("    Get this from @BotFather on Telegram"))
		}
		lines = append(lines, "")
		lines = append(lines, styleDim.Render("    Esc to cancel"))
	case 1:
		lines = append(lines, "  "+styleOK.Render("✓")+" Bot token saved.")
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  Enter your %s User ID: %s", name, m.chInput.View()))
		if name == "Discord" {
			lines = append(lines, styleHint.Render("    Discord Settings → Advanced → Developer Mode"))
			lines = append(lines, styleHint.Render("    → Right-click your avatar → Copy User ID"))
		} else {
			lines = append(lines, styleHint.Render("    Message @userinfobot on Telegram to get your ID"))
		}
		lines = append(lines, "")
		lines = append(lines, styleDim.Render("    Esc to cancel"))
	case 2:
		lines = append(lines, "  "+styleOK.Render("✓")+" Bot token saved.")
		lines = append(lines, "  "+styleOK.Render("✓")+" User ID: "+m.chUserID)
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  Add a display name (optional): %s", m.chInput.View()))
		lines = append(lines, styleHint.Render("    Press Enter to skip"))
	case 3:
		lines = append(lines, fmt.Sprintf("  %s  Enabled: %s", styleDim.Render("Review:"), styleOK.Render("true")))
		lines = append(lines, fmt.Sprintf("           Token: %s", styleOK.Render("set")))
		if m.chUserID != "" {
			lines = append(lines, fmt.Sprintf("           User:  %s", styleValue.Render(m.chUserID)))
		}
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  Press %s to save, %s to cancel",
			styleKey.Render("Enter"), styleKey.Render("Esc")))
	}

	return strings.Join(lines, "\n") + "\n"
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
		"%s %s    %s %s\n%s %s\n%s %s    %s %s",
		styleLabel.Render("Mode:"), styleOK.Render("Local"),
		styleLabel.Render("System:"), styleValue.Render(osStr),
		styleLabel.Render("Agent:"), styleValue.Render(verStr),
		styleLabel.Render("Workspace:"), wsPath,
		styleLabel.Render("Service:"), styleValue.Render(backend),
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
