package tui

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
	msg := cmd().(routingActionMsg)
	if !msg.ok {
		t.Fatalf("expected ok routing action, got: %#v", msg)
	}

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

func TestEditWorkspace_BrowseRoundTrip(t *testing.T) {
	execStub := &routingTestExec{
		home:     "/Users/tester",
		shellOut: "project/\n",
	}
	m := NewRoutingModel(execStub)
	m.mappings = []routingRow{
		{Channel: "discord", ChatID: "123", Workspace: "~/picoclaw/workspace"},
	}
	m.selectedRow = 0

	edited, _ := m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")}, nil)
	if edited.mode != routingEditWorkspace {
		t.Fatalf("mode = %v, want %v", edited.mode, routingEditWorkspace)
	}

	browsing, cmd := edited.updateEditWorkspace(tea.KeyMsg{Type: tea.KeyCtrlB}, nil)
	if browsing.mode != routingBrowseFolder {
		t.Fatalf("mode = %v, want %v", browsing.mode, routingBrowseFolder)
	}
	if browsing.browserTarget != browseTargetEditWorkspace {
		t.Fatalf("browser target = %v, want %v", browsing.browserTarget, browseTargetEditWorkspace)
	}
	if browsing.browserPath != "/Users/tester/picoclaw/workspace" {
		t.Fatalf("browserPath = %q, want %q", browsing.browserPath, "/Users/tester/picoclaw/workspace")
	}
	_ = cmd().(routingDirListMsg)

	restored, _ := browsing.updateBrowseFolder(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(" ")}, nil)
	if restored.mode != routingEditWorkspace {
		t.Fatalf("mode = %v, want %v", restored.mode, routingEditWorkspace)
	}
	if restored.editWorkspaceInput.Value() != "/Users/tester/picoclaw/workspace" {
		t.Fatalf("editWorkspaceInput = %q, want %q", restored.editWorkspaceInput.Value(), "/Users/tester/picoclaw/workspace")
	}
}

func TestPickRoom_UsesExpandedWorkspaceFromSnapshot(t *testing.T) {
	execStub := &routingTestExec{home: "/Users/tester"}
	m := NewRoutingModel(execStub)
	m.mode = routingPickRoom
	m.discordRooms = []discordRoom{
		{ChannelID: "123", GuildName: "Guild", ChannelName: "general"},
	}
	m.roomCursor = 0

	next, _ := m.updatePickRoom(tea.KeyMsg{Type: tea.KeyEnter}, &VMSnapshot{WorkspacePath: "~/picoclaw/workspace"})
	if next.mode != routingAddWizard {
		t.Fatalf("mode = %v, want %v", next.mode, routingAddWizard)
	}
	if next.wizardStep != addStepWorkspace {
		t.Fatalf("wizardStep = %d, want %d", next.wizardStep, addStepWorkspace)
	}
	if next.wizardInput.Value() != "/Users/tester/picoclaw/workspace" {
		t.Fatalf("workspace input = %q, want %q", next.wizardInput.Value(), "/Users/tester/picoclaw/workspace")
	}
}
