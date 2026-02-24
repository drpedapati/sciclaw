package tui

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type chatTestExec struct {
	home     string
	output   string
	err      error
	commands []string
}

func (e *chatTestExec) Mode() Mode { return ModeLocal }

func (e *chatTestExec) ExecShell(_ time.Duration, shellCmd string) (string, error) {
	e.commands = append(e.commands, shellCmd)
	return e.output, e.err
}

func (e *chatTestExec) ExecCommand(_ time.Duration, _ ...string) (string, error) { return "", nil }

func (e *chatTestExec) ReadFile(_ string) (string, error) { return "", os.ErrNotExist }

func (e *chatTestExec) WriteFile(_ string, _ []byte, _ os.FileMode) error { return nil }

func (e *chatTestExec) ConfigPath() string { return "/tmp/config.json" }

func (e *chatTestExec) AuthPath() string { return "/tmp/auth.json" }

func (e *chatTestExec) HomePath() string { return e.home }

func (e *chatTestExec) BinaryPath() string { return "sciclaw" }

func (e *chatTestExec) AgentVersion() string { return "vtest" }

func (e *chatTestExec) ServiceInstalled() bool { return false }

func (e *chatTestExec) ServiceActive() bool { return false }

func (e *chatTestExec) InteractiveProcess(_ ...string) *exec.Cmd { return exec.Command("true") }

func TestSendChatCmd_PropagatesProviderErrorOutput(t *testing.T) {
	const providerErr = "Error: LLM call failed: claude API call"
	exec := &chatTestExec{
		home:   "/Users/tester",
		output: providerErr,
		err:    errors.New("exit status 1"),
	}

	cmd := sendChatCmd(exec, "hi")
	msg, ok := cmd().(chatResponseMsg)
	if !ok {
		t.Fatalf("unexpected msg type: %T", msg)
	}
	if msg.err == nil {
		t.Fatalf("expected error")
	}
	if !strings.Contains(msg.err.Error(), "exit status 1") {
		t.Fatalf("expected wrapped exit status, got %v", msg.err)
	}
	if !strings.Contains(msg.err.Error(), providerErr) {
		t.Fatalf("expected command output in error, got %v", msg.err)
	}
	if len(exec.commands) == 0 || !strings.Contains(exec.commands[0], "2>/dev/null") {
		t.Fatalf("expected chat command to suppress stderr (2>/dev/null), got %q", strings.Join(exec.commands, " | "))
	}
}
