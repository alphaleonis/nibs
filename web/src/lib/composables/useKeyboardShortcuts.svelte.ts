/**
 * Composable for global keyboard shortcuts.
 *
 * Registers tinykeys shortcuts via $effect with automatic cleanup.
 * Reads from: editor orchestration context, selection context,
 * confirm dialog context, and mutation store.
 */

import { bindGlobalShortcuts } from "../keyboard";
import type { SelectionState } from "../selection.svelte";
import type { ConfirmDialogState } from "./useConfirmDialog.svelte";
import type { EditorOrchestrationState } from "./useEditorOrchestration.svelte";
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
 * @param opts.editor - Editor orchestration state (from context)
 * @param opts.confirmDialog - Confirm dialog state (from context)
 * @param opts.mutations - Mutation store (from context)
 * @param opts.getContextMenuNibId - Reactive getter for context menu nib ID (for action targeting)
 */
export function useKeyboardShortcuts(opts: {
  selection: SelectionState;
  editor: EditorOrchestrationState;
  confirmDialog: ConfirmDialogState;
  mutations: MutationStore;
  getContextMenuNibId: () => string | null;
}): void {
  const { selection, editor, confirmDialog, mutations } = opts;

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
        }
      },
    });
  }

  $effect(() => {
    const unbind = bindGlobalShortcuts(
      {
        Escape: (e: KeyboardEvent) => {
          if (e.defaultPrevented) return;
          // Enhanced Escape hierarchy: editor modal -> detail panel -> deselect -> clear focus
          if (editor.editorOpen) return; // Editor has its own Escape handling
          if (selection.panelOpen) {
            e.preventDefault();
            selection.close();
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
          if (editor.editorOpen) return;
          if (isInputFocused()) return;
          e.preventDefault();
          editor.handleCreateNew("task");
        },
        "$mod+n": (e: KeyboardEvent) => {
          if (editor.editorOpen) return;
          e.preventDefault();
          editor.handleCreateNew("task");
        },
        e: (e: KeyboardEvent) => {
          if (editor.editorOpen) return;
          if (isInputFocused()) return;
          if (!selection.focusedNibId) return;
          e.preventDefault();
          editor.handleEditNib(selection.focusedNibId);
        },
        Delete: (e: KeyboardEvent) => {
          if (editor.editorOpen) return;
          if (isInputFocused()) return;
          if (getActionTargetIds().length === 0) return;
          e.preventDefault();
          handleDelete();
        },
        Backspace: (e: KeyboardEvent) => {
          if (editor.editorOpen) return;
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
