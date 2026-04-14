package addons

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Rollbacker reverts an addon to its previously installed commit. Only one
// level of rollback history is kept — see RFC section 8 "Rollback".
//
// All filesystem and process interaction is injected so tests can simulate
// git checkout outcomes without a real repo.
type Rollbacker struct {
	// Store is the registry store the rollback mutates.
	Store *Store
	// Runner executes git commands. Tests inject a fake; production
	// callers pass DefaultRunner{}.
	Runner CommandRunner
	// AddonDir maps an addon name to its install directory. Injected so
	// tests can redirect paths away from the user's real ~/sciclaw.
	AddonDir func(name string) string
	// Now returns the current time for InstalledAt bookkeeping; tests
	// inject a fixed clock so round-trip assertions are deterministic.
	Now func() time.Time
	// Registry, when non-nil, tracks live sidecars across this sciclaw
	// process. Rollback uses it to stop the running sidecar BEFORE the
	// git checkout swaps the on-disk binary, and to restart it from the
	// new (older) binary AFTER the checkout completes. When nil (the
	// short-lived CLI case), Rollback only rewrites persisted state and
	// relies on the gateway's Reconciler to converge the live set on
	// its next tick. Mirrors the Registry=nil vs non-nil split in
	// Lifecycle.
	Registry *SidecarRegistry
	// Launcher spawns the replacement sidecar after a successful
	// rollback. When nil a defaultLauncher is used. Matches Lifecycle.
	Launcher SidecarLauncher
	// StopTimeout bounds the graceful-shutdown wait for the old sidecar
	// before rollback forcibly kills it. Defaults to stopSidecarTimeout
	// (10s) when zero.
	StopTimeout time.Duration
	// StartTimeout bounds the replacement sidecar's spawn + /health
	// probe. Defaults to 10s when zero.
	StartTimeout time.Duration
}

// Rollback reverts the named addon to the commit recorded in its registry
// entry's PreviousCommit pointer.
//
// Sequence (mirrors RFC section 8):
//  1. Load the registry entry; error if missing.
//  2. Error if PreviousCommit is nil — "nothing to roll back to".
//  3. Run `git checkout <previous_commit>` in the addon directory.
//  4. Re-parse the addon's addon.json at the previous commit (the manifest
//     may have changed between commits).
//  5. Re-compute manifest / bootstrap / sidecar hashes via ComputeHashes.
//  6. Update the registry entry: InstalledCommit becomes the previous commit,
//     PreviousCommit is cleared, *_sha256 fields are refreshed, and State,
//     Source, and Track are preserved.
//  7. Save.
//
// After rollback, PreviousCommit is cleared — further rollback requires
// another upgrade. The current InstalledCommit is discarded and is NOT
// preserved as new rollback history.
func (r *Rollbacker) Rollback(ctx context.Context, name string) (*RegistryEntry, error) {
	if r == nil {
		return nil, fmt.Errorf("addons.Rollbacker is nil")
	}
	if r.Store == nil {
		return nil, fmt.Errorf("addons.Rollbacker.Store is nil")
	}
	if r.Runner == nil {
		return nil, fmt.Errorf("addons.Rollbacker.Runner is nil")
	}
	if r.AddonDir == nil {
		return nil, fmt.Errorf("addons.Rollbacker.AddonDir is nil")
	}
	now := r.Now
	if now == nil {
		now = time.Now
	}
	if strings.TrimSpace(name) == "" {
		return nil, fmt.Errorf("Rollback: addon name must be non-empty")
	}

	entry, err := r.Store.Get(name)
	if err != nil {
		return nil, fmt.Errorf("loading registry entry for %q: %w", name, err)
	}
	if entry == nil {
		return nil, fmt.Errorf("addon %q not installed; nothing to roll back", name)
	}
	if entry.PreviousCommit == nil || strings.TrimSpace(*entry.PreviousCommit) == "" {
		return nil, fmt.Errorf("addon %q: nothing to roll back to; no previous version recorded (upgrade at least once first)", name)
	}
	prev := strings.TrimSpace(*entry.PreviousCommit)

	dir := r.AddonDir(name)
	if strings.TrimSpace(dir) == "" {
		return nil, fmt.Errorf("addon %q: addon directory resolver returned empty path", name)
	}

	// L5: if the addon is currently enabled AND we own a SidecarRegistry,
	// stop the live sidecar before swapping the binary on disk. The old
	// sidecar is running against the current commit's binary; without
	// stopping, `git checkout` rewrites the executable under an active
	// process and the next hook call can SIGBUS on macOS or read stale
	// in-memory state on Linux.
	wasEnabled := entry.State == StateEnabled && r.Registry != nil
	if wasEnabled {
		if err := r.stopLiveSidecar(ctx, name); err != nil {
			return nil, fmt.Errorf("addon %q: stopping live sidecar before rollback: %w", name, err)
		}
	}

	// git checkout to the previous commit. Use --detach because the
	// previous commit is not necessarily a branch tip.
	script := fmt.Sprintf("git -C %s checkout --detach %s", shellQuote(dir), shellQuote(prev))
	if out, runErr := r.Runner.Run(ctx, dir, script, nil); runErr != nil {
		return nil, fmt.Errorf("addon %q: git checkout %s failed: %w (output: %s)", name, prev, runErr, strings.TrimSpace(string(out)))
	}

	// Re-parse the manifest from the rolled-back tree. The manifest may
	// differ from the current one — for example if the upgrade added a
	// new sidecar binary or changed requirements.
	manifestPath := filepath.Join(dir, "addon.json")
	manifest, err := ParseManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("addon %q: rollback checkout succeeded but manifest at previous commit is invalid: %w", name, err)
	}

	// Recompute hashes for the rolled-back tree so the registry stays
	// authoritative. ComputeHashes tolerates missing bootstrap/sidecar
	// paths by returning empty strings, matching how the install path
	// populates the entry.
	manifestSHA, bootstrapSHA, sidecarSHA, err := ComputeHashes(dir, manifest)
	if err != nil {
		return nil, fmt.Errorf("addon %q: hashing previous commit: %w", name, err)
	}

	updated := &RegistryEntry{
		Version:           manifest.Version,
		InstalledAt:       now().UTC().Format(time.RFC3339),
		InstalledCommit:   prev,
		ManifestSHA256:    manifestSHA,
		BootstrapSHA256:   bootstrapSHA,
		SidecarSHA256:     sidecarSHA,
		State:             entry.State,
		Source:            entry.Source,
		Track:             entry.Track,
		SignedTag:         nil, // previous commit may not correspond to a tag
		SignatureVerified: false,
		PreviousCommit:    nil, // one-level history: cleared after rollback
	}

	// L5 restart: spawn a new sidecar from the rolled-back binary. If
	// the restart fails we drop state to StateInstalled so the user
	// knows to run `sciclaw addon enable` again, mirroring Upgrade.
	var restartErr error
	if wasEnabled && manifest.Sidecar.Binary != "" {
		side, lerr := r.launcher().Launch(ctx, name, dir, manifest.Sidecar)
		if lerr != nil {
			restartErr = lerr
			updated.State = StateInstalled
		} else {
			r.Registry.Register(name, side)
		}
	}

	if err := r.Store.Set(name, updated); err != nil {
		// Persisting the rolled-back entry failed. If we managed to
		// restart the sidecar against the old binary, tear it down
		// again so we don't leave a live process the registry doesn't
		// know about.
		if wasEnabled && r.Registry != nil && restartErr == nil {
			_ = r.stopLiveSidecar(ctx, name)
		}
		return nil, fmt.Errorf("addon %q: saving rolled-back registry entry: %w", name, err)
	}
	if restartErr != nil {
		return updated, fmt.Errorf("addon %q: rolled back to %s but sidecar failed to restart: %w; run 'sciclaw addon enable %s' to retry", name, shortSHA(prev), restartErr, name)
	}
	return updated, nil
}

// shortSHA returns the first 12 characters of a git SHA for logging,
// or the full string if shorter. Safe to call on test fixtures that use
// synthetic non-40-char commit IDs.
func shortSHA(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// stopLiveSidecar is the mirror of Lifecycle.stopAndUnregister, scoped to
// the rollback path so tests can exercise it without a full Lifecycle.
func (r *Rollbacker) stopLiveSidecar(ctx context.Context, name string) error {
	if r.Registry == nil {
		return nil
	}
	side := r.Registry.Lookup(name)
	if side == nil {
		return nil
	}
	timeout := r.StopTimeout
	if timeout <= 0 {
		timeout = stopSidecarTimeout
	}
	stopCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	// Best-effort graceful stop, ignoring the error — we unregister
	// regardless so a stuck sidecar can't block rollback.
	_ = side.Stop(stopCtx)
	r.Registry.Unregister(name)
	return nil
}

// launcher returns Rollbacker.Launcher if set, otherwise the default.
func (r *Rollbacker) launcher() SidecarLauncher {
	if r.Launcher != nil {
		return r.Launcher
	}
	return defaultLauncher{}
}
