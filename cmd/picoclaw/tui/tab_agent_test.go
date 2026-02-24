package tui

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type serviceTestExec struct {
	home      string
	actionOut string
	actionErr error
	statusOut string
	statusErr error
	calls     []string
}

func (e *serviceTestExec) Mode() Mode { return ModeLocal }

func (e *serviceTestExec) ExecShell(_ time.Duration, shellCmd string) (string, error) {
	e.calls = append(e.calls, shellCmd)
	if strings.Contains(shellCmd, "service status") {
		return e.statusOut, e.statusErr
	}
	return e.actionOut, e.actionErr
}

func (e *serviceTestExec) ExecCommand(_ time.Duration, _ ...string) (string, error) { return "", nil }

func (e *serviceTestExec) ReadFile(_ string) (string, error) { return "", os.ErrNotExist }

func (e *serviceTestExec) WriteFile(_ string, _ []byte, _ os.FileMode) error { return nil }

func (e *serviceTestExec) ConfigPath() string { return "/tmp/config.json" }

func (e *serviceTestExec) AuthPath() string { return "/tmp/auth.json" }

func (e *serviceTestExec) HomePath() string { return e.home }

func (e *serviceTestExec) BinaryPath() string { return "sciclaw" }

func (e *serviceTestExec) AgentVersion() string { return "vtest" }

func (e *serviceTestExec) ServiceInstalled() bool { return true }

func (e *serviceTestExec) ServiceActive() bool { return true }

func (e *serviceTestExec) InteractiveProcess(_ ...string) *exec.Cmd { return exec.Command("true") }

func TestAgentModel_HandleServiceAction(t *testing.T) {
	m := NewAgentModel(&routingTestExec{home: "/Users/tester"})
	m.HandleServiceAction(serviceActionMsg{
		action:   "start",
		ok:       true,
		output:   "service started",
		duration: 1500 * time.Millisecond,
	})

	if !m.logsLoaded {
		t.Fatal("logsLoaded = false, want true")
	}
	if got := m.logsViewport.View(); got == "" {
		t.Fatal("logs viewport content is empty")
	}
}

func TestServiceAction_NormalizesStatusWhenLaunchdReturnsError(t *testing.T) {
	execStub := &serviceTestExec{
		home:      "/Users/tester",
		actionOut: "Service restart failed: kickstart failed:",
		actionErr: errors.New("exit status 1"),
		statusOut: "Gateway service status:\n  Installed: yes\n  Running:   yes\n",
	}

	msg := serviceAction(execStub, "restart")().(serviceActionMsg)
	if !msg.ok {
		t.Fatalf("msg.ok = false, want true; output=%q", msg.output)
	}
	if !msg.normalized {
		t.Fatal("msg.normalized = false, want true")
	}
	if !msg.statusKnown || !msg.running || !msg.installed {
		t.Fatalf("unexpected status parse: known=%v installed=%v running=%v", msg.statusKnown, msg.installed, msg.running)
	}
}

func TestInferServiceActionSuccess(t *testing.T) {
	if !inferServiceActionSuccess("start", true, true) {
		t.Fatal("start should be successful when running=true")
	}
	if inferServiceActionSuccess("stop", true, true) {
		t.Fatal("stop should not be successful when running=true")
	}
	if !inferServiceActionSuccess("stop", true, false) {
		t.Fatal("stop should be successful when running=false")
	}
	if !inferServiceActionSuccess("install", true, false) {
		t.Fatal("install should be successful when installed=true")
	}
	if !inferServiceActionSuccess("uninstall", false, false) {
		t.Fatal("uninstall should be successful when installed=false")
	}
}
