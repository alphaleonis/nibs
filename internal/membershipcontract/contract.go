// Package membershipcontract renders the Go↔TS parity contract for the two
// rules deciding which milestone queue a nib is in — membership.ResolvedMilestoneID
// (DIRECT assignment) and (*membership.View).MilestoneOf (DERIVED membership,
// which inherits up the structural parent chain) — as a TypeScript module the
// web's test suite replays.
//
// The web has to apply those rules itself: `Nib.milestone` is reported VERBATIM
// on the wire (schema.graphqls, and the field is autobound with no resolver of
// its own), so a dangling or non-milestone assignment arrives at the client as
// written. A client that gets a rule wrong draws a row in a section whose queue
// it is not in, and then reorders it in a region the server does not agree it
// belongs to. The derived rule is also what the server's own `noMilestone`
// filter answers over (internal/graph/filters.go), so a client grouping by the
// direct one disagrees with `no:milestone` about which nibs are backlog.
//
// Two implementations of one rule drift silently, so the fixture below is
// rendered together with the answers Go gives for it, and the pair is
// committed. What the tests hold:
//
//   - TestGeneratedMembershipContractIsFresh (here) reddens when the committed
//     bytes stop matching what the current rules, fixture and renderer produce.
//   - web/src/lib/membership.test.ts reddens when a regenerated answer stops
//     matching the TS mirror's — Go changed and was regenerated but
//     resolvedMilestoneId or milestoneOf was not, or a mirror changed and Go did
//     not.
//   - TestContractFixtureDiscriminatesEachRuleDecision and
//     TestContractFixtureDiscriminatesEachMilestoneOfDecision (here) redden when
//     the fixture stops being able to tell a rule from a mutant of it, so a
//     fixture edit cannot hollow the two above out.
//
// All of them are BOUNDED BY FIXTURE COVERAGE: a change on either side that
// moves no fixture row's answer renders identical bytes and replays
// identically, so it fails nothing. Two kinds of test here narrow that bound
// rather than remove it — TestResolvedMilestoneIDShapeIsPinned and
// TestMilestoneOfShapeIsPinned redden on a change to a Go rule's field reads or
// clause count and state the obligation such a change carries (a fixture row
// exercising it, and a mutant for it), and
// TestRuleIsComputableFromTheWireProjection reddens when a rule starts reading a
// field the wire projection cannot carry AND a fixture row varies it. What the
// projection carries on the parent axis is the RESOLVED parent, so following a
// parent link cannot redden that test while telling a DANGLING link apart from
// no parent at all does — `t9` links to a nib that does not exist, and is the
// row that varies the two readings. `status:` is outside the projection
// altogether, but no fixture row sets it, so a clause reading `status:` is free
// there and only the shape pins see it.
//
// Both watch the GO side only, and that is where the deliberate residual is:
// a further clause over the modeled fields, moving no fixture row's answer,
// still passes everything when it is added to a TS MIRROR. Closing it
// symmetrically would mean driving a TypeScript parser from Go. Added to a Go
// rule the same clause reddens that rule's shape pin, which is why those tests
// exist.
//
// TestRenderedTypeIsTheWireType holds the projection those tests answer over:
// `type` is the effective type and `parentId` the resolved parent, and the test
// reads the Nib.type and Nib.parentId resolvers out of internal/graph rather
// than restating what they do.
//
// The rendering shape follows internal/webvocab: `task codegen` writes the file
// via go:generate, and the committed bytes are pinned by a test, so the web
// never depends on running Go. It differs from that sibling in what holds its
// consumer in place: vocabulary.ts is imported by app code, so deleting its
// consumer breaks the build, while this module is imported only by
// web/src/lib/membership.test.ts — nothing but that replay reads it, so if the
// replay goes the two copies of the rule are free to drift apart in silence.
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
// degenerate cases the direct rule has clauses for — a milestone-typed subject,
// an assignment naming no nib, an assignment naming a non-milestone — alongside
// assignments that do resolve, and the shapes the derived rule's walk decides
// on: an unassigned nib under an assigned one, an unassigned nib TWO levels
// under one, an assigned nib under a differently assigned one, a milestone-typed
// ancestor, and a parent cycle. So the contract discriminates rather than merely
// agreeing on "".
// TestContractFixtureDiscriminatesEachRuleDecision and its MilestoneOf sibling
// prove it does, by running a mutant with each decision undone and requiring
// the answers to differ.
//
// The `note` strings number the clauses in ResolvedMilestoneID's own order:
// 1 the subject is not itself a milestone, 2 the target exists, 3 the target is
// milestone-typed. They ship verbatim into the generated module, where they are
// the only clause legend either side carries.
//
// Parent links carry the derived rule: t2 is the row whose two answers differ
// (the direct rule confers nothing from a parent, the walk inherits), t10 is
// the row only a TRANSITIVE walk answers, and t7, t8 and the c1/c2 cycle are
// the walk's remaining decisions. They are checkable on the TS side because
// MembershipNib carries the parent link the mirror's walk needs; the contract's
// parent column is what holds the two walks together.
//
// Aliases are deliberately MILESTONE-side only. The rendered parent column is
// the View's own reading, which is exact, and so is the resolution MilestoneOf
// performs; a short-form `parent:` would therefore resolve on the wire — where
// Core.Get retries with the configured prefix prepended — and miss here. The
// two never actually disagree on a Core-backed store, because nibcore rewrites
// every stored link id to full form in memory before either side reads it
// (canonicalize.go applies one resolve to `parent:` and `milestone:` alike), so
// such a row would model no store anyone can present while making this file
// contradict itself about which resolution the column is.
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

// contractRow is the wire projection of one fixture nib, paired with the
// answers the two rules give for the FULL nib. It is the single place the
// generated file's columns are decided — the renderer has nothing else to print
// — so pinning it pins the file. See TestRenderedTypeIsTheWireType.
type contractRow struct {
	ID        string
	Type      string
	Milestone string
	// ParentID is the RESOLVED parent — the reading Nib.parentId reports — so ""
	// covers both no parent and a link naming no nib.
	ParentID    string
	Aliases     []string
	Resolved    string
	MilestoneOf string
	Note        string
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

// fixtureNibs returns the fixture's nibs in fixture order.
func fixtureNibs(f []fixtureNib) []*nib.Nib {
	out := make([]*nib.Nib, 0, len(f))
	for _, r := range f {
		out = append(out, r.nib)
	}
	return out
}

// resolvedParents inverts the View's structural axis: nib id → the id its
// `parent:` link resolves to, absent for a root and for a link naming no nib.
//
// It is READ OUT of the View rather than re-derived from the stored links, so
// the rendered parent column is by construction the same resolution
// MilestoneOf's walk performs, rather than a second copy of that rule free to
// drift from it.
//
// The near-miss no replay can catch is rendering the RAW stored link instead:
// every fixture row's link either names a fixture nib, where the two readings
// are the same string, or names none, where the mirror's walk stops on the
// lookup miss and answers "" exactly as it does for a null parent — so no TS
// answer moves and the replay stays green. TestRenderedTypeIsTheWireType is what
// catches that copy, by re-deriving the column from the fixture's own index and
// naming the row it differs on.
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
// both Go rules.
//
// The two answers are computed over two different lookups, because the rules
// take theirs differently: ResolvedMilestoneID is handed one, and the fixture's
// canonicalizes (see fixtureLookup), while MilestoneOf is a View method and the
// View indexes its slice by exact id. That is not a harness choice — no View
// canonicalizes — and t6 is where it shows: `milestone: m4` resolves to nibs-m4
// for the direct rule and to nothing for the walk. The generated header states
// it so the replay uses the matching lookup for each column.
func rows() []contractRow {
	f := fixture()
	lookup := fixtureLookup(f)
	all := fixtureNibs(f)
	view := membership.Compute(all)
	parents := resolvedParents(view, all)

	out := make([]contractRow, 0, len(f))
	for _, r := range f {
		out = append(out, contractRow{
			ID: r.nib.ID,
			// The EFFECTIVE type: what the Nib.type resolver reports
			// (internal/graph/schema.resolvers.go), not the stored field.
			Type:        r.nib.EffectiveType(),
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
