import { COLUMNS } from "./columns";
import { isDragAllowed } from "./filter";
import type { ViewShape } from "./tree";
import { TREE_VIEW_LEVEL, VIEW_LEVEL_LABELS } from "./types";
import type { NibFilter, ViewLevel, TableSort } from "./types";

/**
 * Toast id shared by every drag-block explanation, so repeated blocked attempts
 * replace the live toast instead of stacking copies (svelte-sonner dedupes by
 * id). One id for every reason, so a change of gate rewrites the message in
 * place.
 */
export const DRAG_BLOCK_TOAST_ID = "drag-block";

/**
 * The view the Flat gate's remedy switches to, read by both the action label
 * and TreeTable's switch. The Tree, because a drop there rewrites the `order`
 * key Flat displays; a drop in a milestone section rewrites `milestoneOrder`
 * instead.
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
   * Whether this gate also means row adjacency no longer shows where an ordering
   * region's run starts and stops. A gate unrelated to display order — read-only
   * mode, connection state — would block drag with adjacency intact.
   */
  readonly breaksAdjacency: boolean;
  /** The explanation while this gate is closed, or null while it is open. */
  check(ctx: GateContext): DragBlock | null;
}

/**
 * The gates, in precedence order: the first closed gate supplies the message
 * and its action. Flat leads, since clearing a sort there would lift nothing.
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
 * rows is a claim about the data. Asks every gate, not the precedence winner.
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
 * Whether rows in this shape sit in an order the `order` key can express. No
 * default arm, so a new view shape fails to compile here until it answers.
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
 * A Flat view intermixes real parents, and a search or client-side sort shows
 * rows in an order the `order` key does not carry, so a drop would fight what is
 * on screen. TreeTable derives `dragAllowed` from this and shows the block as a
 * toast on a drag attempt.
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

/** The reason of every gate. */
export const GATE_REASONS: readonly DragBlockReason[] = GATES.map((gate) => gate.reason);
