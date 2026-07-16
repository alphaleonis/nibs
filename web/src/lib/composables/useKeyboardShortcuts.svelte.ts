/**
 * Composable for global keyboard shortcuts.
 *
 * Registers tinykeys shortcuts via $effect with automatic cleanup.
 * Reads from: editor orchestration context, selection context,
 * confirm dialog context, and mutation store.
 */

import { bindGlobalShortcuts } from "../keyboard";
import type { SelectionState } from "../selection.svelte";
import type { HistoryNav } from "./useHistoryNav.svelte";
import type { ConfirmDialogState } from "./useConfirmDialog.svelte";
import type { ActiveView } from "./useActiveView.svelte";
import type { MutationStore } from "../mutations/store.svelte";
import { deleteBatch } from "../mutations/commands";

/** Returns true if focus is inside an input/textarea/select/contenteditable */
function isInputFocused(): boolean {
  const el = document.activeElement;
  const tag = el?.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
  if (el instanceof HTMLElement && el.isContentEditable) return true;
  return false;
}

/**
 * Set up global keyboard shortcuts. Must be called during component
 * initialization so that the $effect is registered.
 *
 * @param opts.selection - Selection state (from context)
 * @param opts.nav - History navigation controller (from context)
 * @param opts.view - Active-nib-view presenter (from context)
 * @param opts.confirmDialog - Confirm dialog state (from context)
 * @param opts.mutations - Mutation store (from context)
 * @param opts.getContextMenuNibId - Reactive getter for context menu nib ID (for action targeting)
 */
export function useKeyboardShortcuts(opts: {
  selection: SelectionState;
  nav: HistoryNav;
  view: ActiveView;
  confirmDialog: ConfirmDialogState;
  mutations: MutationStore;
  getContextMenuNibId: () => string | null;
}): void {
  const { selection, nav, view, confirmDialog, mutations } = opts;

  /** True while the full-screen modal presentation is up — global row shortcuts
   *  (create/edit/delete) must not act on the table behind it. */
  function modalOpen(): boolean {
    return view.isOpen && view.presentation === "expanded";
  }

  /** True while a confirm dialog is up. The row shortcuts open confirms of their
   *  own (create/edit route through the dirty-guard; Delete/Backspace open a
   *  delete confirm), and firing a SECOND confirm reuses the single dialog and
   *  abandons the first's pending promise — the leak nibs-an5d fixed. tinykeys
   *  binds a bare `window` listener with no target filtering, so a key pressed
   *  while focus sits on a dialog button still reaches here; this gate stops it.
   *  Escape is deliberately NOT gated on this — bits-ui's escape layer consumes
   *  it (marks it defaultPrevented) so the Escape handler already bails, letting
   *  the dialog close itself. */
  function confirmOpen(): boolean {
    return confirmDialog.open;
  }

  /** Resolves which nib IDs an action should target, checking multi-select,
   *  focused row, then context menu in priority order. */
  function getActionTargetIds(): string[] {
    if (selection.hasMultiSelect) return [...selection.selectedIds];
    if (selection.focusedNibId) return [selection.focusedNibId];
    const ctxId = opts.getContextMenuNibId();
    if (ctxId) return [ctxId];
    return [];
  }

  function handleDelete() {
    const ids = getActionTargetIds();
    if (ids.length === 0) return;

    const count = ids.length;
    confirmDialog.showConfirm({
      title: count > 1 ? `Delete ${count} items` : "Delete nib",
      message: count > 1
        ? `Are you sure you want to delete ${count} items? This action cannot be undone.`
        : `Are you sure you want to delete this nib? This action cannot be undone.`,
      label: count > 1 ? `Delete ${count} items` : "Delete",
      variant: "danger",
      action: async () => {
        confirmDialog.close();
        const result = await mutations.execute(deleteBatch(ids));
        if (result.ok) {
          selection.clearAll();
          nav.replaceClosed(); // heal a stale ?nib=<deleted> URL (nibs-etk3)
        }
      },
    });
  }

  $effect(() => {
    const unbind = bindGlobalShortcuts(
      {
        Escape: (e: KeyboardEvent) => {
          if (e.defaultPrevented) return;
          // Enhanced Escape hierarchy: open view -> deselect -> clear focus.
          if (view.isOpen) {
            e.preventDefault();
            // Routes through the dirty-guard, then nav (URL/history stay in sync).
            view.requestClose();
            return;
          }
          if (selection.hasMultiSelect || selection.selectedIds.size > 0) {
            e.preventDefault();
            selection.deselectAll();
            return;
          }
          if (selection.focusedNibId) {
            e.preventDefault();
            selection.clearFocus();
          }
        },
        n: (e: KeyboardEvent) => {
          if (modalOpen()) return;
          if (confirmOpen()) return;
          if (isInputFocused()) return;
          e.preventDefault();
          view.startCreate({ type: "task" });
        },
        "$mod+n": (e: KeyboardEvent) => {
          if (modalOpen()) return;
          if (confirmOpen()) return;
          e.preventDefault();
          view.startCreate({ type: "task" });
        },
        e: (e: KeyboardEvent) => {
          if (modalOpen()) return;
          if (confirmOpen()) return;
          if (isInputFocused()) return;
          if (!selection.focusedNibId) return;
          e.preventDefault();
          view.open(selection.focusedNibId);
        },
        Delete: (e: KeyboardEvent) => {
          if (modalOpen()) return;
          if (confirmOpen()) return;
          if (isInputFocused()) return;
          if (getActionTargetIds().length === 0) return;
          e.preventDefault();
          handleDelete();
        },
        Backspace: (e: KeyboardEvent) => {
          if (modalOpen()) return;
          if (confirmOpen()) return;
          if (isInputFocused()) return;
          if (getActionTargetIds().length === 0) return;
          e.preventDefault();
          handleDelete();
        },
      },
      {
        Escape: "Close detail panel / deselect",
        n: "Create new task",
        "$mod+n": "Create new task",
        e: "Edit focused nib",
        Delete: "Delete focused/selected nib(s)",
        Backspace: "Delete focused/selected nib(s)",
      },
    );
    return unbind;
  });
}
