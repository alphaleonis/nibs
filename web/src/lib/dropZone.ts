import { getValidChildTypes, isLeafType } from "./typeHierarchy";
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
 * Walks the row list in order — children appear after their parent due to
 * pre-order traversal of the tree.
 */
export function collectDescendantIds(nibIds: string[], rows: RowData[]): Set<string> {
  const result = new Set<string>();
  const ancestors = new Set(nibIds);

  for (const row of rows) {
    if (ancestors.has(row.nib.id)) continue; // skip the dragged items themselves
    // Check if this row's parent is in ancestors or already collected
    if (
      row.nib.parentId &&
      (ancestors.has(row.nib.parentId) || result.has(row.nib.parentId))
    ) {
      result.add(row.nib.id);
    }
  }
  return result;
}
