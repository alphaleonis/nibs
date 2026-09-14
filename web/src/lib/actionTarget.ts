import type { SelectionState } from "./selection.svelte";
import { isSyntheticRowId } from "./tree";

/**
 * The nib ids a delete/bulk action targets: the multi-select set, else the
 * focused row, else the context-menu target.
 *
 * Never returns a synthetic row id (see `isSyntheticRowId`): it names no nib. A
 * real nib heading a section is a legal target. `selectedIds` already excludes
 * synthetic ids, but a synthetic row can be focused or right-clicked, so those
 * two checks are load-bearing.
 *
 * @param contextTargetId - The right-clicked row's id, or null.
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
 * Clears the selection a completed delete/archive consumed, and heals a stale
 * `?nib=<mutated>` URL.
 *
 * The multi-select set, anchor and focus always go. The detail panel closes only
 * when the mutation removed the nib it shows: the panel and the action target can
 * be different rows (open-on-double-click, arrow-key focus), and nothing on this
 * path runs the dirty guard, so closing it otherwise discards unsaved edits.
 *
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
