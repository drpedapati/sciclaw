package vmtui

import (
	"bytes"
	"os/exec"
	"strings"
	"time"
)

const vmName = "sciclaw"

// MountInfo represents a single multipass mount.
type MountInfo struct {
	HostPath string
	VMPath   string
}

// VMInfo holds parsed output from multipass info.
type VMInfo struct {
	State  string
	IPv4   string
	Load   string
	Memory string
	Mounts []MountInfo
}

// VMState returns the current VM state (Running, Stopped, NotFound, etc.).
func VMState() string {
	info, err := runMultipass(3*time.Second, "info", vmName)
	if err != nil {
		return "NotFound"
	}
	return parseInfoField(info, "State")
}

// GetVMInfo fetches full VM info.
func GetVMInfo() VMInfo {
	info, err := runMultipass(5*time.Second, "info", vmName)
	if err != nil {
		return VMInfo{State: "NotFound"}
	}
	return VMInfo{
		State:  parseInfoField(info, "State"),
		IPv4:   parseInfoField(info, "IPv4"),
		Load:   parseInfoField(info, "Load"),
		Memory: parseInfoField(info, "Memory usage"),
		Mounts: parseMounts(info),
	}
}

// VMExec runs a command inside the VM and returns stdout.
func VMExec(timeout time.Duration, args ...string) (string, error) {
	full := append([]string{"exec", vmName, "--"}, args...)
	return runMultipass(timeout, full...)
}

// VMExecShell runs a shell command inside the VM.
func VMExecShell(timeout time.Duration, shellCmd string) (string, error) {
	return VMExec(timeout, "bash", "-lc", shellCmd)
}

// VMCatFile reads a file from inside the VM.
func VMCatFile(path string) (string, error) {
	return VMExec(5*time.Second, "cat", path)
}

// VMAgentVersion gets the sciclaw version string from inside the VM.
func VMAgentVersion() string {
	out, err := VMExec(5*time.Second, "sciclaw", "--version")
	if err != nil || strings.TrimSpace(out) == "" {
		return "unknown"
	}
	return strings.TrimSpace(strings.Split(out, "\n")[0])
}

// VMServiceActive checks if sciclaw-gateway systemd service is active.
func VMServiceActive() bool {
	out, err := VMExecShell(5*time.Second, "systemctl --user is-active sciclaw-gateway 2>/dev/null || echo inactive")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "active"
}

// VMServiceInstalled checks if the systemd unit file exists.
func VMServiceInstalled() bool {
	out, err := VMExecShell(5*time.Second, "test -f ~/.config/systemd/user/sciclaw-gateway.service && echo yes || echo no")
	if err != nil {
		return false
	}
	return strings.TrimSpace(out) == "yes"
}

func runMultipass(timeout time.Duration, args ...string) (string, error) {
	cmd := exec.Command("multipass", args...)
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

func parseInfoField(output, key string) string {
	for _, line := range strings.Split(output, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == key {
			return strings.TrimSpace(parts[1])
		}
	}
	return ""
}

// parseMounts extracts mount entries from multipass info output.
// The format is:
//
//	Mounts:         /host/path => /vm/path
//	                    UID map: ...
//	                    GID map: ...
func parseMounts(output string) []MountInfo {
	var mounts []MountInfo
	inMounts := false
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(line, "Mounts:") {
			inMounts = true
			trimmed = strings.TrimPrefix(trimmed, "Mounts:")
			trimmed = strings.TrimSpace(trimmed)
		} else if len(line) > 0 && line[0] != ' ' && strings.Contains(line, ":") {
			inMounts = false
		}
		if inMounts && strings.Contains(trimmed, " => ") {
			parts := strings.SplitN(trimmed, " => ", 2)
			if len(parts) == 2 {
				mounts = append(mounts, MountInfo{
					HostPath: strings.TrimSpace(parts[0]),
					VMPath:   strings.TrimSpace(parts[1]),
				})
			}
		}
	}
	return mounts
}
