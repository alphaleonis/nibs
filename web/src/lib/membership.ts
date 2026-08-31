/**
 * The client's copy of the server's milestone-membership rules — the mirrors of
 * `membership.ResolvedMilestoneID` and `(*membership.View).MilestoneOf`
 * (internal/membership/membership.go). The first is DIRECT assignment, the
 * second the DERIVED membership that inherits up the parent chain; a grouping
 * lens wants the second, and `nibs list --backlog` and the `noMilestone` filter
 * are its complement.
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
 * server-filtered. For `resolvedMilestoneId` narrowing the domain moves answers
 * only toward "" — the same key resolves to the same nib or to none — so
 * filtering a milestone out of the result set makes its members read as
 * unassigned, and they are drawn in Backlog while the server still holds them
 * in that milestone's queue. That is the safe direction (a row in Backlog
 * claims no queue it is not in), and it is the direction the parity contract
 * cannot see: its lookup is always total over the fixture. `milestoneOf` does
 * NOT inherit that property — its walk continues past the step the narrowing
 * emptied — and its own doc says what it does instead. "" is MEMBERLESS in the
 * MILESTONE ordering scope, not a group — see `Scope` in
 * internal/graph/orderer.go.
 *
 * Held to the Go rules by ./generated/membershipContract.ts, replayed in
 * membership.test.ts. See internal/membershipcontract for which direction of
 * drift each test catches, and for the bound they all share.
 */

/**
 * The subject of the rules: the four wire fields they read, and nothing else.
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
  /**
   * The RESOLVED parent, which is what `Nib.parentId` reports: null both for a
   * nib with no parent and for one whose stored link names no nib
   * (internal/graph/schema.resolvers.go, `(*nibResolver).ParentID`). The raw
   * stored link is a different wire field, `storedParentId`, and this is not
   * it.
   *
   * Only `milestoneOf` reads it. Go walks the raw `parent:` and resolves it
   * through the View's index at each step (internal/membership/membership.go),
   * so the two sides start from different fields, and the wire's reading is the
   * wider one: `Nib.parentId` resolves through `(*Core).Get`, which retries with
   * the configured prefix prepended, while the View indexes by exact id. They
   * agree anyway because nibcore rewrites every stored link id to its full form
   * in memory before either side reads it — internal/nibcore/canonicalize.go
   * applies one resolve to `parent:` and `milestone:` alike — so a Core-backed
   * store never holds a short-form parent that the wire resolves and the walk
   * misses.
   */
  readonly parentId: string | null;
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

/**
 * The one type the rules below treat as a container of its own. Exported so a
 * caller that has to ask the same question — a grouping lens deciding which
 * nibs HEAD its sections — spells it the way the rules do, rather than growing
 * a second literal that a vocabulary change would leave behind.
 */
export const MILESTONE_TYPE = "milestone";

/**
 * Whether a nib of this type can be in a milestone's queue at all — the mirror
 * of `nibtypes.ValidateAxes`, which refuses the milestone axis for a
 * milestone-typed subject ("a milestone is a waypoint, not work"), and so the
 * first of `resolvedMilestoneId`'s three clauses.
 *
 * Exported because a caller can hold a TYPE and no nib: a drag deciding whether
 * the rows it carries could ever join the queue under the cursor is asking this
 * and nothing else. `resolvedMilestoneId` calls it rather than restating it, so
 * the generated contract — which has a milestone-typed subject carrying a
 * resolvable assignment — pins this predicate through that caller.
 */
export function canBeInMilestoneQueue(type: string): boolean {
  return type !== MILESTONE_TYPE;
}

/**
 * The id of the milestone whose queue this nib is directly in, or "" for a nib
 * in no queue at all.
 *
 * Three clauses, all three load-bearing: the subject is not itself a milestone
 * (a milestone is a container of its own, even when hand-edited data assigns
 * it), the target exists, and the target is milestone-typed.
 *
 * It reads the nib's OWN `milestone:` field — there is no ancestor walk, so a
 * task under an assigned epic is in no queue of its own. That is a grouping
 * lens's question, and `milestoneOf` below is its answer; this is the step that
 * rule takes at each nib, not a substitute for it. "" is MEMBERLESS in the
 * MILESTONE ordering scope, not a group: see `Scope` in
 * internal/graph/orderer.go.
 *
 * Returns the TARGET's id rather than the stored string, mirroring Go, so a
 * lookup that resolves an id to its canonical form is honored.
 */
export function resolvedMilestoneId(subject: MembershipNib, lookup: MembershipLookup): string {
  if (subject.milestone === "" || !canBeInMilestoneQueue(subject.type)) return "";
  const target = lookup(subject.milestone);
  if (!target || target.type !== MILESTONE_TYPE) return "";
  return target.id;
}

/**
 * The id of the milestone this nib TRANSITIVELY belongs to, or "" for a nib in
 * the backlog — the mirror of Go's `(*membership.View).MilestoneOf`, and the
 * rule the server's own `noMilestone` filter answers over
 * (internal/graph/filters.go). Group by anything else and the client disagrees
 * with `no:milestone` about which nibs are backlog.
 *
 * The subject's own resolved assignment when it has one, else the nearest
 * resolved assignment up the structural parent chain. The walk stops at a
 * milestone-typed ancestor — a milestone parent is decomposition data, not an
 * assignment, so its subtree does not inherit whatever the milestone is nested
 * under — and the visited set makes a parent cycle terminate rather than
 * recurse forever, since `parent:` is hand-editable and nothing forbids one.
 *
 * Each step goes through `resolvedMilestoneId` rather than restating its three
 * clauses: the generated contract pins the mirror, not its callers, so a second
 * copy of the direct rule would drift with nothing to catch it. The
 * milestone-typed test in the loop is NOT that restatement — it decides whether
 * the WALK continues, which is a question `resolvedMilestoneId` has no way to
 * answer, and Go's walk carries the same test for the same reason.
 *
 * The lookup must be EXACT — it must not canonicalize an id to its full form.
 * Go's walk can only ever use the View's own `byID` index, so a canonicalizing
 * lookup here would follow a link Go's walk misses; contract row `t6` is that
 * shape, answering `nibs-m4` for `resolvedMilestoneId` and "" for `milestoneOf`.
 * `resolvedMilestoneId` states the opposite precondition, so one
 * `MembershipLookup` serves two rules that want different things from it and
 * nothing in the type system separates them. A lookup built from loaded rows —
 * a `Map.get` closure — is exact, which is why this is a documented
 * precondition rather than a second type.
 *
 * Its behavior under a NARROWED lookup is the opposite of the direct rule's,
 * and a lens has to know it, in both directions:
 *
 * Losing a MILESTONE from the lookup moves `resolvedMilestoneId` only toward ""
 * (see the module doc), but here the walk continues past the step that answered
 * "" and can land on an ANCESTOR's assignment — so a filter hiding one milestone
 * can draw its members in a different milestone's section rather than in
 * Backlog.
 *
 * Losing an intermediate ANCESTOR collapses the answer instead: `lookup(parentId)`
 * returns nothing, the loop exits, and the row reads as Backlog while the
 * server's `noMilestone` — answered over the whole store — still holds it in its
 * milestone's queue. That is the direction an ordinary filter produces, because
 * the epics and features carrying the assignments drop off the page while their
 * tasks remain; `tree.ts` already promotes a node to a root when its parent is
 * absent from the loaded set, so a page routinely lacks them.
 *
 * The parity contract is blind to both — its lookup is total over the fixture —
 * so membership.test.ts holds a case of each.
 */
export function milestoneOf(subject: MembershipNib, lookup: MembershipLookup): string {
  const visited = new Set<string>();
  let current: MembershipNib | null | undefined = subject;
  while (current && !visited.has(current.id)) {
    visited.add(current.id);
    if (current.type === MILESTONE_TYPE) return "";
    const assigned = resolvedMilestoneId(current, lookup);
    if (assigned !== "") return assigned;
    const parentId = current.parentId;
    if (parentId === null) return "";
    current = lookup(parentId);
  }
  return "";
}
