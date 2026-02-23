package tui

import (
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type homeTestExec struct {
	interactiveFails bool
}

func (e *homeTestExec) Mode() Mode { return ModeLocal }

func (e *homeTestExec) ExecShell(_ time.Duration, _ string) (string, error) { return "", nil }

func (e *homeTestExec) ExecCommand(_ time.Duration, _ ...string) (string, error) { return "", nil }

func (e *homeTestExec) ReadFile(_ string) (string, error) { return "", os.ErrNotExist }

func (e *homeTestExec) WriteFile(_ string, _ []byte, _ os.FileMode) error { return nil }

func (e *homeTestExec) ConfigPath() string { return "/tmp/config.json" }

func (e *homeTestExec) AuthPath() string { return "/tmp/auth.json" }

func (e *homeTestExec) HomePath() string { return "/Users/tester" }

func (e *homeTestExec) AgentVersion() string { return "vtest" }

func (e *homeTestExec) ServiceInstalled() bool { return false }

func (e *homeTestExec) ServiceActive() bool { return false }

func (e *homeTestExec) InteractiveProcess(_ ...string) *exec.Cmd {
	if e.interactiveFails {
		return exec.Command("sh", "-c", "exit 1")
	}
	return exec.Command("true")
}

func TestHomeWizard_AuthExecFailurePropagatesError(t *testing.T) {
	done, ok := onboardExecCallback(wizardAuth)(errors.New("exit status 1")).(onboardExecDoneMsg)
	if !ok {
		t.Fatalf("expected onboardExecDoneMsg")
	}
	if done.err == nil {
		t.Fatalf("expected auth error to propagate")
	}
	if done.step != wizardAuth {
		t.Fatalf("expected wizardAuth step, got %d", done.step)
	}
}

func TestHomeWizard_UpdateAuthStartsExecAndSetsLoading(t *testing.T) {
	m := NewHomeModel(&homeTestExec{})
	m.onboardActive = true
	m.onboardStep = wizardAuth

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter}, nil)
	if cmd == nil {
		t.Fatalf("expected auth exec command")
	}
	if !next.onboardLoading {
		t.Fatalf("expected onboardLoading=true while auth exec is running")
	}
}

func TestHomeWizard_HandleExecDone_DoesNotAdvanceOnFailures(t *testing.T) {
	t.Run("auth failure stays on auth step", func(t *testing.T) {
		m := NewHomeModel(&homeTestExec{})
		m.onboardActive = true
		m.onboardStep = wizardAuth

		m.HandleExecDone(onboardExecDoneMsg{step: wizardAuth, err: errors.New("exit status 1")})
		if m.onboardStep != wizardAuth {
			t.Fatalf("expected wizard step to remain auth, got %d", m.onboardStep)
		}
		if !strings.Contains(strings.ToLower(m.onboardResult), "not completed") {
			t.Fatalf("expected retry guidance in onboarding result, got %q", m.onboardResult)
		}
	})

	t.Run("channel failure stays on channel step", func(t *testing.T) {
		m := NewHomeModel(&homeTestExec{})
		m.onboardActive = true
		m.onboardStep = wizardChannel

		m.HandleExecDone(onboardExecDoneMsg{step: wizardChannel, err: errors.New("exit status 1")})
		if m.onboardStep != wizardChannel {
			t.Fatalf("expected wizard step to remain channel, got %d", m.onboardStep)
		}
		if !strings.Contains(strings.ToLower(m.onboardResult), "not completed") {
			t.Fatalf("expected retry guidance in onboarding result, got %q", m.onboardResult)
		}
	})
}
