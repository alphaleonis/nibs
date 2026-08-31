// Package membershipcontract renders the Go↔TS parity contract for
// membership.ResolvedMilestoneID — the rule deciding which milestone queue a
// nib is in — as a TypeScript module the web's test suite replays.
//
// The web has to apply that rule itself: `Nib.milestone` is reported VERBATIM
// on the wire (schema.graphqls, and the field is autobound with no resolver of
// its own), so a dangling or non-milestone assignment arrives at the client as
// written. A client that gets the rule wrong draws a row in a section whose
// queue it is not in, and then reorders it in a region the server does not
// agree it belongs to.
//
// Two implementations of one rule drift silently, so the fixture below is
// rendered together with the answers Go gives for it, and the pair is
// committed. What the tests hold:
//
//   - TestGeneratedMembershipContractIsFresh (here) reddens when the committed
//     bytes stop matching what the current rule, fixture and renderer produce.
//   - web/src/lib/membership.test.ts reddens when a regenerated answer stops
//     matching the TS mirror's — Go changed and was regenerated but
//     resolvedMilestoneId was not, or the mirror changed and Go did not.
//   - TestContractFixtureDiscriminatesEachRuleDecision (here) reddens when the
//     fixture stops being able to tell the rule from a mutant of it, so a
//     fixture edit cannot hollow the two above out.
//
// All three are BOUNDED BY FIXTURE COVERAGE: a change on either side that moves
// no fixture row's answer renders identical bytes and replays identically, so it
// fails nothing. Two tests here narrow that bound rather than remove it —
// TestResolvedMilestoneIDShapeIsPinned reddens on a change to the Go rule's
// field reads or clause count and states the obligation such a change carries (a
// fixture row exercising it, and a mutant for it), and
// TestRuleIsComputableFromTheWireProjection reddens when the rule starts
// reading a field the wire projection cannot carry AND a fixture row varies it
// (`parent:` does, `status:` does not — only the shape pin sees the second).
//
// Both watch the GO side only, and that is where the deliberate residual is:
// a fourth clause over the three modeled fields, moving no fixture row's answer,
// still passes everything when it is added to the TS MIRROR. Closing it
// symmetrically would mean driving a TypeScript parser from Go. Added to the Go
// rule the same clause reddens the shape pin, which is why that test exists.
//
// TestRenderedTypeIsTheWireType holds the projection those tests answer over:
// `type` is the effective type, and the test reads the Nib.type resolver out of
// internal/graph rather than restating what it does.
//
// The rendering shape follows internal/webvocab: `task codegen` writes the file
// via go:generate, and the committed bytes are pinned by a test, so the web
// never depends on running Go. It differs from that sibling in the way that
// matters for keeping the mechanism armed: vocabulary.ts is imported by app
// code, so deleting its consumer breaks the build, while this module is
// imported only by a test. TestWebImportsTheContract is what makes deleting
// that test loud instead of free.
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

// fixtureNib is one authored fixture row: a nib, the other ids the fixture's
// lookup resolves to it, and why the row is in the fixture.
type fixtureNib struct {
	nib     *nib.Nib
	aliases []string
	note    string
}

// fixture is the parity fixture, authored as stored nibs. It carries the three
// degenerate cases the rule has clauses for — a milestone-typed subject, an
// assignment naming no nib, an assignment naming a non-milestone — alongside
// assignments that do resolve, so the contract discriminates rather than merely
// agreeing on "". TestContractFixtureDiscriminatesEachRuleDecision proves it
// does, by running a mutant of the rule with each decision undone and requiring
// the answers to differ.
//
// The `note` strings number the clauses in ResolvedMilestoneID's own order:
// 1 the subject is not itself a milestone, 2 the target exists, 3 the target is
// milestone-typed. They ship verbatim into the generated module, where they are
// the only clause legend either side carries.
//
// Parent links earn their place on ONE row: t2, whose answer an ancestor walk
// would move from "" to "m1". They are not checkable on the TS side at all and
// do not need to be — MembershipNib has no parent field, so a mirror walking
// ancestors does not compile, which is stronger than a fixture row.
//
// aliases model a CANONICALIZING lookup: nibcore.Core.Get tries the id, then
// the configured prefix prepended, so a stored `milestone: abc` resolves to the
// nib `nibs-abc`. The two store-backed callers build their Lookup on that Get
// (graph/orderer.go via NibReader, cmd/close.go via Resolver.Reader); the two
// in-memory ones (membership's own View, nibcore/link_health.go) are plain
// maps.
//
// One row (t6, answering nibs-m4 for a stored `m4`) is the only one where the
// target's id and the stored string differ, so it is the whole of what lets the
// contract tell "return target.ID" from "return b.Milestone" — the fourth
// mutant in TestContractFixtureDiscriminatesEachRuleDecision has no other row
// to fail on. It is NOT a divergence a store can present: nibcore canonicalizes
// every stored link id in memory on load (canonicalize.go — "every id stored in
// c.nibs is a FULL id"), so those two callers are handed a Milestone that
// already equals target.ID, and Core.Get's prefix fallback answers user-typed
// ids rather than stored link fields. link_health.go's
// closedMilestoneQueuesInMap says the same about the same divergence, as does
// TestClosedMilestoneQueueAgreesWithMembership. The row couples the two
// derivations so they cannot drift apart later; it models no defect anyone can
// hit today.
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
			note: "no assignment of its own; its parent e1 is assigned to m1 and confers nothing (no ancestor walk)",
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
			note: "the answer is the TARGET's id, not the stored string: `milestone: m4` resolves to nibs-m4",
		},
	}
}

// contractRow is the wire projection of one fixture nib, paired with the answer
// the rule gives for the FULL nib. It is the single place the generated file's
// columns are decided — the renderer has nothing else to print — so pinning it
// pins the file. See TestRenderedTypeIsTheWireType.
type contractRow struct {
	ID        string
	Type      string
	Milestone string
	Aliases   []string
	Resolved  string
	Note      string
}

// fixtureLookup builds the fixture's Lookup: exact id first, then the alias
// table, which is the shape of a canonicalizing store lookup.
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

// rows projects the fixture to what the wire reports and answers each row with
// the Go rule.
func rows() []contractRow {
	f := fixture()
	lookup := fixtureLookup(f)

	out := make([]contractRow, 0, len(f))
	for _, r := range f {
		out = append(out, contractRow{
			ID: r.nib.ID,
			// The EFFECTIVE type: what the Nib.type resolver reports
			// (internal/graph/schema.resolvers.go), not the stored field.
			Type:      r.nib.EffectiveType(),
			Milestone: r.nib.Milestone,
			Aliases:   r.aliases,
			Resolved:  membership.ResolvedMilestoneID(r.nib, lookup),
			Note:      r.note,
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
	b.WriteString("// The Go↔TS parity contract for the milestone-membership rule: a fixture of\n")
	b.WriteString("// nibs as the wire reports them, each paired with the milestone id Go's\n")
	b.WriteString("// membership.ResolvedMilestoneID resolved for it. membership.test.ts replays\n")
	b.WriteString("// the fixture through the TypeScript mirror and requires the same answers.\n")
	b.WriteString("//\n")
	b.WriteString("// `type` is the EFFECTIVE type, which is what the `Nib.type` resolver reports\n")
	b.WriteString("// for a stored nib omitting the field; `milestone` is verbatim, as the wire\n")
	b.WriteString("// gives it. So this is the client's view of the store the Go answers were\n")
	b.WriteString("// computed over, not the store itself.\n")
	b.WriteString("//\n")
	b.WriteString("// The clause numbers in `note` are the rule's own order: 1 the subject is not\n")
	b.WriteString("// itself a milestone, 2 the target exists, 3 the target is milestone-typed.\n\n")

	b.WriteString("/** One fixture nib and the answer Go gave for it. */\n")
	b.WriteString("export interface ContractCase {\n")
	b.WriteString("  readonly id: string;\n")
	b.WriteString("  readonly type: string;\n")
	b.WriteString("  readonly milestone: string;\n")
	b.WriteString("  /**\n")
	b.WriteString("   * Other ids the lookup Go answered over resolves to this row, so the replay\n")
	b.WriteString("   * can rebuild that lookup. A store lookup canonicalizes (nibcore Core.Get\n")
	b.WriteString("   * tries the id, then the configured prefix prepended); a lookup built from\n")
	b.WriteString("   * loaded rows on the client does not, so on the client this is always empty\n")
	b.WriteString("   * in practice. The contract carries it because the Go rule is shared with\n")
	b.WriteString("   * server-side callers whose lookup does canonicalize.\n")
	b.WriteString("   */\n")
	b.WriteString("  readonly aliases: readonly string[];\n")
	b.WriteString("  /** membership.ResolvedMilestoneID's answer, computed by Go at generation. */\n")
	b.WriteString("  readonly resolvedMilestoneId: string;\n")
	b.WriteString("  /** Which clause of the rule this row exercises. */\n")
	b.WriteString("  readonly note: string;\n")
	b.WriteString("}\n\n")

	b.WriteString("export const MEMBERSHIP_CONTRACT: readonly ContractCase[] = [\n")
	for _, r := range rows() {
		b.WriteString("  {\n")
		fmt.Fprintf(&b, "    id: %q,\n", r.ID)
		fmt.Fprintf(&b, "    type: %q,\n", r.Type)
		fmt.Fprintf(&b, "    milestone: %q,\n", r.Milestone)
		fmt.Fprintf(&b, "    aliases: [%s],\n", renderStrings(r.Aliases))
		fmt.Fprintf(&b, "    resolvedMilestoneId: %q,\n", r.Resolved)
		fmt.Fprintf(&b, "    note: %q,\n", r.Note)
		b.WriteString("  },\n")
	}
	b.WriteString("];\n")

	return b.String()
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
