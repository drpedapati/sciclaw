//go:build windows

package addons

// locks_windows.go is a no-op fallback for Windows where syscall.Flock
// is not available. Install/Upgrade/Uninstall are not serialized on
// Windows; concurrent CLI invocations may race. This is acceptable
// because sciClaw's primary targets are Linux and macOS servers.

import (
	"fmt"
	"strings"
)

// AddonLock is a held lock. On Windows this is a no-op.
type AddonLock struct{}

// Release is a no-op on Windows.
func (l *AddonLock) Release() {}

// AcquireLock on Windows validates the name but does not take a real
// file lock. Concurrent installs may race.
func AcquireLock(sciclawHome, name string) (*AddonLock, error) {
	if err := ValidateAddonName(name); err != nil {
		return nil, err
	}
	if strings.TrimSpace(sciclawHome) == "" {
		return nil, fmt.Errorf("addon lock: sciclawHome is empty")
	}
	return &AddonLock{}, nil
}
