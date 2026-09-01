import { describe, it, expect } from "vitest";
import { milestoneOf, resolvedMilestoneId } from "./membership";
import type { MembershipLookup, MembershipNib } from "./membership";
import { MEMBERSHIP_CONTRACT } from "./generated/membershipContract";
import type { ContractCase } from "./generated/membershipContract";
import type { TreeNib } from "./types";

function lookupOver(nibs: readonly MembershipNib[]): MembershipLookup {
  const byId = new Map(nibs.map((n) => [n.id, n]));
  return (id) => byId.get(id);
}

const m1: MembershipNib = { id: "m1", type: "milestone", milestone: "", parentId: null };
const e1: MembershipNib = { id: "e1", type: "epic", milestone: "m1", parentId: null };
const lookup = lookupOver([m1, e1]);

describe("resolvedMilestoneId", () => {
  it("resolves an assignment naming a milestone", () => {
    expect(resolvedMilestoneId(e1, lookup)).toBe("m1");
  });

  it("answers empty for a nib carrying no assignment", () => {
    expect(resolvedMilestoneId({ id: "t1", type: "task", milestone: "", parentId: null }, lookup)).toBe(
      "",
    );
  });

  it("reads the nib's own field, never an ancestor's", () => {
    // `MembershipNib` carries a parent link (`milestoneOf` walks it), so this is
    // a runtime case. t2 in the parity contract is the same shape, held against
    // Go.
    expect(
      resolvedMilestoneId({ id: "t1", type: "task", milestone: "", parentId: "e1" }, lookup),
    ).toBe("");
  });

  it("answers empty for a milestone-typed subject, however it is assigned", () => {
    expect(
      resolvedMilestoneId({ id: "m2", type: "milestone", milestone: "m1", parentId: null }, lookup),
    ).toBe("");
    expect(
      resolvedMilestoneId({ id: "m3", type: "milestone", milestone: "m3", parentId: null }, lookup),
    ).toBe("");
  });

  it("answers empty for an assignment naming no nib", () => {
    expect(
      resolvedMilestoneId({ id: "t2", type: "task", milestone: "ghost", parentId: null }, lookup),
    ).toBe("");
  });

  it("answers empty for an assignment naming a nib that is not a milestone", () => {
    expect(
      resolvedMilestoneId({ id: "t3", type: "task", milestone: "e1", parentId: null }, lookup),
    ).toBe("");
  });

  it("returns the target's id rather than the stored string", () => {
    // A lookup that resolves a short form to the full nib: the answer is the id
    // the lookup landed on, so a caller can key a section by it.
    const shortForms: MembershipLookup = (id) => (id === "m1" || id === "1" ? m1 : undefined);
    expect(
      resolvedMilestoneId({ id: "t5", type: "task", milestone: "1", parentId: null }, shortForms),
    ).toBe("m1");
  });
});

describe("milestoneOf", () => {
  // A chain the walk has to climb: t under f under e, and only e is assigned.
  const e: MembershipNib = { id: "e", type: "epic", milestone: "m1", parentId: null };
  const f: MembershipNib = { id: "f", type: "feature", milestone: "", parentId: "e" };
  const t: MembershipNib = { id: "t", type: "task", milestone: "", parentId: "f" };
  const chain = lookupOver([m1, e, f, t]);

  it("answers the nib's own resolved assignment when it has one", () => {
    expect(milestoneOf(e, chain)).toBe("m1");
  });

  it("climbs to the nearest assigned ancestor", () => {
    expect(milestoneOf(t, chain)).toBe("m1");
  });

  it("prefers its own assignment over an ancestor's", () => {
    const m2: MembershipNib = { id: "m2", type: "milestone", milestone: "", parentId: null };
    const own: MembershipNib = { id: "own", type: "task", milestone: "m2", parentId: "e" };
    expect(milestoneOf(own, lookupOver([m1, m2, e, own]))).toBe("m2");
  });

  it("stops at a milestone-typed ancestor rather than climbing past it", () => {
    // m4 is nested under the assigned epic e — illegal data the rule is written
    // for. The walk must not hand e's queue to m4's subtree.
    const m4: MembershipNib = { id: "m4", type: "milestone", milestone: "", parentId: "e" };
    const under: MembershipNib = { id: "under", type: "task", milestone: "", parentId: "m4" };
    expect(milestoneOf(under, lookupOver([m1, m4, e, under]))).toBe("");
  });

  it("answers empty for a milestone-typed subject, however it is assigned", () => {
    expect(milestoneOf({ id: "m5", type: "milestone", milestone: "m1", parentId: "e" }, chain)).toBe(
      "",
    );
  });

  it("terminates on a parent cycle", () => {
    // The visited set is the only thing that ends this walk, and a mirror that
    // lost it spins SYNCHRONOUSLY. vitest's testTimeout is timer-driven, and a
    // timer cannot fire while the worker's event loop is held, so the symptom is
    // a `task test` that never finishes rather than a red test — start here when
    // a web lane stalls. Go's cycle mutant budgets its steps for the same reason
    // (internal/membershipcontract).
    const c1: MembershipNib = { id: "c1", type: "task", milestone: "", parentId: "c2" };
    const c2: MembershipNib = { id: "c2", type: "task", milestone: "", parentId: "c1" };
    expect(milestoneOf(c1, lookupOver([c1, c2]))).toBe("");
  });

  it("stops where the parent chain does — no link, and one the lookup cannot resolve", () => {
    expect(milestoneOf({ id: "r", type: "task", milestone: "", parentId: null }, chain)).toBe("");
    expect(milestoneOf({ id: "d", type: "task", milestone: "", parentId: "ghost" }, chain)).toBe("");
  });
});

/**
 * The parity replay. Every row of the committed contract is fed to both TS rules
 * and must produce the answers Go produced for it at generation time.
 *
 * It reddens whenever a regenerated answer and a mirror disagree — Go changed
 * and was regenerated while resolvedMilestoneId or milestoneOf was not, or a
 * mirror changed and Go did not. It is BOUNDED BY THE FIXTURE in both
 * directions: a change on either side that moves no fixture row's answer passes
 * silently here. internal/membershipcontract carries the Go-side backstops for
 * that bound and says what they do and do not close.
 *
 * Do not delete this replay. It is the only consumer of the generated contract:
 * nothing else in web/ imports MEMBERSHIP_CONTRACT, and the Go-side tests
 * compare Go to Go. Without it the Go and TypeScript copies of the
 * milestone-membership rule are free to drift apart with nothing going red.
 * If the replay needs restructuring, restructure it — keep the contract driven
 * through both mirrors.
 */
describe("Go↔TS parity for the milestone-membership rules", () => {
  // Rows are projected to the wire fields the rules read, so the replay cannot
  // reach an expected answer riding along on the same object.
  function subjectOf(c: ContractCase): MembershipNib {
    return { id: c.id, type: c.type, milestone: c.milestone, parentId: c.parentId };
  }

  // `aliases` are the other keys the lookup Go answered over resolves to a row —
  // a store lookup canonicalizes short ids, a client one does not — so the replay
  // has to rebuild that lookup rather than key rows by id alone. Two maps, ids
  // checked first, because that is the precedence Go's fixtureLookup has: merging
  // them would decide an id/alias collision by insertion order instead of by
  // kind, and the resulting disagreement would read as a Go/TS rule divergence
  // rather than as the harness artifact it would be.
  //
  // The mode is not a knob: each column was computed over the lookup its Go rule
  // is given, and only "canonicalizing" matches ResolvedMilestoneID's. MilestoneOf
  // is a View method and a View indexes its slice by id alone, so replaying that
  // column through aliases would disagree with Go on t6 — see the generated
  // header.
  function lookupOverContract(
    mode: "canonicalizing" | "exact",
    hiddenId?: string,
  ): MembershipLookup {
    const byId = new Map<string, MembershipNib>();
    const byAlias = new Map<string, MembershipNib>();
    for (const c of MEMBERSHIP_CONTRACT) {
      if (c.id === hiddenId) continue;
      const row = subjectOf(c);
      byId.set(c.id, row);
      if (mode === "canonicalizing") for (const alias of c.aliases) byAlias.set(alias, row);
    }
    return (id) => byId.get(id) ?? byAlias.get(id);
  }
  const contractLookup = lookupOverContract("canonicalizing");
  const exactLookup = lookupOverContract("exact");

  it.each(MEMBERSHIP_CONTRACT)("resolvedMilestoneId — $id — $note", (c) => {
    expect(resolvedMilestoneId(subjectOf(c), contractLookup)).toBe(c.resolvedMilestoneId);
  });

  it.each(MEMBERSHIP_CONTRACT)("milestoneOf — $id — $note", (c) => {
    expect(milestoneOf(subjectOf(c), exactLookup)).toBe(c.milestoneOf);
  });

  // A contract whose rows all answered the same way would be replayed happily by
  // a mirror that always answered that way. The Go side proves more than this —
  // TestContractFixtureDiscriminatesEachRuleDecision runs a mutant with each
  // decision undone and requires it to disagree — but a fixture regenerated down
  // to nothing should not pass here either.
  it("carries every answer shape, so the replay above discriminates", () => {
    expect(MEMBERSHIP_CONTRACT.some((c) => c.resolvedMilestoneId !== "")).toBe(true);
    expect(MEMBERSHIP_CONTRACT.some((c) => c.milestone !== "" && c.resolvedMilestoneId === "")).toBe(
      true,
    );
    // Without a row whose answer differs from the stored string, the replay
    // cannot tell `return target.id` from `return subject.milestone`.
    expect(
      MEMBERSHIP_CONTRACT.some(
        (c) => c.resolvedMilestoneId !== "" && c.resolvedMilestoneId !== c.milestone,
      ),
    ).toBe(true);
  });

  // The same floor for the derived rule, and one clause more: the two columns
  // must disagree somewhere, or a mirror that simply returned the direct rule's
  // answer — the one mistake the whole nib exists to prevent — would replay
  // clean.
  it("carries a row where the two rules part, so the milestoneOf replay discriminates", () => {
    expect(MEMBERSHIP_CONTRACT.some((c) => c.milestoneOf !== "")).toBe(true);
    expect(
      MEMBERSHIP_CONTRACT.some((c) => c.milestoneOf !== "" && c.resolvedMilestoneId === ""),
    ).toBe(true);
    // A nib whose walk reaches an assigned ancestor and is stopped short of it
    // anyway, so "stop at a milestone-typed ancestor" is not free to drop. The
    // step ABOVE the milestone has to be checked too: with the milestone a root,
    // a walk that climbed past it would reach the root and still answer "", and
    // the decision would be undetectable.
    expect(
      MEMBERSHIP_CONTRACT.some((c) => {
        const parent = MEMBERSHIP_CONTRACT.find((p) => p.id === c.parentId);
        if (c.milestoneOf !== "" || parent === undefined || parent.type !== "milestone") return false;
        const above = MEMBERSHIP_CONTRACT.find((g) => g.id === parent.parentId);
        return above !== undefined && above.resolvedMilestoneId !== "";
      }),
    ).toBe(true);
  });

  // The hazard `milestoneOf` carries and `resolvedMilestoneId` does not, made a
  // test rather than a sentence. Losing a milestone from the lookup empties the
  // step that resolved it, and the walk then CONTINUES — so a row can land in an
  // ancestor's queue instead of in Backlog. A lens filtering milestones out of
  // the page has to reckon with that; the direct rule's own narrowing test below
  // is the contrast.
  it("can move an answer to an ancestor's queue when the lookup loses one milestone", () => {
    const moved = MEMBERSHIP_CONTRACT.filter((c) => c.milestoneOf !== "").filter((c) => {
      const after = milestoneOf(subjectOf(c), lookupOverContract("exact", c.milestoneOf));
      return after !== "" && after !== c.milestoneOf;
    });
    expect(moved.map((c) => c.id)).not.toEqual([]);
  });

  // The other narrowing direction, and the one an ordinary filter produces: the
  // rows carrying the assignments are epics and features, so a type or status
  // filter drops them while their tasks remain. The walk then finds nothing at
  // `lookup(parentId)`, exits, and every inherited answer collapses to Backlog
  // while the server's `noMilestone` — answered over the whole store — still
  // holds those rows in a queue.
  it("collapses an inherited answer toward empty when the lookup loses the row's parent", () => {
    let checked = 0;
    for (const c of MEMBERSHIP_CONTRACT) {
      // Inherited: the answer comes from an ancestor, not from the row itself.
      if (c.milestoneOf === "" || c.resolvedMilestoneId !== "" || c.parentId === null) continue;
      checked++;
      expect(milestoneOf(subjectOf(c), lookupOverContract("exact", c.parentId))).toBe("");
    }
    expect(checked).toBeGreaterThan(0);
  });

  // The module doc's claim about the lookup's domain, made checkable: the client
  // builds its lookup from the rows the page holds, that set is server-filtered,
  // and a narrower domain moves answers only toward "" — never to a different
  // milestone. That is why a filter hiding a milestone draws its members in
  // Backlog rather than in someone else's queue.
  //
  // ONE milestone is hidden, not all of them. With none left, "toward ''" and
  // "always ''" are indistinguishable and the "never to a different milestone"
  // half is unobservable: hiding every milestone leaves the assertion green
  // under a mirror answering `subject.milestone` instead of the target's id, and
  // under one dropping the milestone-typed-subject clause. Hiding one catches
  // both. Against the suite as a whole it still discriminates nothing the replay
  // above does not; what it adds is the INPUT — a partial lookup, which the
  // replay never supplies and the real client always has.
  it("moves answers only toward empty when the lookup's domain loses one milestone", () => {
    const answered = MEMBERSHIP_CONTRACT.map((c) => c.resolvedMilestoneId).filter((id) => id !== "");
    const hidden = answered[0] ?? "";
    expect(hidden).not.toBe("");

    const narrowed = lookupOverContract("canonicalizing", hidden);
    for (const c of MEMBERSHIP_CONTRACT) {
      const want = c.resolvedMilestoneId === hidden ? "" : c.resolvedMilestoneId;
      expect(resolvedMilestoneId(subjectOf(c), narrowed)).toBe(want);
    }
  });
});

// A lens reads rows, not fixtures, so the subject type has to accept one. This
// is the compile-time half of that claim: `MembershipNib` must stay a subset of
// what the wire already puts on every tree row.
type _RowIsASubject = TreeNib extends MembershipNib ? true : never;
const _rowIsASubjectCheck: _RowIsASubject = true;
void _rowIsASubjectCheck;
