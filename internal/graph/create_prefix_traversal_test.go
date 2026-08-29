package graph

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
)

// TestCreateNibRefusesPrefixWithAPathSeparator pins the door `nibs new
// --prefix` and the GraphQL `prefix` input share: the custom prefix is
// concatenated straight onto the drawn short id (nib.NewID), and the id then
// becomes the nib's filename under data/. A prefix carrying a path separator
// therefore steers the write somewhere else: `a/b-` buries the nib in a
// subdirectory whose name the id loses on the next load, and `../../` leaves the
// store altogether — filepath.Join CLEANS its result, so the traversal resolves
// upward instead of failing, and MkdirAll creates whatever directories it names.
func TestCreateNibRefusesPrefixWithAPathSeparator(t *testing.T) {
	prefixes := []string{"../../", "../../../elsewhere/pwn-", `..\..\`, "/absolute-", "a/b-"}

	for _, prefix := range prefixes {
		t.Run(prefix, func(t *testing.T) {
			resolver, core := setupTestResolver(t)
			// The store is <tmp>/.nibs, so <tmp> is the first thing a traversal
			// reaches. Snapshot it to prove nothing lands there.
			outside := filepath.Dir(core.Root())
			before := dirEntryNames(t, outside)

			p := prefix
			got, err := resolver.Mutation().CreateNib(context.Background(), model.CreateNibInput{Title: "Escape", Prefix: &p})
			if err == nil {
				t.Fatalf("CreateNib(prefix=%q) = %v, want a refusal — the id would not survive the filename round trip", prefix, got.ID)
			}
			if !errors.Is(err, nib.ErrIDNotFilename) {
				t.Errorf("CreateNib(prefix=%q) error = %v, want one wrapping nib.ErrIDNotFilename", prefix, err)
			}
			if after := dirEntryNames(t, outside); len(after) != len(before) {
				t.Errorf("a refused create wrote outside the store: %v -> %v", before, after)
			}
			if n := len(core.All()); n != 0 {
				t.Errorf("store holds %d nibs after a refused create, want 0", n)
			}
		})
	}
}

func dirEntryNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}
