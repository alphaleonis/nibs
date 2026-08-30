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

/**
 * Check if a nib type can be a child of the target parent type.
 */
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
  // Synthetic "No X" bucket rows are display-only containers, not real nibs.
  // Any zone dropping onto one would issue reorderNib/reparent against a
  // synthetic id, which the backend rejects ("sibling nib not found"). Reject
  // all zones so the bucket is never a valid drop target.
  if (isSyntheticRowId(targetNib.id)) return false;

  // Can't drop on self
  if (draggedIds.includes(targetNib.id)) return false;

  // Can't drop on own descendants (cycle prevention)
  if (descendantIds.has(targetNib.id)) return false;

  if (zone === "reparent") {
    // Target must be able to have children
    if (isLeafType(targetNib.type)) return false;
    // ALL dragged types must be valid children of target type
    return draggedTypes.every((t) => isValidParent(t, targetNib.type));
  }

  // For reorder (before/after): validity depends on sibling context,
  // which is checked at the call site
  return true;
}

/**
 * Check if dragged types can be placed as siblings in a different parent.
 * parentType is the type of the target's parent, or null for root level.
 * At root level, any type is allowed. Otherwise, all dragged types must be
 * valid children of the parent type.
 */
export function isValidCrossParentDrop(
  draggedTypes: string[],
  parentType: string | null,
): boolean {
  if (parentType === null) return true; // root level accepts any type
  return draggedTypes.every(t => isValidParent(t, parentType));
}

/**
 * Collect all descendant IDs of the given nib IDs from the flat row list.
 *
 * Order-independent by construction: it indexes the rows by parent first, then
 * walks that adjacency map outward from the seeds. Row order is not a contract
 * — a DFS flatten happens to place every parent before its children, but a
 * queue-ordered section carries no such guarantee, and a single forward pass
 * silently under-collects when a child precedes its parent. This set is what
 * `isValidDropTarget` rejects drops against, so an incomplete one would let a
 * row be dropped onto its own descendant and form a cycle.
 *
 * The result set doubles as the visited guard, so a malformed parent cycle
 * among the rows terminates rather than looping.
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
      // Skip the dragged items themselves, and anything already collected.
      if (seeds.has(childId) || result.has(childId)) continue;
      result.add(childId);
      queue.push(childId);
    }
  }
  return result;
}
