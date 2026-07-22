package nibcore

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
)

// TestAcquireFileLockSerializesHolders proves the advisory lock actually
// serializes concurrent holders: with the lock, at most one goroutine is ever
// inside the critical section. Each goroutine opens its own descriptor on the
// same path, so flock/LockFileEx must mutually exclude them. Remove the locking
// and maxConcurrent jumps above 1 — the guard bites.
func TestAcquireFileLockSerializesHolders(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "test.lock")

	var mu sync.Mutex // guards the observation counters, NOT the lock under test
	active, maxConcurrent := 0, 0

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := acquireFileLock(lockPath)
			if err != nil {
				t.Errorf("acquireFileLock: %v", err)
				return
			}
			defer func() { _ = release() }()

			mu.Lock()
			active++
			if active > maxConcurrent {
				maxConcurrent = active
			}
			mu.Unlock()

			time.Sleep(2 * time.Millisecond) // hold the critical section

			mu.Lock()
			active--
			mu.Unlock()
		}()
	}
	wg.Wait()

	if maxConcurrent != 1 {
		t.Fatalf("advisory lock did not serialize holders: max concurrent = %d, want 1", maxConcurrent)
	}
}

// TestAcquireFileLockReleaseAllowsReacquire confirms a released lock can be taken
// again (no permanent leak of the descriptor/lock).
func TestAcquireFileLockReleaseAllowsReacquire(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "test.lock")

	release, err := acquireFileLock(lockPath)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := release(); err != nil {
		t.Fatalf("release: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		r2, err := acquireFileLock(lockPath)
		if err != nil {
			done <- err
			return
		}
		done <- r2()
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("re-acquire after release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("re-acquire blocked after release — lock was not freed")
	}
}

// TestUpdateAcquiresWriteLock proves the write path participates in the advisory
// lock: while another holder owns the lock, Core.Update blocks, and it proceeds
// once the lock is released. Without the per-op lock in Update, the first select
// would observe an immediate completion — the guard bites.
func TestUpdateAcquiresWriteLock(t *testing.T) {
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, NibsDir)
	if err := os.MkdirAll(nibsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	core := New(nibsDir, config.Default())
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := core.Create(&nib.Nib{ID: "lock1", Slug: "lock", Title: "Lock", Status: "todo"}); err != nil {
		t.Fatalf("create: %v", err)
	}

	// Hold the Core's write lock externally, as a second process would.
	release, err := acquireFileLock(core.lockPath)
	if err != nil {
		t.Fatalf("external acquire: %v", err)
	}

	updateDone := make(chan error, 1)
	go func() {
		fresh, err := core.GetForUpdate("lock1")
		if err != nil {
			updateDone <- err
			return
		}
		fresh.Title = "Updated"
		updateDone <- core.Update(fresh, nil)
	}()

	select {
	case err := <-updateDone:
		t.Fatalf("Update completed while the write lock was held externally (err=%v)", err)
	case <-time.After(150 * time.Millisecond):
		// Expected: Update is blocked on the lock.
	}

	if err := release(); err != nil {
		t.Fatalf("external release: %v", err)
	}

	select {
	case err := <-updateDone:
		if err != nil {
			t.Fatalf("Update after lock release: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Update did not complete after the write lock was released")
	}

	got, err := core.Get("lock1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Title != "Updated" {
		t.Fatalf("Title = %q, want Updated", got.Title)
	}
}
