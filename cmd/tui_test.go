package cmd

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// TestTUIHoldsTheServeInterlock pins the announcement `nibs tui` owes the rest of
// the tool for as long as a session lasts.
//
// A TUI is the same hazard to `nibs migrate` as a serve — it reads config.yml
// once, keeps nibs in memory across the whole session, watches the store and
// writes back — so a clone parked on the write lock can put a pre-migration
// render over migrated files, and nothing detects it afterwards. It took no
// interlock at all, so migrate never saw it.
//
// The shared side, matching serve: another TUI and a live serve must still be
// admitted beside it. What it excludes is the exclusive side migrate holds.
func TestTUIHoldsTheServeInterlock(t *testing.T) {
	t.Run("a live migration fences the TUI out", func(t *testing.T) {
		app := setupServeTestApp(t)
		fence, err := nibcore.AcquireServeExclusion(app.Core.Root())
		if err != nil {
			t.Fatalf("simulating a running migration: %v", err)
		}
		defer func() { _ = fence.Release() }()

		started := false
		err = runTUISession(app, func() error {
			started = true
			return nil
		})
		if err == nil {
			t.Fatal("the TUI started under a running migration")
		}
		if started {
			t.Error("the TUI ran its session under a running migration")
		}
		if !strings.Contains(err.Error(), "migrate") {
			t.Errorf("the refusal does not say what is holding the store: %v", err)
		}
	})

	t.Run("the session holds it for as long as it runs", func(t *testing.T) {
		app := setupServeTestApp(t)
		root := app.Core.Root()

		if err := runTUISession(app, func() error {
			probe, err := nibcore.AcquireServeExclusion(root)
			if err == nil {
				_ = probe.Release()
				return errors.New("migrate was not fenced out of a live TUI")
			}
			if !errors.Is(err, nibcore.ErrStoreServed) {
				return fmt.Errorf("probing the interlock: %w", err)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}

		fence, err := nibcore.AcquireServeExclusion(root)
		if err != nil {
			t.Fatalf("the announcement outlived the session, so migrate stays fenced forever: %v", err)
		}
		_ = fence.Release()
	})

	t.Run("a panicking session gives it back", func(t *testing.T) {
		app := setupServeTestApp(t)
		func() {
			defer func() { _ = recover() }()
			_ = runTUISession(app, func() error { panic("the TUI blew up") })
		}()

		fence, err := nibcore.AcquireServeExclusion(app.Core.Root())
		if err != nil {
			t.Fatalf("a panicking TUI left the store fenced against migrate forever: %v", err)
		}
		_ = fence.Release()
	})

	t.Run("two TUIs coexist", func(t *testing.T) {
		app := setupServeTestApp(t)
		inner := false
		if err := runTUISession(app, func() error {
			return runTUISession(app, func() error {
				inner = true
				return nil
			})
		}); err != nil {
			t.Fatalf("a second TUI was refused by the first: %v", err)
		}
		if !inner {
			t.Error("the second TUI never ran")
		}
	})

	t.Run("a TUI and a serve coexist", func(t *testing.T) {
		app := setupServeTestApp(t)
		root := app.Core.Root()

		// The shared side is what a live `nibs serve` holds for its lifetime.
		serving, err := nibcore.AcquireServeLock(root)
		if err != nil {
			t.Fatalf("simulating a live serve: %v", err)
		}
		defer func() { _ = serving.Release() }()

		if err := runTUISession(app, func() error {
			// ...and the other order: a serve started under a live TUI.
			second, err := nibcore.AcquireServeLock(root)
			if err != nil {
				return fmt.Errorf("a serve was refused by a live TUI: %w", err)
			}
			return second.Release()
		}); err != nil {
			t.Fatalf("a TUI was refused beside a live serve: %v", err)
		}
	})

	t.Run("the session's own error is what comes back", func(t *testing.T) {
		app := setupServeTestApp(t)
		want := errors.New("the TUI failed on its own terms")
		if err := runTUISession(app, func() error { return want }); !errors.Is(err, want) {
			t.Errorf("runTUISession returned %v, want the session's own error", err)
		}
	})
}

// TestSetPrefixNamesALiveTUI is the consequence the interlock buys beyond
// migrate: `liveServeNote` probes this very lock, so a rename that lands under a
// live TUI now says so. It stayed silent before, while the TUI it left behind
// went on refusing every create against a prefix no file in the store carries.
func TestSetPrefixNamesALiveTUI(t *testing.T) {
	const note = "restart it"

	_, nibsDir, cfgPath := setupSetPrefixTest(t, "tnib-",
		testNibSpec{filename: "tnib-aaa--root.md", id: "tnib-aaa"},
	)
	app := tuiTestApp(t, nibsDir)

	var out string
	if err := runTUISession(app, func() error {
		out = captureStdout(t, func() {
			if err := runSetPrefixCmd(t, cfgPath, nibsDir, "new-"); err != nil {
				t.Fatalf("set-prefix failed: %v", err)
			}
		})
		return nil
	}); err != nil {
		t.Fatalf("runTUISession: %v", err)
	}

	if !strings.Contains(out, note) {
		t.Errorf("set-prefix stayed silent about the live TUI whose creates it just started refusing:\n%s", out)
	}
}

// tuiTestApp builds an App on an existing store directory, so a test can hold
// the TUI's session lock against the same store a CLI verb is run against.
func tuiTestApp(t *testing.T, nibsDir string) *App {
	t.Helper()
	core := nibcore.New(nibsDir, config.Default())
	if err := core.Load(); err != nil {
		t.Fatalf("loading core at %s: %v", nibsDir, err)
	}
	t.Cleanup(func() { _ = core.Close() })
	return &App{Core: core}
}
