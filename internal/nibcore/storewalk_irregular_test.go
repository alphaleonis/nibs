package nibcore

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/testskip"
)

// irregularDeadline is generous, because the only thing it has to separate is
// "returned" from "blocked in open(2) forever". Every walk under it visits a
// handful of short files.
const irregularDeadline = 10 * time.Second

// withinDeadline runs work and fails if it has not returned in time.
//
// The goroutine NEVER touches t: a test that has already failed its deadline is
// unwinding, and calling t from another goroutine then races the framework. The
// channel is buffered so a call that unblocks after the deadline still completes
// its send and exits instead of leaking forever.
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

const irregularTestNib = `---
version: 1
title: Real
status: todo
type: task
priority: normal
---

Body.
`

// TestWalkStoreFilesReportsAnIrregularFileWithoutOpeningIt pins the rule at the
// one place every walker inherits it.
//
// A FIFO named `*.md` used to be handed to the caller as an ordinary candidate,
// and the first thing every caller does is OPEN it — which blocks until a writer
// appears, i.e. forever. Unlike a malformed nib, an irregular file cannot be read
// to discover that it is bad: reading IS the hang. So the walk has to answer from
// the directory entry, before any open.
func TestWalkStoreFilesReportsAnIrregularFileWithoutOpeningIt(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "t0001--pipe.md")
	mkfifoT(t, fifo)
	real := filepath.Join(dir, "t0002--real.md")
	writeNibFile(t, dir, "t0002--real.md", irregularTestNib)

	var visited []string
	var skipped []string
	err := withinDeadline(t, "WalkStoreFiles", func() error {
		return WalkStoreFiles(dir, func(path string, walkErr error) error {
			if walkErr != nil {
				if !errors.Is(walkErr, ErrNotRegularFile) {
					return walkErr
				}
				skipped = append(skipped, path)
				return nil
			}
			visited = append(visited, path)
			return nil
		})
	})
	if err != nil {
		t.Fatalf("WalkStoreFiles: %v", err)
	}
	if len(skipped) != 1 || skipped[0] != fifo {
		t.Errorf("skipped = %v, want exactly [%s]", skipped, fifo)
	}
	// The ordinary file beside it must still be walked: one bad entry degrades
	// to one missing nib, never to a dead store.
	if len(visited) != 1 || visited[0] != real {
		t.Errorf("visited = %v, want exactly [%s]", visited, real)
	}
}

// TestWalkStoreFilesStillVisitsASymlinkedNibFile is the counterweight, and the
// reason the test is not simply `d.Type().IsRegular()`.
//
// os.DirFS reports a symlink AS a symlink, so a link is not a regular entry —
// but a link to a real nib file is ordinary (a dotfile manager or a
// partially-synced store produces them) and was loaded before this guard
// existed. Judging the entry rather than what it leads to would drop those nibs
// out of every query silently.
func TestWalkStoreFilesStillVisitsASymlinkedNibFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "target.md")
	if err := os.WriteFile(target, []byte(irregularTestNib), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	link := filepath.Join(dir, "t0003--linked.md")
	if err := os.Symlink(target, link); err != nil {
		testskip.SymlinkUnavailable(t, err)
	}

	var visited []string
	err := WalkStoreFiles(dir, func(path string, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		visited = append(visited, path)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkStoreFiles: %v", err)
	}
	if len(visited) != 1 || visited[0] != link {
		t.Errorf("visited = %v, want exactly [%s]; a link to a real nib file is a nib file", visited, link)
	}
}

// TestWalkStoreFilesResolvesASymlinkBeforeJudgingIt pins the other half of that
// resolution: a link is followed to decide, so a link AT a FIFO is the same hang
// wearing a different name and must be skipped like the FIFO itself.
func TestWalkStoreFilesResolvesASymlinkBeforeJudgingIt(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(t.TempDir(), "pipe")
	mkfifoT(t, fifo)
	link := filepath.Join(dir, "t0004--linked-pipe.md")
	if err := os.Symlink(fifo, link); err != nil {
		testskip.SymlinkUnavailable(t, err)
	}

	var skipped []string
	err := withinDeadline(t, "WalkStoreFiles over a link to a FIFO", func() error {
		return WalkStoreFiles(dir, func(path string, walkErr error) error {
			if walkErr != nil {
				if !errors.Is(walkErr, ErrNotRegularFile) {
					return walkErr
				}
				skipped = append(skipped, path)
				return nil
			}
			return nil
		})
	})
	if err != nil {
		t.Fatalf("WalkStoreFiles: %v", err)
	}
	if len(skipped) != 1 || skipped[0] != link {
		t.Errorf("skipped = %v, want exactly [%s]", skipped, link)
	}
}

// TestWalkStoreFilesHandsOnALinkItCannotResolve pins that the guard does not
// swallow a better diagnostic. A dangling link is already reported by the
// opener's own error ("no such file"), which names what is wrong; answering it
// with "not a regular file" would trade a precise reason for a vague one, and
// opening a broken link cannot block.
func TestWalkStoreFilesHandsOnALinkItCannotResolve(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "t0005--dangling.md")
	if err := os.Symlink(filepath.Join(dir, "no-such-target"), link); err != nil {
		testskip.SymlinkUnavailable(t, err)
	}

	var visited, skipped []string
	err := WalkStoreFiles(dir, func(path string, walkErr error) error {
		if walkErr != nil {
			if !errors.Is(walkErr, ErrNotRegularFile) {
				return walkErr
			}
			skipped = append(skipped, path)
			return nil
		}
		visited = append(visited, path)
		return nil
	})
	if err != nil {
		t.Fatalf("WalkStoreFiles: %v", err)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v, want none; a dangling link earns the opener's own error", skipped)
	}
	if len(visited) != 1 || visited[0] != link {
		t.Errorf("visited = %v, want exactly [%s]", visited, link)
	}
}

// TestLoadSkipsAnIrregularFileAndReportsIt is the load-level half of the
// operator's decision: an irregular file is a load DIAGNOSTIC, not a refusal.
//
// A config is required to run at all, so refusing an unreadable one is the only
// honest answer. One nib among two hundred is not required — making it fatal
// locks the user out of every command, INCLUDING `nibs check`, the one that would
// tell them which file is wrong.
func TestLoadSkipsAnIrregularFileAndReportsIt(t *testing.T) {
	nibsDir := setupNibsDir(t)
	data := storeData(t, nibsDir)
	mkfifoT(t, filepath.Join(data, "pipe01--x.md"))
	writeNibFile(t, data, "good01--good.md", irregularTestNib)

	core := New(nibsDir, config.Default())
	if err := withinDeadline(t, "Core.Load", core.Load); err != nil {
		t.Fatalf("Load() returned an error; one irregular file must not brick the whole store: %v", err)
	}

	// Every other nib in the store still loads.
	if _, err := core.Get("good01"); err != nil {
		t.Errorf("valid nib good01 missing after load: %v", err)
	}
	if _, err := core.Get("pipe01"); err == nil {
		t.Error("the FIFO was loaded as a nib")
	}

	// Skipped is not enough — silence re-introduces exactly the invisibility
	// `nibs check` reporting unparseable files exists to remove.
	unparseable, _ := core.LoadDiagnostics()
	var found *UnparseableFile
	for i := range unparseable {
		if strings.Contains(unparseable[i].Path, "pipe01--x.md") {
			found = &unparseable[i]
		}
	}
	if found == nil {
		t.Fatalf("the skipped FIFO is in no load diagnostic: %+v", unparseable)
	}
	if found.NibID != "pipe01" {
		t.Errorf("diagnostic NibID = %q, want %q — the id comes from the filename, which parses whatever the file is", found.NibID, "pipe01")
	}
	if !strings.Contains(found.Reason, "regular file") {
		t.Errorf("diagnostic Reason = %q, want it to say the file is not a regular one", found.Reason)
	}
}
