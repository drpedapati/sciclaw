package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sipeed/picoclaw/pkg/addons"
)

// --- parseInstallFlags ------------------------------------------------------

func TestParseInstallFlags_Empty(t *testing.T) {
	f, err := parseInstallFlags(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Commit != "" || f.Version != "" || f.Track != "" || f.Name != "" || f.Yes {
		t.Fatalf("expected zero struct, got %+v", f)
	}
	// Empty flags → NewAutoRef() which is all-empty.
	ref := f.toInstallRef()
	if ref.Commit != "" || ref.Version != "" || ref.Track != "" {
		t.Fatalf("expected auto ref, got %+v", ref)
	}
}

func TestParseInstallFlags_Commit(t *testing.T) {
	f, err := parseInstallFlags([]string{"--commit", "abc123", "--yes"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Commit != "abc123" {
		t.Fatalf("commit: %q", f.Commit)
	}
	if !f.Yes {
		t.Fatal("yes not set")
	}
	ref := f.toInstallRef()
	if ref.Commit != "abc123" {
		t.Fatalf("ref.Commit: %q", ref.Commit)
	}
}

func TestParseInstallFlags_Version(t *testing.T) {
	f, err := parseInstallFlags([]string{"--version", "v0.1.0"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Version != "v0.1.0" {
		t.Fatalf("version: %q", f.Version)
	}
	ref := f.toInstallRef()
	if ref.Version != "v0.1.0" {
		t.Fatalf("ref.Version: %q", ref.Version)
	}
}

func TestParseInstallFlags_Track(t *testing.T) {
	f, err := parseInstallFlags([]string{"--track", "main"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Track != "main" {
		t.Fatalf("track: %q", f.Track)
	}
	ref := f.toInstallRef()
	if ref.Track != "main" {
		t.Fatalf("ref.Track: %q", ref.Track)
	}
}

func TestParseInstallFlags_NameOverride(t *testing.T) {
	f, err := parseInstallFlags([]string{"--name", "webtop", "--commit", "deadbeef"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.Name != "webtop" {
		t.Fatalf("name: %q", f.Name)
	}
	if f.Commit != "deadbeef" {
		t.Fatalf("commit: %q", f.Commit)
	}
}

func TestParseInstallFlags_RejectsMultiplePins(t *testing.T) {
	cases := [][]string{
		{"--commit", "abc", "--version", "v1"},
		{"--commit", "abc", "--track", "main"},
		{"--version", "v1", "--track", "main"},
		{"--commit", "abc", "--version", "v1", "--track", "main"},
	}
	for _, args := range cases {
		if _, err := parseInstallFlags(args); err == nil {
			t.Fatalf("expected error for args %v", args)
		}
	}
}

func TestParseInstallFlags_UnknownFlag(t *testing.T) {
	_, err := parseInstallFlags([]string{"--nope"})
	if err == nil {
		t.Fatal("expected unknown-flag error")
	}
	if !strings.Contains(err.Error(), "unknown flag") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseInstallFlags_MissingValue(t *testing.T) {
	for _, flag := range []string{"--commit", "--version", "--track", "--name"} {
		if _, err := parseInstallFlags([]string{flag}); err == nil {
			t.Fatalf("expected error for trailing %s", flag)
		}
	}
}

func TestParseInstallFlags_Help(t *testing.T) {
	_, err := parseInstallFlags([]string{"--help"})
	if !errors.Is(err, errShowHelp) {
		t.Fatalf("expected errShowHelp, got %v", err)
	}
}

func TestInstallFlags_DescribeRef(t *testing.T) {
	cases := map[string]installFlags{
		"commit abc":                                                          {Commit: "abc"},
		"version v1.2.3":                                                      {Version: "v1.2.3"},
		"branch main (track mode — not recommended for production)":           {Track: "main"},
		"latest signed tag (auto)":                                            {},
	}
	for want, f := range cases {
		if got := f.describeRef(); got != want {
			t.Fatalf("describeRef(%+v) = %q, want %q", f, got, want)
		}
	}
}

// --- formatAddonListTable ---------------------------------------------------

func TestFormatAddonListTable_Rows(t *testing.T) {
	rows := []addonListRow{
		{
			Name:    "webtop",
			Version: "0.1.0",
			State:   "enabled",
			Commit:  "abc123def456",
			Source:  "https://github.com/sciclaw/webtop",
		},
		{
			Name:    "jupyter",
			Version: "0.2.0",
			State:   "installed",
			Commit:  "feedface0000",
			Source:  "https://github.com/sciclaw/jupyter",
		},
	}
	out := formatAddonListTable(rows)
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "VERSION") ||
		!strings.Contains(out, "STATE") || !strings.Contains(out, "COMMIT") ||
		!strings.Contains(out, "SOURCE") {
		t.Fatalf("missing header: %q", out)
	}
	if !strings.Contains(out, "webtop") || !strings.Contains(out, "jupyter") {
		t.Fatalf("missing rows: %q", out)
	}
	if !strings.Contains(out, "enabled") || !strings.Contains(out, "installed") {
		t.Fatalf("missing states: %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("expected trailing newline")
	}
}

func TestFormatAddonListTable_HeaderOnly(t *testing.T) {
	out := formatAddonListTable(nil)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 header line, got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "NAME") {
		t.Fatalf("expected header, got %q", lines[0])
	}
}

func TestRowFromEntry(t *testing.T) {
	entry := &addons.RegistryEntry{
		Version:         "1.2.3",
		State:           addons.StateEnabled,
		InstalledCommit: "0123456789abcdef0123456789abcdef01234567",
		Source:          "https://example.com/repo",
	}
	row := rowFromEntry("demo", entry)
	if row.Name != "demo" || row.Version != "1.2.3" || row.State != "enabled" {
		t.Fatalf("bad row: %+v", row)
	}
	if row.Commit != "0123456789ab" {
		t.Fatalf("expected short commit, got %q", row.Commit)
	}
}

func TestShortCommit(t *testing.T) {
	if got := shortCommit("abc"); got != "abc" {
		t.Fatalf("short input: %q", got)
	}
	if got := shortCommit("0123456789abcdef"); got != "0123456789ab" {
		t.Fatalf("truncation: %q", got)
	}
}

// --- writeSBOMToTarget ------------------------------------------------------

func TestWriteSBOMToTarget_File(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "out.json")
	sbom := &addons.SBOM{
		Name:            "webtop",
		Version:         "0.1.0",
		InstalledCommit: "deadbeef",
		Platform:        "linux/amd64",
		ExportedAt:      "2026-04-13T00:00:00Z",
	}
	if err := writeSBOMToTarget(sbom, path); err != nil {
		t.Fatalf("writeSBOMToTarget: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var round addons.SBOM
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatalf("unmarshal: %v (data=%s)", err, data)
	}
	if round.Name != "webtop" || round.Version != "0.1.0" {
		t.Fatalf("round trip lost fields: %+v", round)
	}
}

func TestWriteSBOMToTarget_StdoutPipesToCaller(t *testing.T) {
	// We can't easily intercept os.Stdout from within the function, but
	// addons.WriteSBOM(writer, sbom) is the underlying primitive — we test
	// here that passing empty output falls through to stdout without an
	// error. File-mode is covered by the test above.
	//
	// Redirect stdout temporarily.
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	sbom := &addons.SBOM{
		Name:            "jupyter",
		Version:         "0.2.0",
		InstalledCommit: "cafebabe",
		Platform:        "linux/arm64",
		ExportedAt:      "2026-04-13T00:00:00Z",
	}
	errCh := make(chan error, 1)
	go func() { errCh <- writeSBOMToTarget(sbom, "") }()

	buf := make([]byte, 4096)
	// Close writer after call finishes so read doesn't block forever.
	if err := <-errCh; err != nil {
		_ = w.Close()
		t.Fatalf("writeSBOMToTarget: %v", err)
	}
	_ = w.Close()
	n, _ := r.Read(buf)
	got := string(buf[:n])
	if !strings.Contains(got, "\"name\": \"jupyter\"") {
		t.Fatalf("stdout missing name: %q", got)
	}
	if !strings.Contains(got, "\"version\": \"0.2.0\"") {
		t.Fatalf("stdout missing version: %q", got)
	}
}

// --- promptYes / promptTypedName --------------------------------------------

func TestPromptYes_AssumeYes(t *testing.T) {
	in := strings.NewReader("")
	out := &bytes.Buffer{}
	if !promptYes(in, out, "Proceed?", true) {
		t.Fatal("expected true when assumeYes is set")
	}
	// With assumeYes we shouldn't have prompted the user.
	if out.Len() != 0 {
		t.Fatalf("unexpected prompt output: %q", out.String())
	}
}

func TestPromptYes_YesInputs(t *testing.T) {
	for _, input := range []string{"y\n", "Y\n", "yes\n", "YES\n", "yEs\n"} {
		out := &bytes.Buffer{}
		if !promptYes(strings.NewReader(input), out, "?", false) {
			t.Fatalf("input %q should be yes", input)
		}
	}
}

func TestPromptYes_NoInputs(t *testing.T) {
	for _, input := range []string{"\n", "n\n", "N\n", "no\n", "maybe\n"} {
		out := &bytes.Buffer{}
		if promptYes(strings.NewReader(input), out, "?", false) {
			t.Fatalf("input %q should be no", input)
		}
	}
}

func TestPromptTypedName(t *testing.T) {
	out := &bytes.Buffer{}
	if !promptTypedName(strings.NewReader("webtop\n"), out, "webtop") {
		t.Fatal("matching name should confirm")
	}
	out.Reset()
	if promptTypedName(strings.NewReader("wrongname\n"), out, "webtop") {
		t.Fatal("mismatched name should not confirm")
	}
	out.Reset()
	if promptTypedName(strings.NewReader(" webtop \n"), out, "webtop") == false {
		t.Fatal("leading/trailing whitespace should be trimmed")
	}
}

// --- display helpers --------------------------------------------------------

func TestDisplayOrDash(t *testing.T) {
	if displayOrDash("") != "-" {
		t.Fatal("empty should be dash")
	}
	if displayOrDash("abc") != "abc" {
		t.Fatal("non-empty should pass through")
	}
}

func TestStrOrDash(t *testing.T) {
	if strOrDash(nil) != "-" {
		t.Fatal("nil should be dash")
	}
	empty := ""
	if strOrDash(&empty) != "-" {
		t.Fatal("empty-string pointer should be dash")
	}
	val := "main"
	if strOrDash(&val) != "main" {
		t.Fatal("non-empty pointer should pass through")
	}
}

func TestResolveUnderDir(t *testing.T) {
	if got := resolveUnderDir("/opt/addon", "bin/install.sh"); got != "/opt/addon/bin/install.sh" {
		t.Fatalf("relative: %q", got)
	}
	if got := resolveUnderDir("/opt/addon", "/etc/passwd"); got != "/etc/passwd" {
		t.Fatalf("absolute: %q", got)
	}
}

func TestFormatStatusReport(t *testing.T) {
	track := "main"
	tag := "v0.1.0"
	prev := "0000000011112222"
	entry := &addons.RegistryEntry{
		Version:           "1.0.0",
		InstalledAt:       "2026-04-13T00:00:00Z",
		InstalledCommit:   "deadbeef",
		ManifestSHA256:    "aabbcc",
		BootstrapSHA256:   "",
		SidecarSHA256:     "112233",
		State:             addons.StateInstalled,
		Source:            "https://example.com/repo",
		Track:             &track,
		SignedTag:         &tag,
		SignatureVerified: true,
		PreviousCommit:    &prev,
	}
	report := formatStatusReport("demo", "/tmp/addons/demo", entry)
	required := []string{
		"Addon: demo",
		"Directory:        /tmp/addons/demo",
		"Version:          1.0.0",
		"State:            installed",
		"Source:           https://example.com/repo",
		"Installed commit: deadbeef",
		"Manifest SHA256:  aabbcc",
		"Bootstrap SHA256: -",
		"Sidecar SHA256:   112233",
		"Track:            main",
		"Signed tag:       v0.1.0",
		"Signature verified: true",
		"Previous commit:  0000000011112222",
	}
	for _, line := range required {
		if !strings.Contains(report, line) {
			t.Fatalf("expected line %q in report:\n%s", line, report)
		}
	}
}

// --- fake lifecycle / interface round-trip ---------------------------------

type fakeLifecycle struct {
	installCalls  []addons.InstallOptions
	upgradeCalls  []struct {
		Name string
		Ref  addons.InstallRef
	}
	uninstallCalls []struct {
		Name  string
		Force bool
	}
	entries  map[string]*addons.RegistryEntry
	enabled  map[string]bool
	failWith error
}

func newFakeLifecycle() *fakeLifecycle {
	return &fakeLifecycle{
		entries: map[string]*addons.RegistryEntry{},
		enabled: map[string]bool{},
	}
}

func (f *fakeLifecycle) Install(ctx context.Context, opts addons.InstallOptions) (*addons.RegistryEntry, error) {
	f.installCalls = append(f.installCalls, opts)
	if f.failWith != nil {
		return nil, f.failWith
	}
	name := opts.Name
	if name == "" {
		name = "demo"
	}
	entry := &addons.RegistryEntry{
		Version:         "0.1.0",
		InstalledAt:     time.Now().UTC().Format(time.RFC3339),
		InstalledCommit: "abc123abc123",
		ManifestSHA256:  "m",
		State:           addons.StateInstalled,
		Source:          opts.Source,
	}
	f.entries[name] = entry
	return entry, nil
}

func (f *fakeLifecycle) Enable(ctx context.Context, name string) (*addons.RegistryEntry, error) {
	e, ok := f.entries[name]
	if !ok {
		return nil, errors.New("not installed")
	}
	e.State = addons.StateEnabled
	f.enabled[name] = true
	return e, nil
}

func (f *fakeLifecycle) Disable(ctx context.Context, name string) (*addons.RegistryEntry, error) {
	e, ok := f.entries[name]
	if !ok {
		return nil, errors.New("not installed")
	}
	e.State = addons.StateInstalled
	f.enabled[name] = false
	return e, nil
}

func (f *fakeLifecycle) Uninstall(ctx context.Context, name string, force bool) error {
	f.uninstallCalls = append(f.uninstallCalls, struct {
		Name  string
		Force bool
	}{Name: name, Force: force})
	delete(f.entries, name)
	delete(f.enabled, name)
	return nil
}

func (f *fakeLifecycle) Upgrade(ctx context.Context, name string, ref addons.InstallRef) (*addons.RegistryEntry, error) {
	f.upgradeCalls = append(f.upgradeCalls, struct {
		Name string
		Ref  addons.InstallRef
	}{Name: name, Ref: ref})
	if f.failWith != nil {
		return nil, f.failWith
	}
	e, ok := f.entries[name]
	if !ok {
		return nil, errors.New("not installed")
	}
	// Mimic the "no-op upgrade" path via the sentinel wrap.
	if ref.Commit != "" && ref.Commit == e.InstalledCommit {
		return e, addons.ErrAlreadyAtCommit
	}
	prev := e.InstalledCommit
	e.PreviousCommit = &prev
	e.InstalledCommit = "upgraded-commit"
	return e, nil
}

func (f *fakeLifecycle) List(ctx context.Context) ([]*addons.RegistryEntry, error) {
	out := make([]*addons.RegistryEntry, 0, len(f.entries))
	for _, e := range f.entries {
		out = append(out, e)
	}
	return out, nil
}

func (f *fakeLifecycle) AddonDir(name string) string {
	return filepath.Join("/tmp/fake-addons", name)
}

// Compile-time assertion: the fake satisfies the interface used by command
// handlers (mirrors the assertion for *addons.Lifecycle in addon_cmd.go).
var _ addonLifecycle = (*fakeLifecycle)(nil)

func TestFakeLifecycle_InstallEnableDisableUninstall(t *testing.T) {
	f := newFakeLifecycle()
	ctx := context.Background()

	entry, err := f.Install(ctx, addons.InstallOptions{
		Name:   "webtop",
		Source: "https://example/repo",
		Ref:    addons.NewCommitRef("abc"),
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if entry.State != addons.StateInstalled {
		t.Fatalf("expected installed state, got %s", entry.State)
	}
	if len(f.installCalls) != 1 || f.installCalls[0].Name != "webtop" {
		t.Fatalf("install calls: %+v", f.installCalls)
	}
	if _, err := f.Enable(ctx, "webtop"); err != nil {
		t.Fatalf("enable: %v", err)
	}
	if !f.enabled["webtop"] {
		t.Fatal("expected enabled flag")
	}
	if _, err := f.Disable(ctx, "webtop"); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if err := f.Uninstall(ctx, "webtop", false); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if len(f.uninstallCalls) != 1 || f.uninstallCalls[0].Force {
		t.Fatalf("uninstall calls: %+v", f.uninstallCalls)
	}
	if _, ok := f.entries["webtop"]; ok {
		t.Fatal("entry should be gone after uninstall")
	}
}

func TestFakeLifecycle_UpgradeAlreadyAtCommit(t *testing.T) {
	f := newFakeLifecycle()
	ctx := context.Background()
	// Seed with a known commit.
	f.entries["demo"] = &addons.RegistryEntry{
		Version:         "0.1.0",
		InstalledCommit: "pinned",
		State:           addons.StateInstalled,
	}
	_, err := f.Upgrade(ctx, "demo", addons.NewCommitRef("pinned"))
	if !errors.Is(err, addons.ErrAlreadyAtCommit) {
		t.Fatalf("expected ErrAlreadyAtCommit, got %v", err)
	}
}

func TestFakeLifecycle_UpgradeAdvancesCommit(t *testing.T) {
	f := newFakeLifecycle()
	ctx := context.Background()
	f.entries["demo"] = &addons.RegistryEntry{
		Version:         "0.1.0",
		InstalledCommit: "old",
		State:           addons.StateInstalled,
	}
	updated, err := f.Upgrade(ctx, "demo", addons.NewCommitRef("new"))
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if updated.InstalledCommit != "upgraded-commit" {
		t.Fatalf("commit not advanced: %s", updated.InstalledCommit)
	}
	if updated.PreviousCommit == nil || *updated.PreviousCommit != "old" {
		t.Fatalf("previous commit not recorded: %+v", updated.PreviousCommit)
	}
	if len(f.upgradeCalls) != 1 || f.upgradeCalls[0].Ref.Commit != "new" {
		t.Fatalf("upgrade calls: %+v", f.upgradeCalls)
	}
}

// --- sciclawVersionForAddons ------------------------------------------------

func TestSciclawVersionForAddons_DevFallback(t *testing.T) {
	saved := version
	defer func() { version = saved }()
	version = "dev"
	if got := sciclawVersionForAddons(); got != "0.3.0-dev" {
		t.Fatalf("expected dev fallback, got %q", got)
	}
	version = ""
	if got := sciclawVersionForAddons(); got != "0.3.0-dev" {
		t.Fatalf("expected empty fallback, got %q", got)
	}
	version = "0.5.2"
	if got := sciclawVersionForAddons(); got != "0.5.2" {
		t.Fatalf("expected passthrough, got %q", got)
	}
	version = "v0.5.2"
	if got := sciclawVersionForAddons(); got != "0.5.2" {
		t.Fatalf("expected v-stripped, got %q", got)
	}
}

// --- sanity: addons.Lifecycle satisfies the CLI interface ------------------

func TestLifecycleSatisfiesInterface(t *testing.T) {
	// Compile-time assertion is in addon_cmd.go; this test ensures the
	// assertion stays live if someone ever moves it.
	var _ addonLifecycle = (*addons.Lifecycle)(nil)
}

// --- run* handler tests with an injected fake env --------------------------

// testEnv builds an *addonEnv wired to an in-memory fake lifecycle with a
// tiny registry-lookup shim backed by the fake's entries map.
func testEnv(in string) (*addonEnv, *fakeLifecycle, *bytes.Buffer, *bytes.Buffer) {
	fl := newFakeLifecycle()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	env := &addonEnv{
		Lifecycle: fl,
		Stdin:     strings.NewReader(in),
		Stdout:    stdout,
		Stderr:    stderr,
		entryByName: func(name string) (*addons.RegistryEntry, error) {
			e, ok := fl.entries[name]
			if !ok {
				return nil, nil
			}
			return e, nil
		},
		names: func() ([]string, error) {
			names := make([]string, 0, len(fl.entries))
			for n := range fl.entries {
				names = append(names, n)
			}
			// Sort for deterministic output.
			for i := 0; i < len(names); i++ {
				for j := i + 1; j < len(names); j++ {
					if names[j] < names[i] {
						names[i], names[j] = names[j], names[i]
					}
				}
			}
			return names, nil
		},
	}
	return env, fl, stdout, stderr
}

// --- list -------------------------------------------------------------------

func TestRunList_Empty(t *testing.T) {
	env, _, stdout, _ := testEnv("")
	code := runList(context.Background(), env)
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	if !strings.Contains(stdout.String(), "no addons installed") {
		t.Fatalf("expected empty message, got %q", stdout.String())
	}
}

func TestRunList_WithEntries(t *testing.T) {
	env, fl, stdout, _ := testEnv("")
	fl.entries["webtop"] = &addons.RegistryEntry{
		Version: "0.1.0", State: addons.StateEnabled,
		InstalledCommit: "aaaaaaaaaaaa1111", Source: "https://example/webtop",
	}
	fl.entries["jupyter"] = &addons.RegistryEntry{
		Version: "0.2.0", State: addons.StateInstalled,
		InstalledCommit: "bbbbbbbbbbbb2222", Source: "https://example/jupyter",
	}
	code := runList(context.Background(), env)
	if code != 0 {
		t.Fatalf("exit=%d", code)
	}
	out := stdout.String()
	for _, needle := range []string{"NAME", "VERSION", "STATE", "COMMIT", "SOURCE", "webtop", "jupyter", "enabled", "installed"} {
		if !strings.Contains(out, needle) {
			t.Fatalf("missing %q in output:\n%s", needle, out)
		}
	}
}

// --- status -----------------------------------------------------------------

func TestRunStatus_MissingArg(t *testing.T) {
	env, _, _, stderr := testEnv("")
	if code := runStatus(context.Background(), env, nil); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("missing usage: %q", stderr.String())
	}
}

func TestRunStatus_UnknownName(t *testing.T) {
	env, _, _, stderr := testEnv("")
	if code := runStatus(context.Background(), env, []string{"ghost"}); code != 1 {
		t.Fatalf("want 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "is not installed") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunStatus_KnownName(t *testing.T) {
	env, fl, stdout, _ := testEnv("")
	fl.entries["demo"] = &addons.RegistryEntry{
		Version: "1.0.0", State: addons.StateInstalled,
		InstalledCommit: "deadbeef", Source: "https://example/demo",
	}
	// Manifest read will fail because fakeLifecycle.AddonDir points at a
	// non-existent /tmp/fake-addons/... path. The handler must still
	// print the registry report and not crash.
	code := runStatus(context.Background(), env, []string{"demo"})
	if code != 0 {
		t.Fatalf("want 0, got %d", code)
	}
	out := stdout.String()
	if !strings.Contains(out, "Addon: demo") {
		t.Fatalf("missing header: %q", out)
	}
	if !strings.Contains(out, "deadbeef") {
		t.Fatalf("missing commit: %q", out)
	}
}

// --- install ----------------------------------------------------------------

func TestRunInstall_NoArgs(t *testing.T) {
	env, _, _, _ := testEnv("")
	if code := runInstall(context.Background(), env, nil); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
}

func TestRunInstall_Help(t *testing.T) {
	env, _, stdout, _ := testEnv("")
	if code := runInstall(context.Background(), env, []string{"--help"}); code != 0 {
		t.Fatalf("want 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("no usage: %q", stdout.String())
	}
}

func TestRunInstall_ConflictingFlags(t *testing.T) {
	env, _, _, stderr := testEnv("")
	args := []string{"https://example/repo", "--commit", "abc", "--version", "v1"}
	if code := runInstall(context.Background(), env, args); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "at most one") {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

func TestRunInstall_AbortedAtPrompt(t *testing.T) {
	env, fl, stdout, _ := testEnv("n\n")
	args := []string{"https://example/repo", "--commit", "abc"}
	code := runInstall(context.Background(), env, args)
	if code != 1 {
		t.Fatalf("want 1, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Aborted") {
		t.Fatalf("expected abort message: %q", stdout.String())
	}
	if len(fl.installCalls) != 0 {
		t.Fatalf("install should not have been called, got %d calls", len(fl.installCalls))
	}
}

func TestRunInstall_Success(t *testing.T) {
	env, fl, stdout, _ := testEnv("")
	args := []string{"https://example/repo", "--commit", "abc", "--name", "demo", "--yes"}
	code := runInstall(context.Background(), env, args)
	if code != 0 {
		t.Fatalf("want 0, got %d", code)
	}
	if len(fl.installCalls) != 1 {
		t.Fatalf("expected 1 install call, got %d", len(fl.installCalls))
	}
	call := fl.installCalls[0]
	if call.Source != "https://example/repo" || call.Name != "demo" {
		t.Fatalf("bad install call: %+v", call)
	}
	if call.Ref.Commit != "abc" {
		t.Fatalf("bad ref: %+v", call.Ref)
	}
	if !strings.Contains(stdout.String(), "Next:") {
		t.Fatalf("missing next hint: %q", stdout.String())
	}
}

func TestRunInstall_FailureWithHint(t *testing.T) {
	env, fl, _, stderr := testEnv("")
	fl.failWith = errors.New("addon \"webtop\" requires \"docker\" on PATH; install it and retry")
	args := []string{"https://example/repo", "--yes"}
	if code := runInstall(context.Background(), env, args); code != 1 {
		t.Fatalf("want 1, got %d", code)
	}
	e := stderr.String()
	if !strings.Contains(e, "Install failed") {
		t.Fatalf("missing error: %q", e)
	}
	if !strings.Contains(e, "Hint:") {
		t.Fatalf("missing hint: %q", e)
	}
}

func TestPrintInstallFailureHint_Classifier(t *testing.T) {
	cases := map[string]string{
		"addon requires docker on PATH; install":    "install the missing binary",
		"no signed tags found":                      "pin explicitly",
		"git not found on PATH":                     "install git",
		"addon is already installed (state=enabled)": "sciclaw addon upgrade",
		"does not support platform \"windows\"":     "addon.json requires.platform",
	}
	for errMsg, want := range cases {
		buf := &bytes.Buffer{}
		printInstallFailureHint(buf, errors.New(errMsg))
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("error %q → hint %q, want substring %q", errMsg, buf.String(), want)
		}
	}
}

// --- enable / disable -------------------------------------------------------

func TestRunEnable_NoArgs(t *testing.T) {
	env, _, _, _ := testEnv("")
	if code := runEnable(context.Background(), env, nil); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
}

func TestRunEnable_Success(t *testing.T) {
	env, fl, stdout, _ := testEnv("")
	fl.entries["demo"] = &addons.RegistryEntry{
		Version: "1.0.0", State: addons.StateInstalled,
		InstalledCommit: "commitabcdef00",
	}
	if code := runEnable(context.Background(), env, []string{"demo"}); code != 0 {
		t.Fatalf("want 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "enabled") {
		t.Fatalf("missing success msg: %q", stdout.String())
	}
	if !fl.enabled["demo"] {
		t.Fatal("fake should mark enabled")
	}
}

func TestRunEnable_IntegrityHint(t *testing.T) {
	env, fl, _, stderr := testEnv("")
	// Seed entry so fake's Enable path returns an error path. We need a
	// custom failure — override via a wrapper fake.
	fl.entries["demo"] = &addons.RegistryEntry{State: addons.StateInstalled}
	// Replace the lifecycle so Enable returns an integrity error.
	env.Lifecycle = &failingEnable{fake: fl}
	code := runEnable(context.Background(), env, []string{"demo"})
	if code != 1 {
		t.Fatalf("want 1, got %d", code)
	}
	e := stderr.String()
	if !strings.Contains(e, "Hint:") || !strings.Contains(e, "verify") || !strings.Contains(e, "upgrade") {
		t.Fatalf("missing integrity hint: %q", e)
	}
}

// failingEnable wraps fakeLifecycle but returns an integrity-style error
// from Enable so the runEnable hint-branch is covered.
type failingEnable struct{ fake *fakeLifecycle }

func (f *failingEnable) Install(ctx context.Context, opts addons.InstallOptions) (*addons.RegistryEntry, error) {
	return f.fake.Install(ctx, opts)
}
func (f *failingEnable) Enable(ctx context.Context, name string) (*addons.RegistryEntry, error) {
	return nil, errors.New("integrity check failed: commit drift expected X actual Y")
}
func (f *failingEnable) Disable(ctx context.Context, name string) (*addons.RegistryEntry, error) {
	return f.fake.Disable(ctx, name)
}
func (f *failingEnable) Uninstall(ctx context.Context, name string, force bool) error {
	return f.fake.Uninstall(ctx, name, force)
}
func (f *failingEnable) Upgrade(ctx context.Context, name string, ref addons.InstallRef) (*addons.RegistryEntry, error) {
	return f.fake.Upgrade(ctx, name, ref)
}
func (f *failingEnable) List(ctx context.Context) ([]*addons.RegistryEntry, error) {
	return f.fake.List(ctx)
}
func (f *failingEnable) AddonDir(name string) string { return f.fake.AddonDir(name) }

func TestRunDisable_NoArgs(t *testing.T) {
	env, _, _, _ := testEnv("")
	if code := runDisable(context.Background(), env, nil); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
}

func TestRunDisable_Success(t *testing.T) {
	env, fl, stdout, _ := testEnv("")
	fl.entries["demo"] = &addons.RegistryEntry{Version: "1.0.0", State: addons.StateEnabled}
	if code := runDisable(context.Background(), env, []string{"demo"}); code != 0 {
		t.Fatalf("want 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "disabled") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

// --- uninstall --------------------------------------------------------------

func TestRunUninstall_NoArgs(t *testing.T) {
	env, _, _, _ := testEnv("")
	if code := runUninstall(context.Background(), env, nil); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
}

func TestRunUninstall_Force(t *testing.T) {
	env, fl, stdout, _ := testEnv("")
	fl.entries["demo"] = &addons.RegistryEntry{Version: "1.0.0", State: addons.StateInstalled}
	if code := runUninstall(context.Background(), env, []string{"demo", "--force"}); code != 0 {
		t.Fatalf("want 0, got %d", code)
	}
	if len(fl.uninstallCalls) != 1 || !fl.uninstallCalls[0].Force {
		t.Fatalf("unexpected uninstall calls: %+v", fl.uninstallCalls)
	}
	if !strings.Contains(stdout.String(), "uninstalled") {
		t.Fatalf("missing success: %q", stdout.String())
	}
}

func TestRunUninstall_ConfirmByType(t *testing.T) {
	env, fl, stdout, _ := testEnv("demo\n")
	fl.entries["demo"] = &addons.RegistryEntry{Version: "1.0.0", State: addons.StateInstalled}
	if code := runUninstall(context.Background(), env, []string{"demo"}); code != 0 {
		t.Fatalf("want 0, got %d", code)
	}
	if len(fl.uninstallCalls) != 1 {
		t.Fatalf("expected 1 uninstall call, got %d", len(fl.uninstallCalls))
	}
	if !strings.Contains(stdout.String(), "uninstalled") {
		t.Fatalf("missing success: %q", stdout.String())
	}
}

func TestRunUninstall_AbortedByMismatch(t *testing.T) {
	env, fl, stdout, _ := testEnv("wrong\n")
	fl.entries["demo"] = &addons.RegistryEntry{Version: "1.0.0", State: addons.StateInstalled}
	if code := runUninstall(context.Background(), env, []string{"demo"}); code != 1 {
		t.Fatalf("want 1, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Aborted") {
		t.Fatalf("expected abort: %q", stdout.String())
	}
	if len(fl.uninstallCalls) != 0 {
		t.Fatalf("should not have called uninstall: %+v", fl.uninstallCalls)
	}
}

func TestRunUninstall_UnknownFlag(t *testing.T) {
	env, _, _, stderr := testEnv("")
	if code := runUninstall(context.Background(), env, []string{"demo", "--bogus"}); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unknown flag") {
		t.Fatalf("missing unknown flag error: %q", stderr.String())
	}
}

func TestRunUninstall_Help(t *testing.T) {
	env, _, stdout, _ := testEnv("")
	if code := runUninstall(context.Background(), env, []string{"demo", "--help"}); code != 0 {
		t.Fatalf("want 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("missing usage: %q", stdout.String())
	}
}

// --- upgrade ----------------------------------------------------------------

func TestRunUpgrade_NoArgs(t *testing.T) {
	env, _, _, _ := testEnv("")
	if code := runUpgrade(context.Background(), env, nil); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
}

func TestRunUpgrade_Help(t *testing.T) {
	env, _, stdout, _ := testEnv("")
	if code := runUpgrade(context.Background(), env, []string{"--help"}); code != 0 {
		t.Fatalf("want 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("missing usage: %q", stdout.String())
	}
}

func TestRunUpgrade_Unknown(t *testing.T) {
	env, _, _, stderr := testEnv("")
	if code := runUpgrade(context.Background(), env, []string{"ghost", "--yes"}); code != 1 {
		t.Fatalf("want 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "is not installed") {
		t.Fatalf("expected not-installed: %q", stderr.String())
	}
}

func TestRunUpgrade_Success(t *testing.T) {
	env, fl, stdout, _ := testEnv("")
	fl.entries["demo"] = &addons.RegistryEntry{
		Version: "0.1.0", State: addons.StateInstalled, InstalledCommit: "old",
	}
	if code := runUpgrade(context.Background(), env, []string{"demo", "--commit", "new", "--yes"}); code != 0 {
		t.Fatalf("want 0, got %d", code)
	}
	if len(fl.upgradeCalls) != 1 || fl.upgradeCalls[0].Ref.Commit != "new" {
		t.Fatalf("bad upgrade calls: %+v", fl.upgradeCalls)
	}
	if !strings.Contains(stdout.String(), "Upgrade diff:") {
		t.Fatalf("missing diff: %q", stdout.String())
	}
}

func TestRunUpgrade_AlreadyAtCommit(t *testing.T) {
	env, fl, stdout, _ := testEnv("")
	fl.entries["demo"] = &addons.RegistryEntry{
		Version: "0.1.0", State: addons.StateInstalled, InstalledCommit: "pinned",
	}
	if code := runUpgrade(context.Background(), env, []string{"demo", "--commit", "pinned", "--yes"}); code != 0 {
		t.Fatalf("want 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "already at pinned") {
		t.Fatalf("missing already-at message: %q", stdout.String())
	}
}

func TestRunUpgrade_AbortAtPrompt(t *testing.T) {
	env, fl, stdout, _ := testEnv("n\n")
	fl.entries["demo"] = &addons.RegistryEntry{
		Version: "0.1.0", State: addons.StateInstalled, InstalledCommit: "old",
	}
	if code := runUpgrade(context.Background(), env, []string{"demo", "--commit", "new"}); code != 1 {
		t.Fatalf("want 1, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Aborted") {
		t.Fatalf("expected abort: %q", stdout.String())
	}
	if len(fl.upgradeCalls) != 0 {
		t.Fatalf("upgrade should not have been called")
	}
}

func TestRunUpgrade_ConflictingFlags(t *testing.T) {
	env, _, _, stderr := testEnv("")
	args := []string{"demo", "--commit", "abc", "--version", "v1"}
	if code := runUpgrade(context.Background(), env, args); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "at most one") {
		t.Fatalf("missing conflict error: %q", stderr.String())
	}
}

// --- verify -----------------------------------------------------------------

func TestRunVerify_NoArgs(t *testing.T) {
	env, _, _, _ := testEnv("")
	if code := runVerify(context.Background(), env, nil); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
}

func TestRunVerify_Unknown(t *testing.T) {
	env, _, _, stderr := testEnv("")
	if code := runVerify(context.Background(), env, []string{"ghost"}); code != 1 {
		t.Fatalf("want 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "is not installed") {
		t.Fatalf("missing not-installed: %q", stderr.String())
	}
}

// --- rollback ---------------------------------------------------------------

func TestRunRollback_NoArgs(t *testing.T) {
	env, _, _, _ := testEnv("")
	if code := runRollback(context.Background(), env, nil); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
}

func TestRunRollback_Unknown(t *testing.T) {
	env, _, _, stderr := testEnv("")
	if code := runRollback(context.Background(), env, []string{"ghost"}); code != 1 {
		t.Fatalf("want 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "is not installed") {
		t.Fatalf("missing not-installed: %q", stderr.String())
	}
}

func TestRunRollback_NoPreviousCommit(t *testing.T) {
	env, fl, _, stderr := testEnv("")
	fl.entries["demo"] = &addons.RegistryEntry{Version: "1.0.0", State: addons.StateInstalled}
	if code := runRollback(context.Background(), env, []string{"demo"}); code != 1 {
		t.Fatalf("want 1, got %d", code)
	}
	if !strings.Contains(stderr.String(), "no previous_commit") {
		t.Fatalf("missing no-previous-commit msg: %q", stderr.String())
	}
}

func TestRunRollback_Aborted(t *testing.T) {
	env, fl, stdout, _ := testEnv("n\n")
	prev := "prev-commit"
	fl.entries["demo"] = &addons.RegistryEntry{
		Version: "1.0.0", State: addons.StateInstalled,
		InstalledCommit: "curr-commit", PreviousCommit: &prev,
	}
	if code := runRollback(context.Background(), env, []string{"demo"}); code != 1 {
		t.Fatalf("want 1, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Aborted") {
		t.Fatalf("expected abort: %q", stdout.String())
	}
}

func TestRunRollback_UnknownFlag(t *testing.T) {
	env, _, _, stderr := testEnv("")
	if code := runRollback(context.Background(), env, []string{"demo", "--bogus"}); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unknown flag") {
		t.Fatalf("missing unknown-flag: %q", stderr.String())
	}
}

func TestRunRollback_Help(t *testing.T) {
	env, _, stdout, _ := testEnv("")
	if code := runRollback(context.Background(), env, []string{"demo", "--help"}); code != 0 {
		t.Fatalf("want 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("missing usage: %q", stdout.String())
	}
}

// --- sbom -------------------------------------------------------------------

func TestRunSBOM_NoArgs(t *testing.T) {
	env, _, _, _ := testEnv("")
	if code := runSBOM(context.Background(), env, nil); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
}

func TestRunSBOM_MissingOutputValue(t *testing.T) {
	env, _, _, stderr := testEnv("")
	if code := runSBOM(context.Background(), env, []string{"demo", "--output"}); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "requires a value") {
		t.Fatalf("missing error: %q", stderr.String())
	}
}

func TestRunSBOM_UnknownFlag(t *testing.T) {
	env, _, _, stderr := testEnv("")
	if code := runSBOM(context.Background(), env, []string{"demo", "--bogus"}); code != 2 {
		t.Fatalf("want 2, got %d", code)
	}
	if !strings.Contains(stderr.String(), "unknown flag") {
		t.Fatalf("missing error: %q", stderr.String())
	}
}

func TestRunSBOM_Help(t *testing.T) {
	env, _, stdout, _ := testEnv("")
	if code := runSBOM(context.Background(), env, []string{"demo", "--help"}); code != 0 {
		t.Fatalf("want 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "Usage:") {
		t.Fatalf("missing usage: %q", stdout.String())
	}
}

// --- help -------------------------------------------------------------------

func TestAddonHelp(t *testing.T) {
	buf := &bytes.Buffer{}
	addonHelp(buf)
	out := buf.String()
	required := []string{"list", "status", "install", "enable", "disable", "uninstall", "upgrade", "verify", "rollback", "sbom", "--commit", "--version", "--track"}
	for _, s := range required {
		if !strings.Contains(out, s) {
			t.Fatalf("help missing %q:\n%s", s, out)
		}
	}
}

func TestAddonInstallHelp(t *testing.T) {
	buf := &bytes.Buffer{}
	addonInstallHelp(buf)
	if !strings.Contains(buf.String(), "Usage:") || !strings.Contains(buf.String(), "--commit") {
		t.Fatalf("missing content: %q", buf.String())
	}
}

func TestAddonUpgradeHelp(t *testing.T) {
	buf := &bytes.Buffer{}
	addonUpgradeHelp(buf)
	if !strings.Contains(buf.String(), "Usage:") || !strings.Contains(buf.String(), "--commit") {
		t.Fatalf("missing content: %q", buf.String())
	}
}

// --- gitCloneFn edge cases --------------------------------------------------

func TestGitCloneFn_EmptyArgs(t *testing.T) {
	if err := gitCloneFn(context.Background(), "", "/tmp/x"); err == nil {
		t.Fatal("expected error for empty URL")
	}
	if err := gitCloneFn(context.Background(), "https://x", ""); err == nil {
		t.Fatal("expected error for empty dest")
	}
}

// --- sciclawHomeDir smoke ---------------------------------------------------

func TestSciclawHomeDir_Nonempty(t *testing.T) {
	if dir := sciclawHomeDir(); dir == "" || dir == "/" {
		t.Fatalf("unexpected home dir: %q", dir)
	}
}
