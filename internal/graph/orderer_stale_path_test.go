package graph

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
)

// stalePathFixture is a parent with one keyed and one UNKEYED child, where the
// unkeyed child's file has been renamed out from under the loaded store. The
// unkeyed key is what puts backfillKeys on the write path at all — a keyed set
// short-circuits — and the rename is the state `nibs config set-prefix` leaves
// behind for a process that loaded the store before it ran.
//
// Returns the path nothing is at any more, and the file the nib actually lives
// in now.
func stalePathFixture(t *testing.T, core *nibcore.Core, parentID, childID string) (stale, live string) {
	t.Helper()
	mustCreate(t, core, &nib.Nib{ID: parentID, Title: "Parent", Status: "todo", Order: "a0"})
	mustCreate(t, core, &nib.Nib{ID: parentID + "k", Title: "Keyed Sibling", Status: "todo", Parent: parentID, Order: "a0"})
	mustCreate(t, core, &nib.Nib{ID: childID, Title: "Unkeyed Sibling", Status: "todo", Parent: parentID})

	stored, err := core.Get(childID)
	if err != nil {
		t.Fatalf("get %s: %v", childID, err)
	}
	if stored.Order != "" {
		t.Fatalf("%s already carries order %q; the fixture cannot reach the backfill write", childID, stored.Order)
	}
	stale = filepath.Join(core.Root(), filepath.FromSlash(stored.Path))
	live = filepath.Join(filepath.Dir(stale), "renamed-"+filepath.Base(stale))
	if err := os.Rename(stale, live); err != nil {
		t.Fatalf("simulating the rename another process made: %v", err)
	}
	return stale, live
}

// TestBackfillOrderKeysRefusesAStalePathRatherThanResurrectingIt is the
// sibling-order half of the set-prefix resurrection bug, pinned where it
// originates rather than one layer down.
//
// backfillKeys persists a key through Writer.Update, using the path this
// process recorded when it loaded the store. `nibs config set-prefix` renames
// every one of those files, so a creating write would put each rewritten sibling
// back at its pre-rename name. That second copy of the nib would carry a prefix
// the config no longer declares, and nobody would be told, since backfillKeys
// returns no error.
//
// Core.Update's non-creating writer is what closes it, so this guard sits above
// that fix rather than beside it: it fails if the orderer is ever pointed at a
// creating writer of its own.
func TestBackfillOrderKeysRefusesAStalePathRatherThanResurrectingIt(t *testing.T) {
	r, core := setupTestResolver(t)
	core.SetWarnWriter(nil)
	stale, live := stalePathFixture(t, core, "bp1", "bs1")

	before, err := os.ReadFile(live)
	if err != nil {
		t.Fatalf("read the live file: %v", err)
	}

	members := r.Orderer.keyedMembers(ScopeParent, "bp1")

	if _, err := os.Lstat(stale); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the backfill resurrected %s at its pre-rename path (lstat err = %v)", filepath.Base(stale), err)
	}
	after, err := os.ReadFile(live)
	if err != nil {
		t.Fatalf("read the live file after the backfill: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the live file changed even though the backfill could not reach it")
	}

	// The refused clone's key is DISCARDED: the returned slice and the shared
	// store pointer both keep the unkeyed nib, so the sibling falls back to title
	// sort rather than showing a key that was never persisted.
	for _, m := range members {
		if m.ID == "bs1" && m.Order != "" {
			t.Errorf("returned member bs1 carries order %q, want none — the write was refused", m.Order)
		}
	}
	shared, err := core.Get("bs1")
	if err != nil {
		t.Fatalf("get bs1: %v", err)
	}
	if shared.Order != "" {
		t.Errorf("shared bs1 carries a phantom order %q that was never persisted", shared.Order)
	}
}

// TestBackfillOrderKeysStalePathWarnsOnceAndThenStaysQuiet is the classification
// half: the refusal above lands in backfillKeys' best-effort arm, which cannot
// propagate, so all it can choose is how loudly to say it.
//
// Once, not never and not per retry. Never is too quiet: a hand-deleted nib
// file reaches the same arm (errors.Is matches the chain, and fsutil's bare
// os.Lstat miss is in it), and it is neither store-wide nor self-healing. Per
// retry is too loud: the condition is one store, not one per nib, so each
// positioned write would warn once per unkeyed sibling.
func TestBackfillOrderKeysStalePathWarnsOnceAndThenStaysQuiet(t *testing.T) {
	r, core := setupTestResolver(t)

	// nibcore writes to its own warnWriter, which setupTestResolver leaves at the
	// os.Stderr captured in New() — captureStderr's package-var swap cannot
	// intercept that, so it is captured directly.
	var nibcoreWarn bytes.Buffer
	core.SetWarnWriter(&nibcoreWarn)

	stalePathFixture(t, core, "fp1", "fs1")

	const retries = 4
	ordererStderr := captureStderr(t, func() {
		// The sibling never gets a key persisted, so needsBackfill never clears
		// and every positioned write to the group retries.
		for range retries {
			_ = r.Orderer.keyedMembers(ScopeParent, "fp1")
		}
	})

	lines := warningLines(ordererStderr)
	if len(lines) != 1 {
		t.Errorf("the orderer emitted %d warnings across %d retries, want exactly 1 (the latch): %q", len(lines), retries, lines)
	}
	if len(lines) > 0 && !strings.Contains(lines[0], "fs1") {
		t.Errorf("the one warning does not name the sibling it is about: %q", lines[0])
	}
	// The nibcore half of the same property. No line reaches this buffer today —
	// Core.Update returns its refusal without logging — and that is what it pins:
	// a warn added beside that return is the flood in its other spelling, once per
	// unkeyed sibling per retry, and reaches the same stderr by the other route,
	// where this Orderer's latch cannot bound it.
	if got := strings.TrimSpace(nibcoreWarn.String()); got != "" {
		t.Errorf("nibcore warned about a moved store across %d retries; want none, got %q", retries, got)
	}
}

// TestBackfillOrderKeysStalePathLatchIsPerOrderer pins the latch's SCOPE, which
// is what makes warning once rather than never a real signal: a fresh Orderer
// warns again. `nibs serve` and `nibs tui` build one at startup and keep it for
// the process, so one line per process is what a long-lived server gets; every
// CLI command builds its own (App.newResolver), so none of them inherits another
// command's silence.
func TestBackfillOrderKeysStalePathLatchIsPerOrderer(t *testing.T) {
	r, core := setupTestResolver(t)
	core.SetWarnWriter(nil)
	stalePathFixture(t, core, "lp1", "ls1")

	first := warningLines(captureStderr(t, func() { _ = r.Orderer.keyedMembers(ScopeParent, "lp1") }))
	if len(first) != 1 {
		t.Fatalf("the first Orderer emitted %d warnings, want 1: %q", len(first), first)
	}

	fresh := NewOrderer(r.Reader, r.Writer)
	second := warningLines(captureStderr(t, func() { _ = fresh.keyedMembers(ScopeParent, "lp1") }))
	if len(second) != 1 {
		t.Errorf("a fresh Orderer emitted %d warnings, want 1 — the latch must not be shared: %q", len(second), second)
	}
}

// warningLines splits captured stderr into the non-empty lines it holds, so a
// count can be asserted rather than a substring.
func warningLines(captured string) []string {
	trimmed := strings.TrimSpace(captured)
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}
