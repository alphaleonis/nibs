package nibcore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/store"
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

// TestStoreLockReleaseIdempotent pins Release's tolerated double-call: this
// package's own tests pair loadMigrationCore's t.Cleanup release with an
// explicit early Release (TestMigrationMethodsRequireLockToken exercises the
// released-token refusal), so teardown re-releases the same token. The second
// call must be a no-op returning nil rather than running the platform release
// closure against a closed descriptor (EBADF on Unix), and the token must
// keep proving nothing: released stays set, so requireStoreLock still refuses
// it (pinned by TestMigrationMethodsRequireLockToken).
func TestStoreLockReleaseIdempotent(t *testing.T) {
	nibsRoot := filepath.Join(t.TempDir(), store.DirName)
	if err := os.MkdirAll(nibsRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	lock, err := AcquireStoreLock(nibsRoot)
	if err != nil {
		t.Fatalf("AcquireStoreLock: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Fatalf("first Release: %v", err)
	}
	if err := lock.Release(); err != nil {
		t.Errorf("second Release = %v, want nil (idempotent no-op)", err)
	}
	if !lock.released {
		t.Error("second Release cleared the released flag; the token must keep proving nothing")
	}
}

// TestUpdateAcquiresWriteLock proves the write path participates in the advisory
// lock: while another holder owns the lock, Core.Update blocks, and it proceeds
// once the lock is released. Without the per-op lock in Update, the first select
// would observe an immediate completion — the guard bites.
func TestUpdateAcquiresWriteLock(t *testing.T) {
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, store.DirName)
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

// TestAcquireFileLockWaitingWaitsForTheHolderToRelease proves the cancellable
// acquire is a WAIT rather than a try: while another descriptor holds the lock
// it returns nothing at all, and it takes the lock once that holder releases.
//
// It is also the guard on the contention split. acquireFileLockTry answers
// contention with errLockHeld, which this loop reads as "poll again" and the
// serve interlock reads as ErrStoreServed; map contention to anything else and
// the first select below fires with that error instead.
func TestAcquireFileLockWaitingWaitsForTheHolderToRelease(t *testing.T) {
	lockPath := filepath.Join(t.TempDir(), "test.lock")
	held, err := acquireFileLock(lockPath)
	if err != nil {
		t.Fatalf("holding the lock: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan error, 1)
	go func() {
		release, err := acquireFileLockWaiting(ctx, lockPath)
		if err == nil {
			err = release()
		}
		got <- err
	}()

	select {
	case err := <-got:
		t.Fatalf("the wait returned %v while another descriptor held the lock", err)
	case <-time.After(100 * time.Millisecond):
	}

	if err := held(); err != nil {
		t.Fatalf("releasing the lock: %v", err)
	}
	select {
	case err := <-got:
		if err != nil {
			t.Fatalf("the wait after the release: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the wait never took the lock after the holder released it")
	}
}

// TestAcquireFileLockWaitingEndsWithItsContext is the cancellable half: a wait
// for a lock nobody releases ends when the caller's context does, and says so
// in a value that unwraps to the context error.
func TestAcquireFileLockWaitingEndsWithItsContext(t *testing.T) {
	tests := []struct {
		name string
		ctx  func(t *testing.T) context.Context
		want error
	}{
		{
			name: "canceled while waiting",
			ctx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				t.Cleanup(cancel)
				time.AfterFunc(50*time.Millisecond, cancel)
				return ctx
			},
			want: context.Canceled,
		},
		{
			name: "its deadline passes while waiting",
			ctx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
				t.Cleanup(cancel)
				return ctx
			},
			want: context.DeadlineExceeded,
		},
		{
			name: "already over before the first try",
			ctx: func(t *testing.T) context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			want: context.Canceled,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lockPath := filepath.Join(t.TempDir(), "test.lock")
			held, err := acquireFileLock(lockPath)
			if err != nil {
				t.Fatalf("holding the lock: %v", err)
			}
			defer func() { _ = held() }()

			done := make(chan error, 1)
			go func() {
				release, err := acquireFileLockWaiting(tt.ctx(t), lockPath)
				if err == nil {
					_ = release()
					err = errors.New("the wait took a lock another descriptor holds")
				}
				done <- err
			}()

			select {
			case err := <-done:
				var ended *StoreLockWaitEndedError
				if !errors.As(err, &ended) {
					t.Fatalf("error = %v (%T), want *StoreLockWaitEndedError", err, err)
				}
				if !errors.Is(err, tt.want) {
					t.Errorf("error = %v, want it to unwrap to %v", err, tt.want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("the wait outlived the context that was supposed to end it")
			}
		})
	}
}
