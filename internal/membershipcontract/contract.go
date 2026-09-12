// Package membershipcontract renders the Go↔TS parity contract for the two
// milestone-membership rules, membership.ResolvedMilestoneID (direct
// assignment) and (*membership.View).MilestoneOf (derived, inherited up the
// parent chain), as a TypeScript module. web/src/lib/membership.test.ts replays
// it against the mirrors in web/src/lib/membership.ts.
//
// Rule changes are caught only through the fixture: one that moves no fixture
// row's answer renders and replays identically, so nothing fails. A new decision in
// either rule needs a fixture row whose answer it moves, a mutant in the
// discrimination tests that undoes it, a mirror in membership.ts, and
// `task codegen`.
package membershipcontract

//go:generate go run ./gen

import (
	"fmt"
	"strings"

	"github.com/alphaleonis/nibs/internal/membership"
	"github.com/alphaleonis/nibs/internal/nib"
)

// OutputPath is the module-root-relative path of the generated file.
const OutputPath = "web/src/lib/generated/membershipContract.ts"

type fixtureNib struct {
	nib     *nib.Nib
	aliases []string // other ids fixtureLookup resolves to this nib
	note    string   // why the row exists; shipped verbatim in the generated module
}

// fixture is the parity fixture. The discrimination tests name the row each
// decision turns on; keep those rows.
//
// Give aliases only to milestone rows. The parent column and MilestoneOf resolve
// by exact id, while the wire resolves a parent through Core.Get, which retries
// with the prefix, so a short-form `parent:` would resolve there and not here.
func fixture() []fixtureNib {
	return []fixtureNib{
		{
			nib:  &nib.Nib{ID: "m1", Type: "milestone", Title: "First wave"},
			note: "a milestone: a container of its own, never a member",
		},
		{
			nib:  &nib.Nib{ID: "m2", Type: "milestone", Title: "Nested wave", Milestone: "m1"},
			note: "clause 1: a milestone-typed subject, even carrying a resolvable assignment",
		},
		{
			nib:  &nib.Nib{ID: "m3", Type: "milestone", Title: "Self-assigned wave", Milestone: "m3"},
			note: "clause 1: a milestone assigned to itself",
		},
		{
			nib:  &nib.Nib{ID: "e1", Type: "epic", Title: "Assigned epic", Milestone: "m1"},
			note: "an ordinary resolving assignment",
		},
		{
			nib:  &nib.Nib{ID: "t1", Type: "task", Title: "Assigned task", Parent: "e1", Milestone: "m1"},
			note: "wire shape: a resolving assignment on a nib that also has a parent",
		},
		{
			nib:  &nib.Nib{ID: "t2", Type: "task", Title: "Unassigned task", Parent: "e1"},
			note: "the two rules part here: no assignment of its own, so the direct rule answers \"\" while the walk inherits its parent e1's m1",
		},
		{
			nib:  &nib.Nib{ID: "t10", Type: "task", Title: "Grandchild of an assigned epic", Parent: "t2"},
			note: "the walk is TRANSITIVE: neither t10 nor its parent t2 is assigned, so the answer comes from e1 two levels up",
		},
		{
			nib:  &nib.Nib{ID: "t3", Type: "task", Title: "Dangling assignment", Milestone: "ghost"},
			note: "clause 2: the assignment names no nib in the fixture",
		},
		{
			nib:  &nib.Nib{ID: "t4", Type: "task", Title: "Assigned to an epic", Milestone: "e1"},
			note: "clause 3: the target exists but is not milestone-typed",
		},
		{
			nib:  &nib.Nib{ID: "t5", Title: "Typeless task", Milestone: "m1"},
			note: "wire shape: a stored nib omitting `type:`, which the wire reports as the default",
		},
		{
			nib:     &nib.Nib{ID: "nibs-m4", Type: "milestone", Title: "Prefixed wave"},
			aliases: []string{"m4"},
			note:    "a milestone the lookup also answers for the short id \"m4\"",
		},
		{
			nib:  &nib.Nib{ID: "t6", Type: "task", Title: "Short-form assignment", Milestone: "m4"},
			note: "the answer is the TARGET's id, not the stored string: `milestone: m4` resolves to nibs-m4 for the direct rule, and for the walk only if the lookup canonicalizes",
		},
		{
			nib:  &nib.Nib{ID: "m6", Type: "milestone", Title: "Second wave"},
			note: "a second milestone, so a nib and its parent can be assigned to different ones",
		},
		{
			nib:  &nib.Nib{ID: "t7", Type: "task", Title: "Assigned under a differently assigned epic", Parent: "e1", Milestone: "m6"},
			note: "the walk stops at the FIRST resolved assignment: its own m6, never its parent e1's m1",
		},
		{
			nib:  &nib.Nib{ID: "m7", Type: "milestone", Title: "Wave nested under an epic", Parent: "e1"},
			note: "a milestone nested under an assigned epic: hand-edited decomposition data, and still a container of its own",
		},
		{
			nib:  &nib.Nib{ID: "t8", Type: "task", Title: "Task under a nested milestone", Parent: "m7"},
			note: "the walk stops at the milestone-typed ancestor m7 rather than climbing on to e1's m1",
		},
		{
			nib:  &nib.Nib{ID: "c1", Type: "task", Title: "Cycle member", Parent: "c2"},
			note: "a parent cycle with no assignment anywhere in it: the walk terminates on its visited set",
		},
		{
			nib:  &nib.Nib{ID: "c2", Type: "task", Title: "Cycle member", Parent: "c1"},
			note: "the other half of the parent cycle",
		},
		{
			nib:  &nib.Nib{ID: "t9", Type: "task", Title: "Dangling parent", Parent: "ghost"},
			note: "wire shape: a parent link naming no nib, which the wire reports as no parent at all",
		},
	}
}

// contractRow is the wire projection of one fixture nib, paired with the answers
// the two rules give for the full nib.
type contractRow struct {
	ID        string
	Type      string
	Milestone string
	// ParentID is the resolved parent, as Nib.parentId reports it: "" for no
	// parent and for a link naming no nib.
	ParentID    string
	Aliases     []string
	Resolved    string
	MilestoneOf string
	Note        string
}

// fixtureLookup resolves an exact id first, then an alias, modelling a
// canonicalizing store lookup.
func fixtureLookup(f []fixtureNib) membership.Lookup {
	byID := make(map[string]*nib.Nib, len(f))
	aliases := make(map[string]*nib.Nib)
	for _, r := range f {
		byID[r.nib.ID] = r.nib
		for _, a := range r.aliases {
			aliases[a] = r.nib
		}
	}
	return func(id string) *nib.Nib {
		if b, ok := byID[id]; ok {
			return b
		}
		return aliases[id]
	}
}

func fixtureNibs(f []fixtureNib) []*nib.Nib {
	out := make([]*nib.Nib, 0, len(f))
	for _, r := range f {
		out = append(out, r.nib)
	}
	return out
}

// resolvedParents maps each nib id to the id its `parent:` resolves to, absent
// for a root and for a link naming no nib. Read it out of the View; do not
// re-derive it from the stored links.
func resolvedParents(v *membership.View, all []*nib.Nib) map[string]string {
	out := make(map[string]string, len(all))
	for _, parent := range all {
		for _, child := range v.Children(parent.ID) {
			out[child.ID] = parent.ID
		}
	}
	return out
}

// rows projects the fixture to what the wire reports and answers each row with
// both rules. ResolvedMilestoneID uses the canonicalizing fixture lookup and
// MilestoneOf the View's exact-id index, so the two part on t6.
func rows() []contractRow {
	f := fixture()
	lookup := fixtureLookup(f)
	all := fixtureNibs(f)
	view := membership.Compute(all)
	parents := resolvedParents(view, all)

	out := make([]contractRow, 0, len(f))
	for _, r := range f {
		out = append(out, contractRow{
			ID:          r.nib.ID,
			Type:        r.nib.EffectiveType(), // what Nib.type reports, not the stored field
			Milestone:   r.nib.Milestone,
			ParentID:    parents[r.nib.ID],
			Aliases:     r.aliases,
			Resolved:    membership.ResolvedMilestoneID(r.nib, lookup),
			MilestoneOf: view.MilestoneOf(r.nib.ID),
			Note:        r.note,
		})
	}
	return out
}

// Render returns the full generated TypeScript module.
func Render() string {
	var b strings.Builder
	b.WriteString("// Code generated by internal/membershipcontract. DO NOT EDIT.\n")
	b.WriteString("// Regenerate with `task codegen`.\n")
	b.WriteString("//\n")
	b.WriteString("// The Go↔TS parity contract for the milestone-membership rules: a fixture of\n")
	b.WriteString("// nibs as the wire reports them, each paired with the milestone id Go's\n")
	b.WriteString("// membership.ResolvedMilestoneID (DIRECT assignment) and\n")
	b.WriteString("// (*membership.View).MilestoneOf (DERIVED membership, inherited up the parent\n")
	b.WriteString("// chain) resolved for it. membership.test.ts replays the fixture through the\n")
	b.WriteString("// TypeScript mirrors and requires the same answers.\n")
	b.WriteString("//\n")
	b.WriteString("// `type` is the EFFECTIVE type, which is what the `Nib.type` resolver reports\n")
	b.WriteString("// for a stored nib omitting the field; `parentId` is the RESOLVED parent, as\n")
	b.WriteString("// the `Nib.parentId` resolver reports it; `milestone` is verbatim, as the wire\n")
	b.WriteString("// gives it. So this is the client's view of the store the Go answers were\n")
	b.WriteString("// computed over, not the store itself.\n")
	b.WriteString("//\n")
	b.WriteString("// The two answer columns were computed over two different lookups, and the\n")
	b.WriteString("// replay has to match each: `resolvedMilestoneId` over a CANONICALIZING one\n")
	b.WriteString("// (the `aliases` below), `milestoneOf` over exact ids, because it is a method\n")
	b.WriteString("// on a View and a View indexes its slice by id alone. t6 is where the two\n")
	b.WriteString("// part.\n")
	b.WriteString("//\n")
	b.WriteString("// The clause numbers in `note` are the direct rule's own order: 1 the subject\n")
	b.WriteString("// is not itself a milestone, 2 the target exists, 3 the target is\n")
	b.WriteString("// milestone-typed.\n\n")

	b.WriteString("/** One fixture nib and the answers Go gave for it. */\n")
	b.WriteString("export interface ContractCase {\n")
	b.WriteString("  readonly id: string;\n")
	b.WriteString("  readonly type: string;\n")
	b.WriteString("  readonly milestone: string;\n")
	b.WriteString("  /** The resolved parent: null for a root AND for a link naming no nib. */\n")
	b.WriteString("  readonly parentId: string | null;\n")
	b.WriteString("  /**\n")
	b.WriteString("   * Other ids the lookup Go answered over resolves to this row, so the replay\n")
	b.WriteString("   * can rebuild that lookup. A store lookup canonicalizes (nibcore Core.Get\n")
	b.WriteString("   * tries the id, then the configured prefix prepended); a lookup built from\n")
	b.WriteString("   * loaded rows on the client does not, so on the client this is always empty\n")
	b.WriteString("   * in practice. The contract carries it because the Go rule is shared with\n")
	b.WriteString("   * server-side callers whose lookup does canonicalize. It applies to\n")
	b.WriteString("   * `resolvedMilestoneId` only — see the header.\n")
	b.WriteString("   */\n")
	b.WriteString("  readonly aliases: readonly string[];\n")
	b.WriteString("  /** membership.ResolvedMilestoneID's answer, computed by Go at generation. */\n")
	b.WriteString("  readonly resolvedMilestoneId: string;\n")
	b.WriteString("  /** (*membership.View).MilestoneOf's answer, computed by Go at generation. */\n")
	b.WriteString("  readonly milestoneOf: string;\n")
	b.WriteString("  /** Which decision of which rule this row exercises. */\n")
	b.WriteString("  readonly note: string;\n")
	b.WriteString("}\n\n")

	b.WriteString("export const MEMBERSHIP_CONTRACT: readonly ContractCase[] = [\n")
	for _, r := range rows() {
		b.WriteString("  {\n")
		fmt.Fprintf(&b, "    id: %q,\n", r.ID)
		fmt.Fprintf(&b, "    type: %q,\n", r.Type)
		fmt.Fprintf(&b, "    milestone: %q,\n", r.Milestone)
		fmt.Fprintf(&b, "    parentId: %s,\n", renderNullableString(r.ParentID))
		fmt.Fprintf(&b, "    aliases: [%s],\n", renderStrings(r.Aliases))
		fmt.Fprintf(&b, "    resolvedMilestoneId: %q,\n", r.Resolved)
		fmt.Fprintf(&b, "    milestoneOf: %q,\n", r.MilestoneOf)
		fmt.Fprintf(&b, "    note: %q,\n", r.Note)
		b.WriteString("  },\n")
	}
	b.WriteString("];\n")

	return b.String()
}

// renderNullableString renders "" as the wire's null and anything else as a
// quoted string.
func renderNullableString(v string) string {
	if v == "" {
		return "null"
	}
	return fmt.Sprintf("%q", v)
}

// renderStrings renders a string slice as the inside of a TypeScript array
// literal.
func renderStrings(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, v := range values {
		quoted = append(quoted, fmt.Sprintf("%q", v))
	}
	return strings.Join(quoted, ", ")
}
