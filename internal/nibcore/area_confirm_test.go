package nibcore

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/store"
)

// TestConfirmScanAgreesWithTheLoadItReplaced is the safety property the whole
// change rests on: the scan must report the members a full load reports.
//
// It asserts AGREEMENT rather than a hand-written expectation, so it cannot be
// dodged by a file shape nobody thought of — every file below is driven through
// both answers, and a scan that reads one of them differently from nib.Parse
// fails here whatever the "right" answer for that file is. Under-reporting is
// the direction that matters: a member the scan misses is a nib the vocabulary
// write strands with nothing saying to rerun.
func TestConfirmScanAgreesWithTheLoadItReplaced(t *testing.T) {
	core, nibsDir := setupCoreWithDeclaredAreas(t)
	data := store.NewLayout(nibsDir).DataDir()
	archive := store.NewLayout(nibsDir).ArchiveDir()
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatal(err)
	}

	// The two sides of the parser's front-matter ceiling. Just under it is a nib
	// that loads, so the scan must find it — this is the shape a budget sized to
	// the ceiling itself would miss, since the read also spends bytes on the
	// fence. Just over it is refused by nib.Parse, so neither answer may carry it.
	underLimit := "---\nversion: 1\ntitle: Just under\nstatus: todo\narea: web\npad: " +
		strings.Repeat("x", nib.MaxFrontMatterBytes-2048) + "\n---\n\nBody.\n"
	overLimit := "---\nversion: 1\ntitle: Just over\nstatus: todo\narea: web\npad: " +
		strings.Repeat("x", nib.MaxFrontMatterBytes+1024) + "\n---\n\nBody.\n"

	files := map[string]string{
		filepath.Join(data, "nibs-c001.md"): "---\nversion: 1\ntitle: Plain\nstatus: todo\narea: web\n---\n\nBody.\n",
		// A quoted scalar, which a line-matching scan would report with its quotes.
		filepath.Join(data, "nibs-c002.md"): "---\nversion: 1\ntitle: Quoted\nstatus: todo\narea: \"web/ui\"\n---\n\nBody.\n",
		// The other opening fence nib.Parse accepts.
		filepath.Join(data, "nibs-c003.md"): "---yaml\nversion: 1\ntitle: Yaml fence\nstatus: todo\narea: web\n---\n\nBody.\n",
		// CRLF throughout.
		filepath.Join(data, "nibs-c004.md"): "---\r\nversion: 1\r\ntitle: CRLF\r\nstatus: todo\r\narea: web\r\n---\r\n\r\nBody.\r\n",
		// Assigned outside the subtree being asked about.
		filepath.Join(data, "nibs-c005.md"): "---\nversion: 1\ntitle: Elsewhere\nstatus: todo\narea: auth\n---\n\nBody.\n",
		// No area at all.
		filepath.Join(data, "nibs-c006.md"): "---\nversion: 1\ntitle: Unassigned\nstatus: todo\n---\n\nBody.\n",
		// The BODY says area: web. Reading past the closing fence would count it.
		filepath.Join(data, "nibs-c007.md"): "---\nversion: 1\ntitle: Body mentions\nstatus: todo\n---\n\narea: web\n",
		// Front matter nothing parses out of.
		filepath.Join(data, "nibs-c008.md"): "---\nversion: 1\ntitle: [unclosed\nstatus: todo\narea: web\n---\n\nBody.\n",
		// Not a nib file at all: no front matter.
		filepath.Join(data, "notes.md"): "Just a note that says area: web in it.\n",
		// Archived nibs are store content and are walked by both.
		filepath.Join(archive, "nibs-c009.md"): "---\nversion: 1\ntitle: Archived\nstatus: completed\narea: web\n---\n\nBody.\n",
		// A deep subdirectory under data/.
		filepath.Join(data, "nested", "nibs-c010.md"): "---\nversion: 1\ntitle: Nested\nstatus: todo\narea: web/ui\n---\n\nBody.\n",
		filepath.Join(data, "nibs-c011.md"):           underLimit,
		filepath.Join(data, "nibs-c012.md"):           overLimit,
	}
	for path, content := range files {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := core.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	for _, path := range []string{"web", "web/ui", "auth"} {
		core.mu.Lock()
		areas := core.areasState().vocab
		loaded := core.areaMembersLocked(areas, path)
		scanned, err := core.scanAreaMembersOnDiskLocked(areas, path)
		core.mu.Unlock()
		if err != nil {
			t.Fatalf("scan %q: %v", path, err)
		}
		if !slices.Equal(scanned, loaded) {
			t.Errorf("area %q: scan = %v, the load it replaced = %v", path, scanned, loaded)
		}
		if len(loaded) == 0 {
			t.Errorf("area %q: the fixture placed no member there, so agreement proves nothing", path)
		}
	}

	// Named explicitly, because the differential above would also pass if BOTH
	// answers missed them: the two shapes the bounded read is most likely to get
	// wrong on its own.
	core.mu.Lock()
	scanned, err := core.scanAreaMembersOnDiskLocked(core.areasState().vocab, "web")
	core.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"nibs-c009", "nibs-c011"} {
		if !slices.Contains(scanned, id) {
			t.Errorf("scan = %v, want it to carry %s (archived nib / front matter just under the parser's ceiling)", scanned, id)
		}
	}
	if slices.Contains(scanned, "nibs-c007") {
		t.Errorf("scan = %v, want it NOT to carry nibs-c007, whose BODY says area: web", scanned)
	}
	// Refused by nib.Parse, so no load ever counted it either.
	if slices.Contains(scanned, "nibs-c012") {
		t.Errorf("scan = %v, want it NOT to carry nibs-c012, whose front matter the parser refuses", scanned)
	}
}
