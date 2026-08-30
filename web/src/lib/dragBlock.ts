import { COLUMNS } from "./columns";
import { isDragAllowed } from "./filter";
import { viewShapeFor } from "./tree";
import type { ViewShape } from "./tree";
import { DEFAULT_VIEW_LEVEL, VIEW_LEVEL_LABELS } from "./types";
import type { NibFilter, ViewLevel, TableSort } from "./types";

/**
 * Toast id shared by every drag-block explanation, so repeated blocked attempts
 * replace the live toast instead of stacking up copies (svelte-sonner dedupes by
 * id and restarts the dismissed timer on update).
 *
 * ONE id covers all three reasons on purpose: dragBlockFor reports a single gate
 * at a time by precedence, so switching gates mid-toast should rewrite the
 * message in place rather than leave a stale one behind.
 */
export const DRAG_BLOCK_TOAST_ID = "drag-block";

/** Which gate is currently suppressing drag-reorder. */
export type DragBlockReason = "flat" | "search" | "sort";

/** A suppressed-drag explanation plus the label of the action that lifts it. */
export interface DragBlock {
  reason: DragBlockReason;
  message: string;
  actionLabel: string;
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
 * Precedence when several gates are active is fixed (flat > search > sort) so
 * the message and its action always refer to the same gate. Flat leads because
 * reorder is meaningless in that view at all; clearing a sort there would lift
 * nothing.
 */
export function dragBlockFor(
  filter: NibFilter,
  viewLevel: ViewLevel,
  activeSort: TableSort | null,
): DragBlock | null {
  if (!reorderableShape(viewShapeFor(viewLevel))) {
    return {
      reason: "flat",
      message: `Reordering is off in the ${VIEW_LEVEL_LABELS.flat} view`,
      actionLabel: `Switch to ${VIEW_LEVEL_LABELS[DEFAULT_VIEW_LEVEL]}`,
    };
  }
  if (!isDragAllowed(filter)) {
    return {
      reason: "search",
      message: "Reordering is off while a search is active",
      actionLabel: "Clear search",
    };
  }
  if (activeSort) {
    return {
      reason: "sort",
      message: `Reordering is off while sorted by ${COLUMNS[activeSort.field].label}`,
      actionLabel: "Clear sort",
    };
  }
  return null;
}
