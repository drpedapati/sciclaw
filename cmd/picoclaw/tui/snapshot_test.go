package tui

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

type snapshotTestExec struct {
	installed bool
	running   bool
}

func (e *snapshotTestExec) Mode() Mode { return ModeLocal }
func (e *snapshotTestExec) ExecShell(_ time.Duration, _ string) (string, error) {
	return "", os.ErrNotExist
}
func (e *snapshotTestExec) ExecCommand(_ time.Duration, _ ...string) (string, error) {
	return "", os.ErrNotExist
}
func (e *snapshotTestExec) ReadFile(_ string) (string, error) { return "", os.ErrNotExist }
func (e *snapshotTestExec) WriteFile(_ string, _ []byte, _ os.FileMode) error {
	return nil
}
func (e *snapshotTestExec) ConfigPath() string { return "/tmp/config.json" }
func (e *snapshotTestExec) AuthPath() string   { return "/tmp/auth.json" }
func (e *snapshotTestExec) HomePath() string   { return "/tmp" }
func (e *snapshotTestExec) BinaryPath() string { return "sciclaw" }
func (e *snapshotTestExec) AgentVersion() string {
	return "vtest"
}
func (e *snapshotTestExec) ServiceInstalled() bool { return e.installed }
func (e *snapshotTestExec) ServiceActive() bool    { return e.running }
func (e *snapshotTestExec) InteractiveProcess(_ ...string) *exec.Cmd {
	return exec.Command("true")
}

func TestCollectLocalSnapshotSetsVMAvailableWhenVMExists(t *testing.T) {
	origProvider := vmInfoProvider
	t.Cleanup(func() { vmInfoProvider = origProvider })
	vmInfoProvider = func() VMInfo { return VMInfo{State: "Running"} }

	snap := collectLocalSnapshot(&snapshotTestExec{})
	if snap.State != "Local" {
		t.Fatalf("state=%q, want Local", snap.State)
	}
	if !snap.VMAvailable {
		t.Fatal("expected VMAvailable=true when VM state is Running")
	}
}

func TestCollectLocalSnapshotClearsVMAvailableWhenNoVM(t *testing.T) {
	origProvider := vmInfoProvider
	t.Cleanup(func() { vmInfoProvider = origProvider })
	vmInfoProvider = func() VMInfo { return VMInfo{State: "NotFound"} }

	snap := collectLocalSnapshot(&snapshotTestExec{})
	if snap.VMAvailable {
		t.Fatal("expected VMAvailable=false when VM state is NotFound")
	}
}

