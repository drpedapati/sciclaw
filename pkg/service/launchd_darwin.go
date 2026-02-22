//go:build darwin

package service

import (
	"fmt"
	"os"
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
	if err := os.MkdirAll(filepath.Dir(m.plistPath), 0755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.stdoutPath), 0700); err != nil {
		return err
	}

	plist := renderLaunchdPlist(m.label, m.exePath, m.stdoutPath, m.stderrPath)
	if err := writeFileIfChanged(m.plistPath, []byte(plist), 0644); err != nil {
		return err
	}

	_, _ = runCommand(m.runner, 10*time.Second, "launchctl", "bootout", m.serviceTarget)
	if out, err := runCommand(m.runner, 10*time.Second, "launchctl", "bootstrap", m.domainTarget, m.plistPath); err != nil {
		msg := strings.ToLower(string(out))
		if !strings.Contains(msg, "already bootstrapped") {
			return fmt.Errorf("bootstrap failed: %s", oneLine(string(out)))
		}
	}
	if out, err := runCommand(m.runner, 5*time.Second, "launchctl", "enable", m.serviceTarget); err != nil {
		return fmt.Errorf("enable failed: %s", oneLine(string(out)))
	}
	return nil
}

func (m *launchdManager) Uninstall() error {
	_, _ = runCommand(m.runner, 10*time.Second, "launchctl", "bootout", m.serviceTarget)
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
		if strings.Contains(text, "state = running") || strings.Contains(text, "pid =") {
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

func commandErrorDetail(err error, out []byte) string {
	if msg := oneLine(string(out)); msg != "" {
		return msg
	}
	if err != nil {
		return err.Error()
	}
	return ""
}
