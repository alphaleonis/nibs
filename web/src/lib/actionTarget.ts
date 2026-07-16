import type { SelectionState } from "./selection.svelte";
import { isBucketId } from "./tree";

/**
 * Resolves which nib IDs a delete/bulk action should target, in priority order:
 * the multi-select set, then the focused row, then the context-menu target.
 *
 * A synthetic grouping-bucket id (e.g. "__no_milestone__") is NEVER returned. A
 * bucket row has no detail and is unresolvable for any bulk mutation, so admitting
 * one would dispatch a phantom (e.g. `deleteBatch(["__no_milestone__"])`) against a
 * nonexistent nib. The three tiers guard buckets differently:
 *  - `selectedIds` is already bucket-free — SelectionState.select/toggleSelect/
 *    rangeSelect reject bucket ids (nibs-mn0t) — so the filter here is
 *    defense-in-depth.
 *  - `focusedNibId` and the context-menu target are NOT guarded by SelectionState:
 *    a bucket row can be arrow-focused (focus() admits any id) or right-clicked, so
 *    those checks are load-bearing.
 *
 * @param selection - Selection state (multi-select set + focused row).
 * @param contextTargetId - The context-menu / right-clicked row's id, or null.
 */
export function getActionTargetIds(
  selection: SelectionState,
  contextTargetId: string | null,
): string[] {
  if (selection.hasMultiSelect) {
    return [...selection.selectedIds].filter((id) => !isBucketId(id));
  }
  if (selection.focusedNibId && !isBucketId(selection.focusedNibId)) {
    return [selection.focusedNibId];
  }
  if (contextTargetId && !isBucketId(contextTargetId)) {
    return [contextTargetId];
  }
  return [];
}
