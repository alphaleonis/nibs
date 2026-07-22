//go:build windows

package nibcore

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// acquireFileLock blocks until it holds an exclusive lock on the whole file at
// path (creating it if needed) via LockFileEx, then returns a release func that
// unlocks and closes the handle. Mirrors the unix flock implementation so the
// per-operation write lock behaves identically across platforms.
func acquireFileLock(path string) (func() error, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening lock file: %w", err)
	}
	handle := windows.Handle(f.Fd())
	overlapped := new(windows.Overlapped)
	// LOCKFILE_EXCLUSIVE_LOCK without LOCKFILE_FAIL_IMMEDIATELY blocks until the
	// lock is available. Lock the entire file (0xFFFFFFFF bytes, high+low).
	if err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 0xFFFFFFFF, 0xFFFFFFFF, overlapped); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("acquiring lock: %w", err)
	}
	return func() error {
		unlockErr := windows.UnlockFileEx(handle, 0, 0xFFFFFFFF, 0xFFFFFFFF, overlapped)
		closeErr := f.Close()
		if unlockErr != nil {
			return fmt.Errorf("releasing lock: %w", unlockErr)
		}
		if closeErr != nil {
			return fmt.Errorf("closing lock file: %w", closeErr)
		}
		return nil
	}, nil
}
