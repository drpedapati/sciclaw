package addons

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// captureRunner is a CommandRunner that records every call it receives and
// shells out to git when the script is a git command, so the state machine's
// git checkout / git fetch calls actually advance a real test repo.
type captureRunner struct {
	mu    sync.Mutex
	calls []runnerCall
	// optional fail predicate: if non-nil and returns true, Run returns err.
	failIf func(dir, script string) error
}

type runnerCall struct {
	Dir    string
	Script string
	Env    []string
}

func (c *captureRunner) Run(ctx context.Context, dir, script string, env []string) ([]byte, error) {
	c.mu.Lock()
	c.calls = append(c.calls, runnerCall{Dir: dir, Script: script, Env: env})
	c.mu.Unlock()
	if c.failIf != nil {
		if err := c.failIf(dir, script); err != nil {
			return nil, err
		}
	}
	// Execute git commands for real so the test repo state tracks the
	// lifecycle's intent. Bootstrap scripts are no-ops.
	if strings.HasPrefix(script, "git ") {
		parts := strings.Fields(script)
		cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		return cmd.CombinedOutput()
	}
	return nil, nil
}

// lifecycleRepo seeds a real git repo with a manifest and a test bootstrap
// script, then returns its path so the fake Clone function can copy it into
// place. The default bootstrap script drops a marker file in $ADDON_DIR so
// tests can verify it actually ran.
func lifecycleRepo(t *testing.T) string {
	return lifecycleRepoWithBootstrap(t, "#!/bin/sh\ntouch \"$ADDON_DIR/.bootstrap-ran\"\n")
}

// lifecycleRepoWithBootstrap is like lifecycleRepo but lets the caller
// supply a custom install.sh body — used by tests that need a failing
// bootstrap or a script that records different state.
func lifecycleRepoWithBootstrap(t *testing.T, bootstrap string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.email", "t@example.com")
	git("config", "user.name", "t")
	git("config", "commit.gpgsign", "false")
	git("config", "tag.gpgsign", "false")

	writeManifest(t, dir, "0.1.0", `["routing_changed"]`)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "install.sh"), []byte(bootstrap), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin", "test-sidecar"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "first")
	git("tag", "v0.1.0")

	// Second commit so upgrade has somewhere to go.
	writeManifest(t, dir, "0.2.0", `["routing_changed"]`)
	git("add", ".")
	git("commit", "-q", "-m", "second")
	git("tag", "v0.2.0")

	return dir
}

func writeManifest(t *testing.T, dir, version, hooks string) {
	t.Helper()
	body := fmt.Sprintf(`{
  "name": "testaddon",
  "version": "%s",
  "requires": {"sciclaw": ">=0.1.0", "runtime": [], "platform": ["darwin","linux","windows"]},
  "sidecar": {"binary": "test-sidecar"},
  "provides": {"hooks": %s},
  "bootstrap": {"install": "./bin/install.sh", "uninstall": "./bin/install.sh"}
}`, version, hooks)
	if err := os.WriteFile(filepath.Join(dir, "addon.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// fakeClone copies the seed repo into dest — the tests use this to mock out
// `git clone` without touching the network.
func fakeClone(seed string) func(context.Context, string, string) error {
	return func(ctx context.Context, repoURL, dest string) error {
		return copyTree(seed, dest)
	}
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode())
	})
}

func newTestLifecycle(t *testing.T, runner *captureRunner) *Lifecycle {
	t.Helper()
	home := t.TempDir()
	store := NewStore(home)
	l := New(store, home, "0.2.0", "linux")
	l.LookPath = func(string) (string, error) { return "/usr/bin/sh", nil }
	l.Runner = runner
	l.Now = func() time.Time { return time.Date(2026, 4, 13, 14, 22, 0, 0, time.UTC) }
	return l
}

func TestLifecycle_InstallHappyPath(t *testing.T) {
	seed := lifecycleRepo(t)
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.Clone = fakeClone(seed)

	entry, err := l.Install(context.Background(), InstallOptions{
		Source: "https://example.com/testaddon",
		Ref:    NewAutoRef(),
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if entry.State != StateInstalled {
		t.Errorf("state = %s, want installed", entry.State)
	}
	if entry.InstalledCommit == "" {
		t.Error("InstalledCommit not set")
	}
	if entry.SignedTag == nil || *entry.SignedTag != "v0.2.0" {
		t.Errorf("SignedTag = %v, want v0.2.0", entry.SignedTag)
	}
	if entry.ManifestSHA256 == "" {
		t.Error("ManifestSHA256 not set")
	}
	if _, err := os.Stat(l.AddonDir("testaddon")); err != nil {
		t.Errorf("addon dir missing: %v", err)
	}

	// Registry round-trips.
	got, err := l.Store.Get("testaddon")
	if err != nil || got == nil {
		t.Fatalf("Store.Get: err=%v entry=%v", err, got)
	}
	if got.InstalledAt != "2026-04-13T14:22:00Z" {
		t.Errorf("InstalledAt = %q", got.InstalledAt)
	}

	// Bootstrap install script left its marker file, proving it actually
	// ran via direct exec with $ADDON_DIR in the environment.
	marker := filepath.Join(l.AddonDir("testaddon"), ".bootstrap-ran")
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("bootstrap marker %s missing: %v", marker, err)
	}
}

func TestLifecycle_InstallFailsOnRequirementMismatch(t *testing.T) {
	seed := lifecycleRepo(t)
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.SciclawVers = "0.0.1" // below manifest's >=0.1.0
	l.Clone = fakeClone(seed)

	_, err := l.Install(context.Background(), InstallOptions{
		Source: "https://example.com/testaddon",
		Ref:    NewVersionRef("v0.1.0"),
	})
	if err == nil || !strings.Contains(err.Error(), "requires sciclaw") {
		t.Errorf("expected version mismatch error, got %v", err)
	}
	// Staging dir should have been cleaned up.
	if _, err := os.Stat(l.AddonDir("testaddon")); err == nil {
		t.Error("addon dir should not exist after failed install")
	}
}

func TestLifecycle_InstallFailsIfAlreadyInstalled(t *testing.T) {
	seed := lifecycleRepo(t)
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.Clone = fakeClone(seed)
	if _, err := l.Install(context.Background(), InstallOptions{Source: "https://example.com/testaddon", Ref: NewAutoRef()}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	_, err := l.Install(context.Background(), InstallOptions{Source: "https://example.com/testaddon", Ref: NewAutoRef()})
	if err == nil || !strings.Contains(err.Error(), "already installed") {
		t.Errorf("expected already-installed error, got %v", err)
	}
}

func TestLifecycle_InstallNameMismatch(t *testing.T) {
	seed := lifecycleRepo(t)
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.Clone = fakeClone(seed)
	_, err := l.Install(context.Background(), InstallOptions{
		Name:   "wrongname",
		Source: "https://example.com/testaddon",
		Ref:    NewAutoRef(),
	})
	if err == nil || !strings.Contains(err.Error(), "name mismatch") {
		t.Errorf("expected name mismatch, got %v", err)
	}
}

func TestLifecycle_EnableDisableRoundTrip(t *testing.T) {
	seed := lifecycleRepo(t)
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.Clone = fakeClone(seed)
	if _, err := l.Install(context.Background(), InstallOptions{Source: "https://example.com/testaddon", Ref: NewAutoRef()}); err != nil {
		t.Fatalf("install: %v", err)
	}

	enabled, err := l.Enable(context.Background(), "testaddon")
	if err != nil {
		t.Fatalf("enable: %v", err)
	}
	if enabled.State != StateEnabled {
		t.Errorf("state = %s, want enabled", enabled.State)
	}

	// Second enable is a no-op but must not clobber other fields.
	again, err := l.Enable(context.Background(), "testaddon")
	if err != nil {
		t.Fatalf("enable idempotent: %v", err)
	}
	if again.InstalledCommit != enabled.InstalledCommit {
		t.Error("idempotent enable changed InstalledCommit")
	}

	disabled, err := l.Disable(context.Background(), "testaddon")
	if err != nil {
		t.Fatalf("disable: %v", err)
	}
	if disabled.State != StateInstalled {
		t.Errorf("state = %s, want installed", disabled.State)
	}
	if disabled.InstalledCommit != enabled.InstalledCommit {
		t.Error("disable mutated InstalledCommit")
	}
	if disabled.ManifestSHA256 != enabled.ManifestSHA256 {
		t.Error("disable mutated ManifestSHA256")
	}

	reenabled, err := l.Enable(context.Background(), "testaddon")
	if err != nil {
		t.Fatalf("re-enable: %v", err)
	}
	if reenabled.State != StateEnabled {
		t.Errorf("re-enable state = %s", reenabled.State)
	}
}

func TestLifecycle_UninstallBlocksWhenEnabledUnlessForce(t *testing.T) {
	seed := lifecycleRepo(t)
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.Clone = fakeClone(seed)
	if _, err := l.Install(context.Background(), InstallOptions{Source: "https://example.com/testaddon", Ref: NewAutoRef()}); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Enable(context.Background(), "testaddon"); err != nil {
		t.Fatal(err)
	}

	err := l.Uninstall(context.Background(), "testaddon", false)
	if err == nil || !strings.Contains(err.Error(), "disable") {
		t.Errorf("expected refusal to uninstall enabled addon, got %v", err)
	}

	if err := l.Uninstall(context.Background(), "testaddon", true); err != nil {
		t.Errorf("force uninstall: %v", err)
	}
	if _, err := os.Stat(l.AddonDir("testaddon")); err == nil {
		t.Error("addon dir should be gone after uninstall")
	}
	got, _ := l.Store.Get("testaddon")
	if got != nil {
		t.Errorf("registry entry should be gone, got %+v", got)
	}
}

func TestLifecycle_UninstallHappyPathRunsHook(t *testing.T) {
	// Custom bootstrap records a marker in a persistent location (outside
	// the addon dir so it survives RemoveAll) whenever it runs. We assert
	// that running Uninstall caused a second invocation.
	markerDir := t.TempDir()
	body := fmt.Sprintf("#!/bin/sh\necho \"$ADDON_NAME\" >> %s/runs\n", markerDir)
	seed := lifecycleRepoWithBootstrap(t, body)
	l := newTestLifecycle(t, &captureRunner{})
	l.Clone = fakeClone(seed)
	if _, err := l.Install(context.Background(), InstallOptions{Source: "https://example.com/testaddon", Ref: NewAutoRef()}); err != nil {
		t.Fatal(err)
	}
	if err := l.Uninstall(context.Background(), "testaddon", false); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(markerDir, "runs"))
	if err != nil {
		t.Fatalf("reading marker: %v", err)
	}
	runs := strings.Count(string(data), "testaddon")
	if runs < 2 {
		t.Errorf("expected install + uninstall bootstrap to run (2 invocations), got %d", runs)
	}
}

func TestLifecycle_UpgradeAdvancesCommit(t *testing.T) {
	seed := lifecycleRepo(t)
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.Clone = fakeClone(seed)

	// Install pinned to v0.1.0 so an upgrade to auto has somewhere to go.
	if _, err := l.Install(context.Background(), InstallOptions{
		Source: "https://example.com/testaddon",
		Ref:    NewVersionRef("v0.1.0"),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	prev, _ := l.Store.Get("testaddon")

	// Override SignedTag to nil so Upgrade with zero ref re-requires explicit ref.
	// Here we pass v0.2.0 explicitly.
	updated, err := l.Upgrade(context.Background(), "testaddon", NewVersionRef("v0.2.0"))
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if updated.InstalledCommit == prev.InstalledCommit {
		t.Error("upgrade did not advance InstalledCommit")
	}
	if updated.PreviousCommit == nil || *updated.PreviousCommit != prev.InstalledCommit {
		t.Errorf("PreviousCommit = %v, want %s", updated.PreviousCommit, prev.InstalledCommit)
	}
	if updated.Version != "0.2.0" {
		t.Errorf("Version = %q, want 0.2.0", updated.Version)
	}
}

// TestLifecycle_ConcurrentInstallSameNameSerializes is the H2
// regression. Two concurrent Install calls for the same name used to
// race on Store.Get/Set — both could pass the existence check, both
// clone, both rename into finalDir (last wins), and both persist a
// registry entry. The flock taken inside Install now serializes them:
// the first wins outright, the second observes "already installed".
func TestLifecycle_ConcurrentInstallSameNameSerializes(t *testing.T) {
	seed := lifecycleRepo(t)
	// Two separate Lifecycles pointing at the same SciclawHome so they
	// share the same addons/<name>.lock file. This mirrors the real
	// scenario: two CLI processes on the same host.
	home := t.TempDir()
	newL := func() *Lifecycle {
		runner := &captureRunner{}
		store := NewStore(home)
		l := New(store, home, "0.2.0", "linux")
		l.LookPath = func(string) (string, error) { return "/usr/bin/sh", nil }
		l.Runner = runner
		// Use real time so each goroutine gets a unique staging dir
		// (the staging name includes UnixNano).
		l.Now = time.Now
		l.Clone = fakeClone(seed)
		return l
	}

	var wg sync.WaitGroup
	results := make([]error, 2)
	start := make(chan struct{})
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			l := newL()
			<-start
			_, err := l.Install(context.Background(), InstallOptions{
				Source: "https://example.com/testaddon",
				Ref:    NewAutoRef(),
			})
			results[idx] = err
		}(i)
	}
	close(start)
	wg.Wait()

	// Exactly one Install should succeed, the other should fail with
	// "already installed".
	var ok, dup int
	for _, e := range results {
		switch {
		case e == nil:
			ok++
		case strings.Contains(e.Error(), "already installed"):
			dup++
		default:
			t.Errorf("unexpected install error: %v", e)
		}
	}
	if ok != 1 || dup != 1 {
		t.Errorf("expected 1 success + 1 already-installed, got ok=%d dup=%d results=%v", ok, dup, results)
	}
	// Exactly one registry entry should remain.
	store := NewStore(home)
	names, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "testaddon" {
		t.Errorf("expected [testaddon], got %v", names)
	}
}

// TestLifecycle_UpgradeStoreSetFailureRollsBackWorkingTree is the H5
// regression. After Upgrade checks out the new commit, if Store.Set
// fails, the working tree used to be left at the new commit while the
// persisted registry still named the old one — producing an "integrity
// check failed" state on next startup. The fix rolls the working tree
// back to the previous commit before returning the error.
func TestLifecycle_UpgradeStoreSetFailureRollsBackWorkingTree(t *testing.T) {
	seed := lifecycleRepo(t)
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.Clone = fakeClone(seed)

	if _, err := l.Install(context.Background(), InstallOptions{
		Source: "https://example.com/testaddon",
		Ref:    NewVersionRef("v0.1.0"),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	prev, _ := l.Store.Get("testaddon")
	addonDir := l.AddonDir("testaddon")

	// Force Store.Set to fail during Upgrade by making the directory
	// that holds registry.json read-only. Save writes to a .tmp sibling
	// and then os.Rename's it; the rename returns EACCES/EPERM on a
	// read-only parent.
	registryDir := filepath.Dir(l.Store.Path())
	if err := os.Chmod(registryDir, 0o555); err != nil {
		t.Fatalf("chmod ro: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(registryDir, 0o755) })

	_, err := l.Upgrade(context.Background(), "testaddon", NewVersionRef("v0.2.0"))
	if err == nil {
		t.Fatal("expected upgrade to return error when Store.Set fails")
	}
	if !strings.Contains(err.Error(), "saving registry") {
		t.Errorf("error should mention saving registry, got %v", err)
	}
	// The critical assertion: the working tree has been reverted to the
	// previous commit, so disk state and registry agree.
	headCmd := exec.Command("git", "-C", addonDir, "rev-parse", "HEAD")
	out, gerr := headCmd.Output()
	if gerr != nil {
		t.Fatalf("rev-parse after failed upgrade: %v", gerr)
	}
	head := strings.TrimSpace(string(out))
	if head != prev.InstalledCommit {
		t.Errorf("working tree at %s after failed upgrade, want rollback to %s", head, prev.InstalledCommit)
	}
}

func TestLifecycle_UpgradeNoOpWhenAtCommit(t *testing.T) {
	seed := lifecycleRepo(t)
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.Clone = fakeClone(seed)
	if _, err := l.Install(context.Background(), InstallOptions{
		Source: "https://example.com/testaddon",
		Ref:    NewVersionRef("v0.2.0"),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := l.Upgrade(context.Background(), "testaddon", NewVersionRef("v0.2.0"))
	if !errors.Is(err, ErrAlreadyAtCommit) {
		t.Errorf("expected ErrAlreadyAtCommit, got %v", err)
	}
}

func TestLifecycle_UpgradeRequiresStrategyWhenRefZero(t *testing.T) {
	seed := lifecycleRepo(t)
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.Clone = fakeClone(seed)
	if _, err := l.Install(context.Background(), InstallOptions{
		Source: "https://example.com/testaddon",
		Ref:    NewCommitRef(headOf(t, seed)),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	_, err := l.Upgrade(context.Background(), "testaddon", InstallRef{})
	if err == nil || !strings.Contains(err.Error(), "no pinning strategy") {
		t.Errorf("expected no-strategy error, got %v", err)
	}
}

func TestLifecycle_EnableFailsOnIntegrityDrift(t *testing.T) {
	seed := lifecycleRepo(t)
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.Clone = fakeClone(seed)
	if _, err := l.Install(context.Background(), InstallOptions{
		Source: "https://example.com/testaddon",
		Ref:    NewVersionRef("v0.1.0"),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	// Tamper with the manifest after install.
	if err := os.WriteFile(filepath.Join(l.AddonDir("testaddon"), "addon.json"),
		[]byte(`{"name":"testaddon","version":"9.9.9","requires":{"sciclaw":">=0.1.0"},"sidecar":{"binary":"test-sidecar"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := l.Enable(context.Background(), "testaddon")
	if err == nil || !strings.Contains(err.Error(), "manifest_sha256") {
		t.Errorf("expected integrity drift error, got %v", err)
	}
	if err != nil && !strings.Contains(err.Error(), "upgrade") {
		t.Errorf("error should mention upgrade remediation, got %v", err)
	}
}

func TestLifecycle_EnableUnknownAddon(t *testing.T) {
	l := newTestLifecycle(t, &captureRunner{})
	_, err := l.Enable(context.Background(), "ghost")
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("expected not-installed error, got %v", err)
	}
}

func TestLifecycle_DisableUnknownAddon(t *testing.T) {
	l := newTestLifecycle(t, &captureRunner{})
	_, err := l.Disable(context.Background(), "ghost")
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("expected not-installed error, got %v", err)
	}
}

func TestLifecycle_List(t *testing.T) {
	seed := lifecycleRepo(t)
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.Clone = fakeClone(seed)
	if _, err := l.Install(context.Background(), InstallOptions{Source: "https://example.com/testaddon", Ref: NewAutoRef()}); err != nil {
		t.Fatal(err)
	}
	entries, err := l.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("len(entries) = %d, want 1", len(entries))
	}
}

func TestLifecycle_AddonDir(t *testing.T) {
	l := New(NewStore("/srv/sciclaw"), "/srv/sciclaw", "0.1.0", "linux")
	got := l.AddonDir("webtop")
	want := filepath.Join("/srv/sciclaw", "addons", "webtop")
	if got != want {
		t.Errorf("AddonDir = %q, want %q", got, want)
	}
}

func headOf(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse HEAD: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestLifecycle_InstallMissingSource(t *testing.T) {
	l := newTestLifecycle(t, &captureRunner{})
	_, err := l.Install(context.Background(), InstallOptions{})
	if err == nil || !strings.Contains(err.Error(), "source") {
		t.Errorf("expected missing-source error, got %v", err)
	}
}

func TestLifecycle_InstallBootstrapFailureRollsBack(t *testing.T) {
	// Bootstrap script exits non-zero — Install must roll back the addon
	// dir and not persist a registry entry.
	seed := lifecycleRepoWithBootstrap(t, "#!/bin/sh\nexit 1\n")
	l := newTestLifecycle(t, &captureRunner{})
	l.Clone = fakeClone(seed)

	_, err := l.Install(context.Background(), InstallOptions{
		Source: "https://example.com/testaddon",
		Ref:    NewAutoRef(),
	})
	if err == nil || !strings.Contains(err.Error(), "bootstrap") {
		t.Errorf("expected bootstrap failure, got %v", err)
	}
	if _, err := os.Stat(l.AddonDir("testaddon")); err == nil {
		t.Error("addon dir should be removed after bootstrap failure")
	}
	got, _ := l.Store.Get("testaddon")
	if got != nil {
		t.Errorf("registry entry should not have been written, got %+v", got)
	}
}

func TestLifecycle_InstallCheckoutFailure(t *testing.T) {
	seed := lifecycleRepo(t)
	runner := &captureRunner{
		failIf: func(dir, script string) error {
			if strings.HasPrefix(script, "git checkout") {
				return errors.New("checkout boom")
			}
			return nil
		},
	}
	l := newTestLifecycle(t, runner)
	l.Clone = fakeClone(seed)

	_, err := l.Install(context.Background(), InstallOptions{
		Source: "https://example.com/testaddon",
		Ref:    NewAutoRef(),
	})
	if err == nil || !strings.Contains(err.Error(), "checking out") {
		t.Errorf("expected checkout failure, got %v", err)
	}
}

func TestLifecycle_InstallCloneFailure(t *testing.T) {
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.Clone = func(ctx context.Context, src, dst string) error {
		return errors.New("network down")
	}
	_, err := l.Install(context.Background(), InstallOptions{
		Source: "https://example.com/testaddon",
		Ref:    NewAutoRef(),
	})
	if err == nil || !strings.Contains(err.Error(), "cloning") {
		t.Errorf("expected clone failure, got %v", err)
	}
}

func TestLifecycle_InstallWithExplicitNameMatching(t *testing.T) {
	seed := lifecycleRepo(t)
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.Clone = fakeClone(seed)
	entry, err := l.Install(context.Background(), InstallOptions{
		Name:   "testaddon",
		Source: "https://example.com/testaddon",
		Ref:    NewAutoRef(),
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if entry.Version != "0.2.0" {
		t.Errorf("Version = %q", entry.Version)
	}
}

func TestLifecycle_UpgradeViaTrack(t *testing.T) {
	seed := lifecycleRepo(t)
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.Clone = fakeClone(seed)
	// Install pinned to v0.1.0 with track=main so upgrade with zero ref
	// falls through to the track branch.
	if _, err := l.Install(context.Background(), InstallOptions{
		Source: "https://example.com/testaddon",
		Ref:    NewTrackRef("main"),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	prev, _ := l.Store.Get("testaddon")
	if prev.Track == nil || *prev.Track != "main" {
		t.Fatalf("Track = %v, want main", prev.Track)
	}

	// Because track installs pick local branch head, prev.InstalledCommit is
	// already at main's tip. Test the zero-ref fallback path by forcing a
	// git reset back to v0.1.0 first.
	runner.Run(context.Background(), l.AddonDir("testaddon"), "git reset --hard v0.1.0", nil)
	cmd := exec.Command("git", "-C", l.AddonDir("testaddon"), "branch", "-f", "main", "v0.2.0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("branch: %v\n%s", err, out)
	}
	// Re-save entry so store reflects the older commit as baseline.
	prev.InstalledCommit = func() string {
		c := exec.Command("git", "-C", l.AddonDir("testaddon"), "rev-parse", "HEAD")
		out, _ := c.Output()
		return strings.TrimSpace(string(out))
	}()
	if err := l.Store.Set("testaddon", prev); err != nil {
		t.Fatal(err)
	}

	updated, err := l.Upgrade(context.Background(), "testaddon", InstallRef{})
	if err != nil {
		t.Fatalf("upgrade via track: %v", err)
	}
	if updated.InstalledCommit == prev.InstalledCommit {
		t.Error("upgrade via track did not advance commit")
	}
	if updated.Track == nil || *updated.Track != "main" {
		t.Errorf("Track should be preserved, got %v", updated.Track)
	}
}

func TestLifecycle_UpgradeUnknownAddon(t *testing.T) {
	l := newTestLifecycle(t, &captureRunner{})
	_, err := l.Upgrade(context.Background(), "ghost", NewVersionRef("v0.1.0"))
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("expected not-installed error, got %v", err)
	}
}

func TestLifecycle_UpgradeFetchFailure(t *testing.T) {
	seed := lifecycleRepo(t)
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.Clone = fakeClone(seed)
	if _, err := l.Install(context.Background(), InstallOptions{
		Source: "https://example.com/testaddon",
		Ref:    NewVersionRef("v0.1.0"),
	}); err != nil {
		t.Fatal(err)
	}
	runner.failIf = func(dir, script string) error {
		if strings.HasPrefix(script, "git fetch") {
			return errors.New("network boom")
		}
		return nil
	}
	_, err := l.Upgrade(context.Background(), "testaddon", NewVersionRef("v0.2.0"))
	if err == nil || !strings.Contains(err.Error(), "git fetch") {
		t.Errorf("expected fetch failure, got %v", err)
	}
}

func TestLifecycle_UninstallForceMissingEntry(t *testing.T) {
	l := newTestLifecycle(t, &captureRunner{})
	if err := l.Uninstall(context.Background(), "ghost", true); err != nil {
		t.Errorf("force uninstall of missing entry should be no-op, got %v", err)
	}
}

func TestLifecycle_UninstallMissingEntryNoForce(t *testing.T) {
	l := newTestLifecycle(t, &captureRunner{})
	err := l.Uninstall(context.Background(), "ghost", false)
	if err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("expected not-installed error, got %v", err)
	}
}

func TestLifecycle_NowFallsBackToWallClock(t *testing.T) {
	l := New(NewStore(t.TempDir()), t.TempDir(), "0.1.0", "linux")
	got := l.now()
	if got.IsZero() {
		t.Error("now should return non-zero when Now is unset")
	}
}
