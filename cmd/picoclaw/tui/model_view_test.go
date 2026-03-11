package tui

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"
)

type modelViewTestExec struct{}

func (e *modelViewTestExec) Mode() Mode { return ModeLocal }
func (e *modelViewTestExec) ExecShell(_ time.Duration, _ string) (string, error) {
	return "", os.ErrNotExist
}
func (e *modelViewTestExec) ExecCommand(_ time.Duration, _ ...string) (string, error) {
	return "", os.ErrNotExist
}
func (e *modelViewTestExec) ReadFile(_ string) (string, error) { return "", os.ErrNotExist }
func (e *modelViewTestExec) WriteFile(_ string, _ []byte, _ os.FileMode) error {
	return nil
}
func (e *modelViewTestExec) ConfigPath() string { return "/tmp/config.json" }
func (e *modelViewTestExec) AuthPath() string   { return "/tmp/auth.json" }
func (e *modelViewTestExec) HomePath() string   { return "/tmp" }
func (e *modelViewTestExec) BinaryPath() string { return "sciclaw" }
func (e *modelViewTestExec) AgentVersion() string {
	return "vtest"
}
func (e *modelViewTestExec) ServiceInstalled() bool { return true }
func (e *modelViewTestExec) ServiceActive() bool    { return true }
func (e *modelViewTestExec) InteractiveProcess(_ ...string) *exec.Cmd {
	return exec.Command("true")
}

func TestModelViewShowsGatewayStatusAcrossPages(t *testing.T) {
	m := NewModel(&modelViewTestExec{})
	m.width = 100
	m.height = 30
	m.loading = false
	m.snapshot = &VMSnapshot{
		ServiceInstalled: true,
		ServiceRunning:   true,
		ActiveModel:      "gpt-5.4",
	}

	view := stripANSIForModelViewTest(m.View())
	if !strings.Contains(view, "Gateway running") {
		t.Fatalf("expected global gateway chip in header:\n%s", view)
	}
	if !strings.Contains(view, "Gateway: running") {
		t.Fatalf("expected global gateway state in status bar:\n%s", view)
	}
}

func TestModelViewShowsRefreshingStatus(t *testing.T) {
	m := NewModel(&modelViewTestExec{})
	m.width = 100
	m.height = 30
	m.loading = true
	m.lastRefresh = time.Now().Add(-3 * time.Second)
	m.snapshot = &VMSnapshot{
		ServiceInstalled: true,
		ServiceRunning:   true,
		ActiveModel:      "claude-sonnet-4.6",
	}

	view := stripANSIForModelViewTest(m.View())
	if !strings.Contains(view, "Refreshing status...") {
		t.Fatalf("expected polling refresh indicator:\n%s", view)
	}
	if !strings.Contains(view, "Updated 3s ago") && !strings.Contains(view, "Updated 2s ago") && !strings.Contains(view, "Updated 4s ago") {
		t.Fatalf("expected recent refresh timing in status bar:\n%s", view)
	}
}

func TestModelViewShowsGatewayStoppedState(t *testing.T) {
	m := NewModel(&modelViewTestExec{})
	m.width = 100
	m.height = 30
	m.loading = false
	m.snapshot = &VMSnapshot{
		ServiceInstalled: true,
		ServiceRunning:   false,
	}

	view := stripANSIForModelViewTest(m.View())
	if !strings.Contains(view, "Gateway stopped") {
		t.Fatalf("expected stopped gateway indicator:\n%s", view)
	}
	if !strings.Contains(view, "Gateway: stopped") {
		t.Fatalf("expected stopped gateway status bar state:\n%s", view)
	}
}

func stripANSIForModelViewTest(in string) string {
	re := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	return re.ReplaceAllString(in, "")
}
