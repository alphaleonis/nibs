package nibcore

import (
	"errors"
	"testing"
)

// TestServeLockFencesMigrateOutOfALiveServe pins the interlock `nibs migrate`
// needs and the advisory print could only ask for.
//
// It is a SECOND lock file, not the per-operation write lock. Serve holds this
// one for its whole lifetime, and a serve holding the write lock that long would
// block its own mutations — the flock is per descriptor, so a process cannot
// take it twice.
//
// Shared on serve's side so several serves (or a serve and a read-only command)
// coexist; exclusive on migrate's, so one live serve is enough to refuse. Both
// sides are NON-BLOCKING: a migrate that waited would hang for as long as
// somebody left a browser tab open, and a serve that waited would hang mid-boot
// behind a migration it cannot see.
func TestServeLockFencesMigrateOutOfALiveServe(t *testing.T) {
	root := t.TempDir()

	t.Run("two serves of one store coexist", func(t *testing.T) {
		first, err := AcquireServeLock(root)
		if err != nil {
			t.Fatalf("first serve could not take the lock: %v", err)
		}
		defer func() { _ = first.Release() }()

		second, err := AcquireServeLock(root)
		if err != nil {
			t.Fatalf("a second serve was refused by the first: %v", err)
		}
		if err := second.Release(); err != nil {
			t.Errorf("releasing the second serve lock: %v", err)
		}
	})

	t.Run("migrate is refused while a serve holds it", func(t *testing.T) {
		serving, err := AcquireServeLock(root)
		if err != nil {
			t.Fatalf("AcquireServeLock: %v", err)
		}
		defer func() { _ = serving.Release() }()

		_, err = AcquireServeExclusion(root)
		if !errors.Is(err, ErrStoreServed) {
			t.Fatalf("migrate was not fenced out of a live serve: err = %v, want ErrStoreServed", err)
		}
	})

	t.Run("migrate proceeds once the serve is gone", func(t *testing.T) {
		serving, err := AcquireServeLock(root)
		if err != nil {
			t.Fatalf("AcquireServeLock: %v", err)
		}
		if err := serving.Release(); err != nil {
			t.Fatalf("Release: %v", err)
		}

		fence, err := AcquireServeExclusion(root)
		if err != nil {
			t.Fatalf("migrate stayed fenced out after the serve released: %v", err)
		}
		if err := fence.Release(); err != nil {
			t.Errorf("releasing the exclusion: %v", err)
		}
	})

	t.Run("a serve cannot start under a running migrate", func(t *testing.T) {
		fence, err := AcquireServeExclusion(root)
		if err != nil {
			t.Fatalf("AcquireServeExclusion: %v", err)
		}
		defer func() { _ = fence.Release() }()

		if _, err := AcquireServeLock(root); !errors.Is(err, ErrStoreServed) {
			t.Fatalf("a serve booted under a live migration: err = %v, want ErrStoreServed", err)
		}
	})

	t.Run("the serve lock is a different file from the write lock", func(t *testing.T) {
		// Sharing one file would deadlock serve against its own mutations.
		if serveLockPath(root) == writeLockPath(root) {
			t.Error("the serve lock and the per-operation write lock share a file")
		}
	})
}
