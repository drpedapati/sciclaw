package tui

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type doctorTestExec struct {
	out       string
	err       error
	lastShell string
}

func (e *doctorTestExec) Mode() Mode { return ModeLocal }

func (e *doctorTestExec) ExecShell(_ time.Duration, shellCmd string) (string, error) {
	e.lastShell = shellCmd
	return e.out, e.err
}

func (e *doctorTestExec) ExecCommand(_ time.Duration, _ ...string) (string, error) { return "", nil }

func (e *doctorTestExec) ReadFile(_ string) (string, error) { return "", os.ErrNotExist }

func (e *doctorTestExec) WriteFile(_ string, _ []byte, _ os.FileMode) error { return nil }

func (e *doctorTestExec) ConfigPath() string { return "/tmp/config.json" }

func (e *doctorTestExec) AuthPath() string { return "/tmp/auth.json" }

func (e *doctorTestExec) HomePath() string { return "/home/tester" }

func (e *doctorTestExec) BinaryPath() string { return "sciclaw" }

func (e *doctorTestExec) AgentVersion() string { return "vtest" }

func (e *doctorTestExec) ServiceInstalled() bool { return false }

func (e *doctorTestExec) ServiceActive() bool { return false }

func (e *doctorTestExec) InteractiveProcess(_ ...string) *exec.Cmd { return exec.Command("true") }

func TestRunDoctorCmd_ParsesJSONOnNonZeroExit(t *testing.T) {
	execStub := &doctorTestExec{
		out: `{
  "cli":"sciclaw",
  "version":"0.1.66-dev.14",
  "os":"linux",
  "arch":"amd64",
  "timestamp":"2026-02-27T04:21:32Z",
  "checks":[
    {"name":"auth.openai","status":"error","message":"expired (oauth)"},
    {"name":"workspace","status":"ok","message":"/home/ernie/sciclaw"}
  ]
}`,
		err: errors.New("exit status 1"),
	}

	msg, ok := runDoctorCmd(execStub)().(doctorDoneMsg)
	if !ok {
		t.Fatalf("unexpected message type: %T", msg)
	}
	if msg.err != nil {
		t.Fatalf("expected nil error when JSON report is valid, got %v", msg.err)
	}
	if msg.report == nil {
		t.Fatalf("expected report to be present")
	}
	if len(msg.report.Checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(msg.report.Checks))
	}
	if msg.report.Checks[0].Status != dcErr {
		t.Fatalf("expected first check status %q, got %q", dcErr, msg.report.Checks[0].Status)
	}
	if !strings.Contains(execStub.lastShell, "doctor --json") {
		t.Fatalf("expected doctor command, got %q", execStub.lastShell)
	}
}

func TestRunDoctorCmd_ReportsCommandFailureWhenOutputIsNotJSON(t *testing.T) {
	execStub := &doctorTestExec{
		out: "some plain text error",
		err: errors.New("exit status 1"),
	}

	msg := runDoctorCmd(execStub)().(doctorDoneMsg)
	if msg.err == nil {
		t.Fatalf("expected error for non-JSON output")
	}
	if !strings.Contains(msg.err.Error(), "command failed: exit status 1") {
		t.Fatalf("expected wrapped exit status, got %v", msg.err)
	}
	if !strings.Contains(msg.err.Error(), "some plain text error") {
		t.Fatalf("expected raw output in error, got %v", msg.err)
	}
}

