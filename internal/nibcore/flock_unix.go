//go:build !windows

package nibcore

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// acquireFileLock blocks until it holds an exclusive advisory lock (flock LOCK_EX)
// on the file at path, creating the file if needed, then returns a release func
// that drops the lock and closes the descriptor. Advisory locks only constrain
// other cooperating callers (every nibs process), which is sufficient here — a
// non-nibs writer is still caught by the on-disk etag check on the next mutation.
func acquireFileLock(path string) (func() error, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening lock file: %w", err)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("acquiring lock: %w", err)
	}
	return func() error {
		// Closing the descriptor releases the flock; unlock explicitly first so a
		// release error surfaces distinctly from a close error.
		unlockErr := unix.Flock(int(f.Fd()), unix.LOCK_UN)
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
