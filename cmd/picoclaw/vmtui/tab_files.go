package vmtui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type filesMode int

const (
	filesNormal       filesMode = iota
	filesAddMount               // text input wizard for new mount
	filesConfirmRemove          // confirm removal of selected mount
)

// FilesModel handles the Your Files tab.
type FilesModel struct {
	projectDir  string
	lastSync    string
	message     string
	mode        filesMode
	selectedRow int
	addInput    textinput.Model
	addStep     int    // 0=host path, 1=VM path
	pendingHost string // host path collected in step 0
	removeMount MountInfo
}

func NewFilesModel() FilesModel {
	dir := resolveDefaultProject()
	ti := textinput.New()
	ti.CharLimit = 256
	ti.Width = 50
	return FilesModel{projectDir: dir, addInput: ti}
}

func (m FilesModel) Update(msg tea.KeyMsg, snap *VMSnapshot) (FilesModel, tea.Cmd) {
	key := msg.String()
	mounted := projectIsMounted(snap)

	// Add-mount wizard mode.
	if m.mode == filesAddMount {
		switch key {
		case "esc":
			m.mode = filesNormal
			m.addInput.Blur()
			return m, nil
		case "enter":
			return m.handleAddSubmit(snap)
		}
		var cmd tea.Cmd
		m.addInput, cmd = m.addInput.Update(msg)
		return m, cmd
	}

	// Remove confirmation mode.
	if m.mode == filesConfirmRemove {
		switch key {
		case "y", "Y":
			m.mode = filesNormal
			return m, unmountCmd(m.removeMount.VMPath)
		case "n", "N", "esc":
			m.mode = filesNormal
		}
		return m, nil
	}

	// Normal mode.
	mounts := snapshotMounts(snap)
	switch key {
	case "up", "k":
		if m.selectedRow > 0 {
			m.selectedRow--
		}
	case "down", "j":
		if m.selectedRow < len(mounts)-1 {
			m.selectedRow++
		}
	case "a":
		m.mode = filesAddMount
		m.addStep = 0
		m.pendingHost = ""
		m.addInput.SetValue("")
		m.addInput.Placeholder = m.projectDir
		m.addInput.Focus()
		return m, nil
	case "d", "backspace", "delete":
		if m.selectedRow < len(mounts) {
			m.removeMount = mounts[m.selectedRow]
			m.mode = filesConfirmRemove
		}
		return m, nil
	case "p":
		if mounted {
			return m, nil
		}
		return m, m.runVMScript("push", m.projectDir)
	case "g":
		if mounted {
			return m, nil
		}
		return m, m.runVMScript("pull", m.projectDir)
	case "h":
		c := exec.Command("multipass", "exec", vmName, "--", "bash", "--login", "-c", "cd /home/ubuntu/project && exec bash")
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return actionDoneMsg{output: "Shell session ended."}
		})
	case "o":
		c := exec.Command("multipass", "exec", vmName, "--", "env", "HOME=/home/ubuntu", "sciclaw", "onboard")
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return actionDoneMsg{output: "Onboard completed."}
		})
	case "w":
		c := exec.Command("multipass", "exec", vmName, "--", "env", "HOME=/home/ubuntu", "sciclaw", "onboard")
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			return actionDoneMsg{output: "Guided setup completed."}
		})
	case "x":
		return m, stopVM()
	}
	return m, nil
}

func (m FilesModel) handleAddSubmit(snap *VMSnapshot) (FilesModel, tea.Cmd) {
	val := strings.TrimSpace(m.addInput.Value())

	if m.addStep == 0 {
		// Host path submitted.
		if val == "" {
			val = m.projectDir // use default
		}
		m.pendingHost = val
		m.addStep = 1
		m.addInput.SetValue("")
		base := filepath.Base(val)
		m.addInput.Placeholder = "/home/ubuntu/" + base
		return m, nil
	}

	// Step 1: VM path submitted.
	if val == "" {
		val = "/home/ubuntu/" + filepath.Base(m.pendingHost)
	}
	m.mode = filesNormal
	m.addInput.Blur()
	return m, mountCmd(m.pendingHost, val)
}

func (m FilesModel) View(snap *VMSnapshot, width int) string {
	panelW := width - 4
	if panelW < 40 {
		panelW = 40
	}

	var b strings.Builder

	// Mounts panel
	b.WriteString(m.renderMountsPanel(snap, panelW))
	b.WriteString("\n")

	// Project sync panel
	b.WriteString(m.renderSyncPanel(snap, panelW))
	b.WriteString("\n")

	// Other actions panel
	b.WriteString(m.renderActionsPanel(panelW))

	return b.String()
}

func (m FilesModel) renderMountsPanel(snap *VMSnapshot, w int) string {
	mounts := snapshotMounts(snap)
	var lines []string

	if len(mounts) == 0 {
		lines = append(lines, "")
		lines = append(lines, "  No live mounts active.")
		lines = append(lines, "")
		lines = append(lines, styleDim.Render("  Live mounts share a folder between your Mac and the VM."))
		lines = append(lines, styleDim.Render("  Changes appear instantly on both sides — no push/pull needed."))
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %s Add a mount", styleKey.Render("[a]")))
	} else {
		// Table header.
		lines = append(lines, fmt.Sprintf("  %s  %-35s  %s",
			styleDim.Render(" # "),
			styleDim.Render("Host Path"),
			styleDim.Render("VM Path"),
		))
		lines = append(lines, styleDim.Render("  "+strings.Repeat("─", 65)))

		for i, mt := range mounts {
			host := truncatePath(mt.HostPath, 33)
			vm := truncatePath(mt.VMPath, 30)
			num := fmt.Sprintf(" %d ", i+1)

			line := fmt.Sprintf("  %s  %-35s  %s", num, host, vm)
			if i == m.selectedRow && m.mode == filesNormal {
				line = lipgloss.NewStyle().
					Background(lipgloss.Color("#2A2A4A")).
					Bold(true).
					Render(line)
			}
			lines = append(lines, line)
		}

		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  %s Add mount   %s Remove selected",
			styleKey.Render("[a]"),
			styleKey.Render("[d]"),
		))
	}

	// Overlay for add/remove modes.
	if m.mode == filesAddMount {
		lines = append(lines, "")
		lines = append(lines, m.renderAddOverlay())
	}
	if m.mode == filesConfirmRemove {
		lines = append(lines, "")
		lines = append(lines, fmt.Sprintf("  Remove mount %s? %s / %s",
			styleBold.Render(m.removeMount.VMPath),
			styleKey.Render("[y]es"),
			styleKey.Render("[n]o"),
		))
	}

	content := strings.Join(lines, "\n")
	panel := stylePanel.Width(w).Render(content)
	title := stylePanelTitle.Render("Live Mounts")
	return placePanelTitle(panel, title)
}

func (m FilesModel) renderAddOverlay() string {
	var lines []string

	switch m.addStep {
	case 0:
		lines = append(lines, fmt.Sprintf("  Host path: %s", m.addInput.View()))
		lines = append(lines, styleHint.Render("    Enter to use default, or type a path"))
		lines = append(lines, styleDim.Render("    Esc to cancel"))
	case 1:
		lines = append(lines, styleDim.Render(fmt.Sprintf("  Host: %s", m.pendingHost)))
		lines = append(lines, fmt.Sprintf("  VM path: %s", m.addInput.View()))
		lines = append(lines, styleHint.Render("    Enter to use default, or type a path"))
		lines = append(lines, styleDim.Render("    Esc to cancel"))
	}

	return strings.Join(lines, "\n")
}

func (m FilesModel) renderSyncPanel(snap *VMSnapshot, w int) string {
	localDir := m.projectDir
	if localDir == "" {
		localDir = "(not set)"
	}

	mounted := projectIsMounted(snap)

	var lines []string
	lines = append(lines, fmt.Sprintf(" %s %s", styleLabel.Render("Local folder:"), styleValue.Render(localDir)))
	lines = append(lines, fmt.Sprintf(" %s %s", styleLabel.Render("VM folder:"), styleValue.Render("/home/ubuntu/project")))

	if mounted {
		lines = append(lines, fmt.Sprintf(" %s %s",
			styleLabel.Render("Sync mode:"),
			styleOK.Render("Live mount active")))
		lines = append(lines, styleDim.Render("  File changes are reflected immediately — no push/pull needed."))
	} else {
		lines = append(lines, fmt.Sprintf(" %s %s",
			styleLabel.Render("Sync mode:"),
			styleValue.Render("Manual sync (push/pull)")))
	}

	if m.lastSync != "" {
		lines = append(lines, fmt.Sprintf(" %s %s", styleLabel.Render("Last sync:"), styleDim.Render(m.lastSync)))
	}

	lines = append(lines, "")

	if mounted {
		lines = append(lines, styleDim.Render("  Push/pull disabled while project is mounted."))
	} else {
		lines = append(lines, fmt.Sprintf("  %s Send files to VM (push)", styleKey.Render("[p]")))
		lines = append(lines, fmt.Sprintf("  %s Get files from VM (pull)", styleKey.Render("[g]")))
	}

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
	scriptPath := resolveVMScript()
	if scriptPath == "" {
		return func() tea.Msg {
			return actionDoneMsg{output: "Could not find deploy/vm script."}
		}
	}
	var c *exec.Cmd
	if projectDir != "" {
		c = exec.Command("bash", scriptPath, action, projectDir)
	} else {
		c = exec.Command("bash", scriptPath, action)
	}
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

func mountCmd(hostPath, vmPath string) tea.Cmd {
	return func() tea.Msg {
		c := exec.Command("multipass", "mount", "-t", "classic", hostPath, vmName+":"+vmPath)
		out, err := c.CombinedOutput()
		msg := "Mount added."
		if err != nil {
			msg = fmt.Sprintf("Mount failed: %s", strings.TrimSpace(string(out)))
		}
		return actionDoneMsg{output: msg}
	}
}

func unmountCmd(vmPath string) tea.Cmd {
	return func() tea.Msg {
		c := exec.Command("multipass", "umount", vmName+":"+vmPath)
		out, err := c.CombinedOutput()
		msg := "Mount removed."
		if err != nil {
			msg = fmt.Sprintf("Umount failed: %s", strings.TrimSpace(string(out)))
		}
		return actionDoneMsg{output: msg}
	}
}

func projectIsMounted(snap *VMSnapshot) bool {
	if snap == nil {
		return false
	}
	for _, m := range snap.Mounts {
		if m.VMPath == "/home/ubuntu/project" {
			return true
		}
	}
	return false
}

func snapshotMounts(snap *VMSnapshot) []MountInfo {
	if snap == nil {
		return nil
	}
	return snap.Mounts
}

func truncatePath(p string, maxLen int) string {
	if len(p) <= maxLen {
		return p
	}
	return "..." + p[len(p)-maxLen+3:]
}

func resolveDefaultProject() string {
	home, _ := os.UserHomeDir()
	stateFile := filepath.Join(home, ".cache", "sciclaw", "vm-project-path")
	if data, err := os.ReadFile(stateFile); err == nil {
		if dir := strings.TrimSpace(string(data)); dir != "" {
			return dir
		}
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

func resolveVMScript() string {
	if wd, err := os.Getwd(); err == nil {
		p := filepath.Join(wd, "deploy", "vm")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
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
