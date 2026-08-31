package membershipcontract

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
)

// TestGeneratedMembershipContractIsFresh is the Go half of the parity pin: the
// committed contract must be byte-identical to what this package renders now.
// It is what turns a rule change that MOVES A FIXTURE ROW'S ANSWER into a
// failing test even when nobody looks at the web; a rule change that moves no
// row renders the same bytes and passes here (see the package comment).
func TestGeneratedMembershipContractIsFresh(t *testing.T) {
	committed, err := os.ReadFile(filepath.Join(moduleRoot(t), filepath.FromSlash(OutputPath)))
	if err != nil {
		t.Fatalf("reading the committed %s: %v — run `task codegen` and commit the result", OutputPath, err)
	}
	if string(committed) != Render() {
		t.Fatalf("%s is stale: the rule, the fixture or the renderer changed without regenerating it — run `task codegen` and commit the result. If the RULE changed, also check that web/src/lib/membership.ts still agrees and that a fixture row exercises the change.", OutputPath)
	}
}

// TestContractFixtureDiscriminatesEachRuleDecision proves the fixture can tell
// the rule from a broken copy of it. A contract whose rows all resolve the same
// way would still be byte-pinned and still be replayed by the web, and would
// still pass while either side dropped a clause — the failure mode a parity test
// is bought to prevent.
//
// Each mutant below undoes exactly one decision ResolvedMilestoneID makes: its
// three clauses, plus its choice to answer with the target's id rather than the
// stored string. The requirement is that its answer differs from the real
// rule's on the NAMED witness row, which is the same thing as "a TS mirror
// missing this decision fails membership.test.ts on that row".
//
// The witness is pinned rather than searched for. Accepting any differing row
// would let a fixture edit that drops the row a decision actually turns on pass
// on an unrelated row instead, silently; naming it makes such an edit fail by
// name.
//
// The list is a hand-written copy of the rule, not a derivation of it: a new
// decision does not appear here on its own. TestResolvedMilestoneIDShapeIsPinned
// is what makes forgetting to add one loud.
func TestContractFixtureDiscriminatesEachRuleDecision(t *testing.T) {
	f := fixture()
	lookup := fixtureLookup(f)
	byID := make(map[string]*nib.Nib, len(f))
	for _, r := range f {
		byID[r.nib.ID] = r.nib
	}

	mutants := []struct {
		decision string
		witness  string
		fn       func(*nib.Nib, membership.Lookup) string
	}{
		{
			decision: "the subject is not itself a milestone",
			witness:  "m2",
			fn: func(b *nib.Nib, lookup membership.Lookup) string {
				if b.Milestone == "" {
					return ""
				}
				target := lookup(b.Milestone)
				if target == nil || target.EffectiveType() != "milestone" {
					return ""
				}
				return target.ID
			},
		},
		{
			decision: "the target exists",
			witness:  "t3",
			fn: func(b *nib.Nib, lookup membership.Lookup) string {
				if b.Milestone == "" || b.EffectiveType() == "milestone" {
					return ""
				}
				target := lookup(b.Milestone)
				if target == nil {
					// Trusting the stored id is what "no existence check" means.
					return b.Milestone
				}
				if target.EffectiveType() != "milestone" {
					return ""
				}
				return target.ID
			},
		},
		{
			decision: "the target is milestone-typed",
			witness:  "t4",
			fn: func(b *nib.Nib, lookup membership.Lookup) string {
				if b.Milestone == "" || b.EffectiveType() == "milestone" {
					return ""
				}
				target := lookup(b.Milestone)
				if target == nil {
					return ""
				}
				return target.ID
			},
		},
		{
			decision: "the answer is the target's id, not the stored string",
			witness:  "t6",
			fn: func(b *nib.Nib, lookup membership.Lookup) string {
				if b.Milestone == "" || b.EffectiveType() == "milestone" {
					return ""
				}
				target := lookup(b.Milestone)
				if target == nil || target.EffectiveType() != "milestone" {
					return ""
				}
				return b.Milestone
			},
		},
	}

	for _, m := range mutants {
		t.Run(m.decision, func(t *testing.T) {
			b := byID[m.witness]
			if b == nil {
				t.Fatalf("the fixture no longer carries %q, the row this decision turns on — restore it or re-witness the mutant on the row that replaces it", m.witness)
			}
			want := membership.ResolvedMilestoneID(b, lookup)
			if got := m.fn(b, lookup); got == want {
				t.Errorf("fixture row %s no longer distinguishes the rule from one dropping %q — both answer %q, so the contract would pass with that decision missing on either side", m.witness, m.decision, want)
			}
		})
	}
}

// nonTerminating is the answer the cycle mutant below gives when its walk runs
// past every nib in the fixture. The real rule can only return an id or "", so
// no row can produce it by accident.
const nonTerminating = "<did not terminate>"

// TestContractFixtureDiscriminatesEachMilestoneOfDecision is the derived rule's
// half of the discrimination proof: the fixture must be able to tell
// (*membership.View).MilestoneOf from a broken copy of its walk.
//
// The mutants are written over a plain map rather than a View, because a View's
// index is unexported and this package is outside membership. That map is the
// View's own lookup shape — exact ids, no canonicalization — which is why the
// fixture's aliases play no part here and why the generated milestoneOf column
// is computed the same way.
//
// Four of the walk's five decisions are here. The fifth — that an id naming no
// nib is in the backlog — has no mutant, because it has no alternative ANSWER:
// removing the nil guard makes the walk panic rather than decide differently, so
// no fixture row can distinguish it by comparing answers. It is asserted
// directly below instead, and it has no TS counterpart at all: the mirror takes
// a subject, so it never performs that lookup.
//
// Each mutant names the fixture row it is discriminated by, for the reason
// TestContractFixtureDiscriminatesEachRuleDecision states: a searched-for
// witness lets a fixture edit that drops the row a decision turns on pass on an
// unrelated row instead.
func TestContractFixtureDiscriminatesEachMilestoneOfDecision(t *testing.T) {
	f := fixture()
	all := fixtureNibs(f)
	view := membership.Compute(all)

	byID := make(map[string]*nib.Nib, len(all))
	for _, b := range all {
		byID[b.ID] = b
	}
	lookup := membership.Lookup(func(id string) *nib.Nib { return byID[id] })

	if got := view.MilestoneOf("no-such-nib"); got != "" {
		t.Errorf("MilestoneOf over an id naming no fixture nib = %q, want \"\"", got)
	}

	mutants := []struct {
		decision string
		witness  string
		fn       func(string) string
	}{
		{
			decision: "the walk climbs TRANSITIVELY, not one level",
			witness:  "t10",
			fn: func(id string) string {
				b := byID[id]
				if b == nil || b.EffectiveType() == "milestone" {
					return ""
				}
				// Decision undone: the subject's own step, then one step up the
				// parent chain, and no further. Every other parented fixture row
				// answers within that budget, which is why t10 exists.
				for hops := 0; b != nil && hops < 2; hops++ {
					if b.EffectiveType() == "milestone" {
						return ""
					}
					if ms := membership.ResolvedMilestoneID(b, lookup); ms != "" {
						return ms
					}
					if b.Parent == "" {
						return ""
					}
					b = byID[b.Parent]
				}
				return ""
			},
		},
		{
			decision: "the walk stops at a milestone-typed ancestor",
			witness:  "t8",
			fn: func(id string) string {
				b := byID[id]
				if b == nil || b.EffectiveType() == "milestone" {
					return ""
				}
				visited := map[string]bool{}
				for b != nil && !visited[b.ID] {
					visited[b.ID] = true
					// Decision undone: nothing stops the climb at a milestone.
					if ms := membership.ResolvedMilestoneID(b, lookup); ms != "" {
						return ms
					}
					if b.Parent == "" {
						return ""
					}
					b = byID[b.Parent]
				}
				return ""
			},
		},
		{
			decision: "the walk stops at the FIRST resolved assignment",
			witness:  "t7",
			fn: func(id string) string {
				b := byID[id]
				if b == nil || b.EffectiveType() == "milestone" {
					return ""
				}
				visited := map[string]bool{}
				last := ""
				for b != nil && !visited[b.ID] {
					visited[b.ID] = true
					if b.EffectiveType() == "milestone" {
						return ""
					}
					if ms := membership.ResolvedMilestoneID(b, lookup); ms != "" {
						// Decision undone: keep climbing, and let the outermost
						// assignment win instead of the nearest.
						last = ms
					}
					if b.Parent == "" {
						return last
					}
					b = byID[b.Parent]
				}
				return last
			},
		},
		{
			decision: "the walk terminates on a parent cycle",
			witness:  "c1",
			fn: func(id string) string {
				b := byID[id]
				if b == nil || b.EffectiveType() == "milestone" {
					return ""
				}
				// Decision undone: no visited set. A terminating walk visits at
				// most every nib once, so exceeding that count IS the cycle —
				// the budget only makes the non-termination observable instead
				// of hanging this test.
				for steps := 0; b != nil; steps++ {
					if steps > len(all) {
						return nonTerminating
					}
					if b.EffectiveType() == "milestone" {
						return ""
					}
					if ms := membership.ResolvedMilestoneID(b, lookup); ms != "" {
						return ms
					}
					if b.Parent == "" {
						return ""
					}
					b = byID[b.Parent]
				}
				return ""
			},
		},
	}

	for _, m := range mutants {
		t.Run(m.decision, func(t *testing.T) {
			if byID[m.witness] == nil {
				t.Fatalf("the fixture no longer carries %q, the row this decision turns on — restore it or re-witness the mutant on the row that replaces it", m.witness)
			}
			want := view.MilestoneOf(m.witness)
			if got := m.fn(m.witness); got == want {
				t.Errorf("fixture row %s no longer distinguishes MilestoneOf from a walk dropping %q — both answer %q, so the contract would pass with that decision missing on either side", m.witness, m.decision, want)
			}
		})
	}
}

// wireResolverFile holds gqlgen's field resolvers. Resolver BODIES survive
// regeneration (see the header of internal/graph/resolver.go), so reading this
// file is reading the wire's real behavior, not a scratch copy of it.
const wireResolverFile = "internal/graph/schema.resolvers.go"

// TestRenderedTypeIsTheWireType pins the generated file's projection to what the
// wire reports, and pins that to the resolver rather than to prose. Two halves,
// and the contract's whole foundation — that the rows are the client's view of
// the store — needs both:
//
//   - The renderer's own projection is recomputed here on separately built nibs
//     rather than re-read out of rows(), so rendering the STORED `type` is
//     caught. That mutation otherwise regenerates cleanly and replays cleanly
//     (the mirror reads the same wrong column), leaving the typeless row
//     carrying `type: ""`, a value the Nib.type resolver can never emit. The
//     same holds for the STORED `parent:`, which is why the fixture is required
//     below to keep a row whose link names no nib: without one, the raw and
//     resolved readings are the same string everywhere and this half of the
//     comparison cannot tell them apart.
//   - internal/graph's Nib.type and Nib.parentId resolvers are read out of the
//     AST and must still answer through EffectiveType() and resolvedParentID(),
//     and `milestone` must still have no resolver at all. Without this half both
//     sides of the comparison above hardcode the same assumption about the wire
//     and nothing checks it: the resolver could switch to obj.Type and every
//     test in this package would stay green while the generated header's stated
//     foundation became false.
func TestRenderedTypeIsTheWireType(t *testing.T) {
	assertWireProjection(t)

	f := fixture()
	byID := make(map[string]*nib.Nib, len(f))
	for _, r := range f {
		byID[r.nib.ID] = r.nib
	}
	got := rows()
	if len(got) != len(f) {
		t.Fatalf("rows() produced %d rows for %d fixture nibs", len(got), len(f))
	}
	differed := false
	for i, r := range got {
		src := f[i].nib
		if want := src.EffectiveType(); r.Type != want {
			t.Errorf("row %s renders type %q; the wire reports %q", r.ID, r.Type, want)
		}
		if r.Milestone != src.Milestone {
			t.Errorf("row %s renders milestone %q; the wire reports the stored %q verbatim", r.ID, r.Milestone, src.Milestone)
		}
		// The resolved reading, recomputed here rather than read back out of
		// rows(): the stored link when it names a fixture nib, nothing when it
		// does not. Rendering the STORED link instead would leave t9 carrying a
		// parent id no row has.
		want := ""
		if p := byID[src.Parent]; p != nil {
			want = p.ID
		}
		if r.ParentID != want {
			t.Errorf("row %s renders parentId %q; the wire reports the resolved %q", r.ID, r.ParentID, want)
		}
		if src.Parent != want {
			differed = true
		}
		if r.ID != src.ID {
			t.Errorf("row %d renders id %q, want %q", i, r.ID, src.ID)
		}
	}
	if !differed {
		t.Error("no fixture row's stored parent link differs from its resolved reading, so the parentId comparison above cannot tell the two apart — restore a row with a parent naming no nib")
	}
}

// assertWireProjection reads the Nib field resolvers and holds the three facts
// the generated header states: `type` is resolved through EffectiveType,
// `parentId` through resolvedParentID, and `milestone` has no resolver, so the
// wire reports it verbatim.
func assertWireProjection(t *testing.T) {
	t.Helper()
	path := filepath.Join(moduleRoot(t), filepath.FromSlash(wireResolverFile))
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v — if the resolvers moved, update this guard rather than deleting it", wireResolverFile, err)
	}

	resolvers := map[string]*ast.FuncDecl{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Recv == nil || len(fn.Recv.List) == 0 || fn.Body == nil {
			continue
		}
		if receiverTypeName(fn.Recv.List[0].Type) != "nibResolver" {
			continue
		}
		resolvers[fn.Name.Name] = fn
	}

	// Each projected column and the call its resolver must still make. `type`
	// answers through a method on the nib, `parentId` through a package-level
	// helper, so the two are counted by different node kinds.
	for _, want := range []struct {
		column, resolver, callee string
		count                    func(ast.Node, string) int
	}{
		{"type", "Type", "EffectiveType", countSelector},
		{"parentId", "ParentID", "resolvedParentID", countCall},
	} {
		fn := resolvers[want.resolver]
		if fn == nil {
			t.Fatalf("no (*nibResolver).%s in %s — the wire's `%s` is not resolved where this guard looks, so nothing here knows what the contract's `%s` column should hold", want.resolver, wireResolverFile, want.column, want.column)
		}
		if n := want.count(fn.Body, want.callee); n != 1 {
			t.Errorf("(*nibResolver).%s in %s calls %s %d time(s), want 1 — the wire no longer reports that reading, so the contract's `%s` column and rows() are projecting a field the client never sees", want.resolver, wireResolverFile, want.callee, n, want.column)
		}
	}
	if resolvers["Milestone"] != nil {
		t.Errorf("%s now declares a (*nibResolver).Milestone resolver — `milestone` is no longer autobound and reported verbatim, so the contract's `milestone` column is no longer the wire's", wireResolverFile)
	}
}

// countSelector reports how many `X.name` selector expressions appear inside n.
func countSelector(n ast.Node, name string) int {
	count := 0
	ast.Inspect(n, func(node ast.Node) bool {
		if sel, ok := node.(*ast.SelectorExpr); ok && sel.Sel != nil && sel.Sel.Name == name {
			count++
		}
		return true
	})
	return count
}

// countCall reports how many calls to the package-level function name appear
// inside n.
func countCall(n ast.Node, name string) int {
	count := 0
	ast.Inspect(n, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, ok := call.Fun.(*ast.Ident); ok && id.Name == name {
			count++
		}
		return true
	})
	return count
}

// receiverTypeName renders a method receiver's type name, pointer stripped.
func receiverTypeName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.Ident:
		return e.Name
	case *ast.StarExpr:
		return receiverTypeName(e.X)
	case *ast.IndexExpr:
		return receiverTypeName(e.X)
	}
	return ""
}

// TestRuleIsComputableFromTheWireProjection asserts the rules read nothing the
// contract cannot ship. Every answer is recomputed over nibs rebuilt from the
// projected columns alone and must equal the answer over the full fixture nib,
// so a clause reading a field outside the projection reddens here instead of
// shipping a contract the web is structurally unable to satisfy.
//
// The boundary is exactly "does a fixture row VARY that field". The derived rule
// follows the parent link, so the contract carries that axis — as the RESOLVED
// parent, which is why a rule telling a dangling link apart from no parent
// reddens here anyway, on `t9`. `status:` is the field a clause could read for
// free, because no fixture row sets it. The bound is the fixture's, as everywhere
// else here: only the shape pins see a field read the fixture is blind to.
func TestRuleIsComputableFromTheWireProjection(t *testing.T) {
	got := rows()
	projected := make([]fixtureNib, 0, len(got))
	for _, r := range got {
		projected = append(projected, fixtureNib{
			nib:     &nib.Nib{ID: r.ID, Type: r.Type, Milestone: r.Milestone, Parent: r.ParentID},
			aliases: r.Aliases,
		})
	}
	lookup := fixtureLookup(projected)
	view := membership.Compute(fixtureNibs(projected))

	for i, r := range got {
		if want := membership.ResolvedMilestoneID(projected[i].nib, lookup); want != r.Resolved {
			t.Errorf("row %s answers %q over the full nib but %q over the fields the wire carries — the rule reads something the contract cannot ship, so no TS mirror can agree with it", r.ID, r.Resolved, want)
		}
		if want := view.MilestoneOf(r.ID); want != r.MilestoneOf {
			t.Errorf("row %s answers %q over the full nib but %q over the fields the wire carries — MilestoneOf reads something the contract cannot ship, so no TS mirror can agree with it", r.ID, r.MilestoneOf, want)
		}
	}
}

// pinnedRuleShape is the structural fingerprint of ResolvedMilestoneID's body.
// It exists because neither the fixture nor the mutant list above is derived
// from the rule: both are hand-written copies that a new clause does not reach.
// A fourth clause over the three fields the fixture already varies moves no
// row's answer, so it renders identical bytes, replays identically and passes
// every other test in this package and in the web suite.
//
// The fingerprint is deliberately structural rather than textual: it survives
// comment edits, gofmt and local renames, and moves only when the rule gains or
// loses a field read, a branch, a return or a boolean operator — which every
// added clause must do.
var pinnedRuleShape = ruleShape{
	selectors:  map[string]int{"Milestone": 2, "EffectiveType": 2, "ID": 1},
	ifs:        2,
	returns:    3,
	logicalOps: 2,
}

// pinnedMilestoneOfShape is the same fingerprint for the derived rule's walk.
// It is LARGER than the direct rule's because shapeOf follows the call into
// ResolvedMilestoneID, which lives in the same file — so this pin covers the
// walk plus the rule it delegates to, and an inlined copy of those three
// clauses moves it.
var pinnedMilestoneOfShape = ruleShape{
	selectors:  map[string]int{"byID": 2, "EffectiveType": 4, "ID": 3, "lookup": 1, "Parent": 2, "Milestone": 2},
	ifs:        6,
	returns:    8,
	logicalOps: 4,
}

// ruleShape counts the structural features of a function body.
type ruleShape struct {
	selectors  map[string]int
	ifs        int
	returns    int
	logicalOps int
}

func (s ruleShape) String() string {
	names := make([]string, 0, len(s.selectors))
	for name := range s.selectors {
		names = append(names, fmt.Sprintf("%s×%d", name, s.selectors[name]))
	}
	sort.Strings(names)
	return fmt.Sprintf("selectors{%s} ifs=%d returns=%d logicalOps=%d",
		strings.Join(names, " "), s.ifs, s.returns, s.logicalOps)
}

// TestResolvedMilestoneIDShapeIsPinned is the backstop for the direction the
// fixture cannot see. It reddens on any change to the rule's shape and its
// message states the obligation such a change carries — neither the fixture nor
// the mutant list follows automatically.
//
// Modeled on internal/membership/consumer_guard_test.go, which reads the same
// kind of evidence out of the AST rather than trusting prose. The residual it
// does not close: swapping one operator for another of the same kind leaves the
// fingerprint unchanged — one comparison for another, and `||` for `&&`, since
// shapeOf counts both connectives into logicalOps — so that is caught only if it
// moves a fixture row's answer.
func TestResolvedMilestoneIDShapeIsPinned(t *testing.T) {
	assertShapeIsPinned(t, "", "ResolvedMilestoneID", pinnedRuleShape,
		"TestContractFixtureDiscriminatesEachRuleDecision", "resolvedMilestoneId")
}

// TestMilestoneOfShapeIsPinned is the same backstop for the derived rule.
// Without it, the residual the package documents — a clause the fixture's
// answers do not move — would widen from three lines to a whole walk.
//
// MilestoneOf CALLS ResolvedMilestoneID, and shapeOf follows a plain call in
// the same file, so this fingerprint includes the direct rule's. That is the
// property worth having: it goes red if the walk stops delegating and inlines
// the three clauses instead, which is the drift the TS mirror is told to avoid
// for the same reason.
func TestMilestoneOfShapeIsPinned(t *testing.T) {
	assertShapeIsPinned(t, "View", "MilestoneOf", pinnedMilestoneOfShape,
		"TestContractFixtureDiscriminatesEachMilestoneOfDecision", "milestoneOf")
}

// assertShapeIsPinned holds one rule's body to its fingerprint. recv is the
// receiver type name, empty for a plain function.
func assertShapeIsPinned(t *testing.T, recv, name string, pinned ruleShape, mutantTest, mirror string) {
	t.Helper()
	const ruleFile = "internal/membership/membership.go"
	path := filepath.Join(moduleRoot(t), filepath.FromSlash(ruleFile))
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parsing %s: %v — if the rule moved, update this guard rather than deleting it", ruleFile, err)
	}

	var body *ast.BlockStmt
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name {
			continue
		}
		if recv == "" {
			if fn.Recv == nil {
				body = fn.Body
			}
		} else if fn.Recv != nil && len(fn.Recv.List) > 0 && receiverTypeName(fn.Recv.List[0].Type) == recv {
			body = fn.Body
		}
		if body != nil {
			break
		}
	}
	if body == nil {
		t.Fatalf("%s not found in %s — the guard cannot go quiet by losing its subject", name, ruleFile)
	}

	got := shapeOf(f, body)
	if got.String() != pinned.String() {
		t.Errorf("%s's shape changed:\n  got  %s\n  want %s\n"+
			"If the change is BEHAVIOR-PRESERVING — a split if, an extracted helper beside it, a rename — re-pin the fingerprint and stop; nothing else here applies.\n"+
			"If it adds or changes a DECISION, neither the fixture in contract.go nor the mutant list in %s follows it automatically: add a fixture row whose answer the change moves, add a mutant that undoes the new decision, mirror the change in %s (web/src/lib/membership.ts), run `task codegen`, and re-pin.\n"+
			"If the rule's body moved to another FILE, point this guard at that file: shapeOf follows a helper in the same file and nothing further, and TestPinnedRuleShapeIsNotDegenerate refuses the pin that a moved-out body would leave behind.",
			name, got, pinned, mutantTest, mirror)
	}
}

// TestPinnedRuleShapeIsNotDegenerate is the guard on the guard. Re-pinning is
// the one instruction above that always applies, so a pin that watches nothing
// has to be refused rather than accepted quietly — and the degenerate pin is
// reachable: move ResolvedMilestoneID's body into a helper in another file of
// package membership and what is left behind is `return impl(b, lookup)`, whose
// honest fingerprint is selectors{} ifs=0 returns=1 logicalOps=0. Every later
// clause would then live outside this guard's view. shapeOf follows a same-FILE
// helper, so that variant re-pins to a shape that still watches the rule; this
// floor is what makes the cross-file one loud instead of free.
//
// The direct rule's floor is that rule as it stands: three clauses over three
// distinct fields, 2 ifs joined by 2 boolean operators, 3 returns. The walk's
// floor is one MORE than that on every axis, which is what makes a walk whose
// pin sank to "delegate and return" refusable — shapeOf follows the delegation,
// so a body doing nothing else fingerprints as the direct rule and would
// otherwise look like a healthy pin. They are FLOORS, not second copies of the
// pins: a rule that legitimately grows stays above them.
func TestPinnedRuleShapeIsNotDegenerate(t *testing.T) {
	const advice = " — a pin this small watches nothing; if the rule moved, point the guard at where it moved to rather than pinning what it left behind"
	for _, c := range []struct {
		name                                        string
		got                                         ruleShape
		minSelectors, minIfs, minReturns, minLogOps int
	}{
		{"pinnedRuleShape", pinnedRuleShape, 3, 2, 3, 2},
		{"pinnedMilestoneOfShape", pinnedMilestoneOfShape, 4, 3, 4, 3},
	} {
		if n := len(c.got.selectors); n < c.minSelectors {
			t.Errorf("%s names %d distinct field reads, want at least %d%s", c.name, n, c.minSelectors, advice)
		}
		if c.got.ifs < c.minIfs {
			t.Errorf("%s has ifs=%d, want at least %d%s", c.name, c.got.ifs, c.minIfs, advice)
		}
		if c.got.returns < c.minReturns {
			t.Errorf("%s has returns=%d, want at least %d%s", c.name, c.got.returns, c.minReturns, advice)
		}
		if c.got.logicalOps < c.minLogOps {
			t.Errorf("%s has logicalOps=%d, want at least %d%s", c.name, c.got.logicalOps, c.minLogOps, advice)
		}
	}
}

// shapeOf counts the structural features of a function body: how often each
// field or method name is selected, and how many branches, returns and boolean
// operators the body carries.
//
// A call to a plain function declared in the SAME FILE is followed into that
// function's body, so extracting the rule into a helper beside it keeps the
// rule in view instead of leaving a fingerprint that watches the call. Calls
// through a value (the Lookup parameter) and method calls are not followed —
// there is nothing lexical to follow — and the visited set stops recursion.
func shapeOf(file *ast.File, body *ast.BlockStmt) ruleShape {
	sameFile := map[string]*ast.FuncDecl{}
	for _, decl := range file.Decls {
		if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Body != nil {
			sameFile[fn.Name.Name] = fn
		}
	}

	s := ruleShape{selectors: map[string]int{}}
	visited := map[string]bool{}
	var walk func(ast.Node)
	walk = func(root ast.Node) {
		ast.Inspect(root, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.SelectorExpr:
				if n.Sel != nil {
					s.selectors[n.Sel.Name]++
				}
			case *ast.IfStmt:
				s.ifs++
			case *ast.ReturnStmt:
				s.returns++
			case *ast.BinaryExpr:
				if n.Op == token.LAND || n.Op == token.LOR {
					s.logicalOps++
				}
			case *ast.CallExpr:
				if id, ok := n.Fun.(*ast.Ident); ok && !visited[id.Name] {
					if fn, ok := sameFile[id.Name]; ok {
						visited[id.Name] = true
						walk(fn.Body)
					}
				}
			}
			return true
		})
	}
	walk(body)
	return s
}

// moduleRoot walks up from the package directory to the directory holding
// go.mod, so the generated file's path does not depend on test nesting depth.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod in any parent of the working directory")
		}
		dir = parent
	}
}
