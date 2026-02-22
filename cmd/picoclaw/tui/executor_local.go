package tui

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// LocalExecutor runs commands directly on the host.
type LocalExecutor struct {
	home string
}

// NewLocalExecutor creates an executor for local (non-VM) mode.
func NewLocalExecutor() *LocalExecutor {
	home, _ := os.UserHomeDir()
	return &LocalExecutor{home: home}
}

func (e *LocalExecutor) Mode() Mode { return ModeLocal }

func (e *LocalExecutor) ExecShell(timeout time.Duration, shellCmd string) (string, error) {
	return runLocalCmd(timeout, "bash", "-lc", shellCmd)
}

func (e *LocalExecutor) ExecCommand(timeout time.Duration, args ...string) (string, error) {
	return runLocalCmd(timeout, args[0], args[1:]...)
}

func (e *LocalExecutor) ReadFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

func (e *LocalExecutor) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func (e *LocalExecutor) ConfigPath() string {
	return filepath.Join(e.home, ".picoclaw", "config.json")
}

func (e *LocalExecutor) AuthPath() string {
	return filepath.Join(e.home, ".picoclaw", "auth.json")
}

func (e *LocalExecutor) HomePath() string { return e.home }

func (e *LocalExecutor) AgentVersion() string {
	out, err := runLocalCmd(5*time.Second, "sciclaw", "--version")
	if err != nil || strings.TrimSpace(out) == "" {
		return "unknown"
	}
	return strings.TrimSpace(strings.Split(out, "\n")[0])
}

func (e *LocalExecutor) ServiceInstalled() bool {
	out, err := runLocalCmd(5*time.Second, "sciclaw", "service", "status")
	if err != nil {
		return false
	}
	return !strings.Contains(out, "not installed")
}

func (e *LocalExecutor) ServiceActive() bool {
	out, err := runLocalCmd(5*time.Second, "sciclaw", "service", "status")
	if err != nil {
		return false
	}
	return strings.Contains(out, "Running: yes") || strings.Contains(out, "running: yes")
}

func (e *LocalExecutor) InteractiveProcess(args ...string) *exec.Cmd {
	c := exec.Command(args[0], args[1:]...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c
}

func runLocalCmd(timeout time.Duration, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	done := make(chan error, 1)
	if err := cmd.Start(); err != nil {
		return "", err
	}
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		if err != nil {
			return stdout.String(), err
		}
		return stdout.String(), nil
	case <-time.After(timeout):
		_ = cmd.Process.Kill()
		return "", exec.ErrNotFound
	}
}
