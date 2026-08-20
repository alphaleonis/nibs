//go:build !windows

package nibcore

import (
	"errors"
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

// acquireFileLockShared and acquireFileLockExclusiveNB are the serve interlock's
// two sides (see servelock.go). Both are NON-BLOCKING — LOCK_NB — and report
// contention as ErrStoreServed so the caller can tell "the other process holds
// it" from "the filesystem failed", which are different remedies.
func acquireFileLockShared(path string) (func() error, error) {
	return acquireFileLockNB(path, unix.LOCK_SH)
}

func acquireFileLockExclusiveNB(path string) (func() error, error) {
	return acquireFileLockNB(path, unix.LOCK_EX)
}

func acquireFileLockNB(path string, how int) (func() error, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("opening lock file: %w", err)
	}
	if err := unix.Flock(int(f.Fd()), how|unix.LOCK_NB); err != nil {
		_ = f.Close()
		// EWOULDBLOCK and EAGAIN are the same value on Linux but distinct
		// constants elsewhere, so both are tested rather than one assumed.
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, ErrStoreServed
		}
		return nil, fmt.Errorf("acquiring lock: %w", err)
	}
	return func() error {
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
