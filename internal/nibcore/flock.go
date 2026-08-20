package nibcore

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

// writeLockPath returns a stable, per-machine path for the advisory write lock
// guarding the .nibs data directory at root. It lives in the OS temp dir, keyed
// by a hash of the absolute root, so every process targeting the same .nibs
// shares one lock file without placing anything inside either git repo.
//
// The lock is deliberately per-machine: cross-machine coordination is git's job
// (separate clones sync via commits/merges, not by writing shared files), so a
// temp-dir lock — which reliably serializes processes on one host — is exactly
// the right scope. It also sidesteps flock's unreliability on network filesystems
// for the local case.
func writeLockPath(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(os.TempDir(), "nibs-write-"+hex.EncodeToString(sum[:8])+".lock")
}

// StoreLock is proof of holding the store-wide advisory write lock returned
// by AcquireStoreLock. The migration mutators (Core.MigrateV0ToV1,
// Core.NormalizeLegacyPriorities) require it as a parameter, encoding their
// "caller holds the lock" precondition in the type system: they cannot
// self-acquire — the flock is per-descriptor, so re-acquiring in-process
// deadlocks — and a doc-comment convention alone invites a future direct
// caller (serve auto-migrate, the TUI) to run a cross-process-unsynchronized
// read-modify-write.
//
// The token is bound to what it proves (see Core.requireStoreLock): lockPath
// records WHICH store's lock it holds, and released records whether it still
// holds it — so a token acquired for another store, or released early, is
// refused instead of silently passing a non-nil check. Exactly the future
// direct callers above are who could misuse it either way.
type StoreLock struct {
	release func() error
	// lockPath is the writeLockPath key of the store this token was acquired
	// for — the same derivation Core.New records, so the two compare equal
	// for any spelling of the same store root.
	lockPath string
	// released is set by Release. Not synchronized: the token is meant to be
	// acquired, threaded through one migration run, and released by a single
	// goroutine (runMigrations' shape).
	released bool
}

// Release drops the lock and closes its descriptor. Idempotent: a second call
// returns nil without touching the descriptor. Double-release is tolerated
// because this package's own tests pair a t.Cleanup release with an explicit
// early Release (to exercise the released-token refusal), so teardown
// re-releases the same token. The token proves nothing after the first call —
// released stays set and the migration mutators refuse it.
//
// The guard also keeps a second call away from the platform release closures:
// the Unix closure re-derives f.Fd() and would fail safely with EBADF, but
// flock_windows.go captures windows.Handle(f.Fd()) once at acquisition, so a
// post-Close re-release there would operate on a stale handle value the OS may
// have reassigned ([Unverified] on Windows — not testable on this machine).
func (l *StoreLock) Release() error {
	if l.released {
		return nil
	}
	l.released = true
	return l.release()
}

// AcquireStoreLock takes the cross-process advisory write lock guarding the
// .nibs data directory at nibsRoot and returns a *StoreLock token — the
// migration mutators' proof-of-lock parameter, released via Release. It is
// the same lock every Core mutator holds per-operation, so a caller holding
// it excludes every cooperating WRITER (serve's mutations, a concurrent
// CLI's) for the duration — which is what a whole-store migration run needs
// to stage its rewrites.
//
// It does NOT stop serve's READERS or its fsnotify watcher: neither acquires
// this lock, so a live serve can observe half-migrated states mid-run, and a
// writer parked on this lock can hold a pre-acquisition snapshot it writes
// back after release (the stale-clone chain documented at Core.Update).
//
// That residual is closed by a SECOND lock rather than by widening this one:
// AcquireServeExclusion (servelock.go) fences a migration out of a live serve
// process, which is a guarantee no per-operation lock can express — serve cannot
// hold this one for its lifetime without blocking its own mutations. What this
// lock still cannot reach is a serve from a release predating that interlock,
// which never takes it; `nibs migrate` names that case before it applies.
//
// The lock derivation MUST stay keyed on the .nibs directory itself, NOT any
// subdirectory (e.g. a future data/): serve derives its per-mutation lock from
// the same root, and the two stop excluding each other the moment they disagree
// on the key.
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
