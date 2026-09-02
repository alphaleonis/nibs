import type { DropZone } from "../drag.svelte";
import { isValidCrossParentDrop, isValidDropTarget } from "../dropZone";
import { batch, reorderChain, reorderNib, reparentAndReorder, sequence, setParent, updateNib } from "../mutations/commands";
import type { AnyCommand, CommandResult, LeafCommand, SequenceStep } from "../mutations/types";
import { takesAssignmentAxes } from "../membership";
import type { RowData } from "../tableData";
import type { SectionKey } from "../tree";
import { canHaveChildren } from "../typeHierarchy";
import { BY_ID, commonRegion, describeRegion, sameRegion, scopeOf, spellId, type Region, type RegionNamer } from "./region";
import type { AssignableField, SectionEntry } from "./sectionMeaning";

/**
 * What the drop indicator draws, and what the drop means. "into" is the
 * container-entry case: the dragged rows join the target's group rather than
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
  /** The destination container has no nib in this response, so whether it may
   *  hold the dragged types cannot be decided here. */
  | "unknown-destination"
  /** The target is DRAWN in the destination group but is not a member of it, so
   *  nothing can be positioned against it. */
  | "anchor-not-in-destination"
  /** The destination container is drawn inside the target's own subtree, so the
   *  rows would land below the row the indicator points at. */
  | "destination-inside-target"
  /** Expressible only by joining a milestone queue first. */
  | "needs-assignment"
  /** Expressible only by clearing a milestone assignment first. */
  | "needs-unassignment"
  /** A position or reparent that would land the rows in a section other than
   *  the one (or ones) they are in, where either side of that boundary decides
   *  membership by an assignment and not by a position. */
  | "crosses-section"
  /** The dragged types take no assignment on either membership axis. */
  | "unassignable-type"
  /** The rows are already in the section the drop names, so it writes nothing. */
  | "already-in-section"
  /** The section says entering it is meaningless, and carries the sentence. */
  | "entry-refused";

/**
 * Toast id shared by every drop refusal, so a run of refused releases replaces
 * the live toast instead of stacking up copies (svelte-sonner dedupes by id and
 * restarts the dismissed timer on update). Mirrors `DRAG_BLOCK_TOAST_ID`, whose
 * gesture this one continues: a refused drop is what a drag that was NOT blocked
 * can still end in, and retrying slightly differently is the natural response.
 */
export const DROP_REFUSAL_TOAST_ID = "drop-refusal";

export interface DropRefusal {
  reason: DropRefusalReason;
  message: string;
  /**
   * The group the gesture aimed at, on every refusal decided once a destination
   * is known. Carried as data because the caller that renders the remedy needs
   * the id in it — recovering it means reading the same drag a second time,
   * which is the disagreement this module exists to prevent. For
   * `needs-assignment` it is the queue to join.
   */
  region?: Region;
  /** Names the separate write that would make the gesture expressible, when
   *  there is one — so a refusal leads somewhere instead of just saying no.
   *  Never present without `actionCommand`: `refuse` takes the two as one
   *  argument, so a label with nothing behind it cannot be produced. */
  actionLabel?: string;
  /**
   * The write `actionLabel` offers, built here for the reason an accepted plan's
   * `command` is: the anchor and indicator the remedy has to honor are this
   * function's own reading of the drag, and deriving them again anywhere else is
   * a second reading. Read it through `refusalAction`, which takes the pair.
   */
  actionCommand?: AnyCommand;
}

/**
 * The remedy a refusal offers, or null when it offers none — the one place the
 * label and the write behind it are read as the pair they are, so no caller can
 * draw a button with nothing behind it.
 */
export function refusalAction(refusal: DropRefusal): { label: string; command: AnyCommand } | null {
  const { actionLabel, actionCommand } = refusal;
  if (actionLabel === undefined || actionCommand === undefined) return null;
  return { label: actionLabel, command: actionCommand };
}

/**
 * What an accepted drop DOES, as two arms rather than one arm with a nullable
 * `region`.
 *
 * A `position` plan moves rows within an ordering group, so it always names
 * one. An `assign` plan sets a field: the section it lands in has no ordering
 * axis of its own, and there is no group to name. A nullable `region` would
 * assert one by omission, and the styling reads it through `?.`: `isQueueAxis`
 * answers `false` for a missing axis, so an assignment would render in the
 * parent axis's colors at both surfaces that color a drop.
 *
 * The assign arm's indicator is FIXED at "into" rather than carried as data:
 * "into" is the only thing an assignment can draw, and carrying it invites a
 * caller to draw an edge line promising a position it never writes.
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
  /** The rows the table renders, by id — LIVE, so a row arriving mid-drag can be
   *  aimed at. */
  readonly rowsById: ReadonlyMap<string, RowData>;
  /**
   * The dragged rows, by id, as they were when the gesture picked them up.
   *
   * Frozen where `rowsById` is live, because what is being dragged is settled at
   * grab time: resolving it against the live list would make a dragged row that
   * scrolls out of the view mid-gesture look like a selection the filter hides,
   * and the whole rest of the drag would answer `hidden-member`.
   */
  readonly draggedRowsById: ReadonlyMap<string, RowData>;
  /** The row under the cursor. */
  readonly target: RowData;
  /** What `computeDropZone` read off the cursor, before container promotion. */
  readonly zone: DropZone;
  /** `collectDescendantIds(draggedIds, rows)` — a drag-lifetime cache, so this
   *  function is not O(rows) per pointermove. */
  readonly descendantIds: Set<string>;
  /**
   * Spells the ids inside this plan's own prose — the queue a move stays in, the
   * container it enters — as titles.
   *
   * Supplied rather than read off `rowsById`, which holds only rows the table
   * drew: a lens-DECLARED region can name a container with no row at all, and
   * the drag path already keeps a title map folding those in from the rows'
   * `parentNib` (the same route `destContainerType` takes for their type).
   * Building one here would be that work on every pointermove.
   *
   * Omitting it leaves every phrase on ids (`BY_ID`), which is what a caller
   * with nothing loaded should say.
   */
  readonly nameOf?: RegionNamer;
}

/**
 * The ordering group a drop INTO this row lands in, or null when entering it
 * joins no group.
 *
 * A section says what entering it means, and only the `region` arm of that
 * answer is an ordering group: the two arms that are not — an assignment, and a
 * refusal — return null here and are answered by `planDrop` before it asks. A
 * row that draws no section, and a section answering `byRow`, fall through to
 * the type hierarchy, which is what leaves "drop below an epic to make it a
 * child" expressible in every view. `canHaveChildren` is false for a milestone
 * (`VALID_CHILD_TYPES.milestone` is `[]`), so the fallback cannot promote a
 * queue header's edge on its own.
 *
 * A property of the ROW, and only that. Whether the rows being dragged could
 * join the group it names is a different question, and `planDrop` asks it there
 * — it is the one that holds the subject.
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
 * The one decision a drag makes: what the affordance shows AND what the drop
 * writes, as a single value that cannot disagree with itself.
 *
 * Total — every input gets a plan or a refusal carrying a reason — and pure: it
 * reads only the request, so a caller can compute it on pointermove for the
 * indicator and again on pointerup for the mutation, or keep the one it has.
 * `nameOf` is the one input that can answer differently between two such calls
 * (the drag path rebuilds its title map when the rows are replaced), so two
 * plans can carry the same decision under different spellings.
 */
export function planDrop(req: DropRequest): DropPlan {
  // Defaulted once, here, so every phrase below takes a REQUIRED namer. The
  // optional parameter is the request's, not the spelling functions'.
  const { draggedIds, rowsById, draggedRowsById, target, zone, descendantIds, nameOf = BY_ID } = req;

  if (draggedIds.length === 0) {
    return refuse("no-source", "Nothing is being dragged.");
  }

  const dragged: RowData[] = [];
  for (const id of draggedIds) {
    const row = draggedRowsById.get(id);
    if (row === undefined) {
      // Distinct from a mixed selection below: the selection survives a filter
      // change, so a selected row can be absent from the view rather than
      // disagreeing with its fellows.
      //
      // Spelled like every other id in this module's prose, though this is the
      // one phrase that usually falls back to the id: the row is missing, and
      // the drag path builds its namer from the rendered rows. It resolves in
      // the case that is not usual — the namer also carries every rendered row's
      // `parentNib` (useTreeDrag.svelte.ts), so a hidden CONTAINER a visible
      // child still points at gets its title.
      return refuse(
        "hidden-member",
        `${spellId(id, nameOf)} is selected but not shown here — clear the filter (or expand the parent) hiding it, or drop it from the selection.`,
      );
    }
    dragged.push(row);
  }

  const draggedTypes = dragged.map((r) => r.nib.type);

  // A dragged row in no ordering group at all is a container the view
  // fabricated, and it names no nib for any write to take a subject from — so
  // this one stays ahead of everything below, the section branch included.
  const unorderable = dragged.find((r) => r.region === null);
  if (unorderable !== undefined) {
    return refuse(
      "unorderable-source",
      `The ${unorderable.nib.title} section is a container the view drew, not a nib, so it has no position to move.`,
    );
  }

  // Aiming AT a section names the section, so what entering it means is the
  // SECTION's answer rather than the row's type. Only the middle band asks: an
  // edge names a position beside a row, and "into" is the one indicator that
  // names entry.
  //
  // Asked before the refusal below, which is what a fabricated section row would
  // otherwise get — right for a Backlog, and the answer a declared, assigning
  // section must not take.
  //
  // And before the shared-ordering-group check below, because neither of these
  // two answers positions anything: an assignment is one independent `updateNib`
  // per row, which rows in different groups can take as readily as siblings can,
  // and a section refusing entry refuses it for every group at once.
  const drawn = target.drawsSection;
  if (drawn !== null && zone === "reparent") {
    switch (drawn.onEnter.kind) {
      case "assign":
        return planAssignment(drawn.onEnter, drawn.key, dragged, draggedIds, nameOf);
      case "refuse":
        return refuse("entry-refused", drawn.onEnter.message);
      case "region":
      case "byRow":
        // Entering joins an ordering group, or means whatever the row under the
        // cursor means. Both are the machinery below.
        break;
    }
  }

  // The one ordering group the whole dragged set is in — from here down every
  // remaining plan positions rows against each other, and that is the question
  // this answers. `commonRegion` also spells "some row has none" as null, which
  // the guard above has already ruled out.
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

  // The zone-independent guards — the dragged set itself, its own subtree, and a
  // target naming no nib — which is exactly what `isValidDropTarget`'s
  // before/after arm is. Its "reparent" arm bundles a type-hierarchy check keyed
  // on the TARGET's type, and the target is the destination container only when
  // it is the one being entered: a section header declaring where its rows order
  // is not. So the type question is asked once further down, against the
  // destination this plan names, and a queue destination is not asked at all —
  // joining a queue changes no parent link (`reorderNib` refuses `parentId` with
  // `scope: MILESTONE`).
  //
  // Asked BEFORE the destination is worked out, because these two answers do not
  // depend on it and they are the better explanation when both apply: releasing
  // on the row you grabbed is a CANCELED drag, and reporting it as whatever the
  // destination would have been ("a milestone holds no children") describes a
  // drop the user never asked for.
  if (!isValidDropTarget(draggedTypes, target.nib, "before", draggedIds, descendantIds)) {
    // It stays the authority on WHETHER the drop is refused; this only picks
    // which refusal to show, and a fabricated target was already refused above.
    return draggedIds.includes(target.nib.id)
      ? refuse("drop-on-self", "A nib cannot be dropped onto itself.")
      : refuse("drop-on-descendant", "A nib cannot be moved into its own subtree.");
  }

  // A group the dragged rows could never be MEMBERS of is no entry at all, so
  // the row keeps whatever its edges meant without one. Only the milestone axis
  // can answer no — `takesAssignmentAxes` is the client's read of
  // `nibtypes.RefusedAxes` — and dropping to null there is what leaves a
  // milestone header's own sibling reorder expressible: its bottom edge stays a
  // positioned drop, and its middle refuses as the type question it is rather
  // than offering a reassignment the server refuses. One dragged row is enough
  // to decide it, because one move positions one group.
  //
  // Parent-axis entry asks nothing here: the type hierarchy owns that question
  // and `isValidCrossParentDrop` puts it below, against the destination this
  // plan names.
  const declaredEntry = entryRegionOf(target);
  const entry =
    declaredEntry !== null && declaredEntry.axis === "milestone" && !draggedTypes.every(takesAssignmentAxes)
      ? null
      : declaredEntry;
  // The bottom edge of a container reads as "enter it" for the same reason its
  // middle does: below an expanded container is where its first row sits. One
  // exception beyond the entry the block above already nulled, and only on the
  // milestone axis: inside a queue the dragged rows are ALREADY in, the bottom
  // edge of a co-member is an in-queue reorder, and promoting it makes the
  // destination parent-axis — which is then refused either way, by the type
  // hierarchy or, failing that, by the cross-axis policy below. That would take
  // away half of a queue's reorder gestures, and
  // the position after a queue's last row whenever that row is a container. A
  // parent-axis entry needs no exception: it stays expressible from a
  // parent-axis source, which is the affordance the tree views ship today.
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

  // A parent-axis destination that differs from where the rows already are is a
  // CONTAINER CHANGE — which is both what makes the type question worth asking
  // and what a bare reorder cannot express.
  const dragParentId = sharedParentId(dragged);

  // Asked BEFORE the cross-axis policy below, not after: a destination the type
  // hierarchy refuses is impossible whichever axis the source is on, and
  // reporting it as a milestone reassignment would prescribe clearing an
  // assignment — which discards the queue position with it — for a gesture that
  // stays refused afterwards.
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

  // A drop that lands the rows in a DIFFERENT section from the one they are in,
  // where either side decides membership by a field, is one no position write
  // can express: a reorder moves an order key and a reparent moves a parent
  // link, and an assigning section goes on reading its field. Accepting it
  // writes in a list the user never pointed at and leaves the row drawn in the
  // section it was already in.
  //
  // Keyed on the PAIR rather than on the destination alone: the erasure belongs
  // to the boundary, not to one side of it. A test reading only the section
  // aimed at misses the OUT direction — a row dragged FROM an assigning section
  // onto one that answers anything else takes the very same wrong write.
  //
  // Every indicator, not the two edges only. The three bands of one row differ
  // in what they write — an order key or a parent link — and not in which
  // section the row ends up drawn in, which is what decides whether either write
  // says something true. Gating on the band made the identical reparent refuse
  // from the top edge of a container and land from its middle, a refusal a few
  // pixels of cursor travel walked around.
  //
  // Refused rather than performed, so the intent the gesture expressed is not
  // silently discarded — and so it answers the way the milestone axis already
  // answers it. Asked AFTER the type check above for that block's own reason: a
  // destination the hierarchy refuses stays refused once the assignment lands.
  const crossed = target.section;
  const crossedKey = crossed?.key ?? null;
  // The SET of sections the dragged rows sit in, not the one they agree on:
  // "they are in several" and "they are in none" are different facts about the
  // subject, and a single shared answer has to spell both `null`. A drag
  // spanning two sections is crossing this boundary whatever the destination
  // says, because at most one of those sections can be the destination.
  const homeKeys = new Set(dragged.map((r) => r.section?.key ?? null));
  if (homeKeys.size > 1 || !homeKeys.has(crossedKey)) {
    // Exhaustive over the destination's entry arm, no default: `assign` is the
    // only one that offers a write, but a `refuse` section must not fall through
    // to a position write it has just said is meaningless. A fifth arm is a
    // compile error here rather than silently taking the reorder.
    // Exhaustive over the destination's entry arm, no default: `assign` is the
    // only one that offers a write, but a `refuse` section must not fall through
    // to a position write it has just said is meaningless. A fifth arm is a
    // compile error here rather than silently taking the reorder.
    switch (crossed?.onEnter.kind) {
      case "refuse":
        return refuse("entry-refused", crossed.onEnter.message);
      // `undefined` is a destination in no section at all; the other two decline
      // to speak, so the departure side below is the only thing left to say.
      case undefined:
      case "region":
      case "byRow":
        break;
      case "assign": {
        const joining = crossed.onEnter;
        // Asked before the sentence below so the FINAL answer wins where both are
        // true: a subject that can never take the assignment gets the same
        // refusal here that aiming at the section's own row gives it, rather than
        // one naming an assignment as the fix and then withholding it.
        if (!draggedTypes.every(takesAssignmentAxes)) {
          return refuse("unassignable-type", `Cannot put ${listTypes(draggedTypes)} in ${nameSection(joining)}.`);
        }
        // Subject and remedy both come from `assignmentFor`, so the sentence names
        // exactly the rows the batch writes: the dragged rows need not share a
        // section, so some of them can already be in the destination, and a
        // subject phrased over the whole set would be false about those.
        //
        // Null means every dragged row is already in the destination section,
        // which the `homeKeys` guard above cannot reach — and if it ever did, the
        // fall-through past both arms would be the right answer for it anyway.
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
      // No remedy on this side: the destination's section answers something
      // other than `assign`, so it declares no write for entering it — and what
      // would put a row there is the lens's `place`, which this module never
      // sees.
      return refuse("crosses-section", leavingMessage(leaving, dragged.length, nameOf), { region: dest });
    }
  }

  const anchorId = target.nib.id;

  if (!sameRegion(source, dest)) {
    if (dest.axis === "milestone") {
      // The same membership question the entry gate above asks, asked again on
      // the path that never reaches it: a before/after destination is the
      // TARGET's region, not an entry this plan chose, so a milestone dragged
      // beside a queue member arrives here with the gate untouched. Offering the
      // assignment then draws a button whose write `nibtypes.ValidateAxes`
      // refuses ("a milestone cannot be assigned to a milestone").
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
      // `region` is the group this row's DISPLAY POSITION is governed by, so
      // while it is a queue nothing but a queue move changes where the row is
      // drawn — a parent-axis write would land somewhere the indicator did not
      // point.
      return refuse(
        "needs-unassignment",
        `${subjectIs(draggedIds, nameOf)} ordered in ${describeRegion(source, nameOf)}, so clear the milestone assignment before ordering in ${describeRegion(dest, nameOf)}.`,
        { region: dest },
      );
    }
  }

  switch (dest.axis) {
    case "milestone":
      // Reached only when the source is already this queue — every other way in
      // is the reassignment refusal above.
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
          // The friendly wording only where it is true. An entry region a lens
          // DECLARED names some other container than the row under the cursor,
          // and naming the row there would describe a container the command does
          // not touch.
          label: dest.parentId === target.nib.id ? `Move under ${spellId(anchorId, nameOf)}` : `Move into ${describeRegion(dest, nameOf)}`,
          // Entry position differs by axis, and the two mutations force it:
          // `setParent` carries no position, so the server places the row at its
          // own default — last among siblings of the same or higher priority
          // under a container, plain last in the root group (`orderer.go`
          // `defaultPlace` / `placeDefaultByPriority`) — while `Orderer.Move` has
          // no default arm at all ("a Position always names a destination"), so
          // the queue arm above has to name `first`.
          //
          // Not `reparentBatch`, whose parentId is non-null: an entry region can
          // name the root group. The command is otherwise the same value.
          command: batch(draggedIds.map((id) => setParent(id, dest.parentId))),
        };
      }

      // A before/after plan positions against the target, so the target has to be
      // a SERVER member of the destination group. `region` only says where the
      // view DRAWS it, and a lens declaring a region for its rows puts the two
      // out of step. Reparenting the dragged rows does not rescue that — the
      // anchor does not move with them, and the server refuses the anchor either
      // way ("nib X is not a sibling (different parent)"), so this fails closed
      // rather than offering an affordance that errors on drop.
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
          // No `scope`: PARENT is the server's default (`scope: OrderScope! =
          // PARENT`), so a sibling drag sends what it has always sent. And no
          // `parentId`: a PARENT-scope reorder groups by the subject's OWN
          // resolved parent, so the bare form is correct exactly while that
          // parent is `dest.parentId` — which is what this branch tests. Region
          // equality is not that test once a lens declares a region.
          command:
            draggedIds.length === 1
              ? reorderNib(draggedIds[0], anchor(indicator, anchorId))
              : reorderChain(draggedIds, anchorId, indicator),
        };
      }

      // The rows come from another container, so the move is a reparent
      // positioned against the target — unless the container they would land in
      // is itself drawn inside the target's own subtree. `promotedCycleRoots`
      // makes exactly that shape: a severed cycle member keeps a real parent the
      // view renders as its own child, the server accepts the write, and the rows
      // land inside a container drawn BELOW the line they were dropped on. The
      // promoted HEADER case this module means to unlock is a different
      // population — there the hidden container has no row at all, so the walk
      // finds nothing and the drop stands.
      if (dest.parentId !== null && isRenderedUnder(dest.parentId, target.nib.id, rowsById)) {
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
 *  for an entry, which is the one indicator naming no neighbor. */
type QueueLead = { first?: boolean; beforeId?: string; afterId?: string };

function queueLead(indicator: DropIndicator, anchorId: string): QueueLead {
  return indicator === "into" ? { first: true } : anchor(indicator, anchorId);
}

/**
 * Positions a run of nibs within one queue: the first against the drop's anchor,
 * the rest after whichever nib the previous step returned.
 *
 * `reorderChain` is the parent-axis form of this and cannot serve — it takes no
 * `scope`, so a queue move routed through it would rewrite the sibling `order`
 * key instead, or be refused outright when subject and anchor sit under
 * different parents.
 *
 * The return type is a non-empty tuple so the lead step stays a `LeafCommand` to
 * the type system: a caller wanting one write and no sequence around it can then
 * take it without a cast.
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
 * position the drop pointed at.
 *
 * Two writes per row because the axes are independent — an assignment enters the
 * queue at the server's default placement, last, which need not be where the
 * indicator pointed. A `sequence` rather than a `batch`: a MILESTONE reorder is
 * refused while its subject is in no queue ("assigned to no milestone"), so a
 * row's assignment has to have landed before its own positioning runs.
 *
 * INTERLEAVED per row, not every assignment followed by every position. The
 * dispatcher stops a sequence at its first failing step, so a multi-row drag
 * holding one row that cannot be assigned — an exclusivity conflict, say — would
 * otherwise abort with the rows before it assigned but never positioned, parked
 * at the end of the queue instead of where the drop pointed. Interleaved, that
 * same failure leaves the rows before it exactly where the drop asked.
 *
 * Which is also why the run is anchored on the previous DRAGGED id rather than
 * on the previous step's result, the way `queueMoveSteps` chains: a sequence
 * step is handed only the step immediately before it, and interleaving makes
 * that step the next row's `updateNib`, whose result carries no `reorderNib` id.
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
 * The section as a noun phrase a caller can put after a verb — "the
 * web/dashboard area". The VALUE names it, not the declared label: the value is
 * what the write sets, so a sentence built on it cannot describe one section
 * while the command changes the field to another.
 */
function nameSection(entry: AssignEntry): string {
  return `the ${entry.value} ${entry.noun}`;
}

/** The one sentence for this write, so the accepted plan and the refusal's
 *  remedy cannot describe it differently. Both build the batch through
 *  `assignmentFor` over the same rows and section, so it is the same write under
 *  the same label. */
function assignLabel(entry: AssignEntry): string {
  return `Move to ${nameSection(entry)}`;
}

/**
 * The rows an assignment to this section would CHANGE, together with the write
 * for exactly those rows — or null when it would change nothing.
 *
 * The two as one value, and the only way to build either, because both callers
 * that plan this write also phrase a sentence about its subject: the accepted
 * drop onto the section, and the `crosses-section` remedy. Handing out the
 * command alone is what let one of them describe the whole dragged set while
 * writing a subset of it.
 *
 * Rows already in the section are dropped rather than merely tolerated:
 * assigning a value a row already carries still bumps its etag, pulses it as
 * changed, for a change that is not one.
 *
 * A `batch`, not a `sequence`: the rows join by carrying a value, so no row's
 * write depends on another's having landed. That is what separates this from
 * `assignAndPlace`, which interleaves only because a MILESTONE reorder is
 * refused while its subject is in no queue — an assignment with no position to
 * follow it has nothing analogous.
 */
function assignmentFor(entry: AssignEntry, key: SectionKey, dragged: RowData[]): { ids: string[]; command: AnyCommand } | null {
  // Decided on the SECTION the rows are members of rather than on the field they
  // carry: the rows are the table's, and which section holds a row is the
  // question this module can answer from them.
  const ids = dragged.filter((r) => r.section?.key !== key).map((r) => r.nib.id);
  if (ids.length === 0) return null;
  // A computed key, which TypeScript does not check against the object it lands
  // in — `AssignableField` is the check, and it is the string-valued keys of
  // THIS input rather than the generated one, so a section cannot name a field
  // `updateNib` has no argument for.
  return { ids, command: batch(ids.map((id) => updateNib(id, { [entry.field]: entry.value }))) };
}

/**
 * The plan for a drop ONTO a section that assigns, or the refusal explaining why
 * there is none.
 *
 * The type gate is `takesAssignmentAxes`, the same predicate the milestone axis
 * asks: `nibtypes.RefusedAxes` refuses BOTH axes for a milestone-typed subject
 * and neither for anything else, so an area assignment and a milestone
 * assignment are gated by one rule under one name.
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
 * The rows a drop takes OUT of an assigning section, and the sections they
 * leave: every dragged row whose own section assigns and is not the one the drop
 * lands in.
 *
 * A LIST of sections rather than one, because the dragged rows need not share a
 * section. Folding "in several" onto "in none" — which one shared answer must,
 * having only `null` for both — is what let a drag spanning two assigning
 * sections past this check entirely.
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
 * The sentence for a drop leaving assigning sections, in the two shapes its
 * subject can take.
 *
 * Two shapes and not a single list, because "the 2 dragged nibs are in the infra
 * area and the web area" reads as each of them being in both — asserting of
 * every row something true of at most one, which is the conflation this whole
 * refusal exists to stop making.
 */
function leavingMessage(
  leaving: { ids: string[]; sections: AssignEntry[] },
  draggedCount: number,
  nameOf: RegionNamer,
): string {
  // Only the rows that LEAVE are named, and they can be fewer than the drag.
  // `subjectIs` spells one id as that id, which stays true however large the
  // drag is; it is only its plural — "The N dragged nibs" — that would assert
  // something false about the rows staying put. So the count is spelled out
  // exactly where that plural would otherwise lie.
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

/** The sections as one noun phrase, capped the way `listRegions` is: a selection
 *  survives select-all, and this is one line in a message. */
function listSections(entries: AssignEntry[]): string {
  const names = entries.map(nameSection);
  if (names.length <= 3) return names.join(" and ");
  return `${names.slice(0, 3).join(", ")} and ${names.length - 3} more`;
}

/**
 * The one real parent every dragged row already sits under, or `undefined` when
 * they disagree. `null` is a real answer — the root group — so the "no shared
 * answer" case needs a third value rather than folding onto it.
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
 * The type of the container a parent-axis destination names.
 *
 * THREE answers, not two. The root group is `known` with `type: null`, which is
 * what `isValidCrossParentDrop` reads as unconstrained; a container this
 * response did not carry is `known: false`. Folding the second onto the first is
 * what makes a type check silently not run, so the caller has to spend a branch
 * on it.
 */
function destContainerType(
  parentId: string | null,
  target: RowData,
  rowsById: ReadonlyMap<string, RowData>,
): ContainerType {
  if (parentId === null) return { known: true, type: null };
  // Entering the target itself — the type-derived entry region. Read straight
  // off the target so the check cannot depend on the target also being in the
  // map the caller passed.
  if (parentId === target.nib.id) return { known: true, type: target.nib.type };
  // `parentNib` is resolved against the whole response rather than the rendered
  // rows, so it answers for a container the lens gave no row to — which is what a
  // promoted header sits under. It can still be absent: a filter narrows the
  // response to a set that may exclude a real parent, so a resolvable parent id
  // is not necessarily accompanied by its nib (`tableData.ts`, stage 3).
  if (parentId === target.nib.parentId && target.parentNib !== null) {
    return { known: true, type: target.parentNib.type };
  }
  const row = rowsById.get(parentId);
  return row === undefined ? { known: false } : { known: true, type: row.nib.type };
}

/**
 * Whether `containerId` is DRAWN inside `ancestorId`'s subtree, walking the
 * display parent chain — which is what decides where the user sees a drop land.
 * A container with no row here is drawn nowhere, so the answer for it is false.
 *
 * The visited set is the termination guard. `displayParentId` is acyclic as the
 * view tree builds it; this walk does not need that to stay true.
 */
function isRenderedUnder(containerId: string, ancestorId: string, rowsById: ReadonlyMap<string, RowData>): boolean {
  const seen = new Set<string>();
  let current: string | null = containerId;
  while (current !== null && !seen.has(current)) {
    if (current === ancestorId) return true;
    seen.add(current);
    current = rowsById.get(current)?.displayParentId ?? null;
  }
  return false;
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

/**
 * The dragged set as a sentence subject, with its verb already agreed.
 *
 * Spells its id the way the rest of the sentence spells one: both messages built
 * on this also carry a `describeRegion` phrase, so an unspelled subject would
 * put a raw id and a title in one sentence.
 */
function subjectIs(ids: string[], nameOf: RegionNamer): string {
  return ids.length === 1 ? `${spellId(ids[0], nameOf)} is` : `The ${ids.length} dragged nibs are`;
}

function withArticle(word: string): string {
  return `${/^[aeiou]/i.test(word) ? "an" : "a"} ${word}`;
}
