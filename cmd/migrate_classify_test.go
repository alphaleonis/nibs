package cmd

import (
	"slices"
	"testing"
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
