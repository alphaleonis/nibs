package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/store"
	"github.com/alphaleonis/nibs/internal/testskip"
)

// irregularDeadline is generous, because the only thing it separates is
// "returned" from "blocked in open(2) forever".
const irregularDeadline = 10 * time.Second

// runWithinDeadline drives one CLI invocation and fails if it has not returned in
// time, rather than hanging the suite.
//
// THE INNER GOROUTINE NEVER TOUCHES t, which is why this does not simply wrap
// captureStdout: captureStdout takes a *testing.T and calls t.Fatalf on a pipe
// error, and a t call from a non-test goroutine runs Goexit there — the failure
// is swallowed and the select waits out its whole deadline for a result that can
// never arrive. So the pipe is built and torn down on the TEST goroutine, and the
// goroutine does nothing but Execute.
//
// The drain runs in its own goroutine so a command whose output exceeds the pipe
// buffer cannot block in write() while this one is still in select.
func runWithinDeadline(t *testing.T, args ...string) (string, error) {
	t.Helper()
	stdoutMu.Lock()
	defer stdoutMu.Unlock()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w

	drained := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		drained <- buf.String()
	}()

	done := make(chan error, 1)
	rootCmd.SetArgs(args)
	go func() { done <- rootCmd.Execute() }()

	var execErr error
	timedOut := false
	select {
	case execErr = <-done:
	case <-time.After(irregularDeadline):
		timedOut = true
	}

	os.Stdout = orig
	_ = w.Close()
	out := <-drained
	_ = r.Close()

	if timedOut {
		t.Fatalf("`nibs %s` did not return within %s; it is blocked in open(2), which is the hang this guard exists for",
			strings.Join(args, " "), irregularDeadline)
	}
	return out, execErr
}

// withinDeadline runs work and fails if it has not returned in time.
//
// EVERY call that can reach an opener in this file goes through a deadline, not
// only the CLI ones: the check row drives core.Load directly (checkCmd.RunE
// os.Exit(1)s on findings and would take the test binary with it), and a bare
// core.Load there hung the whole suite under the mutation this file's guards are
// verified against — which is the outcome the deadline exists to prevent.
//
// The goroutine NEVER touches t, and the channel is buffered so a call that
// unblocks after the deadline still completes its send and exits.
func withinDeadline(t *testing.T, what string, work func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- work() }()
	select {
	case err := <-done:
		return err
	case <-time.After(irregularDeadline):
		t.Fatalf("%s did not return within %s; it is blocked in open(2), which is the hang this guard exists for", what, irregularDeadline)
		return nil
	}
}

// mkfifoT creates a named pipe at path, skipping through testskip so a platform
// that cannot host one is COUNTED rather than silently untested.
func mkfifoT(t *testing.T, path string) {
	t.Helper()
	if err := mkfifo(path); err != nil {
		testskip.Unavailable(t, testskip.NamedPipes, "mkfifo(%s): %v", path, err)
	}
}

// TestCommandsDoNotHangOnAnIrregularNibFile drives the whole CLI over a store
// holding a FIFO named `*.md`.
//
// There are THREE openers on a walked path — Core.loadNib, readFrontMatterHeader
// (migrate's scans) and the corroboration probe store resolution runs — and every
// one of them hung on this fixture. The guard sits in the shared walk so all
// three inherit it, and this test is what proves the inheritance rather than
// assuming it: `nibs list` reaches the first, `nibs migrate --dry-run` the
// second, and `--nibs-path` at a config-less directory the third.
func TestCommandsDoNotHangOnAnIrregularNibFile(t *testing.T) {
	// buildStore lays out a store carrying a FIFO named like a nib beside a real
	// one, and returns the project directory.
	buildStore := func(t *testing.T) string {
		t.Helper()
		tmp := t.TempDir()
		t.Setenv("NIBS_CONFIG_ROOT", tmp)
		projectDir := filepath.Join(tmp, "proj")
		storeDir := filepath.Join(projectDir, store.DirName)
		mkdirAllT(t, filepath.Join(storeDir, store.DataDirName))
		writeFileT(t, filepath.Join(storeDir, store.ConfigFileName), "nibs:\n  prefix: t-\n  id_length: 4\n")
		writeFileT(t, filepath.Join(storeDir, store.DataDirName, "t-0002--real.md"), layoutNib)
		mkfifoT(t, filepath.Join(storeDir, store.DataDirName, "t-0001--pipe.md"))
		return projectDir
	}

	t.Run("nibs list returns every other nib", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(resetListFlags)
		resetRootPersistentFlags()
		resetListFlags()
		t.Setenv("NIBS_PATH", "")
		t.Chdir(buildStore(t))

		out, err := runWithinDeadline(t, "list", "--all", "--json")
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		ids := envelopeIDs(parseListEnvelope(t, out))
		if !ids["t-0002"] {
			t.Errorf("ids = %v, want the real nib t-0002; one bad file must not cost the rest", ids)
		}
		if ids["t-0001"] {
			t.Error("the FIFO was loaded as a nib")
		}
	})

	// Skipped is not enough. The v0.8.0 entry that made unparseable files visible
	// exists because a silently missing nib is indistinguishable from a nib that
	// was never there; an irregular file must not re-open that hole for a
	// different cause.
	//
	// runCheck rather than the command: checkCmd.RunE calls os.Exit(1) when there
	// are findings, which would take the test binary with it — the split exists
	// for exactly this (see runCheck's doc).
	t.Run("nibs check names the file", func(t *testing.T) {
		t.Cleanup(resetCheckFlags)
		resetCheckFlags()
		nibsDir := filepath.Join(buildStore(t), store.DirName)
		core := nibcore.New(nibsDir, config.Default())
		core.SetWarnWriter(nil)
		if err := withinDeadline(t, "core.Load", core.Load); err != nil {
			t.Fatalf("Load(): %v", err)
		}
		out := captureStdout(t, func() {
			if _, err := runCheck(&App{Core: core}); err != nil {
				t.Errorf("runCheck: %v", err)
			}
		})
		if !strings.Contains(out, "t-0001--pipe.md") {
			t.Errorf("`nibs check` does not name the skipped file:\n%s", out)
		}
	})

	t.Run("nibs migrate --dry-run previews", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(resetMigrateFlags)
		resetRootPersistentFlags()
		resetMigrateFlags()
		t.Setenv("NIBS_PATH", "")
		t.Chdir(buildStore(t))

		if _, err := runWithinDeadline(t, "migrate", "--dry-run"); err != nil {
			t.Fatalf("migrate --dry-run: %v", err)
		}
	})

	// The third opener: store resolution's corroboration probe reads every `*.md`
	// under a candidate directory to decide whether anything there was written by
	// nibs. A FIFO in a pre-layout store hung the resolver itself, before any
	// command had begun.
	t.Run("store resolution over a pre-layout store", func(t *testing.T) {
		t.Cleanup(resetRootPersistentFlags)
		resetRootPersistentFlags()
		t.Setenv("NIBS_PATH", "")
		tmp := t.TempDir()
		t.Setenv("NIBS_CONFIG_ROOT", tmp)
		projectDir := filepath.Join(tmp, "proj")
		dataDir := filepath.Join(projectDir, "nibdata")
		mkdirAllT(t, dataDir)
		writeFileT(t, filepath.Join(projectDir, store.LegacyProjectConfigFileName),
			"nibs:\n  prefix: leg-\n  id_length: 4\n  path: nibdata\n")
		// The FIFO sorts BEFORE the real nib, deliberately: the probe stops at
		// the first file carrying a nibs `status:`, so with the order reversed it
		// never reaches the FIFO and this row passes whether the guard is there
		// or not.
		mkfifoT(t, filepath.Join(dataDir, "leg-a1--pipe.md"))
		writeFileT(t, filepath.Join(dataDir, "leg-b2--one.md"), layoutNib)

		nibsPath = dataDir
		// The directory holds a real nib beside the FIFO, so it corroborates and
		// resolves; what matters is that it ANSWERED.
		err := withinDeadline(t, "resolveStoreDir", func() error {
			_, err := resolveStoreDir()
			return err
		})
		if err != nil {
			t.Fatalf("resolveStoreDir: %v", err)
		}
	})
}
