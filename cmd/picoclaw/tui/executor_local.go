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
	self string // resolved path to current binary
}

// NewLocalExecutor creates an executor for local (non-VM) mode.
func NewLocalExecutor() *LocalExecutor {
	home, _ := os.UserHomeDir()
	self := "sciclaw" // fallback
	if exe, err := os.Executable(); err == nil {
		self = exe
	}
	return &LocalExecutor{home: home, self: self}
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

func (e *LocalExecutor) HomePath() string   { return e.home }
func (e *LocalExecutor) BinaryPath() string { return e.self }

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
	if installed, ok := parseServiceStatusFlag(out, "installed"); ok {
		return installed
	}
	return !strings.Contains(strings.ToLower(out), "not installed")
}

func (e *LocalExecutor) ServiceActive() bool {
	out, err := runLocalCmd(5*time.Second, "sciclaw", "service", "status")
	if err != nil {
		return false
	}
	if running, ok := parseServiceStatusFlag(out, "running"); ok {
		return running
	}
	return strings.Contains(strings.ToLower(out), "running: yes")
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
	cmd.Env = localCommandEnv()
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

func localCommandEnv() []string {
	env := append([]string(nil), os.Environ()...)

	currentPath := ""
	pathIdx := -1
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			pathIdx = i
			currentPath = strings.TrimPrefix(kv, "PATH=")
			break
		}
	}

	var preferred []string
	if exe, err := os.Executable(); err == nil {
		if dir := strings.TrimSpace(filepath.Dir(exe)); dir != "" {
			preferred = append(preferred, dir)
		}
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		preferred = append(preferred, filepath.Join(home, ".local", "bin"))
	}
	preferred = append(preferred, "/opt/homebrew/bin", "/usr/local/bin")

	mergedPath := mergePathList(preferred, currentPath)
	pathKV := "PATH=" + mergedPath
	if pathIdx >= 0 {
		env[pathIdx] = pathKV
		return env
	}
	return append(env, pathKV)
}

func mergePathList(preferred []string, current string) string {
	seen := map[string]struct{}{}
	var ordered []string

	appendPart := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		ordered = append(ordered, p)
	}

	for _, p := range preferred {
		appendPart(p)
	}
	for _, p := range strings.Split(current, string(os.PathListSeparator)) {
		appendPart(p)
	}

	return strings.Join(ordered, string(os.PathListSeparator))
}

func parseServiceStatusFlag(out, key string) (bool, bool) {
	keyLower := strings.ToLower(strings.TrimSpace(key))
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		lineLower := strings.ToLower(line)
		prefix := keyLower + ":"
		if !strings.HasPrefix(lineLower, prefix) {
			continue
		}
		val := strings.TrimSpace(lineLower[len(prefix):])
		switch {
		case strings.HasPrefix(val, "yes"):
			return true, true
		case strings.HasPrefix(val, "no"):
			return false, true
		}
		return false, false
	}
	return false, false
}
