package tui

import (
	"os"
	"os/exec"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type skillsTestExec struct{}

func (e *skillsTestExec) Mode() Mode { return ModeLocal }

func (e *skillsTestExec) ExecShell(_ time.Duration, _ string) (string, error) { return "", nil }

func (e *skillsTestExec) ExecCommand(_ time.Duration, _ ...string) (string, error) { return "", nil }

func (e *skillsTestExec) ReadFile(_ string) (string, error) { return "", os.ErrNotExist }

func (e *skillsTestExec) WriteFile(_ string, _ []byte, _ os.FileMode) error { return nil }

func (e *skillsTestExec) ConfigPath() string { return "/tmp/config.json" }

func (e *skillsTestExec) AuthPath() string { return "/tmp/auth.json" }

func (e *skillsTestExec) HomePath() string { return "/Users/tester" }

func (e *skillsTestExec) AgentVersion() string { return "vtest" }

func (e *skillsTestExec) ServiceInstalled() bool { return false }

func (e *skillsTestExec) ServiceActive() bool { return false }

func (e *skillsTestExec) InteractiveProcess(_ ...string) *exec.Cmd { return exec.Command("true") }

func TestSkillsHandleList_ManualRefreshDoesNotClearBaselineLoading(t *testing.T) {
	m := NewSkillsModel(&skillsTestExec{})
	m.baselineLoading = true

	m.HandleList(skillsListMsg{output: "", fromBaseline: false})
	if !m.baselineLoading {
		t.Fatalf("expected baselineLoading to remain true after manual list refresh")
	}
}

func TestSkillsHandleList_BaselineRefreshClearsBaselineLoading(t *testing.T) {
	m := NewSkillsModel(&skillsTestExec{})
	m.baselineLoading = true

	m.HandleList(skillsListMsg{output: "", fromBaseline: true})
	if m.baselineLoading {
		t.Fatalf("expected baselineLoading=false after baseline install completion")
	}
}

func TestSkillsUpdate_DoesNotStartBaselineInstallWhenAlreadyRunning(t *testing.T) {
	m := NewSkillsModel(&skillsTestExec{})
	m.loaded = true
	m.skills = []skillRow{{Name: "pubmed"}}
	m.baselineLoading = true

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'I'}}, nil)
	if cmd != nil {
		t.Fatalf("expected no baseline install cmd while baselineLoading=true")
	}
	if !next.baselineLoading {
		t.Fatalf("expected baselineLoading to remain true")
	}
}
