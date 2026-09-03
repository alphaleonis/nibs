import { COLUMNS } from "./columns";
import { isDragAllowed } from "./filter";
import type { ViewShape } from "./tree";
import { TREE_VIEW_LEVEL, VIEW_LEVEL_LABELS } from "./types";
import type { NibFilter, ViewLevel, TableSort } from "./types";

/**
 * Toast id shared by every drag-block explanation, so repeated blocked attempts
 * replace the live toast instead of stacking up copies (svelte-sonner dedupes by
 * id and restarts the dismissed timer on update).
 *
 * ONE id covers all three reasons on purpose: the gate walk reports a single gate
 * at a time by precedence, so switching gates mid-toast should rewrite the
 * message in place rather than leave a stale one behind.
 */
export const DRAG_BLOCK_TOAST_ID = "drag-block";

/**
 * The view the Flat gate's remedy leaves Flat FOR — named once, and read by both
 * halves of that one gesture: the action label below, and the switch TreeTable
 * performs when the label is clicked. Two independently written levels would let
 * the toast promise one view and deliver another.
 *
 * Every non-flat view is reorderable (`reorderableShape`), so the mechanism does
 * not force the choice. The Tree is chosen because it rearranges the sequence the
 * user is already looking at: the list query asks for `sort: { field: ORDER }`
 * and Flat's arm of `buildShapedViewTree` preserves that array, which is the same
 * `order` key a drop in the Tree rewrites. A drop inside a milestone section
 * rewrites `milestoneOrder` instead — a different sequence from the one the
 * blocked drag was aimed at.
 */
export const FLAT_BLOCK_REMEDY_VIEW: ViewLevel = TREE_VIEW_LEVEL;

/** Which gate is currently suppressing drag-reorder. */
export type DragBlockReason = "flat" | "search" | "sort";

/** A suppressed-drag explanation plus the label of the action that lifts it. */
export interface DragBlock {
  reason: DragBlockReason;
  message: string;
  actionLabel: string;
}

/** What every gate is asked about. */
interface GateContext {
  filter: NibFilter;
  shape: ViewShape;
  activeSort: TableSort | null;
}

interface DragGate {
  readonly reason: DragBlockReason;
  /**
   * Whether this gate ALSO means adjacency says nothing — whether two rows being
   * neighbors still indicates where an ordering region's run starts and stops.
   *
   * All three answer yes today, which is why one boolean served both questions
   * for a while. They are not the same question: a gate added for a reason that
   * is not about display order — a read-only mode, a connection state, a
   * permission — suppresses the drag while adjacency still holds.
   */
  readonly breaksAdjacency: boolean;
  /** The explanation while this gate is closed, or null while it is open. */
  check(ctx: GateContext): DragBlock | null;
}

/**
 * The gates, in PRECEDENCE order — first match wins, so the message and its
 * action always name the same gate. Flat leads because reorder is meaningless in
 * that view at all; clearing a sort there would lift nothing.
 *
 * One table rather than a chain plus a lookup, because the adjacency question is
 * answered by asking EVERY gate rather than the precedence winner. Reading only
 * the winner is correct just while every non-adjacency gate ranks below every
 * adjacency one — and a read-only or connection gate is exactly the kind that
 * would rank first, which would then report adjacency intact in a view that is
 * also searched.
 */
const GATES: readonly DragGate[] = [
  {
    reason: "flat",
    breaksAdjacency: true,
    check: ({ shape }) =>
      reorderableShape(shape)
        ? null
        : {
            reason: "flat",
            message: `Reordering is off in the ${VIEW_LEVEL_LABELS.flat} view`,
            actionLabel: `Switch to ${VIEW_LEVEL_LABELS[FLAT_BLOCK_REMEDY_VIEW]}`,
          },
  },
  {
    reason: "search",
    breaksAdjacency: true,
    check: ({ filter }) =>
      isDragAllowed(filter)
        ? null
        : {
            reason: "search",
            message: "Reordering is off while a search is active",
            actionLabel: "Clear search",
          },
  },
  {
    reason: "sort",
    breaksAdjacency: true,
    check: ({ activeSort }) =>
      activeSort === null
        ? null
        : {
            reason: "sort",
            message: `Reordering is off while sorted by ${COLUMNS[activeSort.field].label}`,
            actionLabel: "Clear sort",
          },
  },
];

/**
 * Whether row adjacency reflects the ordering key, so a rule drawn between two
 * rows is a claim about the data rather than decoration.
 *
 * The region band reads this; `draggable` reads `shapedDragBlockFor(...) === null`.
 * Two questions, one answer today — but asked of every gate, so the answer does
 * not depend on which one happens to win precedence.
 */
export function shapedAdjacencyReflectsOrdering(
  filter: NibFilter,
  shape: ViewShape,
  activeSort: TableSort | null,
): boolean {
  const ctx = { filter, shape, activeSort };
  return !GATES.some((gate) => gate.breaksAdjacency && gate.check(ctx) !== null);
}

/**
 * Whether rows in this shape sit in an order the `order` key can express — the
 * only kind a drop can rewrite.
 *
 * Exhaustive switch, no default arm: a fourth view shape is a compile error here
 * rather than silently inheriting whichever answer a `=== "flat"` string test
 * fell through to.
 */
function reorderableShape(shape: ViewShape): boolean {
  switch (shape.kind) {
    case "flat":
      return false;
    case "tree":
    case "grouped":
      return true;
  }
}

/**
 * Describes why drag-reorder is currently off, or null when it is available.
 *
 * The three gates are deliberate (see nibs-917g): a Flat view intermixes real
 * parents, and a search or client-side sort displays rows in an order that the
 * `order` key does not carry — so a drop would fight what is on screen. What was
 * missing is any signal to the user, who sees a row that simply refuses to drag.
 * The caller pairs this with a toast raised on a real drag ATTEMPT.
 *
 * The predicate here must stay equivalent to TreeTable's `dragAllowed` — it is
 * the same boolean, and `dragAllowed` is derived from this function so the two
 * can never disagree about whether drag is on.
 *
 * Precedence when several gates are active is `GATES` order, so the message and
 * its action always refer to the same gate.
 */
export function shapedDragBlockFor(
  filter: NibFilter,
  shape: ViewShape,
  activeSort: TableSort | null,
): DragBlock | null {
  const ctx = { filter, shape, activeSort };
  for (const gate of GATES) {
    const block = gate.check(ctx);
    if (block !== null) return block;
  }
  return null;
}

/** Every reason has exactly one gate, so none can be declared and never asked. */
export const GATE_REASONS: readonly DragBlockReason[] = GATES.map((gate) => gate.reason);
