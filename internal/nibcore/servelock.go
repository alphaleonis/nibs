package nibcore

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
)

// ErrStoreServed is returned when the serve interlock is held the other way: a
// migration could not start because a serve is live, or a serve could not start
// because a migration is running. The remedy is to stop the other process, not
// to fix the filesystem.
var ErrStoreServed = errors.New("another nibs process holds this store")

func asStoreServed(err error) error {
	if errors.Is(err, errLockHeld) {
		return ErrStoreServed
	}
	return err
}

// acquireFileLockExclusiveNB is the interlock's exclusive side: one try, no wait.
func acquireFileLockExclusiveNB(path string) (func() error, error) {
	release, err := acquireFileLockTry(path)
	return release, asStoreServed(err)
}

// serveLockPath is the per-machine path of the SERVE-lifetime lock for the store
// at root, derived like writeLockPath but a different file.
//
// It cannot share the write lock's file: serve holds this one for its whole
// lifetime, and the flock is per descriptor (see AcquireStoreLock), so a serve
// holding the write lock that long would block its own mutations.
func serveLockPath(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		abs = root
	}
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(lockDir(), "nibs-serve-"+hex.EncodeToString(sum[:8])+".lock")
}

// ServeLock is proof of holding one side of the serve interlock, released via
// Release. Both sides return the same token.
type ServeLock struct {
	release  func() error
	released bool
}

// Release drops the lock and closes its descriptor. Idempotent: a second call
// returns nil.
func (l *ServeLock) Release() error {
	if l.released {
		return nil
	}
	l.released = true
	return l.release()
}

// AcquireServeLock takes the SHARED side of the interlock, which `nibs serve`
// holds for its whole lifetime to say "this store is being served". Several
// serves of one store coexist; what it excludes is the exclusive side below.
//
// It does not block: a serve that waited would hang mid-boot behind a migration
// it cannot see or report.
func AcquireServeLock(nibsRoot string) (*ServeLock, error) {
	release, err := acquireFileLockShared(serveLockPath(nibsRoot))
	if err != nil {
		return nil, err
	}
	return &ServeLock{release: release}, nil
}

// AcquireServeExclusion takes the EXCLUSIVE side, which `nibs migrate` holds
// across a run so no serve can be live while the store's shape changes. This is
// the enforcement AcquireStoreLock's doc defers here: excluding the serve process
// itself is the guarantee a per-operation lock cannot provide.
//
// It does not block: a migrate that waited would wait for as long as somebody
// left a browser tab open.
func AcquireServeExclusion(nibsRoot string) (*ServeLock, error) {
	release, err := acquireFileLockExclusiveNB(serveLockPath(nibsRoot))
	if err != nil {
		return nil, err
	}
	return &ServeLock{release: release}, nil
}
