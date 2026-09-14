import { getValidChildTypes, isLeafType } from "./typeHierarchy";
import { isSyntheticRowId } from "./tree";
import type { RowData } from "./tableData";
import type { DropZone } from "./drag.svelte";

/**
 * Compute the drop zone based on cursor Y position relative to a row element.
 * Top 30% = "before", bottom 30% = "after", middle 40% = "reparent"
 */
export function computeDropZone(cursorY: number, rowRect: DOMRect): DropZone {
  const relY = cursorY - rowRect.top;
  const ratio = relY / rowRect.height;
  if (ratio < 0.3) return "before";
  if (ratio > 0.7) return "after";
  return "reparent";
}

/** Whether a nib type can be a child of the target parent type. */
export function isValidParent(draggedType: string, targetType: string): boolean {
  const validChildren = getValidChildTypes(targetType);
  return validChildren.includes(draggedType);
}

/**
 * Check if a drop target is valid, considering type hierarchy and cycle prevention.
 * For multi-select, ALL dragged types must be valid children of the target.
 */
export function isValidDropTarget(
  draggedTypes: string[],
  targetNib: { id: string; type: string; parentId: string | null },
  zone: DropZone,
  draggedIds: string[],
  descendantIds: Set<string>,
): boolean {
  // A synthetic "No X" bucket row is no nib, and the backend rejects any reorder
  // or reparent against its id.
  if (isSyntheticRowId(targetNib.id)) return false;

  if (draggedIds.includes(targetNib.id)) return false;

  // Cycle prevention.
  if (descendantIds.has(targetNib.id)) return false;

  if (zone === "reparent") {
    if (isLeafType(targetNib.type)) return false;
    return draggedTypes.every((t) => isValidParent(t, targetNib.type));
  }

  // Before/after validity depends on sibling context, checked by the caller.
  return true;
}

/**
 * Check if dragged types can be placed as siblings in a different parent.
 * `parentType` is the type of the target's parent, or null for the root level,
 * which accepts any type.
 */
export function isValidCrossParentDrop(
  draggedTypes: string[],
  parentType: string | null,
): boolean {
  if (parentType === null) return true;
  return draggedTypes.every(t => isValidParent(t, parentType));
}

/**
 * Collect all descendant IDs of the given nib IDs from the flat row list,
 * excluding the given IDs themselves.
 *
 * Indexes rows by parent before walking, so row order does not matter: a
 * queue-ordered section can list a child before its parent. An incomplete set
 * would let `isValidDropTarget` accept a drop onto a row's own descendant.
 *
 * The result set doubles as the visited guard, so a parent cycle among the rows
 * terminates.
 */
export function collectDescendantIds(nibIds: string[], rows: RowData[]): Set<string> {
  const childrenByParent = new Map<string, string[]>();
  for (const row of rows) {
    const parentId = row.nib.parentId;
    if (!parentId) continue;
    const siblings = childrenByParent.get(parentId);
    if (siblings) siblings.push(row.nib.id);
    else childrenByParent.set(parentId, [row.nib.id]);
  }

  const result = new Set<string>();
  const seeds = new Set(nibIds);
  const queue = [...seeds];
  while (queue.length > 0) {
    for (const childId of childrenByParent.get(queue.pop()!) ?? []) {
      if (seeds.has(childId) || result.has(childId)) continue;
      result.add(childId);
      queue.push(childId);
    }
  }
  return result;
}
