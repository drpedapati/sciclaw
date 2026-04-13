package addons

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// fakeLauncher is a SidecarLauncher that records Launch calls and returns
// a *Sidecar backed by a unix-socket httptest server. Tests use it to
// exercise the Enable/Disable/Upgrade lifecycle hooks without running a
// real addon binary — Stop goes through the normal POST /shutdown path
// which the fake server answers with 204.
type fakeLauncher struct {
	t      *testing.T
	mu     sync.Mutex
	calls  []launchCall
	failOn map[string]error // return this error for the given addon name
	// fakeCleanups holds the test-server stoppers so tests can run serially
	// without leaking file descriptors.
	fakeCleanups []func()
	started      int32
}

type launchCall struct {
	Name     string
	AddonDir string
	Spec     SidecarSpec
}

func (f *fakeLauncher) Launch(ctx context.Context, name, addonDir string, spec SidecarSpec) (*Sidecar, error) {
	f.mu.Lock()
	f.calls = append(f.calls, launchCall{Name: name, AddonDir: addonDir, Spec: spec})
	if err, ok := f.failOn[name]; ok {
		f.mu.Unlock()
		return nil, err
	}
	f.mu.Unlock()

	// Spawn an httptest unix-socket server that answers /health with 200
	// and /shutdown with 204. This gives the *Sidecar a working client so
	// Stop's graceful-shutdown HTTP path does not panic on a nil client.
	sock, ts := unixTestServer(f.t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/shutdown":
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	s := newSidecarAt(sock)
	s.Name = name
	f.mu.Lock()
	f.fakeCleanups = append(f.fakeCleanups, ts.Close)
	f.mu.Unlock()
	atomic.AddInt32(&f.started, 1)
	return s, nil
}

func (f *fakeLauncher) close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.fakeCleanups {
		c()
	}
}

func (f *fakeLauncher) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeLauncher) lastCall() launchCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

// newEnabledTestLifecycle returns a Lifecycle wired with a fake launcher and
// a live SidecarRegistry. The addon "testaddon" is installed from the
// lifecycleRepo seed so the integrity check passes at enable time.
func newEnabledTestLifecycle(t *testing.T) (*Lifecycle, *fakeLauncher) {
	t.Helper()
	seed := lifecycleRepo(t)
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.Clone = fakeClone(seed)
	l.Registry = NewSidecarRegistry()
	fl := &fakeLauncher{t: t}
	t.Cleanup(fl.close)
	l.Launcher = fl

	if _, err := l.Install(context.Background(), InstallOptions{
		Source: "https://example.com/testaddon",
		Ref:    NewVersionRef("v0.1.0"),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	return l, fl
}

func TestLifecycle_EnableSpawnsSidecarAndRegisters(t *testing.T) {
	l, fl := newEnabledTestLifecycle(t)

	entry, err := l.Enable(context.Background(), "testaddon")
	if err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if entry.State != StateEnabled {
		t.Errorf("state = %s, want enabled", entry.State)
	}
	if fl.callCount() != 1 {
		t.Errorf("launcher called %d times, want 1", fl.callCount())
	}
	call := fl.lastCall()
	if call.Name != "testaddon" {
		t.Errorf("launch name = %q, want testaddon", call.Name)
	}
	if call.Spec.Binary != "test-sidecar" {
		t.Errorf("launch spec.Binary = %q, want test-sidecar", call.Spec.Binary)
	}

	got := l.Registry.Lookup("testaddon")
	if got == nil {
		t.Fatal("Registry.Lookup returned nil after Enable")
	}
	if got.Name != "testaddon" {
		t.Errorf("registered sidecar Name = %q", got.Name)
	}
	if !sliceContains(l.Registry.List(), "testaddon") {
		t.Error("Registry.List should contain testaddon")
	}
}

func TestLifecycle_EnableSidecarStartFailureDoesNotFlipState(t *testing.T) {
	l, fl := newEnabledTestLifecycle(t)
	fl.failOn = map[string]error{"testaddon": errors.New("bind: address already in use")}

	_, err := l.Enable(context.Background(), "testaddon")
	if err == nil || !strings.Contains(err.Error(), "sidecar failed to start") {
		t.Fatalf("expected sidecar-start error, got %v", err)
	}

	// Registry must not contain the addon.
	if got := l.Registry.Lookup("testaddon"); got != nil {
		t.Errorf("Registry.Lookup after failed enable = %v, want nil", got)
	}

	// Persistent state must still be installed (not enabled).
	entry, _ := l.Store.Get("testaddon")
	if entry == nil {
		t.Fatal("entry missing after failed enable")
	}
	if entry.State != StateInstalled {
		t.Errorf("state = %s, want installed (enable failure must not flip state)", entry.State)
	}
}

func TestLifecycle_DisableStopsAndUnregisters(t *testing.T) {
	l, _ := newEnabledTestLifecycle(t)
	if _, err := l.Enable(context.Background(), "testaddon"); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	// Sanity: live sidecar registered.
	if l.Registry.Lookup("testaddon") == nil {
		t.Fatal("sidecar not in registry after Enable")
	}

	entry, err := l.Disable(context.Background(), "testaddon")
	if err != nil {
		t.Fatalf("Disable: %v", err)
	}
	if entry.State != StateInstalled {
		t.Errorf("state = %s, want installed", entry.State)
	}
	if got := l.Registry.Lookup("testaddon"); got != nil {
		t.Errorf("Registry.Lookup after Disable = %v, want nil", got)
	}
}

func TestLifecycle_UpgradeRestartsSidecar(t *testing.T) {
	l, fl := newEnabledTestLifecycle(t)
	if _, err := l.Enable(context.Background(), "testaddon"); err != nil {
		t.Fatalf("Enable: %v", err)
	}

	first := l.Registry.Lookup("testaddon")
	if first == nil {
		t.Fatal("no sidecar registered after Enable")
	}

	updated, err := l.Upgrade(context.Background(), "testaddon", NewVersionRef("v0.2.0"))
	if err != nil {
		t.Fatalf("Upgrade: %v", err)
	}
	if updated.State != StateEnabled {
		t.Errorf("state after upgrade = %s, want enabled", updated.State)
	}
	if updated.Version != "0.2.0" {
		t.Errorf("version = %q, want 0.2.0", updated.Version)
	}

	// Launcher should have been called twice (once for enable, once for upgrade).
	if fl.callCount() != 2 {
		t.Errorf("launcher called %d times, want 2", fl.callCount())
	}

	second := l.Registry.Lookup("testaddon")
	if second == nil {
		t.Fatal("no sidecar registered after Upgrade")
	}
	if second == first {
		t.Error("Upgrade did not replace the running sidecar handle")
	}
}

func TestLifecycle_UpgradeRestartFailureDropsToInstalled(t *testing.T) {
	l, fl := newEnabledTestLifecycle(t)
	if _, err := l.Enable(context.Background(), "testaddon"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	// Make the NEXT launch (the post-upgrade restart) fail.
	fl.failOn = map[string]error{"testaddon": errors.New("new binary segfaulted")}

	updated, err := l.Upgrade(context.Background(), "testaddon", NewVersionRef("v0.2.0"))
	if err == nil || !strings.Contains(err.Error(), "new sidecar failed to start") {
		t.Fatalf("expected restart failure, got %v", err)
	}
	if !strings.Contains(err.Error(), "sciclaw addon enable") {
		t.Errorf("error should point at enable command, got %v", err)
	}
	if updated == nil {
		t.Fatal("Upgrade returned nil entry")
	}
	if updated.State != StateInstalled {
		t.Errorf("state = %s, want installed (restart failure must degrade state)", updated.State)
	}

	// Persistent state also installed (Upgrade saved the degraded entry).
	stored, _ := l.Store.Get("testaddon")
	if stored.State != StateInstalled {
		t.Errorf("persisted state = %s, want installed", stored.State)
	}

	// Registry should not contain the addon.
	if got := l.Registry.Lookup("testaddon"); got != nil {
		t.Errorf("Registry.Lookup after failed restart = %v, want nil", got)
	}
}

func TestLifecycle_UpgradeUnchangedPreservesRegistryEntry(t *testing.T) {
	l, fl := newEnabledTestLifecycle(t)
	if _, err := l.Enable(context.Background(), "testaddon"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	first := l.Registry.Lookup("testaddon")
	if first == nil {
		t.Fatal("pre-condition: sidecar not registered")
	}

	// Upgrade to the same commit — ErrAlreadyAtCommit, no sidecar churn.
	_, err := l.Upgrade(context.Background(), "testaddon", NewVersionRef("v0.1.0"))
	if !errors.Is(err, ErrAlreadyAtCommit) {
		t.Fatalf("expected ErrAlreadyAtCommit, got %v", err)
	}
	// Launcher should NOT have been called a second time.
	if fl.callCount() != 1 {
		t.Errorf("launcher called %d times, want 1 (upgrade noop must not restart)", fl.callCount())
	}
	// Registry entry unchanged.
	if l.Registry.Lookup("testaddon") != first {
		t.Error("sidecar handle changed after no-op upgrade")
	}
}

func TestLifecycle_UninstallStopsAndUnregistersEnabled(t *testing.T) {
	l, _ := newEnabledTestLifecycle(t)
	if _, err := l.Enable(context.Background(), "testaddon"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if l.Registry.Lookup("testaddon") == nil {
		t.Fatal("pre-condition: sidecar not registered")
	}

	if err := l.Uninstall(context.Background(), "testaddon", true); err != nil {
		t.Fatalf("Uninstall --force: %v", err)
	}
	if got := l.Registry.Lookup("testaddon"); got != nil {
		t.Errorf("Registry.Lookup after Uninstall = %v, want nil", got)
	}
	if entry, _ := l.Store.Get("testaddon"); entry != nil {
		t.Errorf("Store entry not removed: %+v", entry)
	}
}

func TestLifecycle_EnableWithNilRegistryIsMetadataOnly(t *testing.T) {
	// Omit Registry entirely — Enable must still flip state and must NOT
	// call the launcher (because the registry is the only code path that
	// triggers sidecar spawning).
	seed := lifecycleRepo(t)
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.Clone = fakeClone(seed)
	fl := &fakeLauncher{t: t}
	t.Cleanup(fl.close)
	l.Launcher = fl
	// l.Registry left nil.

	if _, err := l.Install(context.Background(), InstallOptions{
		Source: "https://example.com/testaddon",
		Ref:    NewVersionRef("v0.1.0"),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	if _, err := l.Enable(context.Background(), "testaddon"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if fl.callCount() != 0 {
		t.Errorf("launcher called %d times with nil Registry, want 0", fl.callCount())
	}
}

func TestLifecycle_DisableNilSidecarIsNoOp(t *testing.T) {
	// Enable via the normal path, then clobber the registry entry so
	// Disable observes a nil sidecar — it should still succeed and flip
	// the state.
	l, _ := newEnabledTestLifecycle(t)
	if _, err := l.Enable(context.Background(), "testaddon"); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	l.Registry.Unregister("testaddon")

	entry, err := l.Disable(context.Background(), "testaddon")
	if err != nil {
		t.Fatalf("Disable on missing sidecar: %v", err)
	}
	if entry.State != StateInstalled {
		t.Errorf("state = %s, want installed", entry.State)
	}
}

func sliceContains(ss []string, needle string) bool {
	for _, s := range ss {
		if s == needle {
			return true
		}
	}
	return false
}
