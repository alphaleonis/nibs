package nibcore

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fsnotify/fsnotify"
)

const twinNib = "---\nversion: 2\ntitle: Twin\nstatus: todo\n---\n\nBody.\n"

// feedBatch hands one debounce batch to a watching core and returns what the
// batch wrote to the warn writer. The writer is attached AFTER setup so a
// fixture's own creates never land in the assertion.
func feedBatch(t *testing.T, core *Core, batch map[string]fsnotify.Op) string {
	t.Helper()
	var warnings bytes.Buffer
	core.SetWarnWriter(&warnings)
	core.handleChanges(batch)
	core.SetWarnWriter(nil)
	return warnings.String()
}

// TestWatcherWarnsOnlyWhenADifferentFileClaimsAStoredID pins the signal the
// watcher was missing, and the three ways it must NOT fire.
//
// The load walk warns when two files parse to one id; handleChanges did not, so
// a second file claiming an id already in the store overwrote it in a live
// `nibs serve` with nothing on stderr — and which of the two won depended on the
// debounce batch's map iteration order.
//
// The move rows are the reason this cannot be decided from memory alone. A move
// (slug rename, archive, unarchive) reaches the create half with the store still
// holding the OLD path, so "the stored path differs" is true for every one of
// them; only the old file's absence from disk tells a move from a collision. Each
// is fed as the create half ALONE, which is the ordering a real one-batch move
// takes whenever the map hands the create out first — non-deterministic in
// production, deterministic here.
//
// They are quiet about COLLISIONS only, which is why the assertion looks for that
// word rather than counting warnings: the file createTestNib wrote carries no
// version key, so each move row also trips the legacy-shape warning on arrival.
func TestWatcherWarnsOnlyWhenADifferentFileClaimsAStoredID(t *testing.T) {
	tests := []struct {
		name string
		// arrange writes the fixture and returns the batch to feed.
		arrange  func(t *testing.T, core *Core, nibsDir string) map[string]fsnotify.Op
		wantWarn bool
	}{
		{
			name: "a second file claiming a stored id",
			arrange: func(t *testing.T, core *Core, nibsDir string) map[string]fsnotify.Op {
				createTestNib(t, core, "dup1", "First", "todo")
				twin := dataPath(nibsDir, "dup1--second.md")
				writeNibFileAtomic(t, twin, twinNib)
				return map[string]fsnotify.Op{twin: fsnotify.Create}
			},
			wantWarn: true,
		},
		{
			name: "an ordinary edit of the file already answering for the id",
			arrange: func(t *testing.T, core *Core, nibsDir string) map[string]fsnotify.Op {
				b := createTestNib(t, core, "edit1", "Editable", "todo")
				path := filepath.Join(core.Root(), filepath.FromSlash(b.Path))
				writeNibFileAtomic(t, path, twinNib)
				return map[string]fsnotify.Op{path: fsnotify.Create}
			},
		},
		{
			name: "a slug rename, arriving before its removal half",
			arrange: func(t *testing.T, core *Core, nibsDir string) map[string]fsnotify.Op {
				b := createTestNib(t, core, "ren1", "Renamed", "todo")
				old := filepath.Join(core.Root(), filepath.FromSlash(b.Path))
				renamed := dataPath(nibsDir, "ren1--new-slug.md")
				if err := os.Rename(old, renamed); err != nil {
					t.Fatal(err)
				}
				return map[string]fsnotify.Op{renamed: fsnotify.Create}
			},
		},
		{
			name: "an unarchive, arriving before its removal half",
			arrange: func(t *testing.T, core *Core, nibsDir string) map[string]fsnotify.Op {
				b := createTestNib(t, core, "una1", "Unarchivable", "todo")
				if err := core.Archive(b.ID); err != nil {
					t.Fatal(err)
				}
				stored, err := core.Get(b.ID)
				if err != nil {
					t.Fatal(err)
				}
				archived := filepath.Join(core.Root(), filepath.FromSlash(stored.Path))
				restored := dataPath(nibsDir, filepath.Base(archived))
				if err := os.Rename(archived, restored); err != nil {
					t.Fatal(err)
				}
				return map[string]fsnotify.Op{restored: fsnotify.Create}
			},
		},
		{
			name: "an archive move, arriving before its removal half",
			arrange: func(t *testing.T, core *Core, nibsDir string) map[string]fsnotify.Op {
				b := createTestNib(t, core, "arc1", "Archivable", "todo")
				old := filepath.Join(core.Root(), filepath.FromSlash(b.Path))
				archived := filepath.Join(storeArchive(t, nibsDir), filepath.Base(old))
				if err := os.Rename(old, archived); err != nil {
					t.Fatal(err)
				}
				return map[string]fsnotify.Op{archived: fsnotify.Create}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, nibsDir := setupTestCore(t)
			batch := tt.arrange(t, core, nibsDir)
			setWatching(core)

			warnings := feedBatch(t, core, batch)
			got := strings.Contains(warnings, "duplicate")
			if got != tt.wantWarn {
				if tt.wantWarn {
					t.Errorf("a different file claimed a stored id and the watcher said nothing:\n%s", warnings)
				} else {
					t.Errorf("a move was reported as a duplicate:\n%s", warnings)
				}
			}
		})
	}
}

// TestWatcherDuplicateWarningNamesBothFiles pins what the reader has to act on:
// which two files claim the id, and which of them the store now answers with.
func TestWatcherDuplicateWarningNamesBothFiles(t *testing.T) {
	core, nibsDir := setupTestCore(t)
	first := createTestNib(t, core, "both1", "First", "todo")
	twin := dataPath(nibsDir, "both1--second.md")
	writeNibFileAtomic(t, twin, twinNib)
	setWatching(core)

	warnings := feedBatch(t, core, map[string]fsnotify.Op{twin: fsnotify.Create})
	for _, want := range []string{"both1", "both1--second.md", filepath.Base(first.Path)} {
		if !strings.Contains(warnings, want) {
			t.Errorf("the collision warning does not name %q, so the reader cannot act on it:\n%s", want, warnings)
		}
	}
}

// TestWatcherDuplicateWarningSpendsTheBatchBudget pins that the new warning is
// bounded like its neighbors (nibs-64uw). A bulk collision — a `git pull` that
// adds a second spelling for every nib — is exactly the shape that made an
// unbounded per-file warning worth capping in the first place.
func TestWatcherDuplicateWarningSpendsTheBatchBudget(t *testing.T) {
	const collisions = 60

	core, nibsDir := setupTestCore(t)
	batch := make(map[string]fsnotify.Op, collisions)
	for i := 1; i <= collisions; i++ {
		id := fmt.Sprintf("col%03d", i)
		createTestNib(t, core, id, "First", "todo")
		twin := dataPath(nibsDir, id+"--second.md")
		writeNibFileAtomic(t, twin, twinNib)
		batch[twin] = fsnotify.Create
	}
	setWatching(core)

	warnings := feedBatch(t, core, batch)
	if !strings.Contains(warnings, "duplicate") {
		t.Fatal("no collision was reported, so this test asserts nothing about the budget")
	}
	// Counted by the warn prefix rather than by wording, so rephrasing the message
	// cannot silently disarm the bound.
	if n := countWarnings(warnings); n > maxWarningsPerBatch+1 {
		t.Errorf("%d warnings for %d colliding arrivals; the budget is %d plus the line reporting what it held back",
			n, collisions, maxWarningsPerBatch)
	}
	// That count is the COLLISION count only because nothing else in this batch
	// warns, which rests on twinNib carrying the current format version: drop that
	// key and every arrival also trips the legacy-shape warning, leaving the bound
	// above measuring a mixture while still passing.
	if collisionLines, total := strings.Count(warnings, "duplicate nib id"), countWarnings(warnings); collisionLines != total-1 {
		t.Errorf("%d of the %d warnings are collisions (the other should be only the suppression line); the budget being measured is not this warning's:\n%.512s…",
			collisionLines, total, warnings)
	}
	if !strings.Contains(warnings, "suppressed") {
		t.Errorf("the batch hid warnings without saying so:\n%.512s…", warnings)
	}
}

// TestWatcherCollisionAmongBrandNewArrivals covers the shape the bulk case is
// named for — a pull that adds both spellings at once, with neither previously
// stored. It takes a different route from the other collision tests: the first
// arrival passes through `existed == false` and warns about nothing, so only the
// second can report the pair.
func TestWatcherCollisionAmongBrandNewArrivals(t *testing.T) {
	core, nibsDir := setupTestCore(t)
	first := dataPath(nibsDir, "new1--alpha.md")
	second := dataPath(nibsDir, "new1--beta.md")
	writeNibFileAtomic(t, first, twinNib)
	writeNibFileAtomic(t, second, twinNib)
	setWatching(core)

	warnings := feedBatch(t, core, map[string]fsnotify.Op{
		first:  fsnotify.Create,
		second: fsnotify.Create,
	})
	if n := strings.Count(warnings, "duplicate nib id"); n != 1 {
		t.Errorf("two twins arriving together produced %d collision lines, want exactly 1:\n%s", n, warnings)
	}
}

// TestWatcherCollisionLeavesTheArrivingFileAnswering pins the claim the warning
// makes about the store. "The arriving file wins" is the reader's whole basis for
// deciding which of the two to delete, and nothing else asserts it.
func TestWatcherCollisionLeavesTheArrivingFileAnswering(t *testing.T) {
	core, nibsDir := setupTestCore(t)
	createTestNib(t, core, "win1", "First", "todo")
	twin := dataPath(nibsDir, "win1--second.md")
	writeNibFileAtomic(t, twin, twinNib)
	setWatching(core)

	warnings := feedBatch(t, core, map[string]fsnotify.Op{twin: fsnotify.Create})
	if !strings.Contains(warnings, "duplicate nib id") {
		t.Fatal("no collision was reported, so this test asserts nothing about the winner")
	}
	answering, err := core.Get("win1")
	if err != nil {
		t.Fatalf("Get after the collision: %v", err)
	}
	if want := "win1--second.md"; filepath.Base(answering.Path) != want {
		t.Errorf("the store answers with %s, but the warning said the arriving file (%s) wins",
			answering.Path, want)
	}
}
