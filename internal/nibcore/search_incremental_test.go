package nibcore

import (
	"os"
	"slices"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
)

// idsOf renders a result set as the ids it holds, in the order it holds them —
// the comparable form of a search answer.
func idsOf(nibs []*nib.Nib) []string {
	ids := make([]string, 0, len(nibs))
	for _, b := range nibs {
		ids = append(ids, b.ID)
	}
	return ids
}

// nibFile is the on-disk form of one nib, written the way an external editor or
// a `git pull` in the store leaves it.
func nibFile(title, body string) string {
	return "---\ntitle: " + title + "\nstatus: todo\n---\n\n" + body + "\n"
}

// TestSearch_LoadIndexesOnlyChanges is the point of the incremental re-index: a
// reload costs what CHANGED, not what the store holds. The whole-store version
// of this was the largest part of every load wherever an index was live, charged
// inside c.mu where every reader waits for it (nibs-lx4w).
func TestSearch_LoadIndexesOnlyChanges(t *testing.T) {
	core, nibsDir := setupTestCore(t)
	defer func() { _ = core.Close() }()
	data := storeData(t, nibsDir)

	for _, f := range []struct{ name, title, body string }{
		{"aaa1--one.md", "One", "alpha content"},
		{"bbb2--two.md", "Two", "beta content"},
		{"ccc3--three.md", "Three", "gamma content"},
	} {
		if err := writeTestFile(data, f.name, nibFile(f.title, f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	spy := &spySearchIndex{}
	core.SetSearchIndex(spy)
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := len(spy.indexed); got != 3 {
		t.Fatalf("the load after the injection indexed %d nibs, want all 3", got)
	}

	t.Run("an unchanged store indexes nothing", func(t *testing.T) {
		spy.indexed, spy.deleted = nil, nil
		if err := core.Load(); err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if len(spy.indexed) != 0 || len(spy.deleted) != 0 {
			t.Errorf("indexed = %v, deleted = %v, want neither: nothing changed on disk", spy.indexed, spy.deleted)
		}
	})

	t.Run("only the changed, added and removed nibs", func(t *testing.T) {
		spy.indexed, spy.deleted = nil, nil

		// A body rewritten, a nib added, a nib removed. bbb2 is left alone.
		if err := writeTestFile(data, "aaa1--one.md", nibFile("One", "alpha content, revised")); err != nil {
			t.Fatal(err)
		}
		if err := writeTestFile(data, "ddd4--four.md", nibFile("Four", "delta content")); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(data + "/ccc3--three.md"); err != nil {
			t.Fatal(err)
		}

		if err := core.Load(); err != nil {
			t.Fatalf("Load() error = %v", err)
		}

		slices.Sort(spy.indexed)
		if want := []string{"aaa1", "ddd4"}; !slices.Equal(spy.indexed, want) {
			t.Errorf("indexed = %v, want %v — bbb2 did not change", spy.indexed, want)
		}
		if want := []string{"ccc3"}; !slices.Equal(spy.deleted, want) {
			t.Errorf("deleted = %v, want %v", spy.deleted, want)
		}
	})

	t.Run("a nib restored under a deleted id is indexed again", func(t *testing.T) {
		spy.indexed, spy.deleted = nil, nil

		// Byte-identical to the file removed above: the id was dropped from what
		// this Core believes the index holds, so it must be indexed rather than
		// taken for one already there.
		if err := writeTestFile(data, "ccc3--three.md", nibFile("Three", "gamma content")); err != nil {
			t.Fatal(err)
		}
		if err := core.Load(); err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if want := []string{"ccc3"}; !slices.Equal(spy.indexed, want) {
			t.Errorf("indexed = %v, want %v", spy.indexed, want)
		}
	})
}

// TestSearch_IncrementalReloadMatchesFullRebuild is the correctness half: what
// the index answers after a series of incremental reloads is what a full rebuild
// of the same store state answers. It runs against the real Bleve index, so it
// covers the text actually stored rather than a spy's record of the calls.
func TestSearch_IncrementalReloadMatchesFullRebuild(t *testing.T) {
	core, nibsDir := setupTestCore(t)
	defer func() { _ = core.Close() }()
	data := storeData(t, nibsDir)

	for _, f := range []struct{ name, title, body string }{
		{"aaa1--one.md", "Authentication", "login and session handling"},
		{"bbb2--two.md", "Database", "schema and migrations"},
		{"ccc3--three.md", "Reporting", "charts over the login funnel"},
	} {
		if err := writeTestFile(data, f.name, nibFile(f.title, f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	// Initializes the real index, as a user's first search does.
	if _, err := core.Search("login"); err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	// Mutate the store the way an external writer would: one body replaced so its
	// old terms are gone, one nib added, one removed.
	if err := writeTestFile(data, "aaa1--one.md", nibFile("Authentication", "token exchange only")); err != nil {
		t.Fatal(err)
	}
	if err := writeTestFile(data, "ddd4--four.md", nibFile("Billing", "invoices and login receipts")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(data + "/bbb2--two.md"); err != nil {
		t.Fatal(err)
	}
	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// A second Core over the same directory indexes every nib from scratch.
	rebuilt := New(nibsDir, core.config)
	rebuilt.SetWarnWriter(nil)
	defer func() { _ = rebuilt.Close() }()
	if err := rebuilt.Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	for _, query := range []string{"login", "token", "schema", "invoices", "Authentication", "the"} {
		t.Run(query, func(t *testing.T) {
			incremental, err := core.SearchAll(query)
			if err != nil {
				t.Fatalf("SearchAll(%q) error = %v", query, err)
			}
			full, err := rebuilt.SearchAll(query)
			if err != nil {
				t.Fatalf("SearchAll(%q) error = %v", query, err)
			}
			if !slices.Equal(idsOf(incremental), idsOf(full)) {
				t.Errorf("SearchAll(%q): incremental = %v, full rebuild = %v",
					query, idsOf(incremental), idsOf(full))
			}
		})
	}
}
