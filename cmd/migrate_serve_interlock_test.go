package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/store"
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
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		// The serve can DIE instead of taking the lock: each successful probe
		// below owns the migrate-side exclusion for a moment, and a serve whose
		// own acquire lands inside that moment exits with the fenced-out
		// refusal — by design, since a real migrate holds it for a whole run.
		// That death is the probe's doing (nibs-hgha: it killed a CI run whose
		// cold scheduler let the first probe beat the serve goroutine), so
		// relaunch the serve and keep waiting; any OTHER death is a real
		// failure and reports its error instead of a misleading "does not hold
		// the interlock".
		select {
		case err := <-errCh:
			if err == nil || !strings.Contains(err.Error(), "`nibs migrate` is running") {
				t.Fatalf("startServer exited while the test waited for the lock: %v", err)
			}
			go func() { errCh <- startServer(ctx, app, "127.0.0.1", 0, false, nil) }()
			continue
		default:
		}
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
		serveErr := <-errCh
		t.Fatalf("a running serve does not hold the interlock, so migrate is not fenced out (startServer: %v)", serveErr)
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

// TestMigrateConfirmsBeforeApplying pins the pause that makes migrate's serve
// advice actionable instead of narration. It printed "Stop any running
// `nibs serve` before migrating." immediately before applying, with no way to
// act on it — only --dry-run surfaced it in time.
//
// The pause is INTERACTIVE-ONLY, deliberately differing from --force's policy for
// ambiguous files. --force decides what happens to a user's files, a content
// judgement no script should make silently. This asks "did you stop a process I
// cannot see?" — and every holder this build CAN see is already fenced by
// gateStoreNotHeld, so the residue is an older one, which is a human-noticing
// problem. A script has no human either way, and requiring --yes on all 105
// non-interactive migrate invocations would make the flag the normal case.
func TestMigrateConfirmsBeforeApplying(t *testing.T) {
	build := func(t *testing.T, interactive bool, answer string) string {
		t.Helper()
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(resetMigrateFlags)
		resetRootPersistentFlags()
		resetMigrateFlags()
		forceInteractive(t, interactive)
		if answer != "" {
			withStdin(t, answer)
		}
		_, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
			"leg-a1--one.md": layoutNib,
		})
		return storeDir
	}
	migrated := func(t *testing.T, storeDir string) bool {
		t.Helper()
		_, err := os.Stat(filepath.Join(store.NewLayout(storeDir).DataDir(), "leg-a1--one.md"))
		return err == nil
	}

	t.Run("declining changes nothing", func(t *testing.T) {
		storeDir := build(t, true, "n\n")
		out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
		if err == nil {
			t.Fatalf("migrate applied after the user declined\nout: %s", out)
		}
		if migrated(t, storeDir) {
			t.Error("the declined run migrated the store anyway")
		}
	})

	t.Run("the question says which serves are fenced and which are not", func(t *testing.T) {
		storeDir := build(t, true, "n\n")
		out, _ := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
		if !strings.Contains(out, "serve") {
			t.Errorf("the question does not mention serve at all:\n%s", out)
		}
		if !strings.Contains(strings.ToLower(out), "older") {
			t.Errorf("the question does not say that an OLDER serve is the one it cannot fence:\n%s", out)
		}
	})

	t.Run("accepting applies", func(t *testing.T) {
		storeDir := build(t, true, "y\n")
		if out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty"); err != nil {
			t.Fatalf("migrate after a yes: %v\nout: %s", err, out)
		}
		if !migrated(t, storeDir) {
			t.Error("a yes did not migrate the store")
		}
	})

	t.Run("--yes skips the question at a terminal", func(t *testing.T) {
		storeDir := build(t, true, "")
		migrateYes = true
		t.Cleanup(func() { migrateYes = false })
		out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty", "--yes")
		if err != nil {
			t.Fatalf("migrate --yes: %v\nout: %s", err, out)
		}
		if strings.Contains(out, "[y/N]") {
			t.Errorf("--yes still asked:\n%s", out)
		}
		if !migrated(t, storeDir) {
			t.Error("--yes did not migrate the store")
		}
	})

	t.Run("a run with no terminal proceeds without asking", func(t *testing.T) {
		storeDir := build(t, false, "")
		if out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty"); err != nil {
			t.Fatalf("a scripted migrate was blocked by a question nobody can answer: %v\nout: %s", err, out)
		}
		if !migrated(t, storeDir) {
			t.Error("the store was not migrated")
		}
	})

	t.Run("--dry-run never asks", func(t *testing.T) {
		storeDir := build(t, true, "")
		out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--dry-run")
		if err != nil {
			t.Fatalf("--dry-run: %v\nout: %s", err, out)
		}
		if strings.Contains(out, "[y/N]") {
			t.Errorf("--dry-run asked to proceed with a run it does not perform:\n%s", out)
		}
	})
}

// The interlock does not answer "a serve is running". It answers that SOME other
// nibs process has the store, and since `nibs tui` takes the shared side for its
// whole session, a `nibs tui` is as likely to be the holder as a `nibs serve` —
// on a machine where no serve exists at all. A refusal that names one of them
// sends the other's reader hunting for a process that is not running, and
// nothing else on screen mentions the TUI.
//
// So the property is per-surface and mechanical: wherever the refusal path names
// a `nibs serve`, it names a `nibs tui` beside it. Naming which one holds the
// store is not on offer — the lock cannot tell, and neither can these strings.
func TestTheHeldStoreRefusalSpeaksForEveryHolder(t *testing.T) {
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
	// held runs fn with exactly the lock runTUISession takes over the session,
	// which is the situation the wording has to speak for.
	held := func(t *testing.T, storeDir string, fn func()) {
		t.Helper()
		holding, err := nibcore.AcquireServeLock(storeDir)
		if err != nil {
			t.Fatalf("holding the store the way a `nibs tui` session does: %v", err)
		}
		defer func() { _ = holding.Release() }()
		fn()
	}
	// assertSpeaksForBoth fails for a surface that names one holder without the
	// other. Both spellings are asked for because the reader acts on a command,
	// not on the word "tui" in prose.
	assertSpeaksForBoth := func(t *testing.T, surface, text string) {
		t.Helper()
		if !strings.Contains(text, "`nibs serve`") {
			return
		}
		if !strings.Contains(text, "`nibs tui`") {
			t.Errorf("%s names `nibs serve` and not `nibs tui`, so a reader whose holder is the TUI is told to stop a process that is not running:\n%s",
				surface, text)
		}
	}

	t.Run("the run's refusal", func(t *testing.T) {
		storeDir := build(t)
		held(t, storeDir, func() {
			out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
			if err == nil {
				t.Fatalf("migrate ran while the store was held\nout: %s", out)
			}
			if strings.Contains(err.Error(), "a `nibs serve` is running") {
				t.Errorf("the refusal asserts a serve is running, which the lock never said: %v", err)
			}
			assertSpeaksForBoth(t, "the run's refusal", err.Error())
		})
	})

	t.Run("the dry run's preview", func(t *testing.T) {
		storeDir := build(t)
		held(t, storeDir, func() {
			out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--dry-run")
			if err != nil {
				t.Fatalf("--dry-run must preview, not fail: %v\nout: %s", err, out)
			}
			if !strings.Contains(out, "will refuse") {
				t.Fatalf("premise failed: the preview does not predict the refusal:\n%s", out)
			}
			assertSpeaksForBoth(t, "the dry run's preview", out)
		})
	})

	t.Run("the advice about the holder the gate cannot see", func(t *testing.T) {
		storeDir := build(t)
		out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--dry-run")
		if err != nil {
			t.Fatalf("--dry-run: %v\nout: %s", err, out)
		}
		if !strings.Contains(strings.ToLower(out), "older") {
			t.Fatalf("premise failed: the preview carries no advice about an older holder:\n%s", out)
		}
		assertSpeaksForBoth(t, "the dry run's older-holder advice", out)
	})

	t.Run("the applied run's advice", func(t *testing.T) {
		storeDir := build(t)
		out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
		if err != nil {
			t.Fatalf("migrate: %v\nout: %s", err, out)
		}
		assertSpeaksForBoth(t, "the applied run's older-holder advice", out)
	})

	t.Run("the refusal every other command prints", func(t *testing.T) {
		storeDir := build(t)
		out, err := runRootWith(t, "--nibs-path", storeDir, "list")
		if err == nil {
			t.Fatalf("a pending store did not refuse `nibs list`\nout: %s", out)
		}
		assertSpeaksForBoth(t, "the pending-migration refusal", err.Error())
	})
}
