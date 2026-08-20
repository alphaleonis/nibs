package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/store"
)

// header builds a scanned front-matter header for the classifier table. The
// scan populates values for every top-level key it saw, so a test header is
// just those keys plus the two structural flags.
func header(keys map[string]string, idMarker bool) fmHeader {
	h := fmHeader{hasFrontMatter: true, hasIDMarker: idMarker, values: map[string]string{}}
	for k, v := range keys {
		h.values[k] = v
	}
	h.status = h.values["status"]
	return h
}

// renderedHeader is the shape nib.Render writes: the id comment on the first
// line inside the fence, then the three keys renderFrontMatter never omits.
func renderedHeader() fmHeader {
	return header(map[string]string{"version": "1", "title": "One", "status": "todo"}, true)
}

// TestLayoutVerdictClassifiesByEvidenceAndPosition drives the layout step's
// per-file decision directly: what the file's own header proves, and what its
// position in a pre-layout store adds to that.
//
// The two axes are deliberately not symmetric. Position MODULATES the uncertain
// tier and never overrides the certain one — Core.Load has always loaded nested
// files, so a nib somebody organized into a folder must still migrate, while a
// documentation page in that same folder must not.
func TestLayoutVerdictClassifiesByEvidenceAndPosition(t *testing.T) {
	tests := []struct {
		name string
		rel  string
		h    fmHeader
		want nibFileVerdict
	}{
		{"the rendered shape at the store root", "leg-a1--one.md", renderedHeader(), isNib},
		{"the rendered shape in a subdirectory", "nested/leg-a1--one.md", renderedHeader(), isNib},
		{"the rendered shape nested two deep", "a/b/leg-a1--one.md", renderedHeader(), isNib},

		{
			// A hand-authored nib: Core.Load has always accepted it, because
			// nib.Parse only ever wanted a fence.
			"a title and a known status at the store root", "my-idea.md",
			header(map[string]string{"title": "Fix the login bug", "status": "todo"}, false), assumedNib,
		},
		{
			// The same evidence one directory down is a documentation page:
			// nibs never wrote a nib there, so the tie breaks the other way.
			"a title and a known status in a subdirectory", "notes/architecture.md",
			header(map[string]string{"title": "Architecture notes", "status": "draft"}, false), notANib,
		},
		{
			// `status: draft` is ordinary front matter in a note vault, and
			// `draft` is one of our six — which is why the status VALUE alone
			// cannot decide, only widen the uncertain tier.
			"a title and a draft status at the store root", "NOTES.md",
			header(map[string]string{"title": "Architecture notes", "status": "draft"}, false), assumedNib,
		},

		{
			"a status outside the enum", "page.md",
			header(map[string]string{"title": "Release notes", "status": "published"}, false), notANib,
		},
		{"a title with no status", "page.md", header(map[string]string{"title": "Release notes"}, false), notANib},
		{"a status with no title", "page.md", header(map[string]string{"status": "todo"}, false), notANib},
		{"front matter carrying neither", "page.md", header(map[string]string{"date": "2026-08-01"}, false), notANib},

		{"no front matter at the store root", "README.md", fmHeader{}, notANib},
		{"no front matter in a subdirectory", "notes/README.md", fmHeader{}, notANib},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := layoutVerdict(tt.rel, tt.h); got != tt.want {
				t.Errorf("layoutVerdict(%q) = %v, want %v", tt.rel, got, tt.want)
			}
		})
	}
}

// TestLayoutMovableFilesMovesOnlyWhatItCanVouchFor pins the set the layout step
// derives everything else from: what moves into data/, what stays put, and
// therefore what still counts as pending layout work.
//
// The bar was "carries a front-matter fence", which is nib.Parse's bar rather
// than nib.Render's — so a documentation page was moved into data/ and rewritten
// as a nib render. Moving it is not a neutral outcome: Parse takes a nib's id
// from the FILENAME, so a document that reaches data/ LOADS as a nib.
func TestLayoutMovableFilesMovesOnlyWhatItCanVouchFor(t *testing.T) {
	_, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
		"leg-a1--one.md":         layoutNib,
		"nested/leg-b2--two.md":  layoutNib,
		"archive/leg-c3--old.md": layoutNib,
		"my-idea.md":             "---\ntitle: Fix the login bug\nstatus: todo\n---\n\nBody.\n",
		"CHANGELOG.md":           "---\ntitle: Release notes\nstatus: published\n---\n\nBody.\n",
		"README.md":              "# Store notes\n\nNo front matter here.\n",
		"notes/architecture.md":  "---\ntitle: Architecture notes\nstatus: draft\n---\n\nBody.\n",
	})

	movable, err := layoutMovableFiles(newMigrateEnv(storeDir))
	if err != nil {
		t.Fatalf("layoutMovableFiles: %v", err)
	}

	// archive/ is already in place, so it is not MOVED even though it is a nib.
	want := []string{"leg-a1--one.md", "my-idea.md", "nested/leg-b2--two.md"}
	got := append([]string(nil), movable...)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("movable set = %v, want %v", got, want)
	}
}

// TestMigrateCompletesWithAFileLeftBehind pins the consequence of leaving a file
// where it is: it must stop counting as unfinished migration work.
//
// Pending-ness is DERIVED from what the mover would move, in more than one place,
// and every command refuses while anything is pending. A file the mover declines
// therefore reads as outstanding layout work forever unless every derivation is
// taught the same rule — including scanStore's, which evaluates each CONTENT
// step's predicate against every front-mattered file wherever it sits, so a
// version-less document left at the root keeps the v0 step pending and the run
// ends with "applied but its detection still fires".
func TestMigrateCompletesWithAFileLeftBehind(t *testing.T) {
	t.Cleanup(resetRootPersistentFlags)
	t.Cleanup(resetMigrateFlags)
	resetMigrateFlags()
	const doc = "---\ntitle: Release notes\nstatus: published\n---\n\nBody.\n"
	_, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", map[string]string{
		"leg-a1--one.md": layoutNib,
		"CHANGELOG.md":   doc,
	})

	out, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
	if err != nil {
		t.Fatalf("migrate refused over a file it left behind: %v\nout: %s", err, out)
	}

	after, readErr := os.ReadFile(filepath.Join(storeDir, "CHANGELOG.md"))
	if readErr != nil {
		t.Fatalf("the document was moved out of the store root: %v", readErr)
	}
	if string(after) != doc {
		t.Errorf("migrate rewrote a document it did not write:\n%s", after)
	}
	if _, statErr := os.Stat(filepath.Join(store.NewLayout(storeDir).DataDir(), "leg-a1--one.md")); statErr != nil {
		t.Errorf("the nib did not reach data/: %v", statErr)
	}

	// The store is no longer gated: an ordinary command runs, and a second
	// migrate finds nothing left to do.
	resetRootPersistentFlags()
	resetListFlags()
	t.Cleanup(resetListFlags)
	if _, err := runRootWith(t, "--nibs-path", storeDir, "list", "--all", "--json"); err != nil {
		t.Fatalf("the store stayed gated after migrating: %v", err)
	}

	resetRootPersistentFlags()
	resetMigrateFlags()
	again, err := runRootWith(t, "--nibs-path", storeDir, "migrate", "--allow-dirty")
	if err != nil {
		t.Fatalf("re-running migrate: %v", err)
	}
	if !strings.Contains(again, "up to date") {
		t.Errorf("the left-behind file still reads as pending work:\n%s", again)
	}
}

// TestCheckPartialLoadFollowsTheSameClassification pins check's half of the
// derivation. Its partial-load warning answers "do nib files sit where Core.Load
// does not look?", and it asks layoutMovableFiles — so once the mover
// classifies, a store holding nothing but a documentation page must stop being
// described as partially loaded, or the warning cries wolf on every store with a
// readme in it.
func TestCheckPartialLoadFollowsTheSameClassification(t *testing.T) {
	const partial = "missing from the checks below"

	run := func(t *testing.T, files map[string]string) string {
		t.Helper()
		t.Cleanup(resetRootPersistentFlags)
		t.Cleanup(resetCheckFlags)
		resetCheckFlags()
		_, storeDir := writeLegacyStore(t, "nibs:\n  prefix: leg-\n", files)
		app := checkAppPastTheGate(t, storeDir)
		var runErr error
		out := captureStdout(t, func() { _, runErr = runCheck(app) })
		if runErr != nil {
			t.Fatalf("runCheck error = %v", runErr)
		}
		return out
	}

	t.Run("a document at the store root is not a partial load", func(t *testing.T) {
		out := run(t, map[string]string{
			"CHANGELOG.md": "---\ntitle: Release notes\nstatus: published\n---\n\nBody.\n",
		})
		if strings.Contains(out, partial) {
			t.Errorf("check called a store holding only a document partially loaded:\n%s", out)
		}
	})

	t.Run("a nib at the store root still is", func(t *testing.T) {
		out := run(t, map[string]string{"leg-a1--one.md": layoutNib})
		if !strings.Contains(out, partial) {
			t.Errorf("check no longer warns that a nib sits outside data/:\n%s", out)
		}
	})
}
