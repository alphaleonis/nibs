/**
 * The client's copy of the server's milestone-membership rule — the mirror of
 * `membership.ResolvedMilestoneID` (internal/membership/membership.go).
 *
 * It exists because `Nib.milestone` is reported VERBATIM on the wire: the field
 * is autobound with no resolver of its own, and the schema says so in as many
 * words. An assignment naming a missing nib, or one naming a nib that is not a
 * milestone, arrives here exactly as it is stored — so nothing has applied the
 * rule by the time the web sees the row, and a grouping lens that draws a row
 * into a milestone's section without applying it puts that row in a section
 * whose queue the server does not agree it is in.
 *
 * The lookup's DOMAIN is the caller's, and the two sides do not have the same
 * one. Go answers over the whole store (`membership.Compute(reader.All())`); a
 * client lookup can only span the rows the page holds, and that set is
 * server-filtered. Narrowing the domain moves answers only toward "" — the same
 * key resolves to the same nib or to none — so filtering a milestone out of the
 * result set makes its members read as unassigned, and they are drawn in
 * Backlog while the server still holds them in that milestone's queue. That is
 * the safe direction (a row in Backlog claims no queue it is not in), and it is
 * the direction the parity contract cannot see: its lookup is always total over
 * the fixture. `resolvedMilestoneId` returning "" is MEMBERLESS in the
 * MILESTONE ordering scope, not a group — see `Scope` in
 * internal/graph/orderer.go.
 *
 * Held to the Go rule by ./generated/membershipContract.ts, replayed in
 * membership.test.ts. See internal/membershipcontract for which direction of
 * drift each test catches, and for the bound they all share.
 */

/**
 * The subject of the rule: the three wire fields it reads, and nothing else.
 * `TreeNib` satisfies it structurally, so a lens passes its own rows straight
 * in — a narrower parameter than the row type keeps this module out of the
 * table's shape.
 */
export interface MembershipNib {
  readonly id: string;
  /** The EFFECTIVE type, which is what `Nib.type` reports. */
  readonly type: string;
  /** The stored assignment, verbatim. */
  readonly milestone: string;
}

/**
 * Resolves an id to a nib, or to nothing when the id names none — the mirror of
 * Go's `membership.Lookup`.
 *
 * Both absence values are accepted so neither shape of caller needs an adapter:
 * a closure over a `Map` returns `byId.get(id)` with no `?? null`, and one over
 * a GraphQL result returns `byId[id] ?? null`. Pass a CLOSURE, never the method
 * itself — `const lookup: MembershipLookup = byId.get` type-checks clean (the
 * lib declares `get(key: K): V | undefined` with no `this` parameter) and then
 * throws on the first call, because `Map.prototype.get` needs its receiver.
 */
export type MembershipLookup = (id: string) => MembershipNib | null | undefined;

const MILESTONE_TYPE = "milestone";

/**
 * The id of the milestone whose queue this nib is directly in, or "" for a nib
 * in no queue at all.
 *
 * Three clauses, all three load-bearing: the subject is not itself a milestone
 * (a milestone is a container of its own, even when hand-edited data assigns
 * it), the target exists, and the target is milestone-typed.
 *
 * It reads the nib's OWN `milestone:` field — there is no ancestor walk, so a
 * task under an assigned epic is in no queue of its own. "" is MEMBERLESS in
 * the MILESTONE ordering scope, not a group: see `Scope` in
 * internal/graph/orderer.go.
 *
 * Returns the TARGET's id rather than the stored string, mirroring Go, so a
 * lookup that resolves an id to its canonical form is honored.
 */
export function resolvedMilestoneId(subject: MembershipNib, lookup: MembershipLookup): string {
  if (subject.milestone === "" || subject.type === MILESTONE_TYPE) return "";
  const target = lookup(subject.milestone);
  if (!target || target.type !== MILESTONE_TYPE) return "";
  return target.id;
}
