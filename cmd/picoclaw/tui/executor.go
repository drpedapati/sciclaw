package tui

import (
	"os"
	"os/exec"
	"time"
)

// Mode identifies the TUI execution environment.
type Mode string

const (
	ModeVM    Mode = "vm"
	ModeLocal Mode = "local"
)

// Executor abstracts command execution across VM and local environments.
type Executor interface {
	// Mode returns whether this is VM or local execution.
	Mode() Mode

	// ExecShell runs a shell command and returns stdout.
	ExecShell(timeout time.Duration, shellCmd string) (string, error)

	// ExecCommand runs a command with explicit args and returns stdout.
	ExecCommand(timeout time.Duration, args ...string) (string, error)

	// ReadFile reads a file by path.
	ReadFile(path string) (string, error)

	// WriteFile writes content to a file.
	WriteFile(path string, data []byte, perm os.FileMode) error

	// ConfigPath returns the path to config.json.
	ConfigPath() string

	// AuthPath returns the path to auth.json.
	AuthPath() string

	// HomePath returns the home directory.
	HomePath() string

	// BinaryPath returns the resolved path to the sciclaw binary.
	BinaryPath() string

	// AgentVersion returns the sciclaw version string.
	AgentVersion() string

	// ServiceInstalled checks if the gateway service is installed.
	ServiceInstalled() bool

	// ServiceActive checks if the gateway service is running.
	ServiceActive() bool

	// InteractiveProcess creates an exec.Cmd for use with tea.ExecProcess.
	InteractiveProcess(args ...string) *exec.Cmd
}
