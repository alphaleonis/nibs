package cmd

import (
	"context"
	"sort"
	"testing"

	"github.com/alphaleonis/nibs/internal/graph"
	"github.com/alphaleonis/nibs/internal/graph/model"
	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
	"github.com/alphaleonis/nibs/internal/nibcore"
	"github.com/alphaleonis/nibs/internal/projection"
	"github.com/alphaleonis/nibs/internal/ui"
)

// This file drives one store through every surface that decides what a nib's
// parent is, and requires them to agree. It lives in package cmd because the
// `nibs rel` traversals are unexported here while everything else it drives —
// the GraphQL resolvers and filters, the ordering scope, the membership view,
// the tree builder, the field engine — is exported.
//
// It catches a bad parent read by the WRONG ANSWER it produces rather than by
// how the read is spelled, so an accessor, a helper or a rename changes nothing
// about what is covered. One named surface is an exception, marked where it is
// driven: the parentId filter branch cannot be made to disagree from inside this
// fixture. nibs-oa4m is the shape it exists for: hasParent
// decided parent-ness from the raw stored string while the object graph
// resolved it, so `nibs(filter: {hasParent: true})` returned nibs whose parent
// field was null in the same response, `rel siblings` was asymmetric between a
// root and a dangling-parent nib, and root-level bulk reorder had no legal
// input at all — including the nib failed membership, omitting it failed
// completeness.

// parentAgreementFixture is the store every surface below is driven over. Two
// shapes separate the resolved reading of the parent link from the raw one:
//
//   - A DANGLING link: nibs-dg1 and nibs-dg2 both name nibs-ghost, which is no
//     nib. The stored field is non-empty and nothing can be fetched through it,
//     so a raw reading calls such a nib parented while a resolving one calls it
//     a root. Two of them name the SAME missing nib, which is what separates
//     "each is a root" from "they are a group keyed on a phantom parent".
//   - A SHORT-FORM link: nibs-sho names `par`. What this shape asserts is the
//     setup premise below — that Load canonicalizes it. Once that holds,
//     nibs-sho is indistinguishable from nibs-ful at every surface.
//
// The files are hand-written because the writing resolvers normalize a parent
// id before storing it — neither shape can be created through the CLI or
// GraphQL, only by a hand edit or a merge in .nibs/.
var parentAgreementFixture = map[string]string{
	"nibs-rt1": "",
	"nibs-rt2": "",
	"nibs-par": "",
	"nibs-ful": "parent: nibs-par\n",
	"nibs-sho": "parent: par\n",
	"nibs-dg1": "parent: nibs-ghost\n",
	"nibs-dg2": "parent: nibs-ghost\n",
}

// resolvedParentOf is the answer every surface owes for each fixture nib: the
// id its parent link resolves to, "" for a root.
var resolvedParentOf = map[string]string{
	"nibs-rt1": "",
	"nibs-rt2": "",
	"nibs-par": "",
	"nibs-ful": "nibs-par",
	"nibs-sho": "nibs-par",
	"nibs-dg1": "",
	"nibs-dg2": "",
}

// setupParentAgreement loads the fixture and checks the two premises every test
// in this file rests on. Without them a surface could agree with the others for
// the wrong reason — because the store no longer holds the shape under test.
func setupParentAgreement(t *testing.T) (*graph.Resolver, *nibcore.Core) {
	t.Helper()
	resolver, core := setupParentLinkTest(t, parentAgreementFixture)
	if got := mustGet(t, core, "nibs-dg1"); got.Parent != "nibs-ghost" {
		t.Fatalf("premise failed: nibs-dg1's stored parent = %q, want it left verbatim as %q", got.Parent, "nibs-ghost")
	}
	if got := mustGet(t, core, "nibs-sho"); got.Parent != "nibs-par" {
		t.Fatalf("premise failed: nibs-sho's stored parent = %q, want it canonicalized to %q", got.Parent, "nibs-par")
	}
	return resolver, core
}

// parentQuery carries what a surface needs to answer a question about the
// fixture, so the two tables below share one signature.
type parentQuery struct {
	ctx      context.Context
	resolver *graph.Resolver
	core     *nibcore.Core
	// groupID is the parent group being enumerated; "" names the roots.
	groupID string
	// representative is a member of groupID that a sibling surface can ask
	// FROM. Those surfaces answer "the group minus me", so they add themselves
	// back before returning.
	representative string
}

// parentOfSurfaces is every way this project answers "what is this nib's
// parent". Each returns the resolved parent id, "" for a root.
var parentOfSurfaces = []struct {
	name     string
	parentOf func(t *testing.T, q parentQuery, b *nib.Nib) string
}{
	{"graphql nib.parentId", func(t *testing.T, q parentQuery, b *nib.Nib) string {
		got, err := q.resolver.Nib().ParentID(q.ctx, b)
		if err != nil {
			t.Fatalf("ParentID(%s): %v", b.ID, err)
		}
		if got == nil {
			return ""
		}
		return *got
	}},
	{"graphql nib.parent", func(t *testing.T, q parentQuery, b *nib.Nib) string {
		got, err := q.resolver.Nib().Parent(q.ctx, b)
		if err != nil {
			t.Fatalf("Parent(%s): %v", b.ID, err)
		}
		if got == nil {
			return ""
		}
		return got.ID
	}},
	{"cli rel --rel parent", func(t *testing.T, q parentQuery, b *nib.Nib) string {
		got, err := fetchRel(q.ctx, q.resolver, b, relParent, nil, 1)
		if err != nil {
			t.Fatalf("fetchRel(parent, %s): %v", b.ID, err)
		}
		if len(got) == 0 {
			return ""
		}
		if len(got) > 1 {
			t.Fatalf("fetchRel(parent, %s) returned %d nibs; a nib has at most one parent", b.ID, len(got))
		}
		return got[0].ID
	}},
	{"cli -f parent", func(t *testing.T, q parentQuery, b *nib.Nib) string {
		return projectedField(t, q, b, "parent").(string)
	}},
}

// projectedField runs one nib through the `-f` field engine and returns the
// named field's value — the same path `nibs list -f` and `nibs show -f` take.
func projectedField(t *testing.T, q parentQuery, b *nib.Nib, field string) any {
	t.Helper()
	sel, err := projection.Compile("", field)
	if err != nil {
		t.Fatalf("projection.Compile(%q): %v", field, err)
	}
	projected, err := projection.Project(b, sel, q.resolver.ProjectionResolver(q.ctx))
	if err != nil {
		t.Fatalf("projection.Project(%s, %q): %v", b.ID, field, err)
	}
	value, ok := projected.Get(field)
	if !ok {
		t.Fatalf("projection of %s carries no %q field", b.ID, field)
	}
	return value
}

// TestParentSurfacesAgreeOnResolvedParent drives every "who is the parent"
// surface over the whole fixture and requires one answer per nib.
func TestParentSurfacesAgreeOnResolvedParent(t *testing.T) {
	resolver, core := setupParentAgreement(t)
	q := parentQuery{ctx: context.Background(), resolver: resolver, core: core}

	for _, id := range sortedKeys(resolvedParentOf) {
		want := resolvedParentOf[id]
		b := mustGet(t, core, id)
		for _, surface := range parentOfSurfaces {
			t.Run(id+"/"+surface.name, func(t *testing.T) {
				if got := surface.parentOf(t, q, b); got != want {
					t.Errorf("%s reports %s's parent as %q, want %q — parent-ness has one "+
						"definition and every surface answers with it", surface.name, id, got, want)
				}
			})
		}
	}
}

// parentGroupSurfaces is every way this project enumerates a parent group. The
// empty group id names the roots; the two surfaces that take a parent NIB
// cannot answer for it, since no nib names the root group.
var parentGroupSurfaces = []struct {
	name      string
	rootGroup bool
	members   func(t *testing.T, q parentQuery) []string
}{
	// The hasParent half is the discriminating one. The only parentId target
	// this fixture can name is nibs-par, whose two members' links the loader
	// already canonicalized, so the raw and resolved readings coincide there —
	// and no legal parentId target can dangle, because resolveFilterTarget
	// refuses an unresolvable id before any parent is read. The resolved reading
	// of that branch is pinned by TestParentIDFilterAndFieldResolveShortFormLinks
	// in internal/graph instead.
	{"graphql nibs(hasParent/parentId)", true, func(t *testing.T, q parentQuery) []string {
		filter := &model.NibFilter{}
		if q.groupID == "" {
			no := false
			filter.HasParent = &no
		} else {
			id := q.groupID
			filter.ParentID = &id
		}
		got, err := q.resolver.Query().Nibs(q.ctx, filter, nil)
		if err != nil {
			t.Fatalf("Nibs(%+v): %v", filter, err)
		}
		return nibIDs(got)
	}},
	{"graphql nibs(siblingId)", true, func(t *testing.T, q parentQuery) []string {
		id := q.representative
		got, err := q.resolver.Query().Nibs(q.ctx, &model.NibFilter{SiblingID: &id}, nil)
		if err != nil {
			t.Fatalf("Nibs(siblingId=%q): %v", id, err)
		}
		return append(nibIDs(got), id)
	}},
	{"graphql nib.children", false, func(t *testing.T, q parentQuery) []string {
		got, err := q.resolver.Nib().Children(q.ctx, mustGet(t, q.core, q.groupID), nil, nil)
		if err != nil {
			t.Fatalf("Children(%q): %v", q.groupID, err)
		}
		return nibIDs(got)
	}},
	{"cli rel --rel children", false, func(t *testing.T, q parentQuery) []string {
		got, err := fetchRel(q.ctx, q.resolver, mustGet(t, q.core, q.groupID), relChildren, nil, 1)
		if err != nil {
			t.Fatalf("fetchRel(children, %q): %v", q.groupID, err)
		}
		return nibIDs(got)
	}},
	{"cli rel --rel siblings", true, func(t *testing.T, q parentQuery) []string {
		got, err := fetchSiblings(q.ctx, q.resolver, mustGet(t, q.core, q.representative), nil)
		if err != nil {
			t.Fatalf("fetchSiblings(%q): %v", q.representative, err)
		}
		return append(nibIDs(got), q.representative)
	}},
	{"orderer parent scope", true, func(t *testing.T, q parentQuery) []string {
		return nibIDs(graph.NewOrderer(q.core, q.core).Members(graph.ScopeParent, q.groupID))
	}},
	{"membership view", true, func(t *testing.T, q parentQuery) []string {
		return nibIDs(membership.Compute(q.core.All()).Children(q.groupID))
	}},
	{"tree builder", true, func(t *testing.T, q parentQuery) []string {
		all := q.core.All()
		nodes := ui.BuildTree(all, all, nib.SortByOrder)
		if q.groupID != "" {
			nodes = childNodesOf(t, nodes, q.groupID)
		}
		ids := make([]*nib.Nib, 0, len(nodes))
		for _, n := range nodes {
			ids = append(ids, n.Nib)
		}
		return nibIDs(ids)
	}},
}

// childNodesOf finds the node for id anywhere in the tree and returns its
// children, failing when the tree holds no such node — a nib the tree dropped
// would otherwise read as a node with no children.
func childNodesOf(t *testing.T, nodes []*ui.TreeNode, id string) []*ui.TreeNode {
	t.Helper()
	found, ok := findChildNodes(nodes, id)
	if !ok {
		t.Fatalf("the tree holds no node for %q", id)
	}
	return found
}

// findChildNodes returns id's children and whether the node was found at all,
// which a nil slice cannot distinguish from a childless node.
func findChildNodes(nodes []*ui.TreeNode, id string) ([]*ui.TreeNode, bool) {
	for _, n := range nodes {
		if n.Nib.ID == id {
			return n.Children, true
		}
		if found, ok := findChildNodes(n.Children, id); ok {
			return found, true
		}
	}
	return nil, false
}

// TestParentGroupSurfacesAgreeOnMembership drives every group-enumerating
// surface over the same fixture. The root group is asked from BOTH a genuine
// root and a dangling-parent nib: siblingship is a symmetric relation, and the
// asymmetry nibs-oa4m found — the dangling nib saw every root while no root saw
// it back — is invisible to a test that only ever asks in one direction.
func TestParentGroupSurfacesAgreeOnMembership(t *testing.T) {
	resolver, core := setupParentAgreement(t)
	ctx := context.Background()

	roots := []string{"nibs-rt1", "nibs-rt2", "nibs-par", "nibs-dg1", "nibs-dg2"}
	groups := []struct {
		groupID        string
		representative string
		want           []string
	}{
		{"", "nibs-rt1", roots},
		{"", "nibs-dg1", roots},
		{"", "nibs-dg2", roots},
		{"nibs-par", "nibs-ful", []string{"nibs-ful", "nibs-sho"}},
		{"nibs-par", "nibs-sho", []string{"nibs-ful", "nibs-sho"}},
	}

	for _, group := range groups {
		q := parentQuery{
			ctx:            ctx,
			resolver:       resolver,
			core:           core,
			groupID:        group.groupID,
			representative: group.representative,
		}
		label := group.groupID
		if label == "" {
			label = "roots"
		}
		for _, surface := range parentGroupSurfaces {
			if q.groupID == "" && !surface.rootGroup {
				continue
			}
			t.Run(label+" from "+group.representative+"/"+surface.name, func(t *testing.T) {
				got := surface.members(t, q)
				sort.Strings(got)
				want := append([]string(nil), group.want...)
				sort.Strings(want)
				if len(got) != len(want) {
					t.Errorf("%s enumerates %v, want %v", surface.name, got, want)
					return
				}
				for i := range got {
					if got[i] != want[i] {
						t.Errorf("%s enumerates %v, want %v", surface.name, got, want)
						return
					}
				}
			})
		}
	}

	// The projected child count is the same question asked for a number rather
	// than a set, and it reaches the membership view by a different route than
	// the surface above.
	projected := resolver.ProjectionResolver(ctx)
	if got := projected.ChildCount("nibs-par"); got != 2 {
		t.Errorf("cli -f children reports %d children of nibs-par, want 2", got)
	}
	// A dangling link names no nib, so nothing is a child of one either — the
	// count must not follow the stored spelling to a phantom parent. nibs-ghost
	// IS that phantom, and asking it is what discriminates: both dangling nibs
	// name it, so a children map keyed on the stored spelling buckets them there
	// and this reads 2. The two dangling nibs are asked as well, for the
	// ordinary property that neither has children of its own.
	for _, id := range []string{"nibs-ghost", "nibs-dg1", "nibs-dg2"} {
		if got := projected.ChildCount(id); got != 0 {
			t.Errorf("cli -f children reports %d children of %s, want 0", got, id)
		}
	}
}

// TestStoredParentLinkStaysInspectable is the counterweight. Every surface
// above reports a dangling link as no parent, which is what makes the broken
// link invisible there — so the two inspection surfaces must keep reporting it
// verbatim, or a hand-edited store becomes undiagnosable.
func TestStoredParentLinkStaysInspectable(t *testing.T) {
	resolver, core := setupParentAgreement(t)
	q := parentQuery{ctx: context.Background(), resolver: resolver, core: core}

	dangling := mustGet(t, core, "nibs-dg1")
	got, err := resolver.Nib().StoredParentID(q.ctx, dangling)
	if err != nil {
		t.Fatalf("StoredParentID(nibs-dg1): %v", err)
	}
	if got == nil || *got != "nibs-ghost" {
		t.Errorf("graphql storedParentId = %v, want %q", got, "nibs-ghost")
	}
	if value := projectedField(t, q, dangling, "stored_parent"); value != "nibs-ghost" {
		t.Errorf("cli -f stored_parent = %v, want %q", value, "nibs-ghost")
	}

	// A nib with no link at all reports nothing, so "" is not overloaded to
	// mean both "no link" and "a link naming no nib".
	root, err := resolver.Nib().StoredParentID(q.ctx, mustGet(t, core, "nibs-rt1"))
	if err != nil {
		t.Fatalf("StoredParentID(nibs-rt1): %v", err)
	}
	if root != nil {
		t.Errorf("graphql storedParentId of a root = %q, want nil", *root)
	}
}

// TestRootBulkReorderAcceptsEveryRoot is the composition check the two tables
// cannot make. reorderChildren pre-validates its input against two surfaces —
// membership, which asks each listed nib for its parent, and completeness,
// which reads the group out of the ordering surface — and a caller has no legal
// input for a group the two disagree about: the nib one of them excludes is
// refused when listed and reported missing when omitted.
func TestRootBulkReorderAcceptsEveryRoot(t *testing.T) {
	resolver, core := setupParentAgreement(t)
	ctx := context.Background()

	roots := []string{"nibs-dg2", "nibs-rt1", "nibs-dg1", "nibs-par", "nibs-rt2"}
	got, err := resolver.Mutation().ReorderChildren(ctx, "", roots, nil)
	if err != nil {
		t.Fatalf("reorderChildren over the root group: %v", err)
	}
	assertSameIDs(t, "reorderChildren", got, roots)

	// The order actually took, so the surfaces agreed about the group rather
	// than about the refusal.
	assertOrderedIDs(t, "orderer root members after the reorder",
		graph.NewOrderer(core, core).Members(graph.ScopeParent, ""), roots)
}

// assertOrderedIDs fails unless got holds exactly want, in that order.
func assertOrderedIDs(t *testing.T, what string, got []*nib.Nib, want []string) {
	t.Helper()
	ids := make([]string, 0, len(got))
	for _, b := range got {
		ids = append(ids, b.ID)
	}
	if len(ids) != len(want) {
		t.Errorf("%s = %v, want %v", what, ids, want)
		return
	}
	for i := range ids {
		if ids[i] != want[i] {
			t.Errorf("%s = %v, want %v", what, ids, want)
			return
		}
	}
}

// sortedKeys returns m's keys in a stable order, so subtests are named the same
// way on every run.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
