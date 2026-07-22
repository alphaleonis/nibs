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
