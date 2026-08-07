package nibcore

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// TestSweepDoesNotRevertAnUnpersistedV0Migration guards the sharp edge of the
// raw-link mirror: saveToDisk refreshes it only on a SUCCESSFUL write, so a
// failed write correctly leaves it describing the bytes still on disk.
//
// migrateV0ToV1 is the one path that deliberately keeps an in-memory link
// change after a failed write (read-only mount, full disk, bad permissions) —
// its documented posture is that memory is authoritative and disk converges on
// the next successful write. Without an explicit capture there, the mirror kept
// the PRE-migration spelling and the next sweep restored the legacy `blocking:`
// list onto a version-1 nib, which a later Update would persist. Core.Create
// fires a store-wide sweep on every create, so any unrelated create triggered it.
func TestSweepDoesNotRevertAnUnpersistedV0Migration(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)
	// Written directly: writeLinkNibFile hardcodes `version: 1`, so passing a
	// version line would duplicate the key and make the file unparseable.
	a1 := "---\nversion: 0\ntitle: A\nstatus: todo\ntype: task\nblocking:\n    - nibs-b1\n---\n\nBody.\n"
	if err := os.WriteFile(filepath.Join(nibsDir, "nibs-a1--test.md"), []byte(a1), 0644); err != nil {
		t.Fatalf("write a1: %v", err)
	}
	writeLinkNibFile(t, nibsDir, "nibs-b1", "todo", "")

	orig := renameFn
	renameFn = func(_, _ string) error { return os.ErrPermission }
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	renameFn = orig

	a0, err := core.Get("nibs-a1")
	if err != nil || a0 == nil || len(a0.Blocking) != 0 || a0.Version != 1 {
		t.Fatalf("premise failed: want a1 migrated in memory, got %+v (err %v)", a0, err)
	}

	if err := core.Create(&nib.Nib{ID: "nibs-new1", Version: 1, Title: "New", Status: "todo", Type: "task"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	a, _ := core.Get("nibs-a1")
	b, _ := core.Get("nibs-b1")
	if len(a.Blocking) != 0 {
		t.Errorf("a1.Blocking = %v after an unrelated sweep; the v0 field was restored from a stale shadow", a.Blocking)
	}
	if !slices.Contains(b.BlockedBy, "nibs-a1") {
		t.Errorf("b1.BlockedBy = %v; the migrated edge was dropped by the sweep", b.BlockedBy)
	}
}
