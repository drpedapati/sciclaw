package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type cronMode int

const (
	cronNormal cronMode = iota
	cronConfirmRemove
	cronAddTask
)

type cronListMsg struct{ output string }

type cronRow struct {
	Name     string
	ID       string
	Schedule string
	Status   string
	NextRun  string
}

// CronModel handles the Schedule tab.
type CronModel struct {
	exec        Executor
	mode        cronMode
	loaded      bool
	selectedRow int
	jobs        []cronRow

	// Remove confirmation
	removeJob cronRow

	// Add task input
	addInput textinput.Model
}

func NewCronModel(exec Executor) CronModel {
	ti := textinput.New()
	ti.CharLimit = 200
	ti.Width = 60
	ti.Placeholder = `e.g. "Check PubMed for ALS papers every morning at 9am"`
	return CronModel{exec: exec, addInput: ti}
}

func (m *CronModel) AutoRun() tea.Cmd {
	if !m.loaded {
		return fetchCronList(m.exec)
	}
	return nil
}

func (m *CronModel) HandleList(msg cronListMsg) {
	m.loaded = true
	m.jobs = parseCronList(msg.output)
}

func parseCronList(output string) []cronRow {
	var jobs []cronRow
	lines := strings.Split(output, "\n")

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" || strings.HasPrefix(line, "Scheduled") || strings.HasPrefix(line, "-") {
			continue
		}

		// Job header: "Name (job_id)"
		pIdx := strings.LastIndex(line, "(")
		if pIdx < 0 || !strings.HasSuffix(line, ")") {
			continue
		}

		name := strings.TrimSpace(line[:pIdx])
		id := strings.Trim(line[pIdx:], "()")

		job := cronRow{Name: name, ID: id}

		// Parse indented detail lines.
		for i+1 < len(lines) {
			next := strings.TrimSpace(lines[i+1])
			if next == "" {
				break
			}
			parts := strings.SplitN(next, ":", 2)
			if len(parts) != 2 {
				break
			}
			key := strings.TrimSpace(parts[0])
			val := strings.TrimSpace(parts[1])
			switch key {
			case "Schedule":
				job.Schedule = val
			case "Status":
				job.Status = val
			case "Next run":
				job.NextRun = val
			}
			i++
		}

		jobs = append(jobs, job)
	}
	return jobs
}

func (m CronModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (CronModel, tea.Cmd) {
	key := msg.String()

	if m.mode == cronAddTask {
		switch key {
		case "esc":
			m.mode = cronNormal
			m.addInput.Blur()
			return m, nil
		case "enter":
			val := strings.TrimSpace(m.addInput.Value())
			if val == "" {
				m.mode = cronNormal
				m.addInput.Blur()
				return m, nil
			}
			m.mode = cronNormal
			m.addInput.Blur()
			return m, addCronNatural(m.exec, val)
		}
		var cmd tea.Cmd
		m.addInput, cmd = m.addInput.Update(msg)
		return m, cmd
	}

	if m.mode == cronConfirmRemove {
		switch key {
		case "y", "Y":
			m.mode = cronNormal
			return m, removeCronJob(m.exec, m.removeJob.ID)
		case "n", "N", "esc":
			m.mode = cronNormal
		}
		return m, nil
	}

	// Normal mode.
	switch key {
	case "up", "k":
		if m.selectedRow > 0 {
			m.selectedRow--
		}
	case "down", "j":
		if m.selectedRow < len(m.jobs)-1 {
			m.selectedRow++
		}
	case "e":
		if m.selectedRow < len(m.jobs) {
			job := m.jobs[m.selectedRow]
			if job.Status == "enabled" {
				return m, cronToggle(m.exec, job.ID, "disable")
			}
			return m, cronToggle(m.exec, job.ID, "enable")
		}
	case "d", "backspace", "delete":
		if m.selectedRow < len(m.jobs) {
			m.removeJob = m.jobs[m.selectedRow]
			m.mode = cronConfirmRemove
		}
	case "a":
		m.mode = cronAddTask
		m.addInput.SetValue("")
		m.addInput.Focus()
		return m, nil
	case "l":
		m.loaded = false
		return m, fetchCronList(m.exec)
	}
	return m, nil
}

func (m CronModel) View(snap *VMSnapshot, width int) string {
	panelW := width - 4
	if panelW > 100 {
		panelW = 100
	}
	if panelW < 40 {
		panelW = 40
	}

	if !m.loaded {
		return "\n  Loading scheduled jobs...\n"
	}

	var lines []string

	if len(m.jobs) == 0 {
		lines = append(lines, "")
		lines = append(lines, "  No scheduled jobs.")
		lines = append(lines, "")
		lines = append(lines, "  No scheduled tasks yet.")
		lines = append(lines, "")
		lines = append(lines, styleDim.Render("  Scheduled tasks run your agent automatically on a timer."))
		lines = append(lines, styleDim.Render("  Describe what you want in plain English."))
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %s Add a task   %s Refresh",
			styleKey.Render("[a]"),
			styleKey.Render("[l]"),
		))
	} else {
		// Table header.
		lines = append(lines, fmt.Sprintf("  %-20s  %-22s  %-10s  %s",
			styleDim.Render("Name"),
			styleDim.Render("Schedule"),
			styleDim.Render("Status"),
			styleDim.Render("Next Run"),
		))
		lines = append(lines, styleDim.Render("  "+strings.Repeat("─", 65)))

		for i, job := range m.jobs {
			line := fmt.Sprintf("  %-20s  %-22s  %-10s  %s",
				job.Name, job.Schedule, job.Status, job.NextRun)
			if i == m.selectedRow && m.mode == cronNormal {
				line = lipgloss.NewStyle().
					Background(lipgloss.Color("#2A2A4A")).
					Bold(true).
					Render(line)
			}
			lines = append(lines, line)
		}

		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %s Add a task   %s Toggle   %s Remove   %s Refresh",
			styleKey.Render("[a]"),
			styleKey.Render("[e]"),
			styleKey.Render("[d]"),
			styleKey.Render("[l]"),
		))
	}

	// Add task overlay.
	if m.mode == cronAddTask {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %s", styleBold.Render("What should the agent do on a schedule?")))
		lines = append(lines, fmt.Sprintf("  %s", m.addInput.View()))
		lines = append(lines, styleHint.Render("    Describe the task and how often, e.g. \"Summarize lab notes every Friday at 5pm\""))
		lines = append(lines, styleDim.Render("    Enter to create, Esc to cancel"))
	}

	// Remove confirmation overlay.
	if m.mode == cronConfirmRemove {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  Remove job %s? %s / %s",
			styleBold.Render(m.removeJob.Name),
			styleKey.Render("[y]es"),
			styleKey.Render("[n]o"),
		))
	}

	content := strings.Join(lines, "\n")
	panel := stylePanel.Width(panelW).Render(content)
	title := stylePanelTitle.Render("Scheduled Tasks")
	return placePanelTitle(panel, title)
}

func fetchCronList(exec Executor) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw cron list 2>&1"
		out, _ := exec.ExecShell(10*time.Second, cmd)
		return cronListMsg{output: out}
	}
}

func cronToggle(exec Executor, jobID, action string) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw cron " + action + " " + shellEscape(jobID) + " 2>&1"
		_, _ = exec.ExecShell(10*time.Second, cmd)
		return actionDoneMsg{output: "Job " + action + "d"}
	}
}

func addCronNatural(exec Executor, description string) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw cron add-natural " + shellEscape(description) + " 2>&1"
		out, err := exec.ExecShell(30*time.Second, cmd)
		if err != nil {
			return actionDoneMsg{output: "Failed to add task: " + strings.TrimSpace(out)}
		}
		return actionDoneMsg{output: "Task created: " + strings.TrimSpace(out)}
	}
}

func removeCronJob(exec Executor, jobID string) tea.Cmd {
	return func() tea.Msg {
		cmd := "HOME=" + exec.HomePath() + " sciclaw cron remove " + shellEscape(jobID) + " 2>&1"
		_, _ = exec.ExecShell(10*time.Second, cmd)
		return actionDoneMsg{output: "Job removed"}
	}
}
