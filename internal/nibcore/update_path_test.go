package nibcore

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestUpdateRefusesAStalePathRatherThanRecreatingIt is the resurrection guard on
// the write path every mutating verb funnels through.
//
// Update writes the nib back to the path the store recorded when this process
// loaded it, and `nibs config set-prefix` renames every one of those files. A
// write that ends in a rename CREATES unconditionally, so the leftover path
// became a SECOND copy of the nib under a prefix the config no longer declares:
// the user's edit landed on that ghost, the live file kept the old value, and
// the command exited 0. The non-creating writer makes it an error the caller
// reports instead.
func TestUpdateRefusesAStalePathRatherThanRecreatingIt(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	created := createTestNib(t, core, "up91", "Stale Path", "todo")
	stalePath := filepath.Join(nibsDir, filepath.FromSlash(created.Path))
	renamed := filepath.Join(filepath.Dir(stalePath), "renamed-"+filepath.Base(stalePath))
	if err := os.Rename(stalePath, renamed); err != nil {
		t.Fatalf("simulating the rename another process made: %v", err)
	}
	before, err := os.ReadFile(renamed)
	if err != nil {
		t.Fatalf("read the live file: %v", err)
	}

	edit, err := core.GetForUpdate("up91")
	if err != nil {
		t.Fatalf("GetForUpdate: %v", err)
	}
	edit.Title = "Edited On A Ghost"

	err = core.Update(edit, nil)
	if err == nil {
		t.Fatal("Update wrote to a path nothing is at instead of refusing")
	}
	// HasPrefix, not Contains: the writer already formats the failing path, and a
	// nib's filename begins with its id, so containment is satisfied whether or
	// not Update names the nib itself. Only the leading "<id>: " is Update's.
	if !strings.HasPrefix(err.Error(), "up91: ") {
		t.Errorf("error = %q, want it to LEAD with the nib it could not write", err)
	}
	if _, statErr := os.Lstat(stalePath); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("Update resurrected %s at its pre-rename path", created.Path)
	}
	after, err := os.ReadFile(renamed)
	if err != nil {
		t.Fatalf("read the live file after the refusal: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Error("the live file changed even though Update refused")
	}
}

// TestUpdateWritesThePathTheStoreHoldsNotTheCallersSnapshot is the other half:
// the caller's nib is a clone taken BEFORE Update's lock, and Archive/Unarchive/
// LoadAndUnarchive rewrite Path in place on the stored pointer under that same
// lock. So a clone that predates one of those moves names a file the store has
// already moved, and only the STORED path says where the nib now lives — which
// is also what keeps the non-creating writer from refusing a caller that did
// nothing wrong.
func TestUpdateWritesThePathTheStoreHoldsNotTheCallersSnapshot(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	created := createTestNib(t, core, "up92", "Archived Mid Edit", "todo")
	// Captured up front: Archive rewrites Path in place on this very pointer.
	dataRel := created.Path
	dataFile := filepath.Join(nibsDir, filepath.FromSlash(dataRel))

	// The snapshot the caller edits still carries the pre-archive data/ path.
	edit, err := core.GetForUpdate("up92")
	if err != nil {
		t.Fatalf("GetForUpdate: %v", err)
	}

	if err := core.Archive("up92"); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	edit.Title = "Edited After The Archive"
	if err := core.Update(edit, nil); err != nil {
		t.Fatalf("Update refused a nib the store itself had moved: %v", err)
	}

	if _, statErr := os.Lstat(dataFile); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("Update recreated the nib at its pre-archive path %s", dataRel)
	}

	stored, err := core.Get("up92")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !strings.HasPrefix(stored.Path, "archive/") {
		t.Fatalf("stored path = %q, want the archived one", stored.Path)
	}
	raw, err := os.ReadFile(filepath.Join(nibsDir, filepath.FromSlash(stored.Path)))
	if err != nil {
		t.Fatalf("read the archived file: %v", err)
	}
	if !bytes.Contains(raw, []byte("Edited After The Archive")) {
		t.Error("the edit did not reach the file the store says the nib lives in")
	}
}
