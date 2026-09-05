package nibcore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/store"
)

// twoStaleCores returns two Cores over ONE store, both loaded before either has
// written. That is the shape the store probe exists for: neither map can ever
// hear about the other's nib, and the cross-process write lock serializes the
// writes without refreshing either map. It is a running `nibs serve` beside a
// `nibs new`, or two concurrent CLIs, reproduced in one process without a timing
// race.
func twoStaleCores(t *testing.T) (*Core, *Core, string) {
	t.Helper()
	nibsDir := filepath.Join(t.TempDir(), store.DirName)
	if err := os.MkdirAll(store.NewLayout(nibsDir).DataDir(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	load := func() *Core {
		c := New(nibsDir, config.Default())
		c.SetWarnWriter(nil)
		if err := c.Load(); err != nil {
			t.Fatalf("load: %v", err)
		}
		return c
	}
	return load(), load(), nibsDir
}

// dataFileNames lists the .md files directly under the store's data/ directory.
func dataFileNames(t *testing.T, nibsDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(store.NewLayout(nibsDir).DataDir())
	if err != nil {
		t.Fatalf("read data dir: %v", err)
	}
	var names []string
	for _, e := range entries {
		if filepath.Ext(e.Name()) == ".md" {
			names = append(names, e.Name())
		}
	}
	return names
}

// TestCreateRefusesASuppliedIDOnlyAnotherCoreHasWritten is the reproduction for
// nibs-lmyt on the caller-supplied path. The second Core's map has no trace of
// the first's nib, and two nibs with different titles get different file names,
// so nothing downstream collides either — before the store probe this wrote a
// second file claiming one id and returned nil.
func TestCreateRefusesASuppliedIDOnlyAnotherCoreHasWritten(t *testing.T) {
	coreA, coreB, nibsDir := twoStaleCores(t)

	if err := coreA.Create(&nib.Nib{ID: "tkn1", Slug: "alpha", Title: "Alpha", Status: "todo"}); err != nil {
		t.Fatalf("coreA.Create() error = %v", err)
	}

	err := coreB.Create(&nib.Nib{ID: "tkn1", Slug: "beta", Title: "Beta", Status: "todo"})
	var exists *IDExistsError
	if !errors.As(err, &exists) || exists.ID != "tkn1" {
		t.Errorf("coreB.Create() error = %v, want *IDExistsError for tkn1", err)
	}
	if got := dataFileNames(t, nibsDir); len(got) != 1 || got[0] != "tkn1--alpha.md" {
		t.Errorf("data/ holds %v, want only tkn1--alpha.md — two files claiming one id", got)
	}
}

// TestCreateRedrawsAGeneratedIDOnlyAnotherCoreHasWritten is the same
// reproduction on the generated path, which is the one a user meets by
// accident: the draw is uniform over the id space, so a store another process
// has grown is exactly where a collision becomes likely. The generator is seeded
// rather than left to chance — a natural collision is ~1 in 1.7M per draw.
func TestCreateRedrawsAGeneratedIDOnlyAnotherCoreHasWritten(t *testing.T) {
	coreA, coreB, nibsDir := twoStaleCores(t)
	swapIDGenerator(t, "tkn1", "tkn1", "frs1")

	if err := coreA.Create(&nib.Nib{Slug: "alpha", Title: "Alpha", Status: "todo"}); err != nil {
		t.Fatalf("coreA.Create() error = %v", err)
	}

	b := &nib.Nib{Slug: "beta", Title: "Beta", Status: "todo"}
	if err := coreB.Create(b); err != nil {
		t.Fatalf("coreB.Create() error = %v — a taken draw must be redrawn, not fatal", err)
	}
	if b.ID != "frs1" {
		t.Errorf("coreB.Create assigned id %q, want the redrawn frs1", b.ID)
	}
	got := dataFileNames(t, nibsDir)
	if len(got) != 2 {
		t.Fatalf("data/ holds %v, want two files", got)
	}
	for _, name := range got {
		if id, _ := nib.ParseFilename(name, ""); id == "tkn1" && name != "tkn1--alpha.md" {
			t.Errorf("data/ holds %v — a second file claims tkn1", got)
		}
	}
}

// TestCreateProbesTheWholeStoreForASuppliedID covers what a glob over data/
// would miss. Store content is data/ INCLUDING its subdirectories, plus
// archive/, and the file is planted after the Core loaded so only the store
// probe can see it — the in-memory map never held it.
func TestCreateProbesTheWholeStoreForASuppliedID(t *testing.T) {
	tests := []struct {
		name string
		dir  func(l store.Layout) string
	}{
		{"data root", func(l store.Layout) string { return l.DataDir() }},
		{"data subdirectory", func(l store.Layout) string { return filepath.Join(l.DataDir(), "epics", "deep") }},
		{"archive", func(l store.Layout) string { return l.ArchiveDir() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			core, nibsDir := setupTestCore(t)
			dir := tt.dir(store.NewLayout(nibsDir))
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatalf("mkdir: %v", err)
			}
			writeNibFile(t, dir, "tkn1--planted.md", "---\ntitle: Planted\nstatus: todo\n---\n\nBody.\n")

			err := core.Create(&nib.Nib{ID: "tkn1", Slug: "newcomer", Title: "Newcomer", Status: "todo"})
			var exists *IDExistsError
			if !errors.As(err, &exists) || exists.ID != "tkn1" {
				t.Errorf("Create() error = %v, want *IDExistsError for tkn1", err)
			}
			if _, serr := os.Stat(dataPath(nibsDir, "tkn1--newcomer.md")); !os.IsNotExist(serr) {
				t.Error("the refusal still wrote the newcomer's file")
			}
		})
	}
}

// TestCreateRefusesAnIDClaimedByAnUnparseableFile is the one class where probe
// and loader deliberately part. Load records this file as unparseable and leaves
// its id out of c.nibs; the probe takes it anyway, because the NAME is what
// claims the id and repairing the file must not surface a second nib wearing it.
func TestCreateRefusesAnIDClaimedByAnUnparseableFile(t *testing.T) {
	core, nibsDir := setupTestCore(t)
	writeNibFile(t, storeData(t, nibsDir), "tkn1--broken.md", "---\ntitle: [unterminated\n---\n")
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, err := core.Get("tkn1"); err == nil {
		t.Fatal("the unparseable file loaded as a nib; this test no longer covers the divergence it names")
	}

	err := core.Create(&nib.Nib{ID: "tkn1", Slug: "newcomer", Title: "Newcomer", Status: "todo"})
	var exists *IDExistsError
	if !errors.As(err, &exists) || exists.ID != "tkn1" {
		t.Errorf("Create() error = %v, want *IDExistsError for tkn1", err)
	}
}
