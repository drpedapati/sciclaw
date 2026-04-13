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
}

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

	// Second check: we may not have known the name before clone.
	if opts.Name == "" {
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
		if _, err := l.Runner.Run(ctx, finalDir, script, env); err != nil {
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

// Enable verifies the installed addon still matches its integrity record and
// flips the state to enabled. The actual sidecar spawn is the sidecar
// package's job — lifecycle only manages metadata.
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

	entry.State = StateEnabled
	if err := l.Store.Set(name, entry); err != nil {
		return nil, fmt.Errorf("enable: saving registry: %w", err)
	}
	return entry, nil
}

// Disable marks an addon as installed-but-not-enabled. It does not stop any
// running sidecar — the caller is responsible for calling sidecar.Stop first.
func (l *Lifecycle) Disable(ctx context.Context, name string) (*RegistryEntry, error) {
	entry, err := l.Store.Get(name)
	if err != nil {
		return nil, fmt.Errorf("disable: reading registry: %w", err)
	}
	if entry == nil {
		return nil, fmt.Errorf("disable: addon %q is not installed", name)
	}
	if entry.State == StateInstalled {
		return entry, nil
	}
	entry.State = StateInstalled
	if err := l.Store.Set(name, entry); err != nil {
		return nil, fmt.Errorf("disable: saving registry: %w", err)
	}
	return entry, nil
}

// Uninstall runs the optional uninstall hook, removes the install directory,
// and deletes the registry entry. Enabled addons are refused unless force is
// true, mirroring RFC section 2.
func (l *Lifecycle) Uninstall(ctx context.Context, name string, force bool) error {
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
			if _, err := l.Runner.Run(ctx, dir, script, env); err != nil && !force {
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
func (l *Lifecycle) Upgrade(ctx context.Context, name string, ref InstallRef) (*RegistryEntry, error) {
	entry, err := l.Store.Get(name)
	if err != nil {
		return nil, fmt.Errorf("upgrade: reading registry: %w", err)
	}
	if entry == nil {
		return nil, fmt.Errorf("upgrade: addon %q is not installed", name)
	}

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
	if err := l.Store.Set(name, updated); err != nil {
		return nil, fmt.Errorf("upgrade: saving registry: %w", err)
	}
	return updated, nil
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

