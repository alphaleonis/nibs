import { CLOSED_STATUSES } from "../constants";
import {
  MILESTONE_TYPE,
  milestoneAcceptsAssignment,
  milestoneOf,
  resolvedMilestoneId,
  takesAssignmentAxes,
  type MembershipLookup,
  type MembershipNib,
} from "../membership";
import type { MilestoneOption } from "../milestones";
import { sequence, setMilestoneBatch, updateNib } from "../mutations/commands";
import { refuse, type DropPlan } from "./dropPlan";
import { BY_ID, spellId, type RegionNamer } from "./region";

/** The value of the target that clears a milestone assignment. */
export const NO_MILESTONE_TARGET = "";

/** One drop target in the mapping panel: a milestone, or "No milestone". */
export interface MappingTarget {
  /** The milestone id a drop here assigns, or "" for "No milestone". */
  readonly value: string;
  /** The milestone, or null for "No milestone". */
  readonly milestone: MilestoneOption | null;
}

/**
 * Returns the panel's targets: "No milestone" first, then every milestone not
 * in a closed status, in the given order. `deferred` is closed, so it is left out.
 */
export function mappingTargets(milestones: readonly MilestoneOption[]): MappingTarget[] {
  return [
    { value: NO_MILESTONE_TARGET, milestone: null },
    ...milestones.filter((m) => !CLOSED_STATUSES.includes(m.status)).map((m) => ({ value: m.id, milestone: m })),
  ];
}

export interface MappingDropRequest {
  /** The ids being dragged, in selection order. */
  readonly draggedIds: readonly string[];
  /** Resolves a dragged id to its nib as it was at grab time, or undefined when the view does not show it. */
  readonly draggedNib: (id: string) => MembershipNib | undefined;
  /** The milestone id to assign, or "" to clear. */
  readonly target: string;
  /** An exact lookup over the whole store, so derived membership is the server's. */
  readonly lookup: MembershipLookup;
  /** The nibs whose resolved parent is the given id, over the same store as `lookup`. */
  readonly childrenOf: (id: string) => readonly MembershipNib[];
  /** Resolves an id to its status, over the same store as `lookup`, or undefined when it names no nib. */
  readonly statusOf: (id: string) => string | undefined;
  /** Whether the last refresh of that store failed, so it may be out of date. */
  readonly stale?: boolean;
  /** Spells the ids in this plan's prose as titles. Omitted, ids stay ids. */
  readonly nameOf?: RegionNamer;
}

/**
 * Plans a drop onto a mapping-panel target: a `milestone` write for each dragged
 * nib not already in the target, or the refusal.
 *
 * Membership is derived (`milestoneOf`), so a child of an epic assigned to the
 * target counts as already there. A nib dragged with one of its ancestors moves
 * through that ancestor's write, because the server refuses a nib and its
 * ancestor both being assigned.
 *
 * A drop that clears an assignment before assigning runs as a sequence, and the
 * dispatcher does not undo a step that landed. So that plan is refused when an
 * assign would meet an undragged assigned ancestor or descendant, when the
 * target would refuse an assign, or when the store is `stale`. A drop with no
 * clear is a batch, and the server refuses such a leg on its own.
 */
export function planMappingDrop(req: MappingDropRequest): DropPlan {
  const { draggedIds, draggedNib, target, lookup, childrenOf, statusOf, stale = false, nameOf = BY_ID } = req;
  if (draggedIds.length === 0) return refuse("no-source", "Nothing is being dragged.");

  const subjects: MembershipNib[] = [];
  for (const id of draggedIds) {
    const subject = draggedNib(id);
    // The selection survives a filter change, so a selected nib can be absent from the view.
    if (subject === undefined) {
      return refuse(
        "hidden-member",
        `${spellId(id, nameOf)} is selected but not shown here — clear the filter (or expand the parent) hiding it, or drop it from the selection.`,
      );
    }
    subjects.push(subject);
  }

  const refused = subjects.filter((s) => !takesAssignmentAxes(s.type));
  if (refused.length > 0) {
    return refuse("unassignable-type", `${spellId(refused[0].id, nameOf)} is a ${refused[0].type}, which takes no milestone assignment.`);
  }

  const targetName = target === NO_MILESTONE_TARGET ? "No milestone" : spellId(target, nameOf);
  const dragged = new Set(draggedIds);
  const assigns: string[] = [];
  const clears: string[] = [];
  for (const s of subjects) {
    const ownMilestone = resolvedMilestoneId(s, lookup);
    if (target !== NO_MILESTONE_TARGET && hasDraggedAncestor(s, dragged, lookup)) {
      // Dragged nibs in one parent cycle are each other's dragged ancestors, so none of them would be assigned.
      if (inParentCycle(s, lookup)) {
        return refuse("parent-cycle", `${spellId(s.id, nameOf)} is its own ancestor, so fix its parent before planning it.`);
      }
      // It joins through the dragged ancestor, whose assignment the server
      // refuses while this one stands, even to the same milestone.
      if (ownMilestone !== "") clears.push(s.id);
      continue;
    }
    if (milestoneOf(s, lookup) === target) continue;
    if (target === NO_MILESTONE_TARGET) {
      if (ownMilestone !== "") {
        clears.push(s.id);
        continue;
      }
      // Clearing the subject's own field would leave the inherited membership in place.
      const holder = assignedAncestor(s, lookup);
      if (holder !== null && dragged.has(holder.id)) continue;
      const via = holder === null ? "an ancestor" : spellId(holder.id, nameOf);
      return refuse(
        "inherited-membership",
        `${spellId(s.id, nameOf)} is in ${spellId(milestoneOf(s, lookup), nameOf)} through ${via}, so clear the milestone of ${via} instead.`,
      );
    }
    assigns.push(s.id);
  }

  if (target !== NO_MILESTONE_TARGET && clears.length > 0) {
    if (stale) {
      return refuse(
        "panel-unavailable",
        "The milestone counts could not be refreshed, and this drop clears a milestone before it assigns one, so the panel refuses it until they refresh.",
      );
    }
    const refusal = targetRefusal(target, targetName, assigns, lookup, statusOf, nameOf);
    if (refusal !== null) return refusal;
    const cleared = new Set(clears);
    const ownAfter = (n: MembershipNib) => (cleared.has(n.id) ? "" : resolvedMilestoneId(n, lookup));
    for (const id of assigns) {
      const conflict = assignedRelative(draggedNib(id)!, ownAfter, lookup, childrenOf);
      if (conflict !== null) {
        return refuse(
          "exclusivity-conflict",
          `${spellId(id, nameOf)} cannot join ${targetName}, because ${spellId(conflict.id, nameOf)} is in ${spellId(ownAfter(conflict), nameOf)} and a nib and its ancestor are never both assigned.`,
        );
      }
    }
  }

  if (assigns.length === 0 && clears.length === 0) {
    const subject = subjects.length === 1 ? `${spellId(subjects[0].id, nameOf)} is` : `The ${subjects.length} dragged nibs are`;
    return refuse(
      "already-in-section",
      target === NO_MILESTONE_TARGET ? `${subject} already in no milestone.` : `${subject} already in ${targetName}.`,
    );
  }

  return {
    ok: true,
    kind: "assign",
    assignment: { field: "milestone", value: target },
    indicator: "into",
    label: target === NO_MILESTONE_TARGET ? "Remove from milestone" : `Assign to ${targetName}`,
    command:
      target === NO_MILESTONE_TARGET
        ? setMilestoneBatch(clears, "")
        : clears.length === 0
          ? setMilestoneBatch(assigns, target)
          : // A sequence, because the batch legs run concurrently.
            sequence([
              ...setMilestoneBatch(clears, "").commands,
              ...assigns.map((id) => updateNib(id, { milestone: target })),
            ]),
  };
}

/**
 * The nearest ancestor, else the first descendant, holding an assignment per
 * `ownAfter`, or null when none does: the two checks of the server's
 * `validateAndSetMilestone`.
 */
function assignedRelative(
  subject: MembershipNib,
  ownAfter: (n: MembershipNib) => string,
  lookup: MembershipLookup,
  childrenOf: (id: string) => readonly MembershipNib[],
): MembershipNib | null {
  const visited = new Set<string>([subject.id]);
  let parentId = subject.parentId;
  while (parentId !== null && !visited.has(parentId)) {
    visited.add(parentId);
    const parent = lookup(parentId);
    if (!parent) break;
    if (ownAfter(parent) !== "") return parent;
    parentId = parent.parentId;
  }
  const seen = new Set<string>([subject.id]);
  const queue = [subject.id];
  while (queue.length > 0) {
    for (const child of childrenOf(queue.shift()!)) {
      if (seen.has(child.id)) continue;
      seen.add(child.id);
      if (ownAfter(child) !== "") return child;
      queue.push(child.id);
    }
  }
  return null;
}

/**
 * The refusal the server's checks of an assignment's target would give, as the
 * store last read shows that target, or null when they would pass.
 */
function targetRefusal(
  target: string,
  targetName: string,
  assigns: readonly string[],
  lookup: MembershipLookup,
  statusOf: (id: string) => string | undefined,
  nameOf: RegionNamer,
): DropPlan | null {
  const milestone = lookup(target);
  if (!milestone) {
    return refuse("unknown-destination", `The panel cannot find ${targetName} in the store, so it cannot check that the milestone takes this drop.`);
  }
  if (milestone.type !== MILESTONE_TYPE) return refuse("entry-refused", `${targetName} is not a milestone.`);
  const milestoneStatus = statusOf(target) ?? "";
  // A subject missing from the store counts as open, the case the server refuses.
  const open = assigns.find((id) => !milestoneAcceptsAssignment(milestoneStatus, statusOf(id) ?? ""));
  if (open === undefined) return null;
  return refuse("entry-refused", `${spellId(open, nameOf)} cannot join ${targetName}, which is ${milestoneStatus} and takes no more open work.`);
}

/** Whether the subject's parent chain leads back to the subject. */
function inParentCycle(subject: MembershipNib, lookup: MembershipLookup): boolean {
  const visited = new Set<string>();
  let parentId = subject.parentId;
  while (parentId !== null && !visited.has(parentId)) {
    if (parentId === subject.id) return true;
    visited.add(parentId);
    parentId = lookup(parentId)?.parentId ?? null;
  }
  return false;
}

/** Whether any ancestor of the subject is in the dragged set. */
function hasDraggedAncestor(subject: MembershipNib, dragged: ReadonlySet<string>, lookup: MembershipLookup): boolean {
  const visited = new Set<string>([subject.id]);
  let parentId = subject.parentId;
  while (parentId !== null && !visited.has(parentId)) {
    if (dragged.has(parentId)) return true;
    visited.add(parentId);
    parentId = lookup(parentId)?.parentId ?? null;
  }
  return false;
}

/** The nearest ancestor holding a resolved milestone assignment, or null when none does. */
function assignedAncestor(subject: MembershipNib, lookup: MembershipLookup): MembershipNib | null {
  const visited = new Set<string>([subject.id]);
  let parentId = subject.parentId;
  while (parentId !== null && !visited.has(parentId)) {
    visited.add(parentId);
    const parent = lookup(parentId);
    if (!parent) return null;
    if (resolvedMilestoneId(parent, lookup) !== "") return parent;
    parentId = parent.parentId;
  }
  return null;
}

/** Open and closed counts of the nibs in one milestone's plan. */
export interface MilestoneCount {
  open: number;
  closed: number;
}

/**
 * Returns each milestone's whole-plan counts, keyed by milestone id.
 *
 * Membership is derived over a lookup built from `nibs`, which must be the whole
 * store for the counts to be the server's. Milestones themselves are not
 * counted. A status in `CLOSED_STATUSES` counts as closed, `deferred` included.
 */
export function milestoneCounts(nibs: readonly (MembershipNib & { readonly status: string })[]): Map<string, MilestoneCount> {
  const byId = new Map(nibs.map((n) => [n.id, n]));
  const lookup: MembershipLookup = (id) => byId.get(id);
  const counts = new Map<string, MilestoneCount>();
  for (const n of nibs) {
    if (!takesAssignmentAxes(n.type)) continue;
    const milestone = milestoneOf(n, lookup);
    if (milestone === "") continue;
    let count = counts.get(milestone);
    if (count === undefined) {
      count = { open: 0, closed: 0 };
      counts.set(milestone, count);
    }
    if (CLOSED_STATUSES.includes(n.status)) count.closed++;
    else count.open++;
  }
  return counts;
}
