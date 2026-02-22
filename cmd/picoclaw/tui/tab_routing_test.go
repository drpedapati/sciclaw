package tui

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type routingTestExec struct {
	home      string
	shellOut  string
	shellErr  error
	lastShell string
}

func (e *routingTestExec) Mode() Mode { return ModeLocal }

func (e *routingTestExec) ExecShell(_ time.Duration, shellCmd string) (string, error) {
	e.lastShell = shellCmd
	return e.shellOut, e.shellErr
}

func (e *routingTestExec) ExecCommand(_ time.Duration, _ ...string) (string, error) { return "", nil }

func (e *routingTestExec) ReadFile(_ string) (string, error) { return "", os.ErrNotExist }

func (e *routingTestExec) WriteFile(_ string, _ []byte, _ os.FileMode) error { return nil }

func (e *routingTestExec) ConfigPath() string { return "/tmp/config.json" }

func (e *routingTestExec) AuthPath() string { return "/tmp/auth.json" }

func (e *routingTestExec) HomePath() string { return e.home }

func (e *routingTestExec) AgentVersion() string { return "vtest" }

func (e *routingTestExec) ServiceInstalled() bool { return false }

func (e *routingTestExec) ServiceActive() bool { return false }

func (e *routingTestExec) InteractiveProcess(_ ...string) *exec.Cmd { return exec.Command("true") }

func TestExpandHomeForExecPath(t *testing.T) {
	home := "/Users/tester"
	tests := []struct {
		in   string
		want string
	}{
		{in: "~", want: "/Users/tester"},
		{in: "~/sciclaw", want: "/Users/tester/sciclaw"},
		{in: "  ~/sciclaw/workspace  ", want: "/Users/tester/sciclaw/workspace"},
		{in: "/tmp/workspace", want: "/tmp/workspace"},
		{in: "relative/path", want: "relative/path"},
	}
	for _, tt := range tests {
		if got := expandHomeForExecPath(tt.in, home); got != tt.want {
			t.Fatalf("expandHomeForExecPath(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestFetchDirListCmd_ExpandsHomePath(t *testing.T) {
	execStub := &routingTestExec{
		home:     "/Users/tester",
		shellOut: "alpha/\nbeta/\nnotes.txt\n",
	}
	cmd := fetchDirListCmd(execStub, "~/sciclaw")
	msg := cmd().(routingDirListMsg)

	if msg.err != "" {
		t.Fatalf("unexpected err: %q", msg.err)
	}
	if msg.path != "/Users/tester/sciclaw" {
		t.Fatalf("msg.path = %q, want %q", msg.path, "/Users/tester/sciclaw")
	}
	if got, want := strings.Join(msg.dirs, ","), "alpha,beta"; got != want {
		t.Fatalf("dirs = %q, want %q", got, want)
	}
	if !strings.Contains(execStub.lastShell, "/Users/tester/sciclaw") {
		t.Fatalf("shell cmd did not use expanded path: %q", execStub.lastShell)
	}
	if strings.Contains(execStub.lastShell, "~/sciclaw") {
		t.Fatalf("shell cmd still contains tilde path: %q", execStub.lastShell)
	}
}

func TestRoutingAddMappingCmd_ExpandsWorkspacePath(t *testing.T) {
	execStub := &routingTestExec{
		home:     "/Users/tester",
		shellOut: "ok",
	}
	cmd := routingAddMappingCmd(execStub, "discord", "123", "~/sciclaw/workspace", "u1", "")
	_ = cmd().(actionDoneMsg)

	if !strings.Contains(execStub.lastShell, "--workspace '/Users/tester/sciclaw/workspace'") {
		t.Fatalf("routing add command missing expanded workspace: %q", execStub.lastShell)
	}
}

func TestStartBrowse_UsesExpandedWorkspacePath(t *testing.T) {
	execStub := &routingTestExec{
		home:     "/Users/tester",
		shellOut: "project/\n",
	}
	m := NewRoutingModel(execStub)
	m.wizardInput.SetValue("~/picoclaw/workspace")

	cmd := m.startBrowse(nil)
	if m.browserPath != "/Users/tester/picoclaw/workspace" {
		t.Fatalf("browserPath = %q, want %q", m.browserPath, "/Users/tester/picoclaw/workspace")
	}

	msg := cmd().(routingDirListMsg)
	if msg.path != "/Users/tester/picoclaw/workspace" {
		t.Fatalf("dir-list path = %q, want %q", msg.path, "/Users/tester/picoclaw/workspace")
	}
}
