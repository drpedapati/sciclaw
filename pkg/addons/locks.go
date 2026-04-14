package addons

// locks.go implements per-addon-name exclusive file locks so concurrent
// `sciclaw addon install`/`upgrade`/`uninstall` invocations on the same
// host cannot race. The lock is held by Install/Upgrade/Uninstall for the
// duration of the operation — both CLI processes and in-gateway callers
// converge on the same lockfile at `~/sciclaw/addons/<name>.lock`.
//
// Uses flock(2) which is available on Linux and macOS (the two sciclaw
// targets). On both, flock locks are associated with the open file
// description, so a new Open in the same process produces a fresh lock
// and same-process concurrent callers DO serialize as long as each
// obtains its own lock handle — which the API below guarantees because
// every caller goes through AcquireLock and closes the returned file
// when done.
//
// The lockfile is never unlinked. That's deliberate: an unlink between
// lock and release would let a second process race through ENOENT.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// AddonLock is a held exclusive lock on a per-addon lockfile. Call
// Release when the operation completes. Release is safe to call
// multiple times.
type AddonLock struct {
	f *os.File
}

// Release closes the lock file descriptor, releasing the flock. Safe to
// call multiple times; the second call is a no-op.
func (l *AddonLock) Release() {
	if l == nil || l.f == nil {
		return
	}
	_ = l.f.Close()
	l.f = nil
}

// AcquireLock takes an exclusive flock on the per-name lockfile under
// sciclawHome/addons/. Blocks until the lock is available. Returns the
// lock handle; caller must Release it via defer.
//
// name is validated as a safety guardrail — AcquireLock is often called
// before Manifest.Validate runs, so a hostile --name flag could otherwise
// escape the addons directory via the path join. The addon-name charset
// is [a-z0-9._-]{1,64} with no leading dot, the same rule Manifest.Validate
// enforces.
func AcquireLock(sciclawHome, name string) (*AddonLock, error) {
	if err := ValidateAddonName(name); err != nil {
		return nil, err
	}
	if strings.TrimSpace(sciclawHome) == "" {
		return nil, fmt.Errorf("addon lock: sciclawHome is empty")
	}
	lockDir := filepath.Join(sciclawHome, "addons")
	if err := os.MkdirAll(lockDir, 0o755); err != nil {
		return nil, fmt.Errorf("addon lock: creating %s: %w", lockDir, err)
	}
	lockPath := filepath.Join(lockDir, name+".lock")
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("addon lock: opening %s: %w", lockPath, err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("addon lock: flock %s: %w", lockPath, err)
	}
	return &AddonLock{f: f}, nil
}
