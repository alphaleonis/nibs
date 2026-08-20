package nibcore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
)

// ErrStoreServed is returned when the serve interlock is held the other way: a
// migration could not start because a serve is live, or a serve could not start
// because a migration is running. Callers distinguish it from an I/O failure to
// decide whether the remedy is "stop the other process" or "fix the filesystem".
var ErrStoreServed = errors.New("another nibs process holds this store")

// serveLockPath is the per-machine path of the SERVE-lifetime lock for the store
// at root, derived exactly like writeLockPath and deliberately a different file.
//
// It cannot share the write lock's file. Serve holds this one for its whole
// lifetime, and the flock is per descriptor, so a serve holding the write lock
// that long would block its own mutations — the deadlock AcquireStoreLock's
// warning describes, reached from the other direction.
func serveLockPath(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(os.TempDir(), "nibs-serve-"+hex.EncodeToString(sum[:8])+".lock")
}

// ServeLock is proof of holding one side of the serve interlock, released via
// Release. Both sides return the same token: what differs is the mode they took
// it in, and nothing downstream needs to tell them apart.
type ServeLock struct {
	release  func() error
	released bool
}

// Release drops the lock and closes its descriptor. Idempotent, matching
// StoreLock.Release — serve's shutdown path and a deferred cleanup may both fire.
func (l *ServeLock) Release() error {
	if l.released {
		return nil
	}
	l.released = true
	return l.release()
}

// AcquireServeLock takes the SHARED side of the interlock, which `nibs serve`
// holds for its whole lifetime to say "this store is being served".
//
// Shared, so several serves of one store coexist — two ports against one store is
// a legitimate thing to do, and refusing the second would be a regression with no
// safety story behind it. What it excludes is the exclusive side below.
//
// It does not block. A serve that waited would hang mid-boot behind a migration
// it cannot see or report, where failing tells the user exactly what to do.
func AcquireServeLock(nibsRoot string) (*ServeLock, error) {
	release, err := acquireFileLockShared(serveLockPath(nibsRoot))
	if err != nil {
		return nil, err
	}
	return &ServeLock{release: release}, nil
}

// AcquireServeExclusion takes the EXCLUSIVE side, which `nibs migrate` holds
// across a run so no serve can be live while the store's shape changes.
//
// This is the enforcement AcquireStoreLock's doc comment defers: that lock
// excludes cooperating WRITERS per operation, which leaves serve's readers, its
// watcher, and a writer parked on the lock holding a pre-migration snapshot it
// writes back afterwards. Excluding the serve process itself is the only version
// of that guarantee a per-operation lock cannot provide.
//
// It does not block, for the same reason as the shared side plus a sharper one: a
// migrate that waited would wait for as long as somebody left a browser tab open,
// with nothing on screen saying why.
func AcquireServeExclusion(nibsRoot string) (*ServeLock, error) {
	release, err := acquireFileLockExclusiveNB(serveLockPath(nibsRoot))
	if err != nil {
		return nil, err
	}
	return &ServeLock{release: release}, nil
}
