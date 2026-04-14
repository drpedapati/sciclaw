package addons

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ErrAlreadyAtCommit is returned by Upgrade when the resolved ref matches the
// installed commit already. It is a sentinel, not a hard error — callers who
// want to treat "no upgrade needed" as success can check via errors.Is.
var ErrAlreadyAtCommit = errors.New("addon already at requested commit")

// CommandRunner is defined in signing.go (shared with Verifier and Rollbacker).

// Lifecycle orchestrates install/enable/disable/uninstall/upgrade of addons
// on top of the registry + integrity + resolver primitives from Wave 1.
//
// All external effects (shell, git clone, clock, binary lookup) are injected
// so the state machine is exercisable in unit tests without a real
// filesystem-wide setup.
type Lifecycle struct {
	Store       *Store
	SciclawHome string
	SciclawVers string
	Platform    string
	LookPath    func(string) (string, error)
	Clone       func(ctx context.Context, repoURL, dest string) error
	Runner      CommandRunner
	Now         func() time.Time
	// Registry tracks live sidecar processes. When non-nil, Enable spawns
	// a sidecar and registers it; Disable/Uninstall/Upgrade stop and
	// unregister as appropriate. When nil, state transitions still happen
	// but no process management occurs — useful in CLI-only tests and in
	// the real CLI, which runs in a short-lived process and cannot own
	// long-running sidecars.
	//
	// Ownership model: the gateway sets Registry = liveRegistry so its
	// Lifecycle mutations (e.g. web UI "enable" button) drive processes
	// immediately. The CLI sets Registry = nil and relies on the gateway's
	// *Reconciler to converge the live set against registry.json on the
	// next tick (or immediately, after the CLI touches the reload marker
	// via TriggerReload). This keeps the CLI fast and side-effect-free at
	// the process layer while still giving users a near-synchronous feel.
	Registry *SidecarRegistry
	// Launcher builds and starts a Sidecar for an addon. Exposed as an
	// interface (not a concrete *Sidecar) so tests can inject fakes without
	// spawning real processes. When nil, a defaultLauncher is used that
	// wraps NewSidecar + Sidecar.Start.
	Launcher SidecarLauncher
}

// SidecarLauncher builds and starts the sidecar for an addon and returns a
// handle that the registry can store. Implementations must not leak
// processes on error — if Launch returns an error, any partial process MUST
// already be cleaned up.
//
// This interface exists so Enable/Upgrade are exercisable in tests without
// spawning real binaries. Production uses defaultLauncher which delegates to
// NewSidecar and Sidecar.Start.
type SidecarLauncher interface {
	Launch(ctx context.Context, name, addonDir string, spec SidecarSpec) (*Sidecar, error)
}

// defaultLauncher is the production SidecarLauncher: NewSidecar + Start.
type defaultLauncher struct{}

func (defaultLauncher) Launch(ctx context.Context, name, addonDir string, spec SidecarSpec) (*Sidecar, error) {
	s := NewSidecar(addonDir, spec)
	s.Name = name
	if err := s.Start(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// launcher returns Lifecycle.Launcher if set, otherwise the default.
func (l *Lifecycle) launcher() SidecarLauncher {
	if l.Launcher != nil {
		return l.Launcher
	}
	return defaultLauncher{}
}

// stopSidecarTimeout is the hard cap on a single Sidecar.Stop call issued
// from lifecycle transitions. Matches the RFC section 2 wording ("SIGTERM
// then SIGKILL after 5s") with headroom for the HTTP /shutdown handshake.
const stopSidecarTimeout = 10 * time.Second

// InstallOptions are the user-facing install knobs.
type InstallOptions struct {
	Name   string
	Source string
	Ref    InstallRef
}

// New constructs a Lifecycle with only the required fields populated. Callers
// must assign LookPath, Clone, Runner, and Now before calling any mutating
// method — a zero value would panic. Wiring lives at the CLI layer in a later
// wave; tests assign them explicitly.
func New(store *Store, sciclawHome, sciclawVers, platform string) *Lifecycle {
	return &Lifecycle{
		Store:       store,
		SciclawHome: sciclawHome,
		SciclawVers: sciclawVers,
		Platform:    platform,
	}
}

// AddonDir returns the on-disk install directory for an addon name.
func (l *Lifecycle) AddonDir(name string) string {
	return filepath.Join(l.SciclawHome, "addons", name)
}

// Install clones the addon source, validates the manifest, pins a commit,
// runs the optional install hook, and records a registry entry in the
// installed state. The sequence mirrors RFC section 2.
func (l *Lifecycle) Install(ctx context.Context, opts InstallOptions) (*RegistryEntry, error) {
	if opts.Source == "" {
		return nil, fmt.Errorf("install: source (git URL) is required")
	}
	// Fail fast on "already installed" before touching the filesystem when
	// the caller knew the name up front.
	if opts.Name != "" {
		existing, err := l.Store.Get(opts.Name)
		if err != nil {
			return nil, fmt.Errorf("install: reading registry: %w", err)
		}
		if existing != nil {
			return nil, fmt.Errorf("addon %q is already installed (state=%s); use 'sciclaw addon upgrade %s' instead",
				opts.Name, existing.State, opts.Name)
		}
		// H2: take a per-name lock BEFORE any filesystem work so a
		// concurrent Install/Upgrade/Uninstall of the same name from
		// another CLI process (or from the gateway reconciler) blocks
		// until we're done. Released via defer at function return.
		lock, err := AcquireLock(l.SciclawHome, opts.Name)
		if err != nil {
			return nil, fmt.Errorf("install: %w", err)
		}
		defer lock.Release()
	}
	// Stage the clone so a failed install does not pollute addons/<name>/.
	parent := filepath.Join(l.SciclawHome, "addons")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, fmt.Errorf("install: creating addons dir %s: %w", parent, err)
	}
	stagingName := ".staging-" + filepath.Base(opts.Source) + "-" + fmt.Sprintf("%d", l.now().UnixNano())
	stagingDir := filepath.Join(parent, stagingName)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return nil, fmt.Errorf("install: creating staging dir %s: %w", stagingDir, err)
	}
	cleanup := func() { _ = os.RemoveAll(stagingDir) }

	if err := l.Clone(ctx, opts.Source, stagingDir); err != nil {
		cleanup()
		return nil, fmt.Errorf("install: cloning %s: %w", opts.Source, err)
	}

	manifest, err := ParseManifest(filepath.Join(stagingDir, "addon.json"))
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("install: %w", err)
	}
	if opts.Name != "" && opts.Name != manifest.Name {
		cleanup()
		return nil, fmt.Errorf("install: name mismatch — caller supplied %q but addon.json declares %q",
			opts.Name, manifest.Name)
	}
	name := manifest.Name

	// Second check: we may not have known the name before clone, in
	// which case we also couldn't take the per-name lock up front. Take
	// it here now that we know the name. The ordering means that for
	// opts.Name == "" invocations, TWO concurrent installs of the same
	// underlying addon will both clone successfully but then serialize
	// on the lock, and the loser will see the "already installed"
	// error below. That wastes one clone but keeps correctness.
	if opts.Name == "" {
		lock, err := AcquireLock(l.SciclawHome, name)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("install: %w", err)
		}
		defer lock.Release()
		existing, err := l.Store.Get(name)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("install: reading registry: %w", err)
		}
		if existing != nil {
			cleanup()
			return nil, fmt.Errorf("addon %q is already installed (state=%s); use 'sciclaw addon upgrade %s' instead",
				name, existing.State, name)
		}
	}

	if err := ValidateRequirements(manifest, l.SciclawVers, l.Platform, l.LookPath); err != nil {
		cleanup()
		return nil, fmt.Errorf("install: %w", err)
	}
	resolved, err := Resolve(stagingDir, opts.Ref)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("install: resolving ref: %w", err)
	}

	// Check out the resolved commit so integrity hashes are stable.
	if _, err := l.Runner.Run(ctx, stagingDir, "git checkout -q "+resolved.Commit, nil); err != nil {
		cleanup()
		return nil, fmt.Errorf("install: checking out %s: %w", resolved.Commit, err)
	}
	// Re-parse the manifest at the pinned commit so recorded hash matches disk.
	manifest, err = ParseManifest(filepath.Join(stagingDir, "addon.json"))
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("install: re-parsing manifest at pinned commit: %w", err)
	}

	manifestSHA, bootstrapSHA, sidecarSHA, err := ComputeHashes(stagingDir, manifest)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("install: computing hashes: %w", err)
	}

	// Move staging into final location.
	finalDir := l.AddonDir(name)
	if err := os.RemoveAll(finalDir); err != nil {
		cleanup()
		return nil, fmt.Errorf("install: clearing %s: %w", finalDir, err)
	}
	if err := os.Rename(stagingDir, finalDir); err != nil {
		cleanup()
		return nil, fmt.Errorf("install: moving %s -> %s: %w", stagingDir, finalDir, err)
	}

	env := []string{
		"SCICLAW_HOME=" + l.SciclawHome,
		"ADDON_DIR=" + finalDir,
		"ADDON_NAME=" + name,
	}
	if manifest.Bootstrap.Install != "" {
		script := resolveUnder(finalDir, manifest.Bootstrap.Install)
		if _, err := execScript(ctx, finalDir, script, env); err != nil {
			// Best-effort rollback so retrying is idempotent.
			_ = os.RemoveAll(finalDir)
			return nil, fmt.Errorf("install: bootstrap %q failed: %w; fix the script and retry",
				manifest.Bootstrap.Install, err)
		}
	}

	entry := &RegistryEntry{
		Version:           manifest.Version,
		InstalledAt:       l.now().UTC().Format(time.RFC3339),
		InstalledCommit:   resolved.Commit,
		ManifestSHA256:    manifestSHA,
		BootstrapSHA256:   bootstrapSHA,
		SidecarSHA256:     sidecarSHA,
		State:             StateInstalled,
		Source:            opts.Source,
		Track:             nullableTrack(opts.Ref),
		SignedTag:         nullableTag(resolved.SignedTag),
		SignatureVerified: resolved.SignatureVerified,
		PreviousCommit:    nil,
	}
	if err := l.Store.Set(name, entry); err != nil {
		return nil, fmt.Errorf("install: saving registry: %w", err)
	}
	return entry, nil
}

// Enable verifies the installed addon still matches its integrity record,
// spawns the sidecar process via the registered launcher (when
// l.Registry != nil), registers it, and flips the state to enabled.
//
// If the sidecar fails to start, the registry state is NOT flipped to
// enabled — the caller receives a wrapped error that includes "sidecar
// failed to start" so operators can grep for it.
func (l *Lifecycle) Enable(ctx context.Context, name string) (*RegistryEntry, error) {
	entry, err := l.Store.Get(name)
	if err != nil {
		return nil, fmt.Errorf("enable: reading registry: %w", err)
	}
	if entry == nil {
		return nil, fmt.Errorf("enable: addon %q is not installed; run 'sciclaw addon install <source>' first", name)
	}
	if entry.State == StateEnabled {
		return entry, nil
	}

	dir := l.AddonDir(name)
	manifestPath := filepath.Join(dir, "addon.json")
	manifest, err := ParseManifest(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("enable: %w", err)
	}

	bootstrapPath := ""
	if manifest.Bootstrap.Install != "" {
		bootstrapPath = resolveUnder(dir, manifest.Bootstrap.Install)
	}
	sidecarPath := sidecarBinaryPath(dir, manifest)

	if err := VerifyEntry(dir, entry, manifestPath, bootstrapPath, sidecarPath); err != nil {
		return nil, fmt.Errorf("enable: %w\nrun 'sciclaw addon upgrade %s' to re-pin against the current tree", err, name)
	}

	// Spawn the sidecar BEFORE flipping state — a crashed sidecar must not
	// leave a stale "enabled" entry in the registry file.
	if l.Registry != nil && manifest.Sidecar.Binary != "" {
		side, lerr := l.launcher().Launch(ctx, name, dir, manifest.Sidecar)
		if lerr != nil {
			return nil, fmt.Errorf("enable: sidecar failed to start for %q: %w", name, lerr)
		}
		l.Registry.Register(name, side)
	}

	entry.State = StateEnabled
	if err := l.Store.Set(name, entry); err != nil {
		// Best-effort: the sidecar is already running but the registry
		// write failed, so unregister + stop so we don't leak. The
		// operator will see the save error and retry.
		if l.Registry != nil {
			if side := l.Registry.Lookup(name); side != nil {
				stopCtx, cancel := context.WithTimeout(context.Background(), stopSidecarTimeout)
				_ = side.Stop(stopCtx)
				cancel()
			}
			l.Registry.Unregister(name)
		}
		return nil, fmt.Errorf("enable: saving registry: %w", err)
	}
	return entry, nil
}

// Disable marks an addon as installed-but-not-enabled and, when
// l.Registry != nil, stops+unregisters the live sidecar. Sidecar Stop errors
// are not fatal: we log via the injected StopLog if available, then proceed
// with the state flip. A hung sidecar must never prevent disable from
// completing — operators have to be able to recover the state machine.
func (l *Lifecycle) Disable(ctx context.Context, name string) (*RegistryEntry, error) {
	entry, err := l.Store.Get(name)
	if err != nil {
		return nil, fmt.Errorf("disable: reading registry: %w", err)
	}
	if entry == nil {
		return nil, fmt.Errorf("disable: addon %q is not installed", name)
	}

	// Always attempt to stop + unregister the sidecar, even if the registry
	// entry is already marked "installed" — a caller may have invoked
	// Disable after a crash to clean up leaked process handles.
	l.stopAndUnregister(ctx, name)

	if entry.State == StateInstalled {
		return entry, nil
	}
	entry.State = StateInstalled
	if err := l.Store.Set(name, entry); err != nil {
		return nil, fmt.Errorf("disable: saving registry: %w", err)
	}
	return entry, nil
}

// stopAndUnregister stops a live sidecar (if any) with a bounded timeout and
// removes it from the registry. Errors are discarded: we cannot block
// Disable/Uninstall/Upgrade on a misbehaving addon, and the process is
// killed via SIGKILL as part of Sidecar.Stop's fallback ladder regardless.
func (l *Lifecycle) stopAndUnregister(ctx context.Context, name string) {
	if l.Registry == nil {
		return
	}
	side := l.Registry.Lookup(name)
	if side == nil {
		l.Registry.Unregister(name)
		return
	}
	stopCtx, cancel := context.WithTimeout(ctx, stopSidecarTimeout)
	defer cancel()
	_ = side.Stop(stopCtx)
	l.Registry.Unregister(name)
}

// Uninstall runs the optional uninstall hook, removes the install directory,
// and deletes the registry entry. Enabled addons are refused unless force is
// true, mirroring RFC section 2.
func (l *Lifecycle) Uninstall(ctx context.Context, name string, force bool) error {
	// H2: serialize with concurrent install/upgrade/uninstall of the
	// same name. Blocks until we own the lock.
	lock, err := AcquireLock(l.SciclawHome, name)
	if err != nil {
		return fmt.Errorf("uninstall: %w", err)
	}
	defer lock.Release()
	entry, err := l.Store.Get(name)
	if err != nil {
		return fmt.Errorf("uninstall: reading registry: %w", err)
	}
	if entry == nil {
		if force {
			return nil
		}
		return fmt.Errorf("uninstall: addon %q is not installed", name)
	}
	if entry.State == StateEnabled && !force {
		return fmt.Errorf("uninstall: addon %q is enabled; run 'sciclaw addon disable %s' first, or pass --force", name, name)
	}

	// If the caller reached here with --force on an enabled addon, the
	// live sidecar is still running. Tear it down before removing files so
	// the process does not keep the addon directory mapped.
	l.stopAndUnregister(ctx, name)

	dir := l.AddonDir(name)
	// Best-effort uninstall hook; we still tear down even if it fails, so
	// the user is not left with a partially removed addon.
	if manifest, perr := ParseManifest(filepath.Join(dir, "addon.json")); perr == nil {
		if manifest.Bootstrap.Uninstall != "" {
			script := resolveUnder(dir, manifest.Bootstrap.Uninstall)
			env := []string{
				"SCICLAW_HOME=" + l.SciclawHome,
				"ADDON_DIR=" + dir,
				"ADDON_NAME=" + name,
			}
			if _, err := execScript(ctx, dir, script, env); err != nil && !force {
				return fmt.Errorf("uninstall: bootstrap %q failed: %w; pass --force to ignore", manifest.Bootstrap.Uninstall, err)
			}
		}
	}

	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("uninstall: removing %s: %w", dir, err)
	}
	if err := l.Store.Delete(name); err != nil {
		return fmt.Errorf("uninstall: deleting registry entry: %w", err)
	}
	return nil
}

// Upgrade advances an installed addon to a new commit. If ref is the zero
// value, the prior pinning strategy (track → auto-latest → signed tag) is
// reused. Returns ErrAlreadyAtCommit wrapped with the current entry when the
// resolved commit is unchanged.
//
// If the addon is currently enabled, Upgrade stops the running sidecar
// BEFORE the git checkout (so the on-disk binary is safe to swap), then
// starts a new sidecar after the hashes are recomputed. If the new sidecar
// fails to start, the registry entry moves back to StateInstalled and a
// clear error is returned pointing at `sciclaw addon enable <name>`.
func (l *Lifecycle) Upgrade(ctx context.Context, name string, ref InstallRef) (*RegistryEntry, error) {
	// H2: block concurrent install/upgrade/uninstall of the same name.
	lock, err := AcquireLock(l.SciclawHome, name)
	if err != nil {
		return nil, fmt.Errorf("upgrade: %w", err)
	}
	defer lock.Release()
	entry, err := l.Store.Get(name)
	if err != nil {
		return nil, fmt.Errorf("upgrade: reading registry: %w", err)
	}
	if entry == nil {
		return nil, fmt.Errorf("upgrade: addon %q is not installed", name)
	}

	wasEnabled := entry.State == StateEnabled

	dir := l.AddonDir(name)

	if _, err := l.Runner.Run(ctx, dir, "git fetch --tags --quiet", nil); err != nil {
		return nil, fmt.Errorf("upgrade: git fetch in %s: %w", dir, err)
	}

	effective := ref
	if refIsZero(ref) {
		switch {
		case entry.Track != nil && *entry.Track != "":
			effective = NewTrackRef(*entry.Track)
		case entry.SignedTag != nil && *entry.SignedTag != "":
			effective = NewAutoRef()
		default:
			return nil, fmt.Errorf("upgrade: addon %q has no pinning strategy recorded; pass --commit/--version/--track explicitly", name)
		}
	}

	resolved, err := Resolve(dir, effective)
	if err != nil {
		return nil, fmt.Errorf("upgrade: resolving ref: %w", err)
	}
	if resolved.Commit == entry.InstalledCommit {
		return entry, fmt.Errorf("%w: %s is at %s", ErrAlreadyAtCommit, name, resolved.Commit)
	}

	// Stop the live sidecar before touching the binary on disk. If the
	// addon was not enabled, or the registry is nil, this is a no-op.
	if wasEnabled {
		l.stopAndUnregister(ctx, name)
	}

	if _, err := l.Runner.Run(ctx, dir, "git checkout -q "+resolved.Commit, nil); err != nil {
		return nil, fmt.Errorf("upgrade: checking out %s: %w", resolved.Commit, err)
	}

	manifest, err := ParseManifest(filepath.Join(dir, "addon.json"))
	if err != nil {
		return nil, fmt.Errorf("upgrade: re-parsing manifest: %w", err)
	}
	if err := ValidateRequirements(manifest, l.SciclawVers, l.Platform, l.LookPath); err != nil {
		return nil, fmt.Errorf("upgrade: %w", err)
	}
	manifestSHA, bootstrapSHA, sidecarSHA, err := ComputeHashes(dir, manifest)
	if err != nil {
		return nil, fmt.Errorf("upgrade: computing hashes: %w", err)
	}

	prev := entry.InstalledCommit
	// Start from the original state; if restart fails below we drop back
	// to StateInstalled before saving.
	updated := &RegistryEntry{
		Version:           manifest.Version,
		InstalledAt:       l.now().UTC().Format(time.RFC3339),
		InstalledCommit:   resolved.Commit,
		ManifestSHA256:    manifestSHA,
		BootstrapSHA256:   bootstrapSHA,
		SidecarSHA256:     sidecarSHA,
		State:             entry.State,
		Source:            entry.Source,
		Track:             entry.Track,
		SignedTag:         nullableTag(resolved.SignedTag),
		SignatureVerified: resolved.SignatureVerified,
		PreviousCommit:    &prev,
	}

	// Restart the sidecar from the new binary, if applicable. Failure here
	// drops the state to installed and returns an actionable error.
	var restartErr error
	if wasEnabled && l.Registry != nil && manifest.Sidecar.Binary != "" {
		side, lerr := l.launcher().Launch(ctx, name, dir, manifest.Sidecar)
		if lerr != nil {
			restartErr = lerr
			updated.State = StateInstalled
		} else {
			l.Registry.Register(name, side)
		}
	}

	if err := l.Store.Set(name, updated); err != nil {
		// H5 fix: if Store.Set fails after the git checkout has already
		// advanced the working tree, we'd leave the on-disk state at the
		// new commit but the registry still pointing at the old one. The
		// next integrity check would fail on startup and the operator
		// would see a confusing "commit drift" error. Roll the working
		// tree back to the previous commit so the persistent state
		// remains consistent, and stop any newly-spawned sidecar.
		rollbackErr := l.rollbackUpgradeWorkingTree(ctx, dir, prev)
		if wasEnabled && l.Registry != nil && restartErr == nil {
			// We had successfully restarted the sidecar against the new
			// binary; tear it down so we don't leave a live process
			// running against a version the registry doesn't know about.
			l.stopAndUnregister(ctx, name)
		}
		if rollbackErr != nil {
			return nil, fmt.Errorf("upgrade: saving registry: %w; working-tree rollback to %s ALSO FAILED: %v — run 'sciclaw addon verify %s' and consider manual git checkout", err, prev[:12], rollbackErr, name)
		}
		return nil, fmt.Errorf("upgrade: saving registry: %w (working tree rolled back to %s)", err, prev[:12])
	}
	if restartErr != nil {
		return updated, fmt.Errorf("upgrade: new sidecar failed to start for %q: %w; addon is now installed but not enabled — run 'sciclaw addon enable %s' to retry", name, restartErr, name)
	}
	return updated, nil
}

// rollbackUpgradeWorkingTree is called when Store.Set fails mid-upgrade
// to keep disk state and registry state consistent. It tries a best-effort
// `git checkout -q <prev>` in the addon directory.
func (l *Lifecycle) rollbackUpgradeWorkingTree(ctx context.Context, dir, prevCommit string) error {
	if prevCommit == "" {
		return fmt.Errorf("no previous commit recorded")
	}
	_, err := l.Runner.Run(ctx, dir, "git checkout -q "+prevCommit, nil)
	return err
}

// List returns every registered addon entry sorted by name.
func (l *Lifecycle) List(ctx context.Context) ([]*RegistryEntry, error) {
	names, err := l.Store.List()
	if err != nil {
		return nil, err
	}
	out := make([]*RegistryEntry, 0, len(names))
	for _, n := range names {
		e, err := l.Store.Get(n)
		if err != nil {
			return nil, err
		}
		if e != nil {
			out = append(out, e)
		}
	}
	return out, nil
}

