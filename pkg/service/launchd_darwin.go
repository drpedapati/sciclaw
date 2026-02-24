//go:build darwin

package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type launchdManager struct {
	runner        commandRunner
	exePath       string
	label         string
	domainTarget  string
	serviceTarget string
	plistPath     string
	stdoutPath    string
	stderrPath    string
}

func newLaunchdManager(exePath string, runner commandRunner) Manager {
	home, _ := os.UserHomeDir()
	label := "io.sciclaw.gateway"
	domain := fmt.Sprintf("gui/%d", os.Getuid())
	serviceTarget := fmt.Sprintf("%s/%s", domain, label)
	return &launchdManager{
		runner:        runner,
		exePath:       exePath,
		label:         label,
		domainTarget:  domain,
		serviceTarget: serviceTarget,
		plistPath:     filepath.Join(home, "Library", "LaunchAgents", label+".plist"),
		stdoutPath:    filepath.Join(home, ".picoclaw", "gateway.log"),
		stderrPath:    filepath.Join(home, ".picoclaw", "gateway.err.log"),
	}
}

func (m *launchdManager) Backend() string { return BackendLaunchd }

func (m *launchdManager) Install() error {
	// Fully tear down any prior registration so bootstrap starts from a
	// clean slate. This is idempotent — Uninstall is a no-op when the
	// service was never installed.
	_ = m.Uninstall()

	if err := os.MkdirAll(filepath.Dir(m.plistPath), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.stdoutPath), 0700); err != nil {
		return err
	}

	pathEnv := buildSystemdPath(os.Getenv("PATH"), m.detectBrewPrefix())
	plist := renderLaunchdPlist(m.label, m.exePath, m.stdoutPath, m.stderrPath, pathEnv)
	if err := os.WriteFile(m.plistPath, []byte(plist), 0644); err != nil {
		return err
	}

	// Bootstrap with retry — launchd may need a moment after the bootout
	// in Uninstall to fully release the old service registration.
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Second)
		}
		out, err := runCommand(m.runner, 10*time.Second, "launchctl", "bootstrap", m.domainTarget, m.plistPath)
		if err == nil {
			lastErr = nil
			break
		}
		msg := strings.ToLower(string(out))
		if strings.Contains(msg, "already bootstrapped") {
			lastErr = nil
			break
		}
		lastErr = fmt.Errorf("bootstrap failed: %s", oneLine(string(out)))
	}
	if lastErr != nil {
		return lastErr
	}
	if out, err := runCommand(m.runner, 5*time.Second, "launchctl", "enable", m.serviceTarget); err != nil {
		return fmt.Errorf("enable failed: %s", oneLine(string(out)))
	}
	return nil
}

func (m *launchdManager) Uninstall() error {
	_, _ = runCommand(m.runner, 10*time.Second, "launchctl", "bootout", m.serviceTarget)
	// Clear the disabled override so a future install won't fail on bootstrap.
	_, _ = runCommand(m.runner, 5*time.Second, "launchctl", "enable", m.serviceTarget)
	if err := os.Remove(m.plistPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (m *launchdManager) Start() error {
	if _, err := os.Stat(m.plistPath); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("service is not installed; run `sciclaw service install`")
		}
		return err
	}

	// Ensure the label isn't stuck in the disabled overrides database.
	_, _ = runCommand(m.runner, 5*time.Second, "launchctl", "enable", m.serviceTarget)
	if _, err := runCommand(m.runner, 3*time.Second, "launchctl", "print", m.serviceTarget); err != nil {
		if out, err2 := runCommand(m.runner, 10*time.Second, "launchctl", "bootstrap", m.domainTarget, m.plistPath); err2 != nil {
			msg := strings.ToLower(string(out))
			if !strings.Contains(msg, "already bootstrapped") {
				return fmt.Errorf("bootstrap failed: %s", oneLine(string(out)))
			}
		}
	}
	if out, err := runCommand(m.runner, 5*time.Second, "launchctl", "enable", m.serviceTarget); err != nil {
		return fmt.Errorf("enable failed: %s", commandErrorDetail(err, out))
	}
	if out, err := runCommand(m.runner, 10*time.Second, "launchctl", "kickstart", "-k", m.serviceTarget); err != nil {
		// launchctl may report a kickstart failure even after successfully loading
		// and running the service; treat verified running state as success.
		if st, stErr := m.Status(); stErr == nil && st.Running {
			return nil
		}
		return fmt.Errorf("kickstart failed: %s", commandErrorDetail(err, out))
	}
	return nil
}

func (m *launchdManager) Stop() error {
	if out, err := runCommand(m.runner, 10*time.Second, "launchctl", "bootout", m.serviceTarget); err != nil {
		msg := strings.ToLower(string(out))
		if strings.Contains(msg, "could not find service") || strings.Contains(msg, "no such process") {
			return nil
		}
		return fmt.Errorf("bootout failed: %s", oneLine(string(out)))
	}
	return nil
}

func (m *launchdManager) Restart() error {
	if err := m.Stop(); err != nil {
		return err
	}
	return m.Start()
}

func (m *launchdManager) Status() (Status, error) {
	st := Status{Backend: BackendLaunchd}
	if _, err := os.Stat(m.plistPath); err == nil {
		st.Installed = true
		st.Enabled = true
	}
	out, err := runCommand(m.runner, 3*time.Second, "launchctl", "print", m.serviceTarget)
	if err == nil {
		text := strings.ToLower(string(out))
		if strings.Contains(text, "state = running") || hasNonZeroPID(text) {
			st.Running = true
		}
		st.Detail = oneLine(string(out))
		return st, nil
	}
	if st.Installed {
		st.Detail = "installed but not loaded"
	}
	return st, nil
}

func (m *launchdManager) Logs(lines int) (string, error) {
	sections := map[string]string{}
	if out, err := tailFileLines(m.stdoutPath, lines); err == nil {
		sections[m.stdoutPath] = out
	}
	if out, err := tailFileLines(m.stderrPath, lines); err == nil {
		sections[m.stderrPath] = out
	}
	combined := combineLogSections(sections)
	if strings.TrimSpace(combined) == "" {
		return "", fmt.Errorf("no launchd logs found at %s or %s", m.stdoutPath, m.stderrPath)
	}
	return combined, nil
}

// hasNonZeroPID checks for "pid = <nonzero>" in launchctl print output.
// "pid = 0" appears when the service is bootstrapped but not actually running.
func hasNonZeroPID(text string) bool {
	idx := strings.Index(text, "pid = ")
	if idx < 0 {
		return false
	}
	rest := strings.TrimSpace(text[idx+len("pid = "):])
	// Check that the pid value is not "0".
	if len(rest) > 0 && rest[0] != '0' && rest[0] >= '1' && rest[0] <= '9' {
		return true
	}
	return false
}

func commandErrorDetail(err error, out []byte) string {
	if msg := oneLine(string(out)); msg != "" {
		return msg
	}
	if err != nil {
		return err.Error()
	}
	return ""
}

func (m *launchdManager) detectBrewPrefix() string {
	if _, err := exec.LookPath("brew"); err != nil {
		return ""
	}
	out, err := runCommand(m.runner, 4*time.Second, "brew", "--prefix")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
