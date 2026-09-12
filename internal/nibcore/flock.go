package nibcore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// writeLockPath returns a stable, per-machine path for the advisory write lock
// guarding the .nibs data directory at root. It lives in the OS temp dir, keyed
// by a hash of the absolute root, so every process targeting the same .nibs
// shares one lock file without placing anything inside either git repo.
// Per-machine only: it does not coordinate across hosts.
func writeLockPath(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(lockDir(), "nibs-write-"+hex.EncodeToString(sum[:8])+".lock")
}

// lockDir is the directory every nibs lock file goes in — this one and
// servelock.go's.
func lockDir() string {
	return os.TempDir()
}

// StoreLock is proof of holding the store-wide advisory write lock returned by
// AcquireStoreLock. The migration mutators take it as a parameter: they cannot
// self-acquire, because the flock is per-descriptor (see AcquireStoreLock).
//
// Core.requireStoreLock refuses a token acquired for another store, or released
// early, rather than accepting any non-nil value.
type StoreLock struct {
	release func() error
	// lockPath is the writeLockPath key of the store this token was acquired
	// for — the same derivation Core.New records, so the two compare equal
	// for any spelling of the same store root.
	lockPath string
	// released is set by Release. Not synchronized: acquire the token, thread it
	// through one migration run, and release it on a single goroutine.
	released bool
}

// Release drops the lock and closes its descriptor. Idempotent: a second call
// returns nil without touching the descriptor, and the token proves nothing
// after the first — released stays set and the migration mutators refuse it.
//
// Keep the released short-circuit. The Unix release closure re-derives f.Fd()
// and would fail safely with EBADF, but flock_windows.go captures
// windows.Handle(f.Fd()) once at acquisition, so a post-Close re-release there
// would operate on a stale handle value the OS may have reassigned ([Unverified]
// on Windows — not testable on this machine).
func (l *StoreLock) Release() error {
	if l.released {
		return nil
	}
	l.released = true
	return l.release()
}

// AcquireStoreLock takes the cross-process advisory write lock guarding the
// .nibs data directory at nibsRoot and returns a *StoreLock token — the
// migration mutators' proof-of-lock parameter, released via Release. It is the
// same lock every Core mutator holds per-operation, so a caller holding it
// excludes every cooperating WRITER (serve's mutations, a concurrent CLI's) for
// the duration.
//
// It does NOT stop serve's READERS or its fsnotify watcher: neither acquires
// this lock, so a live serve can observe half-migrated states mid-run, and a
// writer parked on this lock can hold a pre-acquisition snapshot it writes
// back after release (the stale-clone chain documented at Core.Update).
//
// AcquireServeExclusion (servelock.go) closes that residual by fencing a
// migration out of a live serve process. It cannot reach a serve from a release
// predating that interlock, which never takes the serve lock; `nibs migrate`
// names that case before it applies.
//
// The lock derivation MUST stay keyed on the .nibs directory itself, NOT any
// subdirectory (e.g. a future data/): serve derives its per-mutation lock from
// the same root, and the two stop excluding each other the moment they disagree
// on the key.
//
// CANONICAL INVARIANT (the per-descriptor flock rule). This doc is its single
// authoritative statement.
//
// WARNING: the flock is per-file-descriptor, so acquiring it twice in one
// process deadlocks. Code running under this lock (the migration Core methods)
// must not call mutators that take the per-operation lock themselves
// (Create/Update/Delete/...).
func AcquireStoreLock(nibsRoot string) (*StoreLock, error) {
	lockPath := writeLockPath(nibsRoot)
	release, err := acquireFileLock(lockPath)
	if err != nil {
		return nil, err
	}
	return &StoreLock{release: release, lockPath: lockPath}, nil
}

// errLockHeld is the lock layer's contention signal: another descriptor holds
// the file. Unexported — each waiter restates it in its own vocabulary.
var errLockHeld = errors.New("another descriptor holds this lock file")

// How long acquireFileLockWaiting sleeps between tries, doubling from the first
// up to the cap. Polling is not fair: against continuous contenders a waiter
// takes the lock only when it samples a free window.
const (
	storeLockFirstRetry = 1 * time.Millisecond
	storeLockMaxRetry   = 50 * time.Millisecond
)

// StoreLockWaitEndedError reports a wait for the store's cross-process write
// lock that ended before the lock was free, because the caller's context was
// canceled or its deadline passed. Nothing was locked, so nothing was written.
// It unwraps to the context error, so errors.Is reaches context.Canceled and
// context.DeadlineExceeded.
type StoreLockWaitEndedError struct {
	Waited time.Duration
	Cause  error
}

func (e *StoreLockWaitEndedError) Error() string {
	return fmt.Sprintf("the wait for this store's write lock ended after %s: %v",
		e.Waited.Round(time.Millisecond), e.Cause)
}

func (e *StoreLockWaitEndedError) Unwrap() error { return e.Cause }

// acquireFileLockWaiting waits for the exclusive lock at path the way
// acquireFileLock does, but gives up when ctx ends, returning
// *StoreLockWaitEndedError and no lock.
//
// The cancellable wait must poll the non-blocking primitive, not wrap the
// blocking one: that call takes no deadline on either platform, so abandoning it
// leaves a goroutine holding an open descriptor that then takes the lock for a
// caller that has gone. Polling leaves an abandoned wait at zero descriptors and
// zero goroutines.
//
// A context that can never be canceled takes the blocking primitive instead:
// there is nothing for a poll loop to notice.
func acquireFileLockWaiting(ctx context.Context, path string) (func() error, error) {
	if ctx.Done() == nil {
		return acquireFileLock(path)
	}

	started := time.Now()
	delay := storeLockFirstRetry
	timer := time.NewTimer(delay)
	defer timer.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return nil, &StoreLockWaitEndedError{Waited: time.Since(started), Cause: err}
		}
		release, err := acquireFileLockTry(path)
		if !errors.Is(err, errLockHeld) {
			return release, err
		}
		select {
		case <-ctx.Done():
			return nil, &StoreLockWaitEndedError{Waited: time.Since(started), Cause: ctx.Err()}
		case <-timer.C:
		}
		delay = min(2*delay, storeLockMaxRetry)
		timer.Reset(delay)
	}
}
