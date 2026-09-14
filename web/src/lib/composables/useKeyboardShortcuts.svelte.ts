import { bindGlobalShortcuts } from "../keyboard";
import type { SelectionState } from "../selection.svelte";
import type { HistoryNav } from "./useHistoryNav.svelte";
import type { ConfirmDialogState } from "./useConfirmDialog.svelte";
import type { ActiveView } from "./useActiveView.svelte";
import type { MutationStore } from "../mutations/store.svelte";
import { deleteBatch } from "../mutations/commands";
import { getActionTargetIds, clearAfterMutation } from "../actionTarget";

/** Returns true if focus is inside an input/textarea/select/contenteditable */
function isInputFocused(): boolean {
  const el = document.activeElement;
  const tag = el?.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
  if (el instanceof HTMLElement && el.isContentEditable) return true;
  return false;
}

/**
 * Bind the global keyboard shortcuts. Call during component initialization; it
 * registers an `$effect`.
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

  /** True while a confirm dialog is up. Row shortcuts open confirms of their
   *  own, and a second confirm supersedes the first; the window listener still
   *  sees keys pressed on dialog buttons. Escape is not gated: bits-ui's escape
   *  layer preventDefaults it, so the Escape handler bails. */
  function confirmOpen(): boolean {
    return confirmDialog.open;
  }

  function handleDelete() {
    const ids = getActionTargetIds(selection, opts.getContextMenuNibId());
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
          clearAfterMutation(selection, nav, ids);
        }
      },
    });
  }

  $effect(() => {
    const unbind = bindGlobalShortcuts(
      {
        Escape: (e: KeyboardEvent) => {
          if (e.defaultPrevented) return;
          // Close view -> deselect -> clear focus.
          if (view.isOpen) {
            e.preventDefault();
            // Through the dirty-guard.
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
          if (getActionTargetIds(selection, opts.getContextMenuNibId()).length === 0) return;
          e.preventDefault();
          handleDelete();
        },
        Backspace: (e: KeyboardEvent) => {
          if (modalOpen()) return;
          if (confirmOpen()) return;
          if (isInputFocused()) return;
          if (getActionTargetIds(selection, opts.getContextMenuNibId()).length === 0) return;
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
