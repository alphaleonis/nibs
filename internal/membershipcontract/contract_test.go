package membershipcontract

import (
	"os"
	"path/filepath"
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
// decision does not appear here on its own, and nothing makes forgetting to add
// one loud — see the package comment for the obligation a new decision carries.
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

// TestRenderedTypeIsTheWireType pins the generated file's projection to what the
// wire reports. The renderer's own projection is recomputed here on separately
// built nibs rather than re-read out of rows(), so rendering the STORED `type` is
// caught: that mutation otherwise regenerates cleanly and replays cleanly (the
// mirror reads the same wrong column), leaving the typeless row carrying
// `type: ""`, a value the Nib.type resolver can never emit. The same holds for
// the STORED `parent:`, which is why the fixture is required below to keep a row
// whose link names no nib — without one, the raw and resolved readings are the
// same string everywhere and this comparison cannot tell them apart.
//
// What the comparison assumes is the wire's own projection: that internal/graph
// answers `type` through EffectiveType, `parentId` through resolvedParentID, and
// `milestone` verbatim. Both sides here hold that assumption, so a resolver that
// changed its reading would leave this green — the generated header states the
// assumption, and a change to a Nib field resolver is where to re-read it.
func TestRenderedTypeIsTheWireType(t *testing.T) {
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
// else here: a field read the fixture is blind to is a field nothing here sees.
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
