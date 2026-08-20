package graph

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/config"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/projection"
	"github.com/alphaleonis/nibs/internal/store"
)

// writeParentLinkNib writes a nib file by hand and returns its path. Hand-writing
// is load-bearing for every test in this file: the writing resolvers normalize a
// parent id before storing it, so a short-form or dangling link only ever reaches
// the store from a file a human (or a merge in .nibs/) produced. The explicit
// `version: 1` keeps the v0->v1 migration from rewriting the file during Load.
func writeParentLinkNib(t *testing.T, nibsDir, id, frontMatter string) string {
	t.Helper()
	path := filepath.Join(store.NewLayout(nibsDir).DataDir(), id+"--test.md")
	head := "---\nversion: 1\ntitle: " + id + "\nstatus: todo\n"
	// A fixture that declares its own type REPLACES the default rather than
	// adding a second `type:` key, which YAML would reject as a duplicate.
	if !strings.Contains(frontMatter, "type:") {
		head += "type: task\n"
	}
	body := head + frontMatter + "---\n\nBody.\n"
	if err := os.WriteFile(path, []byte(body), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// mustLoadResolverFromFiles builds a Resolver over a Core loaded from
// hand-written files under the "nibs-" prefix. Going through a real Load means
// the link canonicalization pass at the disk-read boundary actually runs, which
// a hand-built reader stub would bypass — and that pass is exactly what the
// short-form tests here are guarding.
//
// files maps a nib id to the extra front matter lines for it.
func mustLoadResolverFromFiles(t *testing.T, files map[string]string) (*Resolver, *nibcore.Core) {
	t.Helper()
	nibsDir := filepath.Join(t.TempDir(), store.DirName)
	if err := os.MkdirAll(store.NewLayout(nibsDir).DataDir(), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for id, frontMatter := range files {
		writeParentLinkNib(t, nibsDir, id, frontMatter)
	}

	core := nibcore.New(nibsDir, config.DefaultWithPrefix("nibs-"))
	core.SetWarnWriter(nil)
	if err := core.Load(); err != nil {
		t.Fatalf("load: %v", err)
	}
	return &Resolver{Reader: core, Writer: core, Validator: core, Blocking: core, Orderer: NewOrderer(core, core)}, core
}

// mustGet fetches a nib the test's premise depends on.
func mustGet(t *testing.T, core *nibcore.Core, id string) *nib.Nib {
	t.Helper()
	b, err := core.Get(id)
	if err != nil {
		t.Fatalf("Get(%q): %v", id, err)
	}
	return b
}

// TestDanglingParentClassifiedAlikeAcrossSurfaces settles one definition of
// "has a parent". Four surfaces answer that question independently — the
// nib.parent resolver, the parentId field, the hasParent filter, and the
// siblingId filter — and a nib whose parent link names no nib is the shape that
// pulls them apart: the stored field is non-empty, but nothing can be fetched
// through it.
//
// The four are asserted together rather than in separate tests because what is
// being pinned is their AGREEMENT, not any one verdict. Whichever way the
// project settles the rule, a change that moves one surface without the others
// has to fail here. storedParentId rides along as the counterweight: it is the
// one field that must NOT follow the rule, so that adopting the resolved
// reading everywhere else does not make a broken link unreadable.
func TestDanglingParentClassifiedAlikeAcrossSurfaces(t *testing.T) {
	resolver, core := mustLoadResolverFromFiles(t, map[string]string{
		"nibs-rt1": "",                     // a genuine root
		"nibs-rt2": "",                     // a second root, so a sibling set is observable
		"nibs-dng": "parent: nibs-ghost\n", // names no nib under either spelling
		"nibs-par": "",
		"nibs-chi": "parent: nibs-par\n", // a real parent, the control
	})
	ctx := context.Background()

	// Premise: canonicalization cannot resolve the link, so it survives load
	// verbatim and the divergence under test is actually present.
	dangling := mustGet(t, core, "nibs-dng")
	if dangling.Parent != "nibs-ghost" {
		t.Fatalf("premise failed: stored parent = %q, want it left verbatim as %q", dangling.Parent, "nibs-ghost")
	}

	blocking := resolver.Blocking
	all := core.All()

	t.Run("the nib.parent resolver reports no parent", func(t *testing.T) {
		parent, err := resolver.Nib().Parent(ctx, dangling)
		if err != nil {
			t.Fatalf("Parent: %v", err)
		}
		if parent != nil {
			t.Errorf("Parent = %q, want nil — the link resolves to nothing", parent.ID)
		}
	})

	t.Run("the parentId field reports no parent", func(t *testing.T) {
		got, err := resolver.Nib().ParentID(ctx, dangling)
		if err != nil {
			t.Fatalf("ParentID: %v", err)
		}
		if got != nil {
			t.Errorf("ParentID = %q, want nil — the link resolves to nothing", *got)
		}
		// The control still reports its parent, so "nil" is not the field going blind.
		child, err := resolver.Nib().ParentID(ctx, mustGet(t, core, "nibs-chi"))
		if err != nil {
			t.Fatalf("ParentID(nibs-chi): %v", err)
		}
		if child == nil || *child != "nibs-par" {
			t.Errorf("ParentID(nibs-chi) = %v, want %q", child, "nibs-par")
		}
	})

	t.Run("storedParentId keeps the broken link inspectable", func(t *testing.T) {
		got, err := resolver.Nib().StoredParentID(ctx, dangling)
		if err != nil {
			t.Fatalf("StoredParentID: %v", err)
		}
		if got == nil || *got != "nibs-ghost" {
			t.Errorf("StoredParentID = %v, want %q — parentId went null, so this is the only way to see the link", got, "nibs-ghost")
		}
		// A nib with no link at all reports nothing, not an empty string.
		root, err := resolver.Nib().StoredParentID(ctx, mustGet(t, core, "nibs-rt1"))
		if err != nil {
			t.Fatalf("StoredParentID(nibs-rt1): %v", err)
		}
		if root != nil {
			t.Errorf("StoredParentID(nibs-rt1) = %q, want nil", *root)
		}
	})

	t.Run("hasParent puts it with the parentless", func(t *testing.T) {
		yes, no := true, false
		got := applyFilterOK(t, ctx, all, &model.NibFilter{HasParent: &yes}, core, blocking)
		assertNibIDs(t, got, []string{"nibs-chi"})
		got = applyFilterOK(t, ctx, all, &model.NibFilter{HasParent: &no}, core, blocking)
		assertNibIDs(t, got, []string{"nibs-rt1", "nibs-rt2", "nibs-dng", "nibs-par"})
	})

	t.Run("siblingId puts it in the root sibling set, both directions", func(t *testing.T) {
		got := applyFilterOK(t, ctx, all, &model.NibFilter{SiblingID: strPtr("nibs-dng")}, core, blocking)
		assertNibIDs(t, got, []string{"nibs-rt1", "nibs-rt2", "nibs-par"})
		got = applyFilterOK(t, ctx, all, &model.NibFilter{SiblingID: strPtr("nibs-rt1")}, core, blocking)
		assertNibIDs(t, got, []string{"nibs-rt2", "nibs-dng", "nibs-par"})
	})
}

// TestParentIDMatchesShortFormStoredLink pins the parentId FILTER against a
// stored parent link written in short form, over a real Load — so what it
// guards is canonicalization reaching the filter: both spellings are rewritten
// to one full id at the disk-read boundary, and the filter finds both. Both
// spellings of the filter ARGUMENT are driven too, since a short --parent and a
// full one must reach the same answer.
//
// It no longer guards the filter's own resolution, which is what it did while
// the filter compared the raw stored string and depended on that pass for
// soundness. The filter now resolves each candidate itself; coverage for that
// is TestParentIDFilterAndFieldResolveShortFormLinks in filters_test.go, which
// injects an unresolved short form through a stub reader — a state no real Load
// produces.
func TestParentIDMatchesShortFormStoredLink(t *testing.T) {
	resolver, core := mustLoadResolverFromFiles(t, map[string]string{
		"nibs-par": "",
		"nibs-sho": "parent: par\n",      // hand-edited short form
		"nibs-ful": "parent: nibs-par\n", // the same link spelled out
		"nibs-oth": "",                   // unrelated root, must never match
	})
	ctx := context.Background()

	// Premise: the short form named a real nib and was resolved on load. Without
	// this the assertions below could pass for the wrong reason.
	if got := mustGet(t, core, "nibs-sho"); got.Parent != "nibs-par" {
		t.Fatalf("premise failed: stored parent = %q, want it canonicalized to %q", got.Parent, "nibs-par")
	}

	all := core.All()
	for _, arg := range []string{"nibs-par", "par"} {
		t.Run("parentId="+arg+" finds both spellings", func(t *testing.T) {
			got := applyFilterOK(t, ctx, all, &model.NibFilter{ParentID: strPtr(arg)}, core, resolver.Blocking)
			assertNibIDs(t, got, []string{"nibs-sho", "nibs-ful"})
		})
	}
}

// orderingFixture is the shape the ordering cases need: two genuine roots, a
// nib whose parent link names no nib, and a real parent with two children as
// the control. Each case loads its own copy — a reorder writes to disk, so one
// shared store would let an earlier case decide a later one's starting order.
//
// Every nib carries an explicit order key. Without one the sibling lookup tries
// to backfill, and on a hand-authored file with no timestamps that write is
// refused on a stable etag divergence and silently dropped (see
// Orderer.backfillKeys) — leaving an anchor at the empty key, which any
// assigned key trivially sorts after. Seeding the keys keeps the positional
// assertion below load-bearing.
func orderingFixture() map[string]string {
	return map[string]string{
		"nibs-rt1": "order: a0\n",                     // a genuine root
		"nibs-rt2": "order: b0\n",                     // a second root
		"nibs-dng": "parent: nibs-ghost\norder: c0\n", // names no nib under either spelling
		"nibs-par": "order: d0\n",                     // a real parent, also a root itself
		"nibs-ch1": "parent: nibs-par\norder: a0\n",   // its children, the control set
		"nibs-ch2": "parent: nibs-par\norder: b0\n",
	}
}

// assertReorderAfter drives reorderNib's --after path and checks both halves of
// the outcome: that the anchor was accepted or refused as expected, and — when
// accepted — that the moved nib landed immediately after the anchor inside the
// anchor's own sibling set, rather than merely avoiding an error.
func assertReorderAfter(t *testing.T, resolver *Resolver, core *nibcore.Core, targetID, anchorID string, wantErr bool) {
	t.Helper()
	_, err := resolver.Mutation().ReorderNib(context.Background(), targetID, &anchorID, nil, nil, nil, nil)
	if wantErr {
		if err == nil {
			t.Fatalf("ReorderNib(%s, after=%s) succeeded, want it refused as a non-sibling", targetID, anchorID)
		}
		if !strings.Contains(err.Error(), "not a sibling") {
			t.Errorf("ReorderNib(%s, after=%s) error = %v, want a not-a-sibling refusal", targetID, anchorID, err)
		}
		return
	}
	if err != nil {
		t.Fatalf("ReorderNib(%s, after=%s): %v", targetID, anchorID, err)
	}

	// Read the placement out of the ANCHOR's sibling set: the move is only
	// correct if the two ended up in one set, which is the agreement under test.
	anchorNib := mustGet(t, core, anchorID)
	ordered := resolver.Orderer.Members(ScopeParent, resolvedParentID(anchorNib, resolver.Reader))
	got := make([]string, 0, len(ordered))
	for _, s := range ordered {
		got = append(got, s.ID)
	}
	anchorAt, movedAt := slices.Index(got, anchorID), slices.Index(got, targetID)
	if anchorAt < 0 || movedAt != anchorAt+1 {
		t.Errorf("sibling order after moving %s after %s = %v, want %s directly after %s",
			targetID, anchorID, got, targetID, anchorID)
	}
}

// TestReorderAnchorAgreesWithTheSiblingSurfaces carries the one parent-ness
// rule into ordering. `nibs mv <id> --after <anchor>` and `nibs rel <id> --rel
// siblings` answer the same question — do these two nibs share a parent — and a
// nib whose parent link names no nib is where a raw emptiness test pulls them
// apart: the listing offers it as a root's sibling and the move then refuses it
// as that root's anchor.
func TestReorderAnchorAgreesWithTheSiblingSurfaces(t *testing.T) {
	tests := []struct {
		name    string
		target  string
		anchor  string
		wantErr bool
	}{
		{"a dangling-parent nib anchors a root", "nibs-rt1", "nibs-dng", false},
		{"a root anchors a dangling-parent nib", "nibs-dng", "nibs-rt2", false},
		{"two genuine roots still anchor each other", "nibs-rt1", "nibs-rt2", false},
		{"children of one parent still anchor each other", "nibs-ch1", "nibs-ch2", false},
		{"a child does not anchor a root", "nibs-rt1", "nibs-ch1", true},
		{"a root does not anchor a child", "nibs-ch1", "nibs-rt1", true},
		{"a child does not anchor a dangling-parent nib", "nibs-dng", "nibs-ch1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver, core := mustLoadResolverFromFiles(t, orderingFixture())
			assertReorderAfter(t, resolver, core, tt.target, tt.anchor, tt.wantErr)
		})
	}
}

// TestDanglingLinksToTheSameMissingNibAreRootSiblings settles the shape where
// two nibs name the SAME nonexistent parent. Neither link resolves, so both are
// roots — members of the one root set, not of a private group keyed on the
// phantom id. Every surface has to read them that way, or naming a missing nib
// twice conjures a sibling group that only the ordering path can see.
func TestDanglingLinksToTheSameMissingNibAreRootSiblings(t *testing.T) {
	resolver, core := mustLoadResolverFromFiles(t, map[string]string{
		"nibs-dg1": "parent: nibs-ghost\norder: a0\n",
		"nibs-dg2": "parent: nibs-ghost\norder: b0\n", // the same missing id
		"nibs-rt1": "order: c0\n",
	})

	t.Run("siblingId puts them in the root set together", func(t *testing.T) {
		got := applyFilterOK(t, context.Background(), core.All(), &model.NibFilter{SiblingID: strPtr("nibs-dg1")}, core, resolver.Blocking)
		assertNibIDs(t, got, []string{"nibs-dg2", "nibs-rt1"})
	})

	t.Run("each anchors the other", func(t *testing.T) {
		assertReorderAfter(t, resolver, core, "nibs-dg1", "nibs-dg2", false)
	})

	t.Run("a genuine root anchors them too", func(t *testing.T) {
		assertReorderAfter(t, resolver, core, "nibs-rt1", "nibs-dg2", false)
	})
}

// TestOrderingAgreesOnShortFormParentLink pins that the loader's link
// canonicalization reaches the ordering surface: both spellings land in one
// sibling set, so either can anchor the other.
//
// It guards canonicalization, NOT the resolved-parent comparison — by the time
// either surface runs, the two stored fields are byte-identical, so a raw
// comparison would answer the same. Coverage for the comparison itself is the
// stub-based TestApplyFilterHasParentResolvesParentLink in filters_test.go,
// which injects an unresolved short form without a Load.
func TestOrderingAgreesOnShortFormParentLink(t *testing.T) {
	resolver, core := mustLoadResolverFromFiles(t, map[string]string{
		"nibs-par": "order: a0\n",
		"nibs-sho": "parent: par\norder: a0\n",      // hand-edited short form
		"nibs-ful": "parent: nibs-par\norder: b0\n", // the same link spelled out
		"nibs-oth": "order: b0\n",                   // unrelated root, never a sibling of either
	})

	t.Run("the two spellings anchor each other", func(t *testing.T) {
		assertReorderAfter(t, resolver, core, "nibs-sho", "nibs-ful", false)
	})

	t.Run("a root does not anchor the short-form child", func(t *testing.T) {
		assertReorderAfter(t, resolver, core, "nibs-sho", "nibs-oth", true)
	})
}

// TestBulkRootReorderIncludesADanglingParentNib guards the pair of checks that
// bracket a root-level bulk reorder: reorderChildren("") requires the list to
// name every root (completeness) and rejects any listed nib that is not one
// (membership). Both have to draw the root set the same way. If only one counts
// a nib whose parent link names no nib as a root, a project holding one can
// neither list that nib nor omit it, and root-level bulk reorder is impossible.
func TestBulkRootReorderIncludesADanglingParentNib(t *testing.T) {
	roots := []string{"nibs-dng", "nibs-rt2", "nibs-rt1", "nibs-par"}

	t.Run("reorderChildren applies the requested root order", func(t *testing.T) {
		resolver, _ := mustLoadResolverFromFiles(t, orderingFixture())
		got, err := resolver.Mutation().ReorderChildren(context.Background(), "", roots, nil)
		if err != nil {
			t.Fatalf("ReorderChildren(root): %v", err)
		}
		// Unsorted: the requested order is deliberately not alphabetical, so a
		// set comparison would pass on a reorder that moved nothing.
		if ids := nibIDsInOrder(got); !slices.Equal(ids, roots) {
			t.Errorf("returned order = %v, want %v", ids, roots)
		}
		if ids := rootIDsInOrder(t, resolver); !slices.Equal(ids, roots) {
			t.Errorf("stored root order = %v, want %v", ids, roots)
		}
	})

	t.Run("reorderSiblings moves a root and a dangling-parent nib as one block", func(t *testing.T) {
		resolver, _ := mustLoadResolverFromFiles(t, orderingFixture())
		first := true
		got, err := resolver.Mutation().ReorderSiblings(context.Background(), []string{"nibs-rt1", "nibs-dng"}, nil, nil, &first, nil)
		if err != nil {
			t.Fatalf("ReorderSiblings: %v", err)
		}
		assertNibIDs(t, got, []string{"nibs-rt1", "nibs-dng"})
		// first=true has to put the block at the FRONT of the root set, contiguous
		// and in the requested order. Membership alone would pass on a no-op.
		ids := rootIDsInOrder(t, resolver)
		if len(ids) < 2 || ids[0] != "nibs-rt1" || ids[1] != "nibs-dng" {
			t.Errorf("root order = %v, want nibs-rt1 then nibs-dng first", ids)
		}
	})
}

// nibIDsInOrder returns the ids as given, preserving order — unlike
// assertNibIDs, which sorts both sides and so cannot see placement.
func nibIDsInOrder(nibs []*nib.Nib) []string {
	ids := make([]string, 0, len(nibs))
	for _, b := range nibs {
		ids = append(ids, b.ID)
	}
	return ids
}

// rootIDsInOrder reads the root sibling set back out of the ordering surface in
// order-key order, which is what a reorder is supposed to have rewritten.
func rootIDsInOrder(t *testing.T, resolver *Resolver) []string {
	t.Helper()
	return nibIDsInOrder(resolver.Orderer.Members(ScopeParent, ""))
}

// TestChildrenResolverSeesShortFormParentLink is the reverse-traversal half. A
// short-form `parent:` resolves when followed FORWARD from the child, which
// masks whether the PARENT can see it — so the child side passing says nothing
// about this direction. The children resolver is the surface the web UI and
// `nibs rel --rel children` both read, and it reaches the link through
// Members, which matches exact map keys — so nothing but
// canonicalization puts the child in this list.
func TestChildrenResolverSeesShortFormParentLink(t *testing.T) {
	resolver, core := mustLoadResolverFromFiles(t, map[string]string{
		"nibs-par": "",
		"nibs-sho": "parent: par\n",      // hand-edited short form
		"nibs-ful": "parent: nibs-par\n", // the same link spelled out
		"nibs-oth": "",                   // unrelated root, must never appear
	})
	ctx := context.Background()

	parent := mustGet(t, core, "nibs-par")
	children, err := resolver.Nib().Children(ctx, parent, nil, nil)
	if err != nil {
		t.Fatalf("Children: %v", err)
	}
	assertNibIDs(t, children, []string{"nibs-sho", "nibs-ful"})
}

// TestTypeChangeIgnoresAStoredParentLinkThatResolvesToNothing carries the one
// parent-ness rule onto the write path. Changing a nib's type re-validates its
// EXISTING parent link against the new type, and a link naming no nib has no
// parent to constrain the type — so the revalidation has nothing to check and
// the change must go through. Reading the raw stored field instead makes the
// mutation abort with "parent nib not found", naming a field the caller never
// touched and dead-ending every type change on such a nib.
//
// The illegal-hierarchy case rides along because the fix must not be "skip the
// revalidation": a link that DOES resolve still constrains the new type.
func TestTypeChangeIgnoresAStoredParentLinkThatResolvesToNothing(t *testing.T) {
	newFixture := func(t *testing.T) (*Resolver, *nibcore.Core) {
		return mustLoadResolverFromFiles(t, map[string]string{
			"nibs-dng": "parent: nibs-ghost\n",           // names no nib under either spelling
			"nibs-epc": "type: epic\n",                   // a real parent, the control
			"nibs-chi": "parent: nibs-epc\ntype: task\n", // legal today: epic parents a task
		})
	}

	t.Run("a type change goes through when the link resolves to nothing", func(t *testing.T) {
		resolver, core := newFixture(t)

		// Premise: the link survived load verbatim, so the divergence is present.
		if got := mustGet(t, core, "nibs-dng").Parent; got != "nibs-ghost" {
			t.Fatalf("premise failed: stored parent = %q, want it left verbatim as %q", got, "nibs-ghost")
		}

		bug := "bug"
		got, err := resolver.Mutation().UpdateNib(context.Background(), "nibs-dng", model.UpdateNibInput{Type: &bug})
		if err != nil {
			t.Fatalf("UpdateNib(type: bug) on a nib whose parent link names no nib: %v", err)
		}
		if got.EffectiveType() != "bug" {
			t.Errorf("type after update = %q, want %q", got.EffectiveType(), "bug")
		}
		// The stored link is untouched — the mutation ignored it, it did not repair it.
		if stored := mustGet(t, core, "nibs-dng").Parent; stored != "nibs-ghost" {
			t.Errorf("stored parent after update = %q, want it left alone as %q", stored, "nibs-ghost")
		}
	})

	t.Run("a type change still fails when the link resolves and the new type is illegal under it", func(t *testing.T) {
		resolver, core := newFixture(t)

		// milestone is the one type that may not have a parent at all, so an epic
		// parent makes this change illegal however the hierarchy is spelled.
		milestone := "milestone"
		_, err := resolver.Mutation().UpdateNib(context.Background(), "nibs-chi", model.UpdateNibInput{Type: &milestone})
		if err == nil {
			t.Fatal("UpdateNib(type: milestone) under an epic parent succeeded, want it refused on the hierarchy")
		}
		if strings.Contains(err.Error(), "not found") {
			t.Errorf("error = %v, want a hierarchy refusal, not a lookup failure", err)
		}
		if got := mustGet(t, core, "nibs-chi").EffectiveType(); got != "task" {
			t.Errorf("type after the refused update = %q, want it unchanged at %q", got, "task")
		}
	})
}

// TestClearingAParentLinkThatResolvesToNothingLeavesTheNibInPlace settles the
// second write-path half. validateAndSetParent decides "did the parent change?"
// so it can recalculate the order key, and deciding it from the raw stored
// string counts dangling -> cleared as a change — sending a nib that was ALREADY
// a root by every surface using the rule to the end of the root order. It is a
// repair that changes nothing semantically yet relocates the nib.
//
// Core.FixBrokenLinks repairs the identical link without touching Order, so the
// two repair paths for one broken link are asserted against each other: `nibs
// set <id> --clear parent` and `nibs check --fix` must leave the nib in the
// same place.
func TestClearingAParentLinkThatResolvesToNothingLeavesTheNibInPlace(t *testing.T) {
	// The fixture puts nibs-dng in the MIDDLE of the root order (nibs-par sorts
	// after it), so "went to the end" and "stayed put" are distinguishable —
	// against a last-place nib OrderLast is indistinguishable from a no-op.
	const danglingOrder = "c0"

	t.Run("setParent(null) leaves the order key byte-identical", func(t *testing.T) {
		resolver, core := mustLoadResolverFromFiles(t, orderingFixture())

		// Premise: the nib is already a root by the rule, so the repair is
		// semantically a no-op and any relocation is pure damage.
		dangling := mustGet(t, core, "nibs-dng")
		if got := resolvedParentID(dangling, core); got != "" {
			t.Fatalf("premise failed: resolvedParentID = %q, want %q", got, "")
		}
		if dangling.Order != danglingOrder {
			t.Fatalf("premise failed: order = %q, want the seeded %q", dangling.Order, danglingOrder)
		}

		if _, err := resolver.Mutation().SetParent(context.Background(), "nibs-dng", nil, nil); err != nil {
			t.Fatalf("SetParent(nibs-dng, null): %v", err)
		}
		after := mustGet(t, core, "nibs-dng")
		if after.Parent != "" {
			t.Errorf("stored parent after the clear = %q, want it cleared", after.Parent)
		}
		if after.Order != danglingOrder {
			t.Errorf("order after clearing a link that resolves to nothing = %q, want it unmoved at %q", after.Order, danglingOrder)
		}
	})

	t.Run("setParent(null) and FixBrokenLinks agree on where the nib ends up", func(t *testing.T) {
		viaSetParent, core := mustLoadResolverFromFiles(t, orderingFixture())
		if _, err := viaSetParent.Mutation().SetParent(context.Background(), "nibs-dng", nil, nil); err != nil {
			t.Fatalf("SetParent(nibs-dng, null): %v", err)
		}

		// A second, independent store from the same fixture — a shared one would
		// let the first repair decide the second's starting state.
		_, fixCore := mustLoadResolverFromFiles(t, orderingFixture())
		fixed, err := fixCore.FixBrokenLinks()
		if err != nil {
			t.Fatalf("FixBrokenLinks: %v", err)
		}
		if fixed == 0 {
			t.Fatal("premise failed: FixBrokenLinks repaired nothing, so it never touched the link under test")
		}

		want := mustGet(t, fixCore, "nibs-dng")
		got := mustGet(t, core, "nibs-dng")
		if got.Order != want.Order {
			t.Errorf("order via setParent(null) = %q, via FixBrokenLinks = %q — the two repair paths disagree", got.Order, want.Order)
		}
	})

	t.Run("clearing a link that DOES resolve still recalculates the order key", func(t *testing.T) {
		resolver, core := mustLoadResolverFromFiles(t, orderingFixture())

		// nibs-ch1 is a real child at "a0"; promoting it to a root has to place it
		// among the roots rather than leave it colliding with nibs-rt1's key.
		before := mustGet(t, core, "nibs-ch1")
		if resolvedParentID(before, core) == "" {
			t.Fatal("premise failed: nibs-ch1's parent link does not resolve, so this is not the control case")
		}

		if _, err := resolver.Mutation().SetParent(context.Background(), "nibs-ch1", nil, nil); err != nil {
			t.Fatalf("SetParent(nibs-ch1, null): %v", err)
		}
		after := mustGet(t, core, "nibs-ch1")
		if after.Order == before.Order {
			t.Errorf("order after clearing a resolvable parent = %q, want it recalculated away from the child key", after.Order)
		}
		// It lands at the end of the root set, which is what Recalculate does.
		if ids := rootIDsInOrder(t, resolver); len(ids) == 0 || ids[len(ids)-1] != "nibs-ch1" {
			t.Errorf("root order after the promotion = %v, want nibs-ch1 last", ids)
		}
	})
}

// TestProjectionSplitsResolvedAndStoredParent carries the one parent-ness rule
// onto the CLI's `-f` surface, which is the other place a client reads a parent
// id from. `-f parent` gives the resolved reading, matching the GraphQL
// parentId field it is the CLI spelling of; `-f stored_parent` gives the raw
// stored link, so `nibs get <id> -f stored_parent` is how a broken link is
// diagnosed once `parent` stops showing it.
//
// It runs over a real Load for the same reason the rest of this file does: the
// projection reads through the store, and the shape under test only exists in
// one a canonicalization pass could not repair.
func TestProjectionSplitsResolvedAndStoredParent(t *testing.T) {
	resolver, core := mustLoadResolverFromFiles(t, map[string]string{
		"nibs-dng": "parent: nibs-ghost\n", // names no nib under either spelling
		"nibs-par": "",
		"nibs-chi": "parent: nibs-par\n", // a real parent, the control
		"nibs-rt1": "",                   // no link at all
	})

	sel, err := projection.ParseFields("parent,stored_parent")
	if err != nil {
		t.Fatalf("ParseFields: %v", err)
	}
	pr := resolver.ProjectionResolver(context.Background())

	tests := []struct {
		id               string
		wantParent       string
		wantStoredParent string
	}{
		{"nibs-dng", "", "nibs-ghost"}, // resolves to nothing; the link stays readable
		{"nibs-chi", "nibs-par", "nibs-par"},
		{"nibs-rt1", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			p, err := projection.Project(mustGet(t, core, tt.id), sel, pr)
			if err != nil {
				t.Fatalf("Project: %v", err)
			}
			if got, _ := p.Get("parent"); got != tt.wantParent {
				t.Errorf("-f parent = %v, want %q", got, tt.wantParent)
			}
			if got, _ := p.Get("stored_parent"); got != tt.wantStoredParent {
				t.Errorf("-f stored_parent = %v, want %q", got, tt.wantStoredParent)
			}
		})
	}
}
