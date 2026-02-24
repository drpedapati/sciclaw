package tui

import (
	"os"
	"os/exec"
	"strings"
	"time"
)

// VMExecutor runs commands inside the multipass VM.
type VMExecutor struct{}

func (e *VMExecutor) Mode() Mode { return ModeVM }

func (e *VMExecutor) ExecShell(timeout time.Duration, shellCmd string) (string, error) {
	return VMExecShell(timeout, shellCmd)
}

func (e *VMExecutor) ExecCommand(timeout time.Duration, args ...string) (string, error) {
	return VMExec(timeout, args...)
}

func (e *VMExecutor) ReadFile(path string) (string, error) {
	return VMCatFile(path)
}

func (e *VMExecutor) WriteFile(path string, data []byte, perm os.FileMode) error {
	// Write via stdin pipe to avoid shell escaping issues.
	escaped := shellEscape(string(data))
	cmd := "printf '%s' " + escaped + " > " + shellEscape(path)
	_, err := VMExecShell(10*time.Second, cmd)
	return err
}

func (e *VMExecutor) ConfigPath() string { return "/home/ubuntu/.picoclaw/config.json" }
func (e *VMExecutor) AuthPath() string   { return "/home/ubuntu/.picoclaw/auth.json" }
func (e *VMExecutor) HomePath() string   { return "/home/ubuntu" }
func (e *VMExecutor) BinaryPath() string { return "sciclaw" } // VM always uses PATH

func (e *VMExecutor) AgentVersion() string {
	return VMAgentVersion()
}

func (e *VMExecutor) ServiceInstalled() bool {
	return VMServiceInstalled()
}

func (e *VMExecutor) ServiceActive() bool {
	return VMServiceActive()
}

func (e *VMExecutor) InteractiveProcess(args ...string) *exec.Cmd {
	withEnv := append([]string{"env", "HOME=/home/ubuntu"}, args...)
	full := append([]string{"exec", vmName, "--"}, withEnv...)
	c := exec.Command("multipass", full...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c
}

// VMInfoSnapshot returns VM-specific info (state, IP, load, memory, mounts).
// Only meaningful in VM mode.
func (e *VMExecutor) VMInfoSnapshot() VMInfo {
	return GetVMInfo()
}

// HostConfigRaw reads the host-side config for drift detection.
func (e *VMExecutor) HostConfigRaw() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(home + "/.picoclaw/config.json")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}
