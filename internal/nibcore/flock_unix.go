//go:build !windows

package nibcore

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"syscall"

	"golang.org/x/sys/unix"
)

// lockDirName is per user because the OS temp dir is usually shared between
// users, and a lock directory another user owns could hold a planted lock file.
func lockDirName() string {
	return "nibs-" + strconv.Itoa(os.Getuid())
}

// prepareLockDir creates dir private to this user, or refuses an existing one
// that is a symlink, another user's, or open to other users. A private
// non-directory passes here and fails the open of the lock file inside it.
func prepareLockDir(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("creating lock directory: %w", err)
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("checking lock directory: %w", err)
	}
	var problem string
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		problem = "it is a symlink"
	case info.Mode().Perm()&0o077 != 0:
		problem = fmt.Sprintf("its mode %04o gives other users access", info.Mode().Perm())
	default:
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			problem = "its owner cannot be read"
		} else if uid := os.Getuid(); int64(st.Uid) != int64(uid) {
			problem = fmt.Sprintf("it is owned by uid %d, not by this user (uid %d)", st.Uid, uid)
		}
	}
	if problem != "" {
		return fmt.Errorf("refusing lock directory %s: %s", dir, problem)
	}
	return nil
}

// acquireFileLock blocks until it holds an exclusive advisory lock (flock LOCK_EX)
// on the file at path, creating the file if needed, then returns a release func
// that drops the lock and closes the descriptor. Advisory: only cooperating
// callers (every nibs process) are constrained.
func acquireFileLock(path string) (func() error, error) {
	f, err := openLockFile(path)
	if err != nil {
		return nil, err
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

// acquireFileLockTry takes the same exclusive lock acquireFileLock waits for, in
// ONE try, reporting contention as errLockHeld.
func acquireFileLockTry(path string) (func() error, error) {
	return acquireFileLockNB(path, unix.LOCK_EX)
}

// acquireFileLockShared is the serve interlock's shared side (see servelock.go).
func acquireFileLockShared(path string) (func() error, error) {
	release, err := acquireFileLockNB(path, unix.LOCK_SH)
	return release, asStoreServed(err)
}

func acquireFileLockNB(path string, how int) (func() error, error) {
	f, err := openLockFile(path)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), how|unix.LOCK_NB); err != nil {
		_ = f.Close()
		// EWOULDBLOCK and EAGAIN are the same value on Linux but distinct
		// constants elsewhere, so both are tested rather than one assumed.
		if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
			return nil, errLockHeld
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
