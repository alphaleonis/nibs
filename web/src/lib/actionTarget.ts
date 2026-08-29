import type { SelectionState } from "./selection.svelte";
import { isSyntheticRowId } from "./tree";

/**
 * Resolves which nib IDs a delete/bulk action should target, in priority order:
 * the multi-select set, then the focused row, then the context-menu target.
 *
 * A synthetic row id (e.g. "/__no_milestone__") is NEVER returned. Such a row has
 * no detail and is unresolvable for any bulk mutation, so admitting one would
 * dispatch a phantom (e.g. `deleteBatch(["/__no_milestone__"])`) against a
 * nonexistent nib. What disqualifies it is naming no nib, not heading a section:
 * a real nib heading one resolves like any other row and is a legal target. The
 * three tiers guard synthetic ids differently:
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
    return [...selection.selectedIds].filter((id) => !isSyntheticRowId(id));
  }
  if (selection.focusedNibId && !isSyntheticRowId(selection.focusedNibId)) {
    return [selection.focusedNibId];
  }
  if (contextTargetId && !isSyntheticRowId(contextTargetId)) {
    return [contextTargetId];
  }
  return [];
}

/**
 * Clears the selection state that a completed delete/archive invalidated, and
 * heals a now-stale `?nib=<mutated>` URL. The counterpart of getActionTargetIds:
 * that resolves what the action hit, this retires what the action consumed.
 *
 * `selectedIds`, the anchor and the focused row all pointed at the mutated rows,
 * so they always go. The detail panel is separate: `selectedNibId` and the action
 * target can be DIFFERENT rows (the "open on double-click" preference selects and
 * focuses a row without moving the panel, and arrow-key nav moves focus alone), so
 * it is closed only when the mutation actually took out the nib it is showing.
 * Closing it otherwise would tear down a nib that was neither targeted nor
 * mutated — and discard any unsaved edits in its buffer, since nothing on this
 * path runs the dirty guard.
 *
 * @param selection - Selection state to clear.
 * @param nav - History nav, for healing the URL when the panel closes.
 * @param mutatedIds - The ids the mutation actually applied to.
 */
export function clearAfterMutation(
  selection: SelectionState,
  nav: { replaceClosed: () => void },
  mutatedIds: readonly string[],
): void {
  selection.deselectAll();
  selection.clearFocus();
  if (selection.selectedNibId !== null && mutatedIds.includes(selection.selectedNibId)) {
    selection.close();
    nav.replaceClosed();
  }
}
