import { CLOSED_STATUSES, RELEASING_STATUSES } from "./constants";

/**
 * The client's copy of the server's milestone-membership rules: mirrors of
 * `membership.ResolvedMilestoneID` (DIRECT assignment) and
 * `(*membership.View).MilestoneOf` (DERIVED membership, inherited up the parent
 * chain) in internal/membership/membership.go. A grouping lens wants the second.
 *
 * `Nib.milestone` is reported verbatim on the wire, so an assignment naming a
 * missing or non-milestone nib arrives as stored. Apply these rules before
 * drawing a row into a milestone's section.
 *
 * Go answers over the whole store; a client lookup spans only the rows the page
 * holds, which the server has filtered. For `resolvedMilestoneId` a narrower
 * lookup moves answers only toward "", so a hidden milestone's members read as
 * unassigned. `milestoneOf` behaves differently; see its doc.
 *
 * Held to the Go rules by ./generated/membershipContract.ts, replayed in
 * membership.test.ts; see internal/membershipcontract.
 */

/** The four wire fields the rules read. `TreeNib` satisfies it structurally. */
export interface MembershipNib {
  readonly id: string;
  /** The EFFECTIVE type, which is what `Nib.type` reports. */
  readonly type: string;
  /** The stored assignment, verbatim. */
  readonly milestone: string;
  /**
   * The RESOLVED parent (`Nib.parentId`): null both for no parent and for a
   * stored link naming no nib. Not `storedParentId`.
   *
   * Go's walk reads the raw `parent:` through an exact-id index instead, while
   * `Nib.parentId` also tries the prefixed form. They agree because nibcore
   * canonicalizes resolvable stored link ids to their full form
   * (internal/nibcore/canonicalize.go).
   */
  readonly parentId: string | null;
}

/**
 * Resolves an id to a nib, or null/undefined when it names none — the mirror of
 * Go's `membership.Lookup`.
 *
 * Pass a closure (`(id) => byId.get(id)`), never `byId.get` itself: that
 * type-checks and then throws on the first call, having lost its receiver.
 */
export type MembershipLookup = (id: string) => MembershipNib | null | undefined;

/** The one type the rules treat as a container of its own. Compare against this, not a literal. */
export const MILESTONE_TYPE = "milestone";

/**
 * Whether a nib of this type may carry either assignment axis (milestone or
 * area) — the mirror of `nibtypes.RefusedAxes`, which refuses both for a
 * milestone and neither for any other type.
 *
 * Exported for callers holding a type and no nib, such as a drag deciding
 * whether its rows could join a group.
 */
export function takesAssignmentAxes(type: string): boolean {
  return type !== MILESTONE_TYPE;
}

/**
 * Whether a milestone in `milestoneStatus` accepts an assignment from a subject
 * in `subjectStatus` — the mirror of the assignment door in
 * `validateAndSetMilestone` (internal/graph/resolver.go). A milestone in a
 * releasing status refuses open work; closed work is always accepted.
 */
export function milestoneAcceptsAssignment(milestoneStatus: string, subjectStatus: string): boolean {
  return !RELEASING_STATUSES.includes(milestoneStatus) || CLOSED_STATUSES.includes(subjectStatus);
}

/**
 * The id of the milestone whose queue this nib is DIRECTLY in, or "": the
 * subject is not a milestone, the target exists, and the target is
 * milestone-typed. There is no ancestor walk; that is `milestoneOf`.
 *
 * Returns the target's id, not the stored string, so a canonicalizing lookup is
 * honored. "" is MEMBERLESS in the MILESTONE ordering scope, not a group: see
 * `Scope` in internal/graph/orderer.go.
 */
export function resolvedMilestoneId(subject: MembershipNib, lookup: MembershipLookup): string {
  if (subject.milestone === "" || !takesAssignmentAxes(subject.type)) return "";
  const target = lookup(subject.milestone);
  if (!target || target.type !== MILESTONE_TYPE) return "";
  return target.id;
}

/**
 * The id of the milestone this nib TRANSITIVELY belongs to, or "" for backlog —
 * the mirror of `(*membership.View).MilestoneOf`, which the server's
 * `noMilestone` filter reads (internal/graph/filters.go).
 *
 * The subject's own resolved assignment, else the nearest one up the parent
 * chain. The walk stops at a milestone-typed ancestor and terminates on a parent
 * cycle. Each step calls `resolvedMilestoneId`; the loop's milestone-type check
 * decides whether the walk continues and is not a copy of that rule.
 *
 * The lookup must be EXACT, not canonicalizing, because Go's walk indexes by
 * exact id: contract row `t6` answers `nibs-m4` for `resolvedMilestoneId` and ""
 * here. A `Map.get` closure over loaded rows is exact.
 *
 * Under a narrowed lookup, unlike `resolvedMilestoneId`:
 * - losing a MILESTONE can move the answer to an ancestor's milestone, not "";
 * - losing an intermediate ANCESTOR collapses the answer to "" while the server
 *   still holds the row in a queue. An ordinary type or status filter does this.
 * The parity contract's lookup is total, so membership.test.ts covers both.
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
