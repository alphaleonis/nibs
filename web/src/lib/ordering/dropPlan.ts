import type { DropZone } from "../drag.svelte";
import { isValidCrossParentDrop, isValidDropTarget } from "../dropZone";
import { batch, reorderChain, reorderNib, reparentAndReorder, sequence, setParent } from "../mutations/commands";
import type { AnyCommand, CommandResult, SequenceStep } from "../mutations/types";
import { canBeInMilestoneQueue } from "../membership";
import type { RowData } from "../tableData";
import { canHaveChildren } from "../typeHierarchy";
import { BY_ID, commonRegion, describeRegion, sameRegion, scopeOf, spellId, type Region, type RegionNamer } from "./region";

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
  | "needs-unassignment";

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
   *  there is one — so a refusal leads somewhere instead of just saying no. */
  actionLabel?: string;
}

export type DropPlan =
  | { ok: true; region: Region; indicator: DropIndicator; label: string; command: AnyCommand }
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
 * The ordering group a drop INTO this row lands in, or null when the row is not
 * something a drop can enter.
 *
 * Two questions live here, and they have to be asked separately: "what group are
 * my children in" (`childRegion`, which only a lens declares) and "what does a
 * drop into me mean". Neither predicate answers alone. `childRegion !== null` is
 * true only for a milestone section row — `tableData.test.ts` asserts "only a
 * milestone section declares a childRegion; every other row carries null" across
 * every view level — so it would delete the "drop below an epic to make it a
 * child" affordance the tree views have; `canHaveChildren` is false for a
 * milestone (`VALID_CHILD_TYPES.milestone` is `[]`), so it cannot promote a
 * queue header's edge at all. The declaration wins where there is one, because a
 * container that says where its rows order says what entering it means.
 *
 * A property of the ROW, and only that. Whether the rows being dragged could
 * join the group it names is a different question, and `planDrop` asks it there
 * — it is the one that holds the subject.
 */
export function entryRegionOf(row: RowData): Region | null {
  if (row.childRegion !== null) return row.childRegion;
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

  const source = commonRegion(dragged.map((r) => r.region));
  if (source === null) {
    const unorderable = dragged.find((r) => r.region === null);
    if (unorderable !== undefined) {
      return refuse(
        "unorderable-source",
        `The ${unorderable.nib.title} section is a container the view drew, not a nib, so it has no position to move.`,
      );
    }
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

  const draggedTypes = dragged.map((r) => r.nib.type);
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
  // can answer no — `canBeInMilestoneQueue` is the client's read of
  // `nibtypes.ValidateAxes` — and dropping to null there is what leaves a
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
    declaredEntry !== null && declaredEntry.axis === "milestone" && !draggedTypes.every(canBeInMilestoneQueue)
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

  if (!sameRegion(source, dest)) {
    if (dest.axis === "milestone") {
      return refuse(
        "needs-assignment",
        `${subjectIs(draggedIds, nameOf)} not in ${describeRegion(dest, nameOf)}, and joining one is an assignment rather than a move.`,
        { region: dest, actionLabel: `Assign to ${spellId(dest.milestoneId, nameOf)}` },
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

  const anchorId = target.nib.id;
  switch (dest.axis) {
    case "milestone":
      // Reached only when the source is already this queue — every other way in
      // is the reassignment refusal above.
      return {
        ok: true,
        region: dest,
        indicator,
        label:
          indicator === "into"
            ? `Move to the front of ${describeRegion(dest, nameOf)}`
            : `Reorder in ${describeRegion(dest, nameOf)}`,
        command: queueMove(draggedIds, dest, indicator === "into" ? { first: true } : anchor(indicator, anchorId)),
      };
    case "parent": {
      if (indicator === "into") {
        return {
          ok: true,
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
  extra: { region?: Region; actionLabel?: string } = {},
): DropPlan {
  const refusal: DropRefusal = { reason, message };
  if (extra.region !== undefined) refusal.region = extra.region;
  if (extra.actionLabel !== undefined) refusal.actionLabel = extra.actionLabel;
  return { ok: false, refusal };
}

function anchor(indicator: "before" | "after", anchorId: string): { beforeId: string } | { afterId: string } {
  return indicator === "before" ? { beforeId: anchorId } : { afterId: anchorId };
}

/**
 * Positions a run of nibs within one queue: the first against the drop's anchor,
 * the rest after whichever nib the previous step returned.
 *
 * `reorderChain` is the parent-axis form of this and cannot serve — it takes no
 * `scope`, so a queue move routed through it would rewrite the sibling `order`
 * key instead, or be refused outright when subject and anchor sit under
 * different parents.
 */
function queueMove(
  ids: string[],
  region: Region,
  lead: { first?: boolean; beforeId?: string; afterId?: string },
): AnyCommand {
  const scope = scopeOf(region);
  if (ids.length === 1) return reorderNib(ids[0], { ...lead, scope });
  const steps: SequenceStep[] = ids.map((id, i) =>
    i === 0
      ? reorderNib(id, { ...lead, scope })
      : (prev: CommandResult) => reorderNib(id, { afterId: prev.data?.reorderNib?.id, scope }),
  );
  return sequence(steps);
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
