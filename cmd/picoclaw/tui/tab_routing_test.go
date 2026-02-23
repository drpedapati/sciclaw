package tui

import (
	"os"
	"os/exec"
	"strconv"
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

	edited, _ := m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f")}, nil)
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

func TestInitAllowSelection_SeparatesKnownUsersFromExtras(t *testing.T) {
	users := []ApprovedUser{
		ParseApprovedUser("111|alice"),
		ParseApprovedUser("222|bob"),
	}
	selected, extras := initAllowSelection(users, "111,333|carol,@dave")

	if !selected["111"] {
		t.Fatalf("expected known user 111 to be selected: %#v", selected)
	}
	if selected["222"] {
		t.Fatalf("did not expect 222 selected by default: %#v", selected)
	}
	if got := strings.Join(extras, ","); got != "333|carol,@dave" {
		t.Fatalf("extras = %q, want %q", got, "333|carol,@dave")
	}
}

func TestBuildAllowCSV_PreservesUserOrderThenExtras(t *testing.T) {
	users := []ApprovedUser{
		ParseApprovedUser("111|alice"),
		ParseApprovedUser("222|bob"),
		ParseApprovedUser("333|carol"),
	}
	selected := map[string]bool{
		"333": true,
		"111": true,
	}
	got := buildAllowCSV(users, selected, []string{"@external"})
	if got != "111,333,@external" {
		t.Fatalf("buildAllowCSV = %q, want %q", got, "111,333,@external")
	}
}

func TestRoutingEditUsers_PickerSavesSelection(t *testing.T) {
	execStub := &routingTestExec{home: "/Users/tester", shellOut: "ok"}
	m := NewRoutingModel(execStub)
	m.mappings = []routingRow{
		{Channel: "discord", ChatID: "123", AllowedSenders: "111"},
	}
	m.selectedRow = 0
	snap := &VMSnapshot{
		Discord: ChannelSnapshot{
			ApprovedUsers: []ApprovedUser{
				ParseApprovedUser("111|alice"),
				ParseApprovedUser("222|bob"),
			},
		},
	}

	next, _ := m.startEditUsers(snap)
	if next.mode != routingEditUsers {
		t.Fatalf("mode = %v, want %v", next.mode, routingEditUsers)
	}
	if !next.editUsersSelected["111"] {
		t.Fatalf("expected 111 selected initially")
	}

	// Move cursor to second user and toggle on.
	next, _ = next.updateEditUsers(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}, snap)
	next, _ = next.updateEditUsers(tea.KeyMsg{Type: tea.KeySpace}, snap)

	saved, cmd := next.updateEditUsers(tea.KeyMsg{Type: tea.KeyEnter}, snap)
	if saved.mode != routingNormal {
		t.Fatalf("mode = %v, want %v", saved.mode, routingNormal)
	}
	_ = cmd().(routingActionMsg)
	if !strings.Contains(execStub.lastShell, "--allow '111,222'") {
		t.Fatalf("expected set-users allow list built from picker, cmd=%q", execStub.lastShell)
	}
}

func TestRoutingExplain_UsesFirstAllowedSenderAndShowsDetails(t *testing.T) {
	execStub := &routingTestExec{
		home: "/Users/tester",
		shellOut: "Routing explain:\n" +
			"  event: route_allowed\n" +
			"  allowed: true\n" +
			"  workspace: /tmp/project-a\n" +
			"  session_key: discord:123@abc123\n" +
			"  reason: matched mapping\n",
	}
	m := NewRoutingModel(execStub)
	m.mappings = []routingRow{
		{Channel: "discord", ChatID: "123", Workspace: "/tmp/project-a", AllowedSenders: "111|alice,222|bob"},
	}
	m.selectedRow = 0
	m.detailVP.Width = 100
	m.detailVP.Height = 40

	next, cmd := m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")}, nil)
	if cmd == nil {
		t.Fatal("expected explain command")
	}
	msg := cmd().(routingActionMsg)
	if msg.action != "explain" || !msg.ok {
		t.Fatalf("unexpected explain action msg: %#v", msg)
	}
	if !strings.Contains(execStub.lastShell, "sciclaw routing explain") {
		t.Fatalf("expected explain command, got %q", execStub.lastShell)
	}
	if !strings.Contains(execStub.lastShell, "--sender '111'") {
		t.Fatalf("expected first allowed sender id in explain command, got %q", execStub.lastShell)
	}

	next.HandleAction(msg)
	detail := next.detailVP.View()
	for _, want := range []string{"Explain output", "route_allowed", "true", "discord:123@abc123", "matched mapping"} {
		if !strings.Contains(detail, want) {
			t.Fatalf("detail view missing %q:\n%s", want, detail)
		}
	}
}

func TestRoutingExplain_RequiresAllowedSender(t *testing.T) {
	execStub := &routingTestExec{home: "/Users/tester"}
	m := NewRoutingModel(execStub)
	m.mappings = []routingRow{
		{Channel: "discord", ChatID: "123", Workspace: "/tmp/project-a", AllowedSenders: ""},
	}
	m.selectedRow = 0

	next, cmd := m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")}, nil)
	if cmd != nil {
		t.Fatal("expected no explain command when sender list is empty")
	}
	if !strings.Contains(next.flashMsg, "Add at least one allowed sender") {
		t.Fatalf("unexpected flash message: %q", next.flashMsg)
	}
}

func TestRoutingListRows_ShowLabelChannelAndTruncatedChatID(t *testing.T) {
	execStub := &routingTestExec{home: "/Users/tester"}
	m := NewRoutingModel(execStub)
	longChatID := "12345678901234567890"
	m.mappings = []routingRow{
		{Channel: "discord", ChatID: longChatID, Workspace: "/tmp/project-a", Label: "project-a"},
	}
	m.selectedRow = 0
	m.rebuildListContent()

	view := m.listVP.View()
	if !strings.Contains(view, "project-a") {
		t.Fatalf("list row missing label: %q", view)
	}
	if !strings.Contains(view, "discord") {
		t.Fatalf("list row missing channel: %q", view)
	}
	if !strings.Contains(view, truncateMiddle(longChatID, 16)) {
		t.Fatalf("list row missing truncated chat id: %q", view)
	}
}

func TestRoutingListScrollSync_KeepsSelectionVisible(t *testing.T) {
	execStub := &routingTestExec{home: "/Users/tester"}
	m := NewRoutingModel(execStub)
	m.listVP.Height = 3
	for i := 0; i < 10; i++ {
		m.mappings = append(m.mappings, routingRow{
			Channel:        "discord",
			ChatID:         "room-" + strconv.Itoa(i),
			Workspace:      "/tmp/project-" + strconv.Itoa(i),
			AllowedSenders: "111",
			Label:          "project-" + strconv.Itoa(i),
		})
	}
	m.rebuildListContent()

	var cmd tea.Cmd
	for i := 0; i < 6; i++ {
		m, cmd = m.updateNormal(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")}, nil)
		if cmd != nil {
			t.Fatalf("did not expect command while navigating list, got %#v", cmd)
		}
	}
	if m.listVP.YOffset == 0 {
		t.Fatalf("expected list to scroll, y-offset=%d", m.listVP.YOffset)
	}
	if !strings.Contains(m.listVP.View(), "▸") {
		t.Fatalf("expected selection indicator in visible list:\n%s", m.listVP.View())
	}
}

func TestRoutingEmptyState_ShowsOnboardingCommandBlock(t *testing.T) {
	execStub := &routingTestExec{home: "/Users/tester"}
	m := NewRoutingModel(execStub)
	m.loaded = true
	m.status.Enabled = false

	view := m.View(nil, 100)
	for _, want := range []string{"No routing mappings yet.", "Quick start:", "sciclaw routing add ...", "sciclaw routing enable"} {
		if !strings.Contains(view, want) {
			t.Fatalf("empty-state view missing %q:\n%s", want, view)
		}
	}
}

func TestRoutingStatusPanel_DisabledHintAndUnroutedWording(t *testing.T) {
	execStub := &routingTestExec{home: "/Users/tester"}
	m := NewRoutingModel(execStub)
	m.status = routingStatusInfo{
		Enabled:          false,
		UnmappedBehavior: "block",
		MappingCount:     2,
	}

	panel := m.renderStatusPanel(100)
	if !strings.Contains(panel, "Unrouted rooms:") {
		t.Fatalf("status panel should use unrouted wording:\n%s", panel)
	}
	if !strings.Contains(panel, "sciclaw routing enable") {
		t.Fatalf("status panel should include enable helper:\n%s", panel)
	}
}
