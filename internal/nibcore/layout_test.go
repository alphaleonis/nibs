package nibcore

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/store"
)

// TestCreateWritesUnderData pins where a new nib lands: inside the store's
// data/ directory, with a store-relative Path carrying the data/ prefix. The
// store root itself holds directories and the config, never nib files.
func TestCreateWritesUnderData(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	b := &nib.Nib{ID: "n1", Slug: "alpha", Title: "Alpha", Status: "todo"}
	if err := core.Create(b); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	want := "data/n1--alpha.md"
	if b.Path != want {
		t.Errorf("Path = %q, want %q", b.Path, want)
	}
	if _, err := os.Stat(filepath.Join(nibsDir, "data", "n1--alpha.md")); err != nil {
		t.Errorf("expected the file under data/: %v", err)
	}
	if _, err := os.Stat(filepath.Join(nibsDir, "n1--alpha.md")); err == nil {
		t.Error("nib file was written to the store root; it belongs under data/")
	}
}

// TestArchiveRoundTripStaysInLayout pins both directions of the archive move.
// Unarchive returning a file to the store ROOT rather than data/ would drop it
// out of the store's content the moment it was written — the file would exist
// but no longer load.
func TestArchiveRoundTripStaysInLayout(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	b := &nib.Nib{ID: "n1", Slug: "alpha", Title: "Alpha", Status: "todo"}
	if err := core.Create(b); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := core.Archive("n1"); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	archived, err := core.Get("n1")
	if err != nil {
		t.Fatalf("Get() after archive error = %v", err)
	}
	if archived.Path != "archive/n1--alpha.md" {
		t.Errorf("archived Path = %q, want %q", archived.Path, "archive/n1--alpha.md")
	}
	if _, err := os.Stat(filepath.Join(nibsDir, "archive", "n1--alpha.md")); err != nil {
		t.Errorf("expected the file under archive/: %v", err)
	}

	if err := core.Unarchive("n1"); err != nil {
		t.Fatalf("Unarchive() error = %v", err)
	}
	restored, err := core.Get("n1")
	if err != nil {
		t.Fatalf("Get() after unarchive error = %v", err)
	}
	if restored.Path != "data/n1--alpha.md" {
		t.Errorf("unarchived Path = %q, want %q — unarchive must return the file to data/, not the store root", restored.Path, "data/n1--alpha.md")
	}
	if _, err := os.Stat(filepath.Join(nibsDir, "data", "n1--alpha.md")); err != nil {
		t.Errorf("expected the file back under data/: %v", err)
	}
}

// TestLoadAndUnarchiveReturnsToData pins the second unarchive path. Both
// Unarchive and LoadAndUnarchive targeted the store root before the layout
// inversion, so both have to be held to data/ or one of them silently drops
// files out of the store.
func TestLoadAndUnarchiveReturnsToData(t *testing.T) {
	core, nibsDir := setupTestCore(t)

	b := &nib.Nib{ID: "n1", Slug: "alpha", Title: "Alpha", Status: "todo"}
	if err := core.Create(b); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := core.Archive("n1"); err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	restored, err := core.LoadAndUnarchive("n1")
	if err != nil {
		t.Fatalf("LoadAndUnarchive() error = %v", err)
	}
	if restored.Path != "data/n1--alpha.md" {
		t.Errorf("Path = %q, want %q", restored.Path, "data/n1--alpha.md")
	}
	if _, err := os.Stat(filepath.Join(nibsDir, "data", "n1--alpha.md")); err != nil {
		t.Errorf("expected the file under data/: %v", err)
	}
}

// TestLoadReadsOnlyDataAndArchive pins what counts as store content: data/
// (subdirectories included) and archive/. A stray .md at the store ROOT is not
// store content — it is the pre-migration shape, and loading it would let a
// store that every command refuses to touch answer queries anyway.
func TestLoadReadsOnlyDataAndArchive(t *testing.T) {
	core, nibsDir := setupTestCore(t)
	l := store.NewLayout(nibsDir)

	body := func(id string) string {
		return "---\nversion: 1\ntitle: " + id + "\nstatus: todo\ntype: task\n---\n\nBody.\n"
	}
	mustWrite := func(path, content string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("MkdirAll error = %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("WriteFile error = %v", err)
		}
	}

	mustWrite(filepath.Join(l.DataDir(), "indata.md"), body("indata"))
	mustWrite(filepath.Join(l.DataDir(), "sub", "insub.md"), body("insub"))
	mustWrite(filepath.Join(l.ArchiveDir(), "inarchive.md"), body("inarchive"))
	mustWrite(filepath.Join(nibsDir, "atroot.md"), body("atroot"))

	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	loaded := map[string]bool{}
	for _, b := range core.All() {
		loaded[b.ID] = true
	}
	for _, id := range []string{"indata", "insub", "inarchive"} {
		if !loaded[id] {
			t.Errorf("nib %q did not load; data/ (subfolders included) and archive/ are store content", id)
		}
	}
	if loaded["atroot"] {
		t.Error("a .md at the store root loaded as a nib; the store root is not store content")
	}
}

// TestWatcherPicksUpDataDirectoryCreatedLater pins the watcher's relayout: the
// store's content directories are created on demand — by `nibs migrate`, by
// the first archive, by a `git pull` in the store repo — so the watched set
// cannot be frozen at the directory listing that existed when the watch
// started. The store root is watched so the new directory's creation is
// observable, and the loop then extends the watch to it; otherwise every nib
// written inside it is invisible for the rest of the process's life.
func TestWatcherPicksUpDataDirectoryCreatedLater(t *testing.T) {
	tmpDir := t.TempDir()
	nibsDir := filepath.Join(tmpDir, store.DirName)
	if err := os.MkdirAll(nibsDir, 0755); err != nil {
		t.Fatalf("mkdir store: %v", err)
	}

	core := New(nibsDir, config.Default())
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if err := core.StartWatching(); err != nil {
		t.Fatalf("StartWatching() error = %v", err)
	}
	defer func() { _ = core.StopWatching() }()

	ch, unsub := core.Subscribe()
	defer unsub()

	// data/ does not exist yet: it appears while the watcher is running.
	dataDir := store.NewLayout(nibsDir).DataDir()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		t.Fatalf("mkdir data dir: %v", err)
	}

	// Extending the watch to the new directory is asynchronous — the create
	// event has to travel through fsnotify first — and a file written before
	// the watch lands is missed for reasons that have nothing to do with the
	// behavior under test. Retrying with a FRESH id each round removes that
	// race without weakening the assertion: if the watch is never extended, no
	// round is ever observed and the loop still fails.
	var seen string
	for round := 1; round <= 10 && seen == ""; round++ {
		id := fmt.Sprintf("late%d", round)
		writeNibFileAtomic(t, filepath.Join(dataDir, id+"--arrived.md"),
			"---\nversion: 1\ntitle: Late\nstatus: todo\ntype: task\n---\n\nBody.\n")
		if events := collectNibEvents(t, ch, id, 300*time.Millisecond); len(events) > 0 {
			seen = id
		}
	}
	if seen == "" {
		t.Fatal("no event for a nib created in a data/ directory that appeared after the watch started; the watched set was frozen at start-up")
	}
	if _, err := core.Get(seen); err != nil {
		t.Errorf("Get(%s) = %v; the nib in the later-created data/ never reached the store", seen, err)
	}
}
