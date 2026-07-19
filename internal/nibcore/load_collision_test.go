package nibcore

import (
	"bytes"
	"strings"
	"testing"
)

// writeCollisionNibFile writes a minimal valid nib .md file into dir. The id is
// derived from the filename, so callers embed the (colliding) id via the
// filename to exercise loadFromDisk's duplicate-id handling — the normal Create
// path writes only one file per id and cannot produce a collision.
func writeCollisionNibFile(t *testing.T, dir, filename, title string) {
	t.Helper()
	content := "---\ntitle: " + title + "\nstatus: todo\n---\n\nBody of " + title + ".\n"
	writeNibFile(t, dir, filename, content)
}

// TestLoadWarnsOnDuplicateIDCollision is the tracer: two on-disk files whose
// filenames parse to the SAME id must make Load() emit a warning naming both
// files and the id, return nil (no error), and keep exactly one nib under that
// id (last-writer-wins). WalkDir visits files in lexical order, so the
// lexically-last filename is the surviving occupant.
func TestLoadWarnsOnDuplicateIDCollision(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeCollisionNibFile(t, nibsDir, "nibs-x9z2--alpha.md", "Alpha")
	writeCollisionNibFile(t, nibsDir, "nibs-x9z2--beta.md", "Beta")

	var buf bytes.Buffer
	core.SetWarnWriter(&buf)

	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	warnings := buf.String()
	if !strings.Contains(warnings, "duplicate nib id") {
		t.Errorf("warnings = %q; want a \"duplicate nib id\" collision warning", warnings)
	}
	for _, want := range []string{"nibs-x9z2", "nibs-x9z2--alpha.md", "nibs-x9z2--beta.md"} {
		if !strings.Contains(warnings, want) {
			t.Errorf("warnings = %q; want it to name %q", warnings, want)
		}
	}
	// Direction matters: the surviving winner (beta, lexically last) must be named
	// as shadowing the loser (alpha), not vice versa. A plain Contains check would
	// pass a swapped-args regression that misreports which file survived.
	iWin := strings.Index(warnings, "nibs-x9z2--beta.md")
	iShadows := strings.Index(warnings, " shadows ")
	iLose := strings.Index(warnings, "nibs-x9z2--alpha.md")
	if iWin < 0 || iShadows <= iWin || iLose <= iShadows {
		t.Errorf("warnings = %q; want winner (beta) named before \" shadows \" before loser (alpha)", warnings)
	}

	// Store holds exactly one nib under the colliding id, and the lexically-last
	// file (beta) is the surviving occupant.
	b, err := core.Get("nibs-x9z2")
	if err != nil {
		t.Fatalf("Get(nibs-x9z2) error = %v", err)
	}
	if b.Title != "Beta" {
		t.Errorf("surviving nib Title = %q, want %q (last file loaded wins)", b.Title, "Beta")
	}
}

// TestLoadNoCollisionWarningForDistinctIDs guards against a false positive: a
// directory of all-distinct ids must not emit any "duplicate nib id" warning.
func TestLoadNoCollisionWarningForDistinctIDs(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeCollisionNibFile(t, nibsDir, "nibs-aaa1--one.md", "One")
	writeCollisionNibFile(t, nibsDir, "nibs-bbb2--two.md", "Two")
	writeCollisionNibFile(t, nibsDir, "nibs-ccc3.md", "Three")

	var buf bytes.Buffer
	core.SetWarnWriter(&buf)

	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if strings.Contains(buf.String(), "duplicate nib id") {
		t.Errorf("warnings = %q; want no \"duplicate nib id\" line for all-distinct ids", buf.String())
	}
}

// TestLoadWarnsOnSluggedSluglessCollision covers the concrete mccz-enabled pair:
// a slugged file and a slugless file for one prefixed id both parse to the same
// id under prefix "nibs-", so Load() must warn.
func TestLoadWarnsOnSluggedSluglessCollision(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeCollisionNibFile(t, nibsDir, "nibs-x9z2--slug.md", "Slugged")
	writeCollisionNibFile(t, nibsDir, "nibs-x9z2.md", "Slugless")

	var buf bytes.Buffer
	core.SetWarnWriter(&buf)

	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	warnings := buf.String()
	if !strings.Contains(warnings, "duplicate nib id") {
		t.Errorf("warnings = %q; want a \"duplicate nib id\" collision warning", warnings)
	}
	for _, want := range []string{"nibs-x9z2", "nibs-x9z2--slug.md", "nibs-x9z2.md"} {
		if !strings.Contains(warnings, want) {
			t.Errorf("warnings = %q; want it to name %q", warnings, want)
		}
	}

	// The slugless file wins: "nibs-x9z2.md" sorts after "nibs-x9z2--slug.md"
	// ('.' 0x2E > '-' 0x2D), so WalkDir loads it last.
	b, err := core.Get("nibs-x9z2")
	if err != nil {
		t.Fatalf("Get(nibs-x9z2) error = %v", err)
	}
	if b.Title != "Slugless" {
		t.Errorf("surviving nib Title = %q, want %q (slugless file loaded last)", b.Title, "Slugless")
	}
}

// TestLoadWarnsOnThreeWayIDCollision pins the chained-shadowing behavior: N
// files sharing an id produce N-1 shadowing events, and the lexically-last file
// is the sole survivor.
func TestLoadWarnsOnThreeWayIDCollision(t *testing.T) {
	core, nibsDir := mustLoadPrefixedCore(t)

	writeCollisionNibFile(t, nibsDir, "nibs-x9z2--alpha.md", "Alpha")
	writeCollisionNibFile(t, nibsDir, "nibs-x9z2--beta.md", "Beta")
	writeCollisionNibFile(t, nibsDir, "nibs-x9z2--gamma.md", "Gamma")

	var buf bytes.Buffer
	core.SetWarnWriter(&buf)

	if err := core.Load(); err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if n := strings.Count(buf.String(), "duplicate nib id"); n != 2 {
		t.Errorf("collision warnings = %d, want 2 (3 same-id files -> 2 shadowing events)", n)
	}

	b, err := core.Get("nibs-x9z2")
	if err != nil {
		t.Fatalf("Get(nibs-x9z2) error = %v", err)
	}
	if b.Title != "Gamma" {
		t.Errorf("surviving nib Title = %q, want %q (lexically-last file wins)", b.Title, "Gamma")
	}
}
