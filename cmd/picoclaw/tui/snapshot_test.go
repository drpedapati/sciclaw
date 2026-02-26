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
	t.Cleanup(func() {
		vmInfoProvider = origProvider
		resetLocalVMHintCacheForTest()
	})
	resetLocalVMHintCacheForTest()
	vmInfoProvider = func() VMInfo { return VMInfo{State: "Running"} }

	snap := collectLocalSnapshot(&snapshotTestExec{})
	if snap.State != "Local" {
		t.Fatalf("state=%q, want Local", snap.State)
	}

	deadline := time.Now().Add(300 * time.Millisecond)
	for !snap.VMAvailable && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		snap = collectLocalSnapshot(&snapshotTestExec{})
	}
	if !snap.VMAvailable {
		t.Fatal("expected VMAvailable=true after async VM hint refresh")
	}
}

func TestCollectLocalSnapshotClearsVMAvailableWhenNoVM(t *testing.T) {
	origProvider := vmInfoProvider
	t.Cleanup(func() {
		vmInfoProvider = origProvider
		resetLocalVMHintCacheForTest()
	})
	resetLocalVMHintCacheForTest()
	vmInfoProvider = func() VMInfo { return VMInfo{State: "NotFound"} }

	snap := collectLocalSnapshot(&snapshotTestExec{})
	deadline := time.Now().Add(300 * time.Millisecond)
	for snap.VMAvailable && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		snap = collectLocalSnapshot(&snapshotTestExec{})
	}
	if snap.VMAvailable {
		t.Fatal("expected VMAvailable=false when VM state is NotFound")
	}
}

func TestCollectLocalSnapshotDoesNotBlockOnSlowVMInfo(t *testing.T) {
	origProvider := vmInfoProvider
	t.Cleanup(func() {
		vmInfoProvider = origProvider
		resetLocalVMHintCacheForTest()
	})
	resetLocalVMHintCacheForTest()
	vmInfoProvider = func() VMInfo {
		time.Sleep(250 * time.Millisecond)
		return VMInfo{State: "Running"}
	}

	start := time.Now()
	_ = collectLocalSnapshot(&snapshotTestExec{})
	elapsed := time.Since(start)
	if elapsed > 120*time.Millisecond {
		t.Fatalf("collectLocalSnapshot blocked for %s; expected non-blocking VM hint refresh", elapsed)
	}
}
