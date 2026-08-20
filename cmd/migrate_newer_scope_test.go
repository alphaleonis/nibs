package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/store"
)

// TestNewerVersionRefusalIsScopedToFilesItSpeaksFor pins which files a
// `version:` above this build's format can speak for.
//
// The key is not a nibs invention — API docs, spec pages and chart front matter
// carry it — so evaluating it on every fenced file let one documentation page in
// a pre-layout store refuse EVERY command, `nibs migrate` included, with a remedy
// ("upgrade nibs") naming a version that does not exist. That state is terminal:
// the only command that could fix the store is one of the refused ones.
//
// Scoping it by classification alone would be wrong in the other direction. A
// future format may well rename keys or add a status this build does not know,
// so a genuine v2 nib would not classify — which is exactly the file the refusal
// exists for. So a version speaks for the store when the file will BE store
// content, or when it carries the id comment nib.Render has written since the
// initial commit, whatever else has changed around it.
func TestNewerVersionRefusalIsScopedToFilesItSpeaksFor(t *testing.T) {
	const newerVersion = "version: 99\n"

	run := func(t *testing.T, files map[string]string) (string, error) {
		t.Helper()
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(resetListFlags)
		resetRootPersistentFlags()
		resetListFlags()
		_, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", files)
		return runRootWith(t, "--nibs-path", storeDir, "list", "--all")
	}

	t.Run("a documentation page's version no longer locks the store", func(t *testing.T) {
		out, err := run(t, map[string]string{
			"leg-a1--one.md": layoutNib,
			// Not a nib under layoutVerdict: no id comment, and a status
			// outside the enum. It stays where it is, so its version says
			// nothing about what wrote this store.
			"api.md": "---\ntitle: API reference\n" + newerVersion + "status: published\n---\n\nDocs.\n",
		})
		if err == nil {
			return // the store is merely pre-layout; the migration refusal is a different test
		}
		if strings.Contains(err.Error(), "newer nibs") {
			t.Errorf("a documentation page's version: locked the store:\n%v\nout: %s", err, out)
		}
	})

	for _, tt := range []struct {
		name  string
		files map[string]string
	}{
		{
			// Carries the id comment, so nibs wrote it however unfamiliar the
			// rest of the header looks — the case the refusal exists for.
			name: "a nib render from a newer nibs still refuses",
			files: map[string]string{
				"leg-a1--one.md": layoutNib,
				"leg-b2--two.md": "---\n# leg-b2\n" + newerVersion + "title: Two\nstatus: invented-later\n---\n\nBody.\n",
			},
		},
		{
			// Already store content by position, whatever its shape.
			name: "a file under data/ still refuses whatever its shape",
			files: map[string]string{
				"leg-a1--one.md": layoutNib,
				"data/api.md":    "---\ntitle: API reference\n" + newerVersion + "status: published\n---\n\nDocs.\n",
			},
		},
		{
			// About to BECOME content: the layout step would move it, so this
			// build would have to migrate a version it does not understand.
			name: "an assumed nib with a newer version still refuses",
			files: map[string]string{
				"leg-a1--one.md": layoutNib,
				"my-idea.md":     "---\ntitle: Fix the login bug\n" + newerVersion + "status: todo\n---\n\nBody.\n",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out, err := run(t, tt.files)
			if err == nil || !strings.Contains(err.Error(), "newer nibs") {
				t.Fatalf("the newer-store refusal did not fire: err=%v\nout: %s", err, out)
			}
		})
	}
}

// TestNewerStoreRefusalNamesAnActionThatClearsIt pins the remedy. "Upgrade nibs"
// is right for a file a newer nibs wrote and wrong for a document about to be
// moved into data/, and the refusal cannot tell which it is holding — so it has
// to name both ways out.
func TestNewerStoreRefusalNamesAnActionThatClearsIt(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetListFlags)
	resetListFlags()
	_, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
		"leg-a1--one.md": layoutNib,
		"my-idea.md":     "---\ntitle: Fix the login bug\nversion: 99\nstatus: todo\n---\n\nBody.\n",
	})

	_, err := runRootWith(t, "--nibs-path", storeDir, "list", "--all")
	if err == nil {
		t.Fatal("expected the newer-store refusal")
	}
	if !strings.Contains(err.Error(), "upgrade nibs") {
		t.Errorf("the refusal dropped the upgrade remedy: %v", err)
	}
	if !strings.Contains(err.Error(), "move") {
		t.Errorf("the refusal offers no way out for a file that is not a nib: %v", err)
	}
}

// TestNewerVersionOutsideTheStoreLeavesMigrateRunnable is the whole point of the
// scoping: the state must not be one only the refused command could fix.
func TestNewerVersionOutsideTheStoreLeavesMigrateRunnable(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()
	_, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
		"leg-a1--one.md": layoutNib,
		"api.md":         "---\ntitle: API reference\nversion: 99\nstatus: published\n---\n\nDocs.\n",
	})

	out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
	if err != nil {
		t.Fatalf("migrate is locked out by a version: in a file it does not migrate: %v\nout: %s", err, out)
	}
	if _, statErr := os.Stat(filepath.Join(store.NewLayout(storeDir).DataDir(), "leg-a1--one.md")); statErr != nil {
		t.Errorf("the nib did not reach data/: %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Join(storeDir, "api.md")); statErr != nil {
		t.Errorf("the documentation page was moved: %v", statErr)
	}
}
