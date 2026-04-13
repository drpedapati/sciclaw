package addons

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newReconcilerTestLifecycle installs a "testaddon" entry via the normal
// Install path so registry.json has a realistic entry (correct hashes,
// resolved commit, etc.) that VerifyEntry will accept at reconcile time.
// Returns the Lifecycle (with nil Registry — the CLI pattern) plus the
// shared fakeLauncher so tests can record Launch calls.
func newReconcilerTestLifecycle(t *testing.T) (*Lifecycle, *fakeLauncher, *SidecarRegistry) {
	t.Helper()
	seed := lifecycleRepo(t)
	runner := &captureRunner{}
	l := newTestLifecycle(t, runner)
	l.Clone = fakeClone(seed)
	// CLI path: no registry — Install/Enable mutate state only.
	l.Registry = nil
	fl := &fakeLauncher{t: t}
	t.Cleanup(fl.close)
	l.Launcher = fl

	if _, err := l.Install(context.Background(), InstallOptions{
		Source: "https://example.com/testaddon",
		Ref:    NewVersionRef("v0.1.0"),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	return l, fl, NewSidecarRegistry()
}

// newTestReconciler returns a Reconciler wired to the lifecycle's Store +
// SciclawHome, a fresh SidecarRegistry, and the test fakeLauncher so nothing
// spawns a real binary.
func newTestReconciler(t *testing.T, l *Lifecycle, reg *SidecarRegistry, fl *fakeLauncher) *Reconciler {
	t.Helper()
	return &Reconciler{
		Store:        l.Store,
		Registry:     reg,
		SciclawHome:  l.SciclawHome,
		Interval:     20 * time.Millisecond,
		StartTimeout: 2 * time.Second,
		StopTimeout:  2 * time.Second,
		Launcher:     fl,
	}
}

// --- Reconcile --------------------------------------------------------------

func TestReconcile_StartsMissingSidecars(t *testing.T) {
	l, fl, reg := newReconcilerTestLifecycle(t)
	// Flip state to enabled by hand (simulating the CLI path) so the
	// Reconciler has something to start.
	entry, _ := l.Store.Get("testaddon")
	entry.State = StateEnabled
	if err := l.Store.Set("testaddon", entry); err != nil {
		t.Fatalf("set enabled: %v", err)
	}

	r := newTestReconciler(t, l, reg, fl)
	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	if fl.callCount() != 1 {
		t.Errorf("launcher called %d times, want 1", fl.callCount())
	}
	if reg.Lookup("testaddon") == nil {
		t.Error("expected testaddon to be registered after Reconcile")
	}
}

func TestReconcile_StopsOrphanSidecars(t *testing.T) {
	l, fl, reg := newReconcilerTestLifecycle(t)
	// Enable, reconcile so the registry picks it up, then demote to
	// installed and reconcile again — the second pass must stop the
	// orphaned sidecar.
	entry, _ := l.Store.Get("testaddon")
	entry.State = StateEnabled
	if err := l.Store.Set("testaddon", entry); err != nil {
		t.Fatalf("set enabled: %v", err)
	}

	r := newTestReconciler(t, l, reg, fl)
	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile enable: %v", err)
	}
	if reg.Lookup("testaddon") == nil {
		t.Fatal("pre-condition: sidecar should be registered after first pass")
	}

	entry.State = StateInstalled
	if err := l.Store.Set("testaddon", entry); err != nil {
		t.Fatalf("set installed: %v", err)
	}
	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile disable: %v", err)
	}
	if got := reg.Lookup("testaddon"); got != nil {
		t.Errorf("Registry.Lookup after Reconcile-down = %v, want nil", got)
	}
}

func TestReconcile_StopsRemovedEntries(t *testing.T) {
	l, fl, reg := newReconcilerTestLifecycle(t)
	entry, _ := l.Store.Get("testaddon")
	entry.State = StateEnabled
	if err := l.Store.Set("testaddon", entry); err != nil {
		t.Fatalf("set enabled: %v", err)
	}

	r := newTestReconciler(t, l, reg, fl)
	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile enable: %v", err)
	}
	// Delete the entry entirely — simulates 'sciclaw addon uninstall'.
	if err := l.Store.Delete("testaddon"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile cleanup: %v", err)
	}
	if got := reg.Lookup("testaddon"); got != nil {
		t.Errorf("Registry.Lookup after entry deletion = %v, want nil", got)
	}
}

func TestReconcile_SkipsHealthy(t *testing.T) {
	l, fl, reg := newReconcilerTestLifecycle(t)
	entry, _ := l.Store.Get("testaddon")
	entry.State = StateEnabled
	if err := l.Store.Set("testaddon", entry); err != nil {
		t.Fatalf("set enabled: %v", err)
	}

	r := newTestReconciler(t, l, reg, fl)
	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("first Reconcile: %v", err)
	}
	before := fl.callCount()
	if before != 1 {
		t.Fatalf("launcher calls after first Reconcile = %d, want 1", before)
	}
	// Second pass: state and registry already agree — nothing should happen.
	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("second Reconcile: %v", err)
	}
	if got := fl.callCount(); got != before {
		t.Errorf("launcher called %d times on no-op pass, want %d", got, before)
	}
}

func TestReconcile_HandlesMissingManifest(t *testing.T) {
	l, fl, reg := newReconcilerTestLifecycle(t)
	entry, _ := l.Store.Get("testaddon")
	entry.State = StateEnabled
	if err := l.Store.Set("testaddon", entry); err != nil {
		t.Fatalf("set enabled: %v", err)
	}
	// Nuke the addon dir so ParseManifest fails. The Reconciler should
	// log an error (captured here) and keep going without panicking.
	if err := os.RemoveAll(l.AddonDir("testaddon")); err != nil {
		t.Fatalf("rm addon dir: %v", err)
	}

	var (
		mu     sync.Mutex
		events []struct {
			name, event string
			err         error
		}
	)
	r := newTestReconciler(t, l, reg, fl)
	r.Log = func(name, event string, err error) {
		mu.Lock()
		events = append(events, struct {
			name, event string
			err         error
		}{name, event, err})
		mu.Unlock()
	}
	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile must not return for per-addon manifest failures: %v", err)
	}
	if fl.callCount() != 0 {
		t.Errorf("launcher should not be called when manifest is missing, got %d", fl.callCount())
	}
	if reg.Lookup("testaddon") != nil {
		t.Error("missing manifest must not leave a registered sidecar")
	}

	mu.Lock()
	defer mu.Unlock()
	var sawManifestError bool
	for _, e := range events {
		if e.name == "testaddon" && e.event == "manifest_error" && e.err != nil {
			sawManifestError = true
		}
	}
	if !sawManifestError {
		t.Errorf("expected manifest_error log event, got %+v", events)
	}
}

func TestReconcile_HandlesLauncherFailure(t *testing.T) {
	l, fl, reg := newReconcilerTestLifecycle(t)
	entry, _ := l.Store.Get("testaddon")
	entry.State = StateEnabled
	if err := l.Store.Set("testaddon", entry); err != nil {
		t.Fatalf("set enabled: %v", err)
	}
	fl.failOn = map[string]error{"testaddon": errors.New("bind failed")}

	var (
		mu     sync.Mutex
		errorCount int
	)
	r := newTestReconciler(t, l, reg, fl)
	r.Log = func(name, event string, err error) {
		if event == "start_error" {
			mu.Lock()
			errorCount++
			mu.Unlock()
		}
	}
	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if reg.Lookup("testaddon") != nil {
		t.Error("launcher failure must not leave a registered sidecar")
	}
	mu.Lock()
	if errorCount != 1 {
		t.Errorf("expected 1 start_error log, got %d", errorCount)
	}
	mu.Unlock()
}

func TestReconcile_ReentrantSafe(t *testing.T) {
	l, fl, reg := newReconcilerTestLifecycle(t)
	entry, _ := l.Store.Get("testaddon")
	entry.State = StateEnabled
	if err := l.Store.Set("testaddon", entry); err != nil {
		t.Fatalf("set enabled: %v", err)
	}

	r := newTestReconciler(t, l, reg, fl)
	var wg sync.WaitGroup
	const N = 8
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			if err := r.Reconcile(context.Background()); err != nil {
				t.Errorf("concurrent Reconcile: %v", err)
			}
		}()
	}
	wg.Wait()

	// No matter how many goroutines hammered Reconcile, the addon must
	// end up registered exactly once and the launcher must have been
	// called exactly once (because the second and later passes find an
	// existing registration).
	if reg.Lookup("testaddon") == nil {
		t.Error("addon not registered after concurrent reconciles")
	}
	if got := fl.callCount(); got != 1 {
		t.Errorf("launcher called %d times, want 1", got)
	}
}

func TestReconcile_NoEnabledEntriesIsNoOp(t *testing.T) {
	l, fl, reg := newReconcilerTestLifecycle(t)
	// testaddon is currently state=installed — nothing to spawn.
	r := newTestReconciler(t, l, reg, fl)
	if err := r.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if fl.callCount() != 0 {
		t.Errorf("launcher called %d times, want 0", fl.callCount())
	}
	if got := reg.Lookup("testaddon"); got != nil {
		t.Errorf("Registry.Lookup unexpectedly populated: %v", got)
	}
}

// --- Run --------------------------------------------------------------------

func TestRun_TickerFires(t *testing.T) {
	l, fl, reg := newReconcilerTestLifecycle(t)
	// Start with state=installed so the first pass is a no-op.
	r := newTestReconciler(t, l, reg, fl)
	r.Interval = 30 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	// Wait for the startup pass to finish, then flip to enabled.
	time.Sleep(10 * time.Millisecond)
	entry, _ := l.Store.Get("testaddon")
	entry.State = StateEnabled
	if err := l.Store.Set("testaddon", entry); err != nil {
		t.Fatalf("set enabled: %v", err)
	}

	// Wait long enough for at least two ticker ticks. We intentionally do
	// NOT touch the reload marker — this proves the ticker alone converges.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if reg.Lookup("testaddon") != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if reg.Lookup("testaddon") == nil {
		t.Fatal("ticker-driven Reconcile did not start the addon")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

func TestRun_ReloadFileTriggersImmediate(t *testing.T) {
	l, fl, reg := newReconcilerTestLifecycle(t)
	r := newTestReconciler(t, l, reg, fl)
	// Very long ticker so the only reason a mid-test reconcile fires is
	// the reload marker path.
	r.Interval = 10 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	// Wait for the startup pass, then flip state and touch the marker.
	time.Sleep(50 * time.Millisecond)
	entry, _ := l.Store.Get("testaddon")
	entry.State = StateEnabled
	if err := l.Store.Set("testaddon", entry); err != nil {
		t.Fatalf("set enabled: %v", err)
	}
	// Sleep a beat so the marker mtime is distinguishable from whatever
	// the initial Run() stat recorded (mtime resolution varies by FS).
	time.Sleep(20 * time.Millisecond)
	if err := TriggerReload(l.SciclawHome); err != nil {
		t.Fatalf("TriggerReload: %v", err)
	}

	// The poll runs every 250ms and the ticker is 10s, so a convergence
	// within ~1s proves the reload path (not the ticker) drove it.
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if reg.Lookup("testaddon") != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if reg.Lookup("testaddon") == nil {
		t.Fatal("reload-marker Reconcile did not start the addon")
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

func TestRun_InitialPass(t *testing.T) {
	l, fl, reg := newReconcilerTestLifecycle(t)
	entry, _ := l.Store.Get("testaddon")
	entry.State = StateEnabled
	if err := l.Store.Set("testaddon", entry); err != nil {
		t.Fatalf("set enabled: %v", err)
	}
	r := newTestReconciler(t, l, reg, fl)
	r.Interval = 10 * time.Second // long so only the startup pass fires

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		if reg.Lookup("testaddon") != nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if reg.Lookup("testaddon") == nil {
		t.Fatal("immediate startup Reconcile did not fire")
	}

	cancel()
	<-done
}

func TestRun_CtxCancelReturns(t *testing.T) {
	l, fl, reg := newReconcilerTestLifecycle(t)
	r := newTestReconciler(t, l, reg, fl)
	r.Interval = 5 * time.Second

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()

	// Give Run() time to reach its select loop, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Run returned %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

// --- TriggerReload ----------------------------------------------------------

func TestTriggerReload_CreatesMarker(t *testing.T) {
	home := t.TempDir()
	if err := TriggerReload(home); err != nil {
		t.Fatalf("TriggerReload: %v", err)
	}
	info, err := os.Stat(filepath.Join(home, "addons", "reload"))
	if err != nil {
		t.Fatalf("stat marker: %v", err)
	}
	if info.Size() == 0 {
		t.Error("marker should be non-empty")
	}
}

func TestTriggerReload_IdempotentOnReInvoke(t *testing.T) {
	home := t.TempDir()
	if err := TriggerReload(home); err != nil {
		t.Fatalf("first TriggerReload: %v", err)
	}
	firstInfo, err := os.Stat(filepath.Join(home, "addons", "reload"))
	if err != nil {
		t.Fatalf("stat first: %v", err)
	}
	// Sleep past whatever filesystem timestamp resolution exists so the
	// second write is guaranteed to bump mtime on platforms with 1-second
	// granularity.
	time.Sleep(10 * time.Millisecond)
	if err := TriggerReload(home); err != nil {
		t.Fatalf("second TriggerReload: %v", err)
	}
	secondInfo, err := os.Stat(filepath.Join(home, "addons", "reload"))
	if err != nil {
		t.Fatalf("stat second: %v", err)
	}
	// Second call must succeed and not decrease the mtime.
	if secondInfo.ModTime().Before(firstInfo.ModTime()) {
		t.Errorf("mtime regressed: %v -> %v", firstInfo.ModTime(), secondInfo.ModTime())
	}
}

func TestTriggerReload_EmptyHomeIsError(t *testing.T) {
	if err := TriggerReload(""); err == nil {
		t.Error("expected error for empty sciclawHome")
	}
}

// --- defaults --------------------------------------------------------------

func TestReconciler_DefaultsFilledIn(t *testing.T) {
	// Unset Interval/Timeouts — the accessor methods must fall back.
	r := &Reconciler{}
	if got := r.interval(); got != defaultReconcileInterval {
		t.Errorf("interval = %v, want %v", got, defaultReconcileInterval)
	}
	if got := r.startTimeout(); got != defaultReconcileStartTimeout {
		t.Errorf("startTimeout = %v", got)
	}
	if got := r.stopTimeout(); got != defaultReconcileStopTimeout {
		t.Errorf("stopTimeout = %v", got)
	}
}

func TestReconciler_NilReceiverGuards(t *testing.T) {
	var r *Reconciler
	if err := r.Reconcile(context.Background()); err == nil {
		t.Error("Reconcile on nil receiver should error")
	}
	if err := r.Run(context.Background()); err == nil {
		t.Error("Run on nil receiver should error")
	}
}

func TestReconciler_RequiresStoreAndRegistry(t *testing.T) {
	r := &Reconciler{}
	if err := r.Reconcile(context.Background()); err == nil {
		t.Error("Reconcile without Store/Registry should error")
	}
}

// --- misc race-sanity helper ------------------------------------------------

// TestReconcile_WriteRaceWithStore simulates the CLI writing to registry.json
// while the Reconciler is mid-pass. The Store has its own lock, so this should
// be safe — this test mostly exists to flush out any -race complaints.
func TestReconcile_WriteRaceWithStore(t *testing.T) {
	l, fl, reg := newReconcilerTestLifecycle(t)
	r := newTestReconciler(t, l, reg, fl)

	var stop atomic.Bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		for !stop.Load() {
			if err := r.Reconcile(context.Background()); err != nil {
				t.Errorf("Reconcile race: %v", err)
				return
			}
		}
	}()

	// Flip state back and forth a few times; each Set takes the Store lock.
	for i := 0; i < 20; i++ {
		entry, _ := l.Store.Get("testaddon")
		if entry == nil {
			continue
		}
		if i%2 == 0 {
			entry.State = StateEnabled
		} else {
			entry.State = StateInstalled
		}
		if err := l.Store.Set("testaddon", entry); err != nil {
			t.Fatalf("set: %v", err)
		}
		time.Sleep(2 * time.Millisecond)
	}

	stop.Store(true)
	<-done
}
