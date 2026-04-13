package addons

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// execScript runs a bootstrap script directly (no shell) with the supplied
// working directory and environment. The script path is passed as argv[0],
// so shell metacharacters in the path are harmless — exec takes a literal
// filename. Used for manifest-declared Bootstrap.Install/Uninstall scripts
// so a hostile manifest path cannot inject commands.
func execScript(ctx context.Context, dir, scriptPath string, env []string) ([]byte, error) {
	if scriptPath == "" {
		return nil, fmt.Errorf("execScript: empty script path")
	}
	cmd := exec.CommandContext(ctx, scriptPath)
	if dir != "" {
		cmd.Dir = dir
	}
	if len(env) > 0 {
		cmd.Env = env
	}
	return cmd.CombinedOutput()
}

// now returns Lifecycle.Now if set, otherwise wall-clock time. Split out so
// tests can freeze time without the whole Lifecycle struct being aware of it.
func (l *Lifecycle) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now()
}

// refIsZero reports whether an InstallRef has no pinning information. Used by
// Upgrade to decide whether to reuse the prior pinning strategy.
func refIsZero(r InstallRef) bool {
	return r.Commit == "" && r.Version == "" && r.Track == ""
}

// nullableTrack converts an InstallRef's Track field to a *string suitable
// for the RegistryEntry.Track JSON pointer.
func nullableTrack(r InstallRef) *string {
	if r.Track == "" {
		return nil
	}
	t := r.Track
	return &t
}

// nullableTag returns a *string for a non-empty tag or nil for an empty one.
func nullableTag(tag string) *string {
	if tag == "" {
		return nil
	}
	return &tag
}

// sidecarBinaryPath returns the best guess for where the sidecar binary lives,
// mirroring ComputeHashes so verification checks the same file that was hashed
// at install time.
func sidecarBinaryPath(addonDir string, manifest *Manifest) string {
	if manifest == nil || manifest.Sidecar.Binary == "" {
		return ""
	}
	primary := resolveUnder(addonDir, manifest.Sidecar.Binary)
	if info, err := os.Lstat(primary); err == nil && info.Mode().IsRegular() {
		return primary
	}
	alt := filepath.Join(addonDir, "bin", manifest.Sidecar.Binary)
	if info, err := os.Lstat(alt); err == nil && info.Mode().IsRegular() {
		return alt
	}
	return ""
}
