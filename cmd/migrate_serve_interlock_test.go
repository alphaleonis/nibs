package cmd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/nibcore"
)

// TestMigrateIsFencedOutOfALiveServe pins the enforcement that replaces the
// advisory "stop any running `nibs serve`" print.
//
// The print could only ask. The race it asks about is real and silent: a web
// update clones a nib, parks on the store's write lock inside Core.Update while
// holding c.mu — so the watcher can never refresh the stale clone — and writes
// the pre-migration render back after migrate releases. The source is v1 by then,
// so no detection ever fires again.
func TestMigrateIsFencedOutOfALiveServe(t *testing.T) {
	build := func(t *testing.T) string {
		t.Helper()
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(resetMigrateFlags)
		resetRootPersistentFlags()
		resetMigrateFlags()
		_, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
			"leg-a1--one.md": layoutNib,
		})
		return storeDir
	}

	t.Run("the run refuses and changes nothing", func(t *testing.T) {
		storeDir := build(t)
		serving, err := nibcore.AcquireServeLock(storeDir)
		if err != nil {
			t.Fatalf("simulating a live serve: %v", err)
		}
		defer func() { _ = serving.Release() }()

		out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
		if err == nil {
			t.Fatalf("migrate ran under a live serve\nout: %s", out)
		}
		if !strings.Contains(err.Error(), "serve") {
			t.Errorf("the refusal does not say what is holding the store: %v", err)
		}
	})

	t.Run("--dry-run predicts it", func(t *testing.T) {
		storeDir := build(t)
		serving, err := nibcore.AcquireServeLock(storeDir)
		if err != nil {
			t.Fatalf("simulating a live serve: %v", err)
		}
		defer func() { _ = serving.Release() }()

		out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--dry-run")
		if err != nil {
			t.Fatalf("--dry-run must preview, not fail: %v\nout: %s", err, out)
		}
		if !strings.Contains(out, "will refuse") || !strings.Contains(out, "serve") {
			t.Errorf("the preview does not predict the refusal:\n%s", out)
		}
	})

	t.Run("it runs once the serve is gone", func(t *testing.T) {
		storeDir := build(t)
		serving, err := nibcore.AcquireServeLock(storeDir)
		if err != nil {
			t.Fatalf("simulating a live serve: %v", err)
		}
		if err := serving.Release(); err != nil {
			t.Fatalf("Release: %v", err)
		}

		if out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty"); err != nil {
			t.Fatalf("migrate stayed fenced after the serve stopped: %v\nout: %s", err, out)
		}
	})
}

// TestServeHoldsTheInterlockWhileItRuns is the other side: the fence is only
// worth anything if serve actually takes it, for its whole lifetime, and gives it
// back on shutdown.
func TestServeHoldsTheInterlockWhileItRuns(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	app := setupServeTestApp(t)
	storeDir := app.Core.Root()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- startServer(ctx, app, "127.0.0.1", 0, false, nil) }()

	// Wait for the lock to appear rather than for a fixed interval: the server
	// binds a listener and starts a watcher first, and a sleep long enough to
	// cover that on a loaded machine is a flake waiting to happen.
	held := false
	for range 100 {
		probe, err := nibcore.AcquireServeExclusion(storeDir)
		if errors.Is(err, nibcore.ErrStoreServed) {
			held = true
			break
		}
		if err != nil {
			t.Fatalf("probing the interlock: %v", err)
		}
		// Release every successful probe: holding it would fence out the very
		// serve this is waiting for, and the test would report the implementation
		// broken when it is the probe that broke it.
		if err := probe.Release(); err != nil {
			t.Fatalf("releasing the probe: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !held {
		cancel()
		<-errCh
		t.Fatal("a running serve does not hold the interlock, so migrate is not fenced out")
	}

	cancel()
	if err := <-errCh; err != nil {
		t.Fatalf("startServer: %v", err)
	}

	fence, err := nibcore.AcquireServeExclusion(storeDir)
	if err != nil {
		t.Fatalf("the interlock survived shutdown, so migrate stays fenced forever: %v", err)
	}
	_ = fence.Release()
}
