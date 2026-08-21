package cmd

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/ui"
)

// setupParentLinkTest builds a Resolver over a Core loaded from hand-written nib
// files under the "nibs-" prefix. files maps a nib id to the extra front matter
// lines for it.
//
// Hand-writing the files and going through a real Load are both load-bearing:
// the writing resolvers normalize a parent id before storing it, so a short-form
// or dangling link only ever reaches the store from a file — and the loader's
// link canonicalization, which these tests exist to guard, only runs on that
// path. The explicit `version: 1` keeps the v0->v1 migration from rewriting the
// file during Load.
func setupParentLinkTest(t *testing.T, files map[string]string) (*graph.Resolver, *nibcore.Core) {
	t.Helper()
	nibsDir := filepath.Join(t.TempDir(), ".nibs")
	if err := os.MkdirAll(storeDataDir(nibsDir), 0755); err != nil {
		t.Fatal(err)
	}
	for id, frontMatter := range files {
		content := "---\nversion: 2\ntitle: " + id + "\nstatus: todo\ntype: task\n" + frontMatter + "---\n\nBody.\n"
		if err := os.WriteFile(dataPath(nibsDir, id+"--test.md"), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	core := nibcore.New(nibsDir, config.DefaultWithPrefix("nibs-"))
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	return &graph.Resolver{Reader: core, Writer: core, Validator: core, Blocking: core, Orderer: graph.NewOrderer(core, core)}, core
}

// mustGet fetches a nib a test's premise or setup depends on, reporting the
// lookup failure itself rather than letting it surface later as a confusing
// assertion mismatch.
func mustGet(t *testing.T, core *nibcore.Core, id string) *nib.Nib {
	t.Helper()
	b, err := core.Get(id)
	if err != nil {
		t.Fatalf("Get(%q): %v", id, err)
	}
	return b
}

// nibIDs returns the sorted ids of nibs, so two surfaces can be compared as
// sets without depending on either one's ordering.
func nibIDs(nibs []*nib.Nib) []string {
	ids := make([]string, 0, len(nibs))
	for _, b := range nibs {
		ids = append(ids, b.ID)
	}
	sort.Strings(ids)
	return ids
}

// assertSameIDs fails unless got holds exactly want, as a set.
func assertSameIDs(t *testing.T, what string, got []*nib.Nib, want []string) {
	t.Helper()
	gotIDs := nibIDs(got)
	wantIDs := append([]string(nil), want...)
	sort.Strings(wantIDs)
	if len(gotIDs) != len(wantIDs) {
		t.Errorf("%s = %v, want %v", what, gotIDs, wantIDs)
		return
	}
	for i := range gotIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Errorf("%s = %v, want %v", what, gotIDs, wantIDs)
			return
		}
	}
}

// siblingsByFilter asks the siblingId filter for targetID's siblings — the
// GraphQL surface the web UI reads.
func siblingsByFilter(t *testing.T, resolver *graph.Resolver, targetID string) []*nib.Nib {
	t.Helper()
	id := targetID
	got, err := resolver.Query().Nibs(context.Background(), &model.NibFilter{SiblingID: &id}, nil)
	if err != nil {
		t.Fatalf("Nibs(siblingId=%q): %v", targetID, err)
	}
	return got
}

// siblingsByTraversal asks fetchSiblings for the same set — the surface
// `nibs rel <id> --rel siblings` reads.
func siblingsByTraversal(t *testing.T, resolver *graph.Resolver, core *nibcore.Core, targetID string) []*nib.Nib {
	t.Helper()
	got, err := fetchSiblings(context.Background(), resolver, mustGet(t, core, targetID), nil)
	if err != nil {
		t.Fatalf("fetchSiblings(%q): %v", targetID, err)
	}
	return got
}

// TestSiblingSurfacesAgreeOnShortFormParentLink pins that the loader's link
// canonicalization reaches both sibling surfaces, so a parent named in short
// form by one child and in full by the other yields one sibling set through
// either route — the filter comparing resolved parents, and fetchSiblings
// walking up to the parent and back down through its children.
//
// It guards canonicalization, NOT the resolved-parent comparison: both stored
// fields are byte-identical by the time either surface runs, so a raw
// comparison would answer the same. The comparison itself is covered by the
// stub-based tests in internal/graph/filters_test.go, which skip the Load.
func TestSiblingSurfacesAgreeOnShortFormParentLink(t *testing.T) {
	resolver, core := setupParentLinkTest(t, map[string]string{
		"nibs-par": "",
		"nibs-sho": "parent: par\n",      // hand-edited short form
		"nibs-ful": "parent: nibs-par\n", // the same link spelled out
		"nibs-oth": "",                   // unrelated root, must never appear
	})

	// Premise: the short form named a real nib and was resolved on load.
	if got := mustGet(t, core, "nibs-sho"); got.Parent != "nibs-par" {
		t.Fatalf("premise failed: stored parent = %q, want it canonicalized to %q", got.Parent, "nibs-par")
	}

	tests := []struct {
		target string
		want   []string
	}{
		{"nibs-ful", []string{"nibs-sho"}},
		{"nibs-sho", []string{"nibs-ful"}},
	}

	for _, tt := range tests {
		t.Run("siblings of "+tt.target, func(t *testing.T) {
			assertSameIDs(t, "siblingId", siblingsByFilter(t, resolver, tt.target), tt.want)
			assertSameIDs(t, "fetchSiblings", siblingsByTraversal(t, resolver, core, tt.target), tt.want)
		})
	}
}

// TestRootSurfacesAgreeOnDanglingParentLink ties the hasParent rule to the
// sibling surfaces. A nib whose parent link names no nib is a root by the
// resolved-parent rule, so all three have to place it there: hasParent:false
// must select it, and it must appear in — and receive — a root sibling set.
//
// Root-ness is the case where the surfaces are easiest to get out of step,
// because each one can decide it locally from the raw stored field and look
// self-consistent while disagreeing with the others.
func TestRootSurfacesAgreeOnDanglingParentLink(t *testing.T) {
	resolver, core := setupParentLinkTest(t, map[string]string{
		"nibs-rt1": "",                     // a genuine root
		"nibs-rt2": "",                     // a second root
		"nibs-dng": "parent: nibs-ghost\n", // names no nib under either spelling
		"nibs-par": "",
		"nibs-chi": "parent: nibs-par\n", // a real parent, the control
	})
	ctx := context.Background()

	// Premise: canonicalization cannot resolve the link, so it survives load
	// verbatim and the divergence under test is actually present.
	if got := mustGet(t, core, "nibs-dng"); got.Parent != "nibs-ghost" {
		t.Fatalf("premise failed: stored parent = %q, want it left verbatim as %q", got.Parent, "nibs-ghost")
	}

	roots := []string{"nibs-rt1", "nibs-rt2", "nibs-dng", "nibs-par"}

	t.Run("hasParent:false selects it as a root", func(t *testing.T) {
		no := false
		got, err := resolver.Query().Nibs(ctx, &model.NibFilter{HasParent: &no}, nil)
		if err != nil {
			t.Fatalf("Nibs(hasParent=false): %v", err)
		}
		assertSameIDs(t, "hasParent:false", got, roots)
	})

	// Every root must report the same sibling set: the roots minus itself. That
	// makes the relation symmetric — the dangling nib does not merely see the
	// other roots, it is seen BY them.
	for _, target := range roots {
		want := make([]string, 0, len(roots)-1)
		for _, id := range roots {
			if id != target {
				want = append(want, id)
			}
		}
		t.Run("root siblings of "+target, func(t *testing.T) {
			assertSameIDs(t, "siblingId", siblingsByFilter(t, resolver, target), want)
			assertSameIDs(t, "fetchSiblings", siblingsByTraversal(t, resolver, core, target), want)
		})
	}
}

// TestDeletedBareTokenCycleStaysVisibleInTree is the end-to-end half of the
// cycle-tolerant tree builder: it drives a REAL cycle into a real Core through
// the removal sweep, then renders it.
//
// `nibs-t1` names its parent by the bare token `e1`, which resolves exactly to
// the bare-token nib while that nib exists. Deleting it re-canonicalizes the
// link onto the prefixed twin `nibs-e1` — which already names `nibs-t1` as its
// own parent, so the rebind closes a parent cycle with no writer having
// validated one. Both nibs then have a parent inside the store and used to
// disappear from the tree entirely.
//
// It lives in cmd because that is the composition root already depending on
// both nibcore (to drive the sweep) and ui (to render), so neither layer has to
// learn about the other.
func TestDeletedBareTokenCycleStaysVisibleInTree(t *testing.T) {
	_, core := setupParentLinkTest(t, map[string]string{
		"e1":      "",
		"nibs-e1": "parent: nibs-t1\n",
		"nibs-t1": "parent: e1\n",
	})

	// Premise: the bare spelling resolves exactly, so load leaves it verbatim
	// and no cycle exists yet. Without this the assertions below could pass for
	// the wrong reason.
	if got := mustGet(t, core, "nibs-t1"); got.Parent != "e1" {
		t.Fatalf("premise failed: stored parent = %q, want the bare spelling %q to survive load", got.Parent, "e1")
	}
	if cycles := core.CheckAllLinks().Cycles; len(cycles) != 0 {
		t.Fatalf("premise failed: cycles before the delete = %+v, want none", cycles)
	}

	if err := core.Delete("e1"); err != nil {
		t.Fatalf(`Delete("e1"): %v`, err)
	}

	// The rebind closed the cycle: this is the state the tree must survive.
	if got := mustGet(t, core, "nibs-t1"); got.Parent != "nibs-e1" {
		t.Fatalf("stored parent = %q, want %q — the removal sweep must rebind onto the prefixed twin", got.Parent, "nibs-e1")
	}
	if cycles := core.CheckAllLinks().Cycles; len(cycles) == 0 {
		t.Fatalf("no cycle after the delete; the scenario under test did not reproduce")
	}

	all := core.All()
	assertSameIDs(t, "store contents", all, []string{"nibs-e1", "nibs-t1"})

	tree := ui.BuildTree(all, all, nib.SortByOrder)
	var rendered []*nib.Nib
	var walk func(nodes []*ui.TreeNode)
	walk = func(nodes []*ui.TreeNode) {
		for _, n := range nodes {
			rendered = append(rendered, n.Nib)
			walk(n.Children)
		}
	}
	walk(tree)
	assertSameIDs(t, "BuildTree over a store holding a parent cycle", rendered, []string{"nibs-e1", "nibs-t1"})
}
