package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// doctorCheckStatus mirrors the CLI's check status values.
type doctorCheckStatus string

const (
	dcOK   doctorCheckStatus = "ok"
	dcWarn doctorCheckStatus = "warn"
	dcErr  doctorCheckStatus = "error"
	dcSkip doctorCheckStatus = "skip"
)

type doctorCheck struct {
	Name    string            `json:"name"`
	Status  doctorCheckStatus `json:"status"`
	Message string            `json:"message,omitempty"`
	Data    map[string]string `json:"data,omitempty"`
}

type doctorReport struct {
	CLI       string        `json:"cli"`
	Version   string        `json:"version"`
	OS        string        `json:"os"`
	Arch      string        `json:"arch"`
	Timestamp string        `json:"timestamp"`
	Checks    []doctorCheck `json:"checks"`
}

type doctorDoneMsg struct {
	report *doctorReport
	err    error
}

// DoctorModel handles the Health Check tab.
type DoctorModel struct {
	exec     Executor
	report   *doctorReport
	running  bool
	errMsg   string
	hasRun   bool
	viewport viewport.Model
}

func NewDoctorModel(exec Executor) DoctorModel {
	vp := viewport.New(60, 20)
	vp.SetContent(styleDim.Render("  Press Enter or [r] to run health check."))
	return DoctorModel{exec: exec, viewport: vp}
}

func (m DoctorModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (DoctorModel, tea.Cmd) {
	switch msg.String() {
	case "r", "enter":
		if !m.running {
			m.running = true
			m.hasRun = true
			return m, runDoctorCmd(m.exec)
		}
	}

	// Forward to viewport for scrolling.
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

// AutoRun returns a command to auto-run doctor on first tab visit.
func (m *DoctorModel) AutoRun() tea.Cmd {
	if m.hasRun {
		return nil
	}
	m.running = true
	m.hasRun = true
	return runDoctorCmd(m.exec)
}

func (m *DoctorModel) HandleResult(msg doctorDoneMsg) {
	m.running = false
	if msg.err != nil {
		m.errMsg = msg.err.Error()
		m.report = nil
		m.viewport.SetContent(styleErr.Render(fmt.Sprintf("  Error running health check: %s", m.errMsg)))
		return
	}
	m.errMsg = ""
	m.report = msg.report
	m.viewport.SetContent(m.renderReport())
	m.viewport.GotoTop()
}

func (m *DoctorModel) HandleResize(width, height int) {
	w := width - 8
	if w < 40 {
		w = 40
	}
	h := height - 10
	if h < 5 {
		h = 5
	}
	m.viewport.Width = w
	m.viewport.Height = h
}

func (m DoctorModel) View(snap *VMSnapshot, width int) string {
	panelW := width - 4
	if panelW < 40 {
		panelW = 40
	}

	var b strings.Builder

	if m.running {
		b.WriteString(fmt.Sprintf("\n  Running health check... This may take up to a minute.\n"))
		b.WriteString(styleDim.Render("  Checking configuration, credentials, tools, and services.\n"))
	} else {
		b.WriteString(m.viewport.View())
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("  %s Run health check   ", styleKey.Render("[Enter]")))
	b.WriteString(styleHint.Render("Scroll with arrow keys"))

	return b.String()
}

func (m DoctorModel) renderReport() string {
	if m.report == nil {
		return ""
	}

	rep := m.report
	var b strings.Builder

	// Header
	b.WriteString(fmt.Sprintf(" %s %s\n", styleLabel.Render("Version:"), styleValue.Render(rep.Version)))
	b.WriteString(fmt.Sprintf(" %s %s/%s\n", styleLabel.Render("System:"), styleValue.Render(rep.OS), styleValue.Render(rep.Arch)))
	b.WriteString(fmt.Sprintf(" %s %s\n", styleLabel.Render("Checked:"), styleDim.Render(rep.Timestamp)))
	b.WriteString("\n")

	// Count by status for summary line.
	counts := map[doctorCheckStatus]int{}
	for _, c := range rep.Checks {
		counts[c.Status]++
	}
	summary := fmt.Sprintf(" %s %d   %s %d   %s %d   %s %d",
		styleOK.Render("✓"), counts[dcOK],
		styleWarn.Render("!"), counts[dcWarn],
		styleErr.Render("✗"), counts[dcErr],
		styleDim.Render("-"), counts[dcSkip],
	)
	b.WriteString(summary)
	b.WriteString("\n\n")

	// Group by severity.
	for _, group := range []struct {
		status doctorCheckStatus
		title  string
	}{
		{dcErr, "Errors"},
		{dcWarn, "Warnings"},
		{dcOK, "Passed"},
		{dcSkip, "Skipped"},
	} {
		var checks []doctorCheck
		for _, c := range rep.Checks {
			if c.Status == group.status {
				checks = append(checks, c)
			}
		}
		if len(checks) == 0 {
			continue
		}

		sort.Slice(checks, func(i, j int) bool { return checks[i].Name < checks[j].Name })

		titleStyle := styleBold
		switch group.status {
		case dcErr:
			titleStyle = styleErr
		case dcWarn:
			titleStyle = styleWarn
		case dcOK:
			titleStyle = styleOK
		case dcSkip:
			titleStyle = styleDim
		}
		b.WriteString(" " + titleStyle.Render(group.title) + "\n")

		for _, c := range checks {
			icon := doctorIcon(c.Status)
			if c.Message != "" {
				b.WriteString(fmt.Sprintf("  %s %s: %s\n", icon, c.Name, c.Message))
			} else {
				b.WriteString(fmt.Sprintf("  %s %s\n", icon, c.Name))
			}
			if len(c.Data) > 0 {
				keys := make([]string, 0, len(c.Data))
				for k := range c.Data {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					b.WriteString(fmt.Sprintf("      %s\n", styleDim.Render(k+"="+c.Data[k])))
				}
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

func doctorIcon(status doctorCheckStatus) string {
	switch status {
	case dcOK:
		return styleOK.Render("✓")
	case dcWarn:
		return styleWarn.Render("!")
	case dcErr:
		return styleErr.Render("✗")
	case dcSkip:
		return styleDim.Render("-")
	default:
		return styleDim.Render("•")
	}
}

func runDoctorCmd(exec Executor) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " " + shellEscape(exec.BinaryPath()) + " doctor --json 2>&1"
		out, err := exec.ExecShell(90*time.Second, cmd)
		if err != nil {
			return doctorDoneMsg{err: fmt.Errorf("command failed: %w", err)}
		}

		var rep doctorReport
		if err := json.Unmarshal([]byte(out), &rep); err != nil {
			// If JSON parsing fails, the output might be plain text.
			return doctorDoneMsg{err: fmt.Errorf("failed to parse results: %w\n\nRaw output:\n%s", err, out)}
		}
		return doctorDoneMsg{report: &rep}
	}
}
