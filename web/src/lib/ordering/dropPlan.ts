import type { DropZone } from "../drag.svelte";
import { isValidCrossParentDrop, isValidDropTarget } from "../dropZone";
import { batch, reorderChain, reorderNib, reparentAndReorder, sequence, setParent, updateNib } from "../mutations/commands";
import type { AnyCommand, CommandResult, LeafCommand, SequenceStep } from "../mutations/types";
import type { ContainmentIndex } from "../containment";
import { takesAssignmentAxes } from "../membership";
import type { RowData } from "../tableData";
import type { SectionKey } from "../tree";
import { canHaveChildren } from "../typeHierarchy";
import { BY_ID, commonRegion, describeRegion, sameRegion, scopeOf, spellId, type Region, type RegionNamer } from "./region";
import type { AssignableField, SectionEntry } from "./sectionMeaning";

/**
 * What the drop indicator draws. "into" joins the target's group rather than
 * taking a position beside the target.
 */
export type DropIndicator = "before" | "after" | "into";

export type DropRefusalReason =
  /** Nothing is being dragged. */
  | "no-source"
  /** A dragged id has no row in the current view. */
  | "hidden-member"
  /** The dragged rows sit in more than one ordering group. */
  | "mixed-source"
  /** A dragged row is in no ordering group at all — a fabricated container. */
  | "unorderable-source"
  /** The target is a fabricated container, so it names no nib to anchor on. */
  | "unorderable-target"
  | "drop-on-self"
  | "drop-on-descendant"
  /** The type hierarchy refuses the dragged types inside the destination. */
  | "invalid-parent-type"
  /** The destination container has no nib in this response, so its type rules
   *  cannot be checked. */
  | "unknown-destination"
  /** The target is drawn in the destination group but is not a member of it. */
  | "anchor-not-in-destination"
  /** The destination container is drawn inside the target's own subtree, so the
   *  rows would land below the row the indicator points at. */
  | "destination-inside-target"
  /** Expressible only by joining a milestone queue first. */
  | "needs-assignment"
  /** Expressible only by clearing a milestone assignment first. */
  | "needs-unassignment"
  /** The rows would land in a different section, and one side of that boundary
   *  decides membership by an assignment, not a position. */
  | "crosses-section"
  /** The dragged types take no assignment on either membership axis. */
  | "unassignable-type"
  /** A reorder beside a row a dragged row is drawn apart from, in an ordering
   *  group they share — a write no view can show. */
  | "position-across-sections"
  /** The rows are already in the section the drop names. */
  | "already-in-section"
  /** The section refuses entry, and carries the sentence saying why. */
  | "entry-refused";

/**
 * Toast id shared by every drop refusal, so repeated refused releases replace
 * the live toast instead of stacking copies (svelte-sonner dedupes by id).
 */
export const DROP_REFUSAL_TOAST_ID = "drop-refusal";

export interface DropRefusal {
  reason: DropRefusalReason;
  message: string;
  /** The group the gesture aimed at, on refusals that name one. For
   *  `needs-assignment` it is the queue to join. */
  region?: Region;
  /** The separate write that would make the gesture expressible. Set only
   *  together with `actionCommand`; read the pair through `refusalAction`. */
  actionLabel?: string;
  /** The write `actionLabel` offers, built from this plan's own anchor and
   *  indicator. */
  actionCommand?: AnyCommand;
}

/** The remedy a refusal offers as a label and command, or null when it offers none. */
export function refusalAction(refusal: DropRefusal): { label: string; command: AnyCommand } | null {
  const { actionLabel, actionCommand } = refusal;
  if (actionLabel === undefined || actionCommand === undefined) return null;
  return { label: actionLabel, command: actionCommand };
}

/**
 * What an accepted drop does. A `position` plan moves rows within the ordering
 * group it names. An `assign` plan sets a field, landing in a section with no
 * ordering group, so it carries no `region` — keep the arms separate rather
 * than making `region` nullable, which surfaces coloring by axis would read as
 * the parent axis.
 *
 * The assign arm's indicator is fixed at "into": an assignment writes no position.
 */
export type DropPlan =
  | { ok: true; kind: "position"; region: Region; indicator: DropIndicator; label: string; command: AnyCommand }
  | {
      ok: true;
      kind: "assign";
      assignment: { field: AssignableField; value: string };
      indicator: "into";
      label: string;
      command: AnyCommand;
    }
  | { ok: false; refusal: DropRefusal };

export interface DropRequest {
  /** The ids being dragged, in selection order. */
  readonly draggedIds: string[];
  /** The rendered rows, by id. Live, so a row arriving mid-drag can be aimed at. */
  readonly rowsById: ReadonlyMap<string, RowData>;
  /** The dragged rows as they were at grab time. Frozen, so a dragged row that
   *  scrolls out of view mid-gesture does not answer `hidden-member`. */
  readonly draggedRowsById: ReadonlyMap<string, RowData>;
  /** The row under the cursor. */
  readonly target: RowData;
  /** What `computeDropZone` read off the cursor, before container promotion. */
  readonly zone: DropZone;
  /** `collectDescendantIds(draggedIds, rows)`, cached for the drag's lifetime. */
  readonly descendantIds: Set<string>;
  /** What the view draws inside what — asked whether the destination container
   *  is drawn inside the target row. */
  readonly containment: ContainmentIndex;
  /**
   * Spells the ids in this plan's prose as titles. Supplied rather than read off
   * `rowsById`: a lens-declared region can name a container with no row.
   * Omitted, ids are spelled as ids (`BY_ID`).
   */
  readonly nameOf?: RegionNamer;
}

/**
 * The ordering group a drop INTO this row joins, or null when entering it joins
 * none.
 *
 * A section's `region` answer is returned; `assign` and `refuse` return null
 * and are handled by `planDrop` first. A `byRow` section, and a row drawing no
 * section, fall back to the type hierarchy. `canHaveChildren` is false for a
 * milestone, so the fallback never promotes a queue header's edge.
 *
 * A property of the row only: whether the dragged rows may join is `planDrop`'s
 * question.
 */
export function entryRegionOf(row: RowData): Region | null {
  const entry = row.drawsSection?.onEnter;
  if (entry !== undefined) {
    switch (entry.kind) {
      case "region":
        return entry.region;
      case "assign":
      case "refuse":
        return null;
      case "byRow":
        break;
    }
  }
  return canHaveChildren(row.nib.type) ? { axis: "parent", parentId: row.nib.id } : null;
}

/**
 * The one decision a drag makes: what the affordance shows and what the drop
 * writes, as a single value.
 *
 * Total and pure, so a caller can compute it on pointermove for the indicator
 * and again on pointerup for the mutation. Only `nameOf` can answer differently
 * between two calls, which changes a plan's wording, not its decision.
 */
export function planDrop(req: DropRequest): DropPlan {
  // Defaulted here so every phrase below takes a required namer.
  const { draggedIds, rowsById, draggedRowsById, target, zone, descendantIds, containment, nameOf = BY_ID } = req;

  if (draggedIds.length === 0) {
    return refuse("no-source", "Nothing is being dragged.");
  }

  const dragged: RowData[] = [];
  for (const id of draggedIds) {
    const row = draggedRowsById.get(id);
    if (row === undefined) {
      // The selection survives a filter change, so a selected row can be absent
      // from the view.
      return refuse(
        "hidden-member",
        `${spellId(id, nameOf)} is selected but not shown here — clear the filter (or expand the parent) hiding it, or drop it from the selection.`,
      );
    }
    dragged.push(row);
  }

  const draggedTypes = dragged.map((r) => r.nib.type);

  // A fabricated container names no nib for any write, so this precedes every
  // other check, the section branch included.
  const unorderable = dragged.find((r) => r.region === null);
  if (unorderable !== undefined) {
    return refuse(
      "unorderable-source",
      `The ${unorderable.nib.title} section is a container the view drew, not a nib, so it has no position to move.`,
    );
  }

  // The middle band on a section row asks the SECTION what entry means, not the
  // row's type. Asked before `unorderable-target`, which a declared, assigning
  // section must not get, and before the shared-group check, because an
  // assignment or an entry refusal positions nothing.
  const drawn = target.drawsSection;
  if (drawn !== null && zone === "reparent") {
    switch (drawn.onEnter.kind) {
      case "assign":
        return planAssignment(drawn.onEnter, drawn.key, dragged, draggedIds, nameOf);
      case "refuse":
        return refuse("entry-refused", drawn.onEnter.message);
      case "region":
      case "byRow":
        break;
    }
  }

  // Every plan from here down positions rows within one group.
  const source = commonRegion(dragged.map((r) => r.region));
  if (source === null) {
    return refuse(
      "mixed-source",
      `These rows are in different ordering groups (${listRegions(dragged, nameOf)}), and one move positions rows within a single group.`,
    );
  }

  if (target.region === null) {
    return refuse(
      "unorderable-target",
      `The ${target.nib.title} section is a container the view drew, not a nib — drop onto a row inside it instead.`,
    );
  }

  // `isValidDropTarget`'s before/after arm is exactly the zone-independent
  // guards: self, own subtree, fabricated target. Its "reparent" arm checks the
  // TARGET's type, which is not always the destination's, so the type question
  // is asked further down against the destination; a queue destination skips it,
  // since joining a queue changes no parent link.
  //
  // Asked before the destination is worked out: releasing on the grabbed row is
  // a canceled drag, and should not be reported as a refused destination.
  if (!isValidDropTarget(draggedTypes, target.nib, "before", draggedIds, descendantIds)) {
    // `isValidDropTarget` decides whether to refuse; this only picks the message.
    return draggedIds.includes(target.nib.id)
      ? refuse("drop-on-self", "A nib cannot be dropped onto itself.")
      : refuse("drop-on-descendant", "A nib cannot be moved into its own subtree.");
  }

  // A milestone-axis entry the dragged types can never join is no entry, so the
  // row's edges keep their positional meaning: a milestone header's bottom edge
  // stays a sibling reorder, and its middle refuses as a type question.
  // `takesAssignmentAxes` mirrors `nibtypes.RefusedAxes`. Parent-axis entry is
  // checked below by `isValidCrossParentDrop`.
  const declaredEntry = entryRegionOf(target);
  const entry =
    declaredEntry !== null && declaredEntry.axis === "milestone" && !draggedTypes.every(takesAssignmentAxes)
      ? null
      : declaredEntry;
  // A container's bottom edge means "enter it", like its middle: below an
  // expanded container is where its first row sits. The exception is the queue
  // the dragged rows are already in: there a co-member's bottom edge is an
  // in-queue reorder, and promoting it would make the destination parent-axis,
  // which is then refused.
  const reordersInSourceQueue = source.axis === "milestone" && sameRegion(source, target.region);
  let indicator: DropIndicator;
  let dest: Region;
  if (zone === "before" || (zone === "after" && (entry === null || reordersInSourceQueue))) {
    indicator = zone;
    dest = target.region;
  } else if (entry === null) {
    return refuse("invalid-parent-type", `Cannot drop into ${withArticle(target.nib.type)}: it holds no children.`);
  } else {
    indicator = "into";
    dest = entry;
  }

  const dragParentId = sharedParentId(dragged);

  // Before the cross-axis checks: a destination the type hierarchy refuses stays
  // refused on either axis, so do not prescribe clearing an assignment for it.
  if (dest.axis === "parent" && dragParentId !== dest.parentId) {
    const container = destContainerType(dest.parentId, target, rowsById);
    if (!container.known) {
      return refuse(
        "unknown-destination",
        `This view does not carry ${describeRegion(dest, nameOf)}, so whether it can hold ${listTypes(draggedTypes)} cannot be decided here.`,
        { region: dest },
      );
    }
    if (!isValidCrossParentDrop(draggedTypes, container.type)) {
      return refuse("invalid-parent-type", `Cannot put ${listTypes(draggedTypes)} in ${describeRegion(dest, nameOf)}.`, {
        region: dest,
      });
    }
  }

  // A drop landing the rows in a different section, where either side decides
  // membership by a field, cannot be expressed by a position write: the row
  // would stay drawn in its old section. Checked in both directions (into and
  // out of an assigning section) and for every indicator, so the three bands of
  // one row agree. After the type check, whose refusal survives an assignment.
  const crossed = target.section;
  const crossedKey = crossed?.key ?? null;
  // The set of sections the dragged rows are in: a drag spanning two sections
  // crosses a boundary whatever the destination.
  const homeKeys = new Set(dragged.map((r) => r.section?.key ?? null));
  const crossesSections = homeKeys.size > 1 || !homeKeys.has(crossedKey);
  if (crossesSections) {
    // A `refuse` destination must not fall through to a position write. This
    // switch is not exhaustive: a new `SectionEntry` kind falls through silently.
    switch (crossed?.onEnter.kind) {
      case "refuse":
        return refuse("entry-refused", crossed.onEnter.message);
      // No section, or one that does not assign: only the departure side below
      // can refuse.
      case undefined:
      case "region":
      case "byRow":
        break;
      case "assign": {
        const joining = crossed.onEnter;
        // Type first, so an unassignable subject is not offered an assignment.
        if (!draggedTypes.every(takesAssignmentAxes)) {
          return refuse("unassignable-type", `Cannot put ${listTypes(draggedTypes)} in ${nameSection(joining)}.`);
        }
        // `assignmentFor` supplies subject and command together, so the sentence
        // names exactly the rows written; some dragged rows may already be in the
        // destination. Its null (every row already there) cannot reach this
        // branch, since those rows would not cross sections.
        const write = assignmentFor(joining, crossed.key, dragged);
        if (write !== null) {
          return refuse(
            "crosses-section",
            `${subjectIs(write.ids, nameOf)} not in ${nameSection(joining)}, and joining one is an assignment rather than a move.`,
            { region: dest, action: { label: assignLabel(joining), command: write.command } },
          );
        }
        break;
      }
    }
    const leaving = leavingAssigned(dragged, crossedKey);
    if (leaving.sections.length > 0) {
      // No remedy: the destination declares no write for entering it.
      return refuse("crosses-section", leavingMessage(leaving, dragged.length, nameOf), { region: dest });
    }
  }

  // A reorder beside a row a dragged row is drawn apart from, in an ordering
  // group they still share: the separator promises a place among rows it is never
  // drawn among. In the Milestones view an unparented Backlog row and a milestone
  // header both order in the root parent group while drawn in different sections.
  //
  // `reordersOnly` holds when the arms below would write only an order key. A
  // reparent changes containment, which the view draws, and `into` draws no
  // separator, so both pass. The anchor-parent clause leaves a non-member anchor
  // to `anchor-not-in-destination`. Asked per selection: rows disagreeing on
  // parent take the reparent arm, and one keeping its container gets an invisible
  // reorder — unreachable while no shipped lens declares a parent-axis
  // `memberRegion`; a lens that does must revisit this per row.
  const reordersOnly =
    sameRegion(source, dest) &&
    (dest.axis === "milestone" || (dragParentId === dest.parentId && dest.parentId === target.nib.parentId));
  if (crossesSections && indicator !== "into" && reordersOnly) {
    // Only the rows drawn apart from the anchor; a straddling selection also holds
    // rows that are not. Non-empty whenever `crossesSections` holds.
    const apart = dragged.filter((r) => (r.section?.key ?? null) !== crossedKey).map((r) => r.nib.id);
    return refuse(
      "position-across-sections",
      // The anchor row, not its section: a header row is a member of no section.
      `${subjectIs(apart, nameOf)} not drawn in the same section as ${target.nib.title}, and a reorder positions a row only among the rows it is drawn with.`,
      { region: dest },
    );
  }

  const anchorId = target.nib.id;

  if (!sameRegion(source, dest)) {
    if (dest.axis === "milestone") {
      // Aiming AT the queue asks for the assignment, so the drop is accepted.
      // `into` reaches here only from the milestone header row: a member row
      // draws no section, and a queued epic's entry is parent-axis. The entry
      // gate above has already nulled this entry for unassignable types.
      //
      // A `position` plan, because the queue orders the rows it receives. It
      // names `first` because `Orderer.Move` has no default placement.
      if (indicator === "into") {
        return {
          ok: true,
          kind: "position",
          region: dest,
          indicator,
          label: `Assign to ${describeRegion(dest, nameOf)}`,
          command: assignAndPlace(draggedIds, dest, queueLead(indicator, anchorId)),
        };
      }

      // The before/after path never passes the entry gate, so check assignability
      // again: `nibtypes.ValidateAxes` refuses assigning a milestone.
      return refuse(
        "needs-assignment",
        `${subjectIs(draggedIds, nameOf)} not in ${describeRegion(dest, nameOf)}, and joining one is an assignment rather than a move.`,
        {
          region: dest,
          action: draggedTypes.every(takesAssignmentAxes)
            ? {
                label: `Assign to ${spellId(dest.milestoneId, nameOf)}`,
                command: assignAndPlace(draggedIds, dest, queueLead(indicator, anchorId)),
              }
            : undefined,
        },
      );
    }
    if (source.axis === "milestone") {
      // While the rows are ordered in a queue, only a queue move changes where
      // they are drawn.
      return refuse(
        "needs-unassignment",
        `${subjectIs(draggedIds, nameOf)} ordered in ${describeRegion(source, nameOf)}, so clear the milestone assignment before ordering in ${describeRegion(dest, nameOf)}.`,
        { region: dest },
      );
    }
  }

  switch (dest.axis) {
    case "milestone":
      // Reached only when the source is already this queue.
      return {
        ok: true,
        kind: "position",
        region: dest,
        indicator,
        label:
          indicator === "into"
            ? `Move to the front of ${describeRegion(dest, nameOf)}`
            : `Reorder in ${describeRegion(dest, nameOf)}`,
        command: queueMove(draggedIds, dest, queueLead(indicator, anchorId)),
      };
    case "parent": {
      if (indicator === "into") {
        return {
          ok: true,
          kind: "position",
          region: dest,
          indicator,
          // A lens-declared entry region can name a container other than the target row.
          label: dest.parentId === target.nib.id ? `Move under ${spellId(anchorId, nameOf)}` : `Move into ${describeRegion(dest, nameOf)}`,
          // `setParent` carries no position, so the server places the row at its
          // default (`defaultPlace` in orderer.go). Not `reparentBatch`: its
          // parentId cannot be null.
          command: batch(draggedIds.map((id) => setParent(id, dest.parentId))),
        };
      }

      // The anchor must be a server member of the destination group; `region`
      // only says where the view draws it, and a lens-declared region can differ.
      // The server refuses a non-sibling anchor even after a reparent.
      if (dest.parentId !== target.nib.parentId) {
        return refuse(
          "anchor-not-in-destination",
          `${target.nib.title} is shown in ${describeRegion(dest, nameOf)} but is not a member of it, so nothing can be positioned against it.`,
          { region: dest },
        );
      }

      if (dragParentId === dest.parentId) {
        return {
          ok: true,
          kind: "position",
          region: dest,
          indicator,
          label: `Reorder in ${describeRegion(dest, nameOf)}`,
          // No `scope`: PARENT is the server default. No `parentId`: a PARENT
          // reorder groups by the subject's own resolved parent, which this branch
          // has confirmed is `dest.parentId`.
          command:
            draggedIds.length === 1
              ? reorderNib(draggedIds[0], anchor(indicator, anchorId))
              : reorderChain(draggedIds, anchorId, indicator),
        };
      }

      // A reparent positioned against the target — unless the destination
      // container is the target itself or inside its subtree in the view tree: a
      // severed cycle member (`promotedCycleRoots`), or a section header parented
      // under one of its own members. The server accepts that write, but the rows
      // land below the line they were dropped on. Read off the view tree, so a
      // collapsed section still refuses; a container with no node (a promoted
      // header's) answers false. The identity check covers a self-parented nib,
      // since `contains` excludes the container itself.
      if (dest.parentId !== null && (dest.parentId === target.nib.id || containment.contains(target.nib.id, dest.parentId))) {
        return refuse(
          "destination-inside-target",
          `${describeRegion(dest, nameOf)} is drawn inside ${target.nib.title}, so the drop would land below the row it points at.`,
          { region: dest },
        );
      }
      return {
        ok: true,
        kind: "position",
        region: dest,
        indicator,
        label: `Move into ${describeRegion(dest, nameOf)}`,
        command: reparentAndReorder(draggedIds, dest.parentId, anchorId, indicator),
      };
    }
  }
}

function refuse(
  reason: DropRefusalReason,
  message: string,
  extra: { region?: Region; action?: { label: string; command: AnyCommand } } = {},
): DropPlan {
  const refusal: DropRefusal = { reason, message };
  if (extra.region !== undefined) refusal.region = extra.region;
  if (extra.action !== undefined) {
    refusal.actionLabel = extra.action.label;
    refusal.actionCommand = extra.action.command;
  }
  return { ok: false, refusal };
}

function anchor(indicator: "before" | "after", anchorId: string): { beforeId: string } | { afterId: string } {
  return indicator === "before" ? { beforeId: anchorId } : { afterId: anchorId };
}

/** Where a drop lands inside a queue: the position it pointed at, or the front
 *  for an entry. */
type QueueLead = { first?: boolean; beforeId?: string; afterId?: string };

function queueLead(indicator: DropIndicator, anchorId: string): QueueLead {
  return indicator === "into" ? { first: true } : anchor(indicator, anchorId);
}

/**
 * Positions a run of nibs within one queue: the first against the drop's anchor,
 * each next after the nib the previous step returned. Not `reorderChain`, which
 * takes no `scope`.
 *
 * The non-empty tuple keeps the lead step a `LeafCommand` without a cast.
 */
function queueMoveSteps(
  ids: string[],
  region: Region,
  lead: QueueLead,
): [LeafCommand, ...SequenceStep[]] {
  const scope = scopeOf(region);
  const [first, ...rest] = ids;
  return [
    reorderNib(first, { ...lead, scope }),
    ...rest.map(
      (id) => (prev: CommandResult) => reorderNib(id, { afterId: prev.data?.reorderNib?.id, scope }),
    ),
  ];
}

function queueMove(ids: string[], region: Region, lead: QueueLead): AnyCommand {
  const [head, ...rest] = queueMoveSteps(ids, region, lead);
  return rest.length === 0 ? head : sequence([head, ...rest]);
}

/**
 * The write a `needs-assignment` refusal offers: join the queue, then take the
 * position the drop pointed at — an assignment alone places the row last.
 *
 * A `sequence`, because a MILESTONE reorder is refused until the row's
 * assignment has landed. Interleaved per row, because the dispatcher stops a
 * sequence at its first failure: rows before a failing one are then already
 * positioned. Each row anchors on the previous dragged id rather than the
 * previous step's result, which is that row's `updateNib`.
 */
function assignAndPlace(
  ids: string[],
  dest: Extract<Region, { axis: "milestone" }>,
  lead: QueueLead,
): AnyCommand {
  const scope = scopeOf(dest);
  return sequence(
    ids.flatMap((id, i) => [
      updateNib(id, { milestone: dest.milestoneId }),
      reorderNib(id, i === 0 ? { ...lead, scope } : { afterId: ids[i - 1], scope }),
    ]),
  );
}

/** The one `SectionEntry` arm that carries a write. */
type AssignEntry = Extract<SectionEntry, { kind: "assign" }>;

/**
 * The section as a noun phrase ("the web/dashboard area"), built from the value
 * the write sets rather than the declared label.
 */
function nameSection(entry: AssignEntry): string {
  return `the ${entry.value} ${entry.noun}`;
}

/** The label for an assignment write, shared by the accepted plan and the
 *  `crosses-section` remedy. */
function assignLabel(entry: AssignEntry): string {
  return `Move to ${nameSection(entry)}`;
}

/**
 * The rows an assignment to this section would change — those not already in
 * it — with the write for exactly those rows, or null when there are none.
 * Returned together so a caller's sentence names the rows the batch writes.
 *
 * A `batch`, not a `sequence`: no row's write depends on another's.
 */
function assignmentFor(entry: AssignEntry, key: SectionKey, dragged: RowData[]): { ids: string[]; command: AnyCommand } | null {
  const ids = dragged.filter((r) => r.section?.key !== key).map((r) => r.nib.id);
  if (ids.length === 0) return null;
  // TypeScript does not check a computed key against the object it lands in;
  // `AssignableField` is the check.
  return { ids, command: batch(ids.map((id) => updateNib(id, { [entry.field]: entry.value }))) };
}

/**
 * The plan for a drop onto an assigning section, or its refusal.
 * `takesAssignmentAxes` gates the area axis as it gates the milestone axis.
 */
function planAssignment(
  entry: AssignEntry,
  key: SectionKey,
  dragged: RowData[],
  draggedIds: string[],
  nameOf: RegionNamer,
): DropPlan {
  const draggedTypes = dragged.map((r) => r.nib.type);
  if (!draggedTypes.every(takesAssignmentAxes)) {
    return refuse("unassignable-type", `Cannot put ${listTypes(draggedTypes)} in ${nameSection(entry)}.`);
  }
  const write = assignmentFor(entry, key, dragged);
  if (write === null) {
    return refuse("already-in-section", `${subjectIs(draggedIds, nameOf)} already in ${nameSection(entry)}.`);
  }
  return {
    ok: true,
    kind: "assign",
    assignment: { field: entry.field, value: entry.value },
    indicator: "into",
    label: assignLabel(entry),
    command: write.command,
  };
}

/**
 * The dragged rows whose own section assigns and is not the destination, and
 * the distinct sections they leave. A list, because the dragged rows need not
 * share a section.
 */
function leavingAssigned(rows: RowData[], crossedKey: SectionKey | null): { ids: string[]; sections: AssignEntry[] } {
  const ids: string[] = [];
  const sections = new Map<SectionKey, AssignEntry>();
  for (const r of rows) {
    const section = r.section;
    if (section === null || section.key === crossedKey || section.onEnter.kind !== "assign") continue;
    ids.push(r.nib.id);
    sections.set(section.key, section.onEnter);
  }
  return { ids, sections: [...sections.values()] };
}

/**
 * The sentence for rows leaving assigning sections. Several sections read
 * "spread across", since "in A and B" would say each row is in both.
 */
function leavingMessage(
  leaving: { ids: string[]; sections: AssignEntry[] },
  draggedCount: number,
  nameOf: RegionNamer,
): string {
  // Name only the leaving rows, spelling the count where "The N dragged nibs"
  // would include rows that stay put.
  const who =
    leaving.ids.length > 1 && leaving.ids.length < draggedCount
      ? `${leaving.ids.length} of the ${draggedCount} dragged nibs are`
      : subjectIs(leaving.ids, nameOf);
  const where =
    leaving.sections.length === 1
      ? `${who} in ${nameSection(leaving.sections[0])}`
      : `${who} spread across ${listSections(leaving.sections)}`;
  return `${where}, and leaving one is an assignment rather than a move.`;
}

/** The sections as one noun phrase, capped like `listRegions`. */
function listSections(entries: AssignEntry[]): string {
  const names = entries.map(nameSection);
  if (names.length <= 3) return names.join(" and ");
  return `${names.slice(0, 3).join(", ")} and ${names.length - 3} more`;
}

/**
 * The parent every dragged row shares, or `undefined` when they disagree.
 * `null` is a real answer — the root group.
 */
function sharedParentId(rows: RowData[]): string | null | undefined {
  const first = rows[0].nib.parentId;
  return rows.every((r) => r.nib.parentId === first) ? first : undefined;
}

/**
 * What a container's type question can be answered with: a type, "nothing
 * constrains this" (the root group), or "this view cannot say".
 */
type ContainerType = { known: true; type: string | null } | { known: false };

/**
 * The type of the container a parent-axis destination names. The root group is
 * `known` with `type: null`, which `isValidCrossParentDrop` reads as
 * unconstrained; a container absent from this response is `known: false` and
 * must not be treated as the root.
 */
function destContainerType(
  parentId: string | null,
  target: RowData,
  rowsById: ReadonlyMap<string, RowData>,
): ContainerType {
  if (parentId === null) return { known: true, type: null };
  // Read off the target, so this does not depend on the target being in `rowsById`.
  if (parentId === target.nib.id) return { known: true, type: target.nib.type };
  // `parentNib` covers a container the lens gave no row to, but a filter can
  // exclude the parent from the response, leaving it null.
  if (parentId === target.nib.parentId && target.parentNib !== null) {
    return { known: true, type: target.parentNib.type };
  }
  const row = rowsById.get(parentId);
  return row === undefined ? { known: false } : { known: true, type: row.nib.type };
}

function listRegions(rows: RowData[], nameOf: RegionNamer): string {
  const names = [...new Set(rows.map((r) => (r.region === null ? "no ordering group" : describeRegion(r.region, nameOf))))];
  // Capped: a selection survives select-all, and this is one line in a message.
  if (names.length <= 3) return names.join(" and ");
  return `${names.slice(0, 3).join(", ")} and ${names.length - 3} more`;
}

function listTypes(types: string[]): string {
  return [...new Set(types)].join(" and ");
}

/** The dragged set as a sentence subject, verb agreed, its id spelled through `nameOf`. */
function subjectIs(ids: string[], nameOf: RegionNamer): string {
  return ids.length === 1 ? `${spellId(ids[0], nameOf)} is` : `The ${ids.length} dragged nibs are`;
}

function withArticle(word: string): string {
  return `${/^[aeiou]/i.test(word) ? "an" : "a"} ${word}`;
}
