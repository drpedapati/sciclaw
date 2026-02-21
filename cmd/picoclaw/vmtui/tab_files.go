package vmtui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// FilesModel handles the Your Files tab.
type FilesModel struct {
	projectDir string
	lastSync   string
	message    string
}

func NewFilesModel() FilesModel {
	dir := resolveDefaultProject()
	return FilesModel{projectDir: dir}
}

func (m FilesModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (FilesModel, tea.Cmd) {
	switch msg.String() {
	case "p":
		// Push: suspend TUI, run the bash vm push.
		return m, m.runVMScript("push", m.projectDir)
	case "g":
		// Pull: suspend TUI, run the bash vm pull.
		return m, m.runVMScript("pull", m.projectDir)
	case "h":
		// Shell
		c := exec.Command("multipass", "exec", vmName, "--", "bash", "--login", "-c", "cd /home/ubuntu/project && exec bash")
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return actionDoneMsg{output: "Shell session ended."}
		})
	case "o":
		// Onboard
		c := exec.Command("multipass", "exec", vmName, "--", "env", "HOME=/home/ubuntu", "sciclaw", "onboard")
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return actionDoneMsg{output: "Onboard completed."}
		})
	case "w":
		// Guided setup wizard — reuse the bash TUI's guided setup via passthrough.
		c := exec.Command("multipass", "exec", vmName, "--", "env", "HOME=/home/ubuntu", "sciclaw", "onboard")
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return actionDoneMsg{output: "Guided setup completed."}
		})
	case "x":
		// Stop VM
		return m, stopVM()
	}
	return m, nil
}

func (m FilesModel) View(snap *VMSnapshot, width int) string {
	panelW := width - 4
	if panelW < 40 {
		panelW = 40
	}

	var b strings.Builder

	// Project sync panel
	b.WriteString(m.renderSyncPanel(snap, panelW))
	b.WriteString("\n")

	// Other actions panel
	b.WriteString(m.renderActionsPanel(panelW))

	return b.String()
}

func (m FilesModel) renderSyncPanel(snap *VMSnapshot, w int) string {
	localDir := m.projectDir
	if localDir == "" {
		localDir = "(not set)"
	}

	var lines []string
	lines = append(lines, fmt.Sprintf(" %s %s", styleLabel.Render("Local folder:"), styleValue.Render(localDir)))
	lines = append(lines, fmt.Sprintf(" %s %s", styleLabel.Render("VM folder:"), styleValue.Render("/home/ubuntu/project")))

	if m.lastSync != "" {
		lines = append(lines, fmt.Sprintf(" %s %s", styleLabel.Render("Last sync:"), styleDim.Render(m.lastSync)))
	}

	lines = append(lines, "")
	lines = append(lines, fmt.Sprintf("  %s Send files to VM (push)", styleKey.Render("[p]")))
	lines = append(lines, fmt.Sprintf("  %s Get files from VM (pull)", styleKey.Render("[g]")))

	content := strings.Join(lines, "\n")
	panel := stylePanel.Width(w).Render(content)
	title := stylePanelTitle.Render("Project Sync")
	return placePanelTitle(panel, title)
}

func (m FilesModel) renderActionsPanel(w int) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("  %s Open VM terminal (shell)", styleKey.Render("[h]")))
	lines = append(lines, fmt.Sprintf("  %s Run initial setup (onboard)", styleKey.Render("[o]")))
	lines = append(lines, fmt.Sprintf("  %s Guided setup wizard", styleKey.Render("[w]")))
	lines = append(lines, "")
	lines = append(lines, styleDim.Render("  VM Management:"))
	lines = append(lines, fmt.Sprintf("  %s Stop VM", styleKey.Render("[x]")))

	content := strings.Join(lines, "\n")
	panel := stylePanel.Width(w).Render(content)
	title := stylePanelTitle.Render("Other Actions")
	return placePanelTitle(panel, title)
}

func (m FilesModel) runVMScript(action, projectDir string) tea.Cmd {
	// Find the deploy/vm script to run push/pull.
	scriptPath := resolveVMScript()
	if scriptPath == "" {
		return func() tea.Msg {
			return actionDoneMsg{output: "Could not find deploy/vm script."}
		}
	}
	c := exec.Command("bash", scriptPath, action, projectDir)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return tea.ExecProcess(c, func(err error) tea.Msg {
		msg := action + " completed."
		if err != nil {
			msg = fmt.Sprintf("%s failed: %v", action, err)
		}
		return actionDoneMsg{output: msg}
	})
}

func resolveDefaultProject() string {
	// Check saved state file.
	home, _ := os.UserHomeDir()
	stateFile := filepath.Join(home, ".cache", "sciclaw", "vm-project-path")
	if data, err := os.ReadFile(stateFile); err == nil {
		if dir := strings.TrimSpace(string(data)); dir != "" {
			return dir
		}
	}
	// Fall back to cwd.
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

func resolveVMScript() string {
	// Try local repo first.
	if wd, err := os.Getwd(); err == nil {
		p := filepath.Join(wd, "deploy", "vm")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	// Try next to the executable.
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		for _, rel := range []string{
			filepath.Join(dir, "..", "share", "sciclaw", "deploy", "vm"),
			filepath.Join(dir, "..", "share", "picoclaw", "deploy", "vm"),
		} {
			p := filepath.Clean(rel)
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

func stopVM() tea.Cmd {
	return func() tea.Msg {
		c := exec.Command("multipass", "stop", vmName)
		out, err := c.CombinedOutput()
		msg := "VM stopped."
		if err != nil {
			msg = fmt.Sprintf("Stop failed: %s", strings.TrimSpace(string(out)))
		}
		return actionDoneMsg{output: msg}
	}
}
