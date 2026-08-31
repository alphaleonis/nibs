import { describe, it, expect } from "vitest";
import { resolvedMilestoneId } from "./membership";
import type { MembershipLookup, MembershipNib } from "./membership";
import { MEMBERSHIP_CONTRACT } from "./generated/membershipContract";
import type { TreeNib } from "./types";

function lookupOver(nibs: readonly MembershipNib[]): MembershipLookup {
  const byId = new Map(nibs.map((n) => [n.id, n]));
  return (id) => byId.get(id);
}

const m1: MembershipNib = { id: "m1", type: "milestone", milestone: "" };
const e1: MembershipNib = { id: "e1", type: "epic", milestone: "m1" };
const lookup = lookupOver([m1, e1]);

describe("resolvedMilestoneId", () => {
  it("resolves an assignment naming a milestone", () => {
    expect(resolvedMilestoneId(e1, lookup)).toBe("m1");
  });

  it("answers empty for a nib carrying no assignment", () => {
    // Also the whole of "reads the nib's own field, never an ancestor's": that
    // property is enforced by MembershipNib's field set — the parameter type has
    // no room to express a parent — so no runtime case can exercise it.
    expect(resolvedMilestoneId({ id: "t1", type: "task", milestone: "" }, lookup)).toBe("");
  });

  it("answers empty for a milestone-typed subject, however it is assigned", () => {
    expect(resolvedMilestoneId({ id: "m2", type: "milestone", milestone: "m1" }, lookup)).toBe("");
    expect(resolvedMilestoneId({ id: "m3", type: "milestone", milestone: "m3" }, lookup)).toBe("");
  });

  it("answers empty for an assignment naming no nib", () => {
    expect(resolvedMilestoneId({ id: "t2", type: "task", milestone: "ghost" }, lookup)).toBe("");
  });

  it("answers empty for an assignment naming a nib that is not a milestone", () => {
    expect(resolvedMilestoneId({ id: "t3", type: "task", milestone: "e1" }, lookup)).toBe("");
  });

  it("returns the target's id rather than the stored string", () => {
    // A lookup that resolves a short form to the full nib: the answer is the id
    // the lookup landed on, so a caller can key a section by it.
    const shortForms: MembershipLookup = (id) => (id === "m1" || id === "1" ? m1 : undefined);
    expect(resolvedMilestoneId({ id: "t5", type: "task", milestone: "1" }, shortForms)).toBe("m1");
  });
});

/**
 * The parity replay. Every row of the committed contract is fed to the TS rule
 * and must produce the answer Go produced for it at generation time.
 *
 * It reddens whenever a regenerated answer and the mirror disagree — Go changed
 * and was regenerated while resolvedMilestoneId was not, or the mirror changed
 * and Go did not. It is BOUNDED BY THE FIXTURE in both directions: a change on
 * either side that moves no fixture row's answer passes silently here.
 * internal/membershipcontract carries the Go-side backstops for that bound and
 * says what they do and do not close.
 */
describe("Go↔TS parity for the milestone-membership rule", () => {
  // Rows are projected to the three wire fields the rule reads, so the replay
  // cannot reach the expected answer riding along on the same object. `aliases`
  // are the other keys the lookup Go answered over resolves to a row — a store
  // lookup canonicalizes short ids, a client one does not — so the replay has to
  // rebuild that lookup rather than key rows by id alone. Two maps, ids checked
  // first, because that is the precedence Go's fixtureLookup has: merging them
  // would decide an id/alias collision by insertion order instead of by kind,
  // and the resulting disagreement would read as a Go/TS rule divergence rather
  // than as the harness artifact it would be.
  function lookupOverContract(hiddenId?: string): MembershipLookup {
    const byId = new Map<string, MembershipNib>();
    const byAlias = new Map<string, MembershipNib>();
    for (const c of MEMBERSHIP_CONTRACT) {
      if (c.id === hiddenId) continue;
      const row: MembershipNib = { id: c.id, type: c.type, milestone: c.milestone };
      byId.set(c.id, row);
      for (const alias of c.aliases) byAlias.set(alias, row);
    }
    return (id) => byId.get(id) ?? byAlias.get(id);
  }
  const contractLookup = lookupOverContract();

  it.each(MEMBERSHIP_CONTRACT)("$id — $note", (c) => {
    const subject: MembershipNib = { id: c.id, type: c.type, milestone: c.milestone };
    expect(resolvedMilestoneId(subject, contractLookup)).toBe(c.resolvedMilestoneId);
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

    const narrowed = lookupOverContract(hidden);
    for (const c of MEMBERSHIP_CONTRACT) {
      const subject: MembershipNib = { id: c.id, type: c.type, milestone: c.milestone };
      const want = c.resolvedMilestoneId === hidden ? "" : c.resolvedMilestoneId;
      expect(resolvedMilestoneId(subject, narrowed)).toBe(want);
    }
  });
});

// A lens reads rows, not fixtures, so the subject type has to accept one. This
// is the compile-time half of that claim: `MembershipNib` must stay a subset of
// what the wire already puts on every tree row.
type _RowIsASubject = TreeNib extends MembershipNib ? true : never;
const _rowIsASubjectCheck: _RowIsASubject = true;
void _rowIsASubjectCheck;
