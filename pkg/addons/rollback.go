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

	if err := r.Store.Set(name, updated); err != nil {
		return nil, fmt.Errorf("addon %q: saving rolled-back registry entry: %w", name, err)
	}
	return updated, nil
}
