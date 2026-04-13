package addons

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Reconciler brings the live sidecar set in a SidecarRegistry into agreement
// with the persisted state in registry.json. It is the bridge between the
// short-lived CLI (which writes state via a nil-Registry Lifecycle) and the
// long-lived gateway (which owns the sidecar processes).
//
// Classic Kubernetes-style control loop: every interval (and on a reload
// marker touch from the CLI), read desired state from the Store and drive the
// Registry to match.
//
// A Reconciler is goroutine-safe: Reconcile may be called from multiple
// goroutines concurrently. An internal mutex serializes reconcile passes so a
// ticker-triggered pass cannot race a reload-triggered pass.
type Reconciler struct {
	Store       *Store
	Registry    *SidecarRegistry
	SciclawHome string

	// Interval is how often the control loop reconciles when no reload
	// marker has been observed. Defaults to 10 seconds when zero.
	Interval time.Duration
	// StartTimeout bounds a single Sidecar.Start spawn inside Reconcile.
	// Defaults to 10 seconds when zero.
	StartTimeout time.Duration
	// StopTimeout bounds a single Sidecar.Stop teardown inside Reconcile.
	// Defaults to 10 seconds when zero.
	StopTimeout time.Duration

	// Launcher is the hook for constructing + starting sidecars. Tests
	// inject a fake; production leaves this nil and gets the default
	// NewSidecar + Start path.
	Launcher SidecarLauncher

	// Log receives per-addon reconcile events. A nil Log is a no-op.
	// Events emitted: "start", "start_error", "stop", "stop_error",
	// "manifest_error", "integrity_error".
	Log func(name, event string, err error)

	// mu serializes Reconcile passes so ticker + reload triggers do not
	// overlap. It is cheaper and simpler than per-addon locks, and a
	// reconcile pass should be sub-second in the common case.
	mu sync.Mutex
}

// defaultReconcileInterval is the fallback when Reconciler.Interval is zero.
// 10 seconds is a pragmatic eventual-consistency window for a lab tool: a
// `sciclaw addon enable` that lands between ticker ticks will feel laggy but
// not broken, and the reload-marker fast path covers the impatient case.
const defaultReconcileInterval = 10 * time.Second

// defaultReconcileStartTimeout / defaultReconcileStopTimeout bound a single
// sidecar spawn/teardown inside Reconcile so one stuck addon cannot hang the
// whole control loop.
const (
	defaultReconcileStartTimeout = 10 * time.Second
	defaultReconcileStopTimeout  = 10 * time.Second
)

// reloadMarkerRelPath is the path (relative to SciclawHome) of the marker
// file the CLI touches after mutating registry.json. Mirrors the
// routing.reload convention in cmd/picoclaw/main.go — poll on mtime, no new
// dependencies.
const reloadMarkerRelPath = "addons/reload"

// reconcilePollInterval is how often Run stats the reload marker when it is
// not otherwise waiting on the ticker. Short enough that a CLI invocation
// feels synchronous (sub-second), long enough to avoid burning CPU.
const reconcilePollInterval = 250 * time.Millisecond

// TriggerReload touches <sciclawHome>/addons/reload so a running gateway's
// Reconciler notices the mtime bump and kicks a reconcile pass immediately.
//
// Called from the CLI after each successful mutation (install/enable/disable/
// upgrade/uninstall/rollback). CLI handlers swallow errors with a warning
// because the ticker-based eventual-consistency path still converges even
// when the marker write fails.
func TriggerReload(sciclawHome string) error {
	if sciclawHome == "" {
		return fmt.Errorf("TriggerReload: sciclawHome is empty")
	}
	dir := filepath.Join(sciclawHome, "addons")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("TriggerReload: creating %s: %w", dir, err)
	}
	marker := filepath.Join(sciclawHome, reloadMarkerRelPath)
	payload := []byte(time.Now().UTC().Format(time.RFC3339Nano) + "\n")
	if err := os.WriteFile(marker, payload, 0o644); err != nil {
		return fmt.Errorf("TriggerReload: writing %s: %w", marker, err)
	}
	return nil
}

// Reconcile performs one idempotent pass. Safe to call concurrently with CLI
// writes to registry.json because the Store has its own lock and the
// Reconciler serializes its own passes.
//
// Pass order:
//  1. Start any addon with state=enabled that is not in the live Registry.
//  2. Stop any name in the live Registry whose persisted state is not enabled
//     (or whose entry has been deleted).
//
// Errors on individual addons are logged via r.Log but do not abort the pass:
// one bad addon cannot starve others. The returned error is non-nil only
// when loading registry.json itself fails, since that is the only condition
// under which we have no idea what the desired state is.
func (r *Reconciler) Reconcile(ctx context.Context) error {
	if r == nil {
		return errors.New("reconciler: nil receiver")
	}
	if r.Store == nil || r.Registry == nil {
		return errors.New("reconciler: Store and Registry are required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	reg, err := r.Store.Load()
	if err != nil {
		return fmt.Errorf("reconciler: loading registry: %w", err)
	}

	// Pass 1: start every enabled-and-not-running addon.
	for name, entry := range reg.Addons {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if entry == nil || entry.State != StateEnabled {
			continue
		}
		if r.Registry.Lookup(name) != nil {
			// Already running. For v1 we trust the registration —
			// real addon crash detection is an open question.
			continue
		}
		r.startAddon(ctx, name, entry)
	}

	// Pass 2: stop every running sidecar that no longer should be.
	for _, name := range r.Registry.List() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		entry, ok := reg.Addons[name]
		if ok && entry != nil && entry.State == StateEnabled {
			continue
		}
		r.stopAddon(ctx, name)
	}

	return nil
}

// startAddon handles the "enabled but not running" branch of Reconcile. Errors
// are logged and swallowed — one bad addon does not abort the pass.
func (r *Reconciler) startAddon(ctx context.Context, name string, entry *RegistryEntry) {
	dir := filepath.Join(r.SciclawHome, "addons", name)
	manifestPath := filepath.Join(dir, "addon.json")
	manifest, err := ParseManifest(manifestPath)
	if err != nil {
		r.logEvent(name, "manifest_error", err)
		return
	}
	if manifest.Sidecar.Binary == "" {
		// No sidecar declared — nothing to spawn. Enabled-but-metadata-only
		// addons exist: hooks, CLI groups, etc. That is not an error.
		return
	}

	// Integrity check mirrors Lifecycle.Enable so the gateway does not spawn
	// a tampered binary on behalf of a short-lived CLI that skipped the
	// check. VerifyEntry is cheap (SHA256 over a small tree) and idempotent.
	bootstrapPath := ""
	if manifest.Bootstrap.Install != "" {
		bootstrapPath = resolveUnder(dir, manifest.Bootstrap.Install)
	}
	sidecarPath := sidecarBinaryPath(dir, manifest)
	if err := VerifyEntry(dir, entry, manifestPath, bootstrapPath, sidecarPath); err != nil {
		r.logEvent(name, "integrity_error", err)
		return
	}

	startCtx, cancel := context.WithTimeout(ctx, r.startTimeout())
	defer cancel()
	side, lerr := r.launcher().Launch(startCtx, name, dir, manifest.Sidecar)
	if lerr != nil {
		r.logEvent(name, "start_error", lerr)
		return
	}
	r.Registry.Register(name, side)
	r.logEvent(name, "start", nil)
}

// stopAddon tears down a live sidecar that no longer has a matching
// enabled entry in the store.
func (r *Reconciler) stopAddon(ctx context.Context, name string) {
	side := r.Registry.Lookup(name)
	if side == nil {
		r.Registry.Unregister(name)
		return
	}
	stopCtx, cancel := context.WithTimeout(ctx, r.stopTimeout())
	defer cancel()
	if err := side.Stop(stopCtx); err != nil {
		r.logEvent(name, "stop_error", err)
	} else {
		r.logEvent(name, "stop", nil)
	}
	r.Registry.Unregister(name)
}

// Run starts the control loop. It performs an immediate Reconcile pass, then
// loops on a ticker plus reload-marker mtime polling. Returns ctx.Err() when
// the context is cancelled.
//
// Reconcile errors are logged (event "reconcile_error") and never propagated —
// the loop must survive transient Store failures.
func (r *Reconciler) Run(ctx context.Context) error {
	if r == nil {
		return errors.New("reconciler: nil receiver")
	}
	// Kick off an immediate pass so a gateway starting with
	// state=enabled addons does not wait for the first ticker tick.
	if err := r.Reconcile(ctx); err != nil {
		r.logEvent("", "reconcile_error", err)
	}

	interval := r.interval()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	pollTicker := time.NewTicker(reconcilePollInterval)
	defer pollTicker.Stop()

	marker := filepath.Join(r.SciclawHome, reloadMarkerRelPath)
	lastSeen := markerMTime(marker)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := r.Reconcile(ctx); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return err
				}
				r.logEvent("", "reconcile_error", err)
			}
		case <-pollTicker.C:
			mt := markerMTime(marker)
			if mt.IsZero() || !mt.After(lastSeen) {
				continue
			}
			lastSeen = mt
			if err := r.Reconcile(ctx); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					return err
				}
				r.logEvent("", "reconcile_error", err)
			}
		}
	}
}

// markerMTime returns the mtime of path, or the zero Time if the file is
// absent or stat fails. Callers must treat a zero return as "no marker yet".
func markerMTime(path string) time.Time {
	st, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return st.ModTime()
}

// launcher returns r.Launcher if set, otherwise the default.
func (r *Reconciler) launcher() SidecarLauncher {
	if r.Launcher != nil {
		return r.Launcher
	}
	return defaultLauncher{}
}

func (r *Reconciler) interval() time.Duration {
	if r.Interval > 0 {
		return r.Interval
	}
	return defaultReconcileInterval
}

func (r *Reconciler) startTimeout() time.Duration {
	if r.StartTimeout > 0 {
		return r.StartTimeout
	}
	return defaultReconcileStartTimeout
}

func (r *Reconciler) stopTimeout() time.Duration {
	if r.StopTimeout > 0 {
		return r.StopTimeout
	}
	return defaultReconcileStopTimeout
}

func (r *Reconciler) logEvent(name, event string, err error) {
	if r.Log == nil {
		return
	}
	r.Log(name, event, err)
}
