import { setContext, getContext } from 'svelte';
import type { SelectionState } from './selection.svelte';
import type { DragState } from './drag.svelte';
import type { ConfirmDialogState } from './composables/useConfirmDialog.svelte';
import type { EditorOrchestrationState } from './composables/useEditorOrchestration.svelte';

export const SELECTION_KEY = 'nibs:selection';
export const DRAG_KEY = 'nibs:drag';
export const CONFIRM_DIALOG_KEY = 'nibs:confirm-dialog';
export const EDITOR_ORCHESTRATION_KEY = 'nibs:editor-orchestration';

export function provideSelection(s: SelectionState) { setContext(SELECTION_KEY, s); }
export function useSelection(): SelectionState {
  const s = getContext<SelectionState>(SELECTION_KEY);
  if (!s) throw new Error('useSelection() called outside provider — call provideSelection() in a parent component');
  return s;
}
export function provideDrag(d: DragState) { setContext(DRAG_KEY, d); }
export function useDrag(): DragState {
  const d = getContext<DragState>(DRAG_KEY);
  if (!d) throw new Error('useDrag() called outside provider — call provideDrag() in a parent component');
  return d;
}

export function provideConfirmDialog(cd: ConfirmDialogState) { setContext(CONFIRM_DIALOG_KEY, cd); }
export function useConfirmDialog(): ConfirmDialogState {
  const cd = getContext<ConfirmDialogState>(CONFIRM_DIALOG_KEY);
  if (!cd) throw new Error('useConfirmDialog() called outside provider — call provideConfirmDialog() in a parent component');
  return cd;
}

export function provideEditorOrchestration(eo: EditorOrchestrationState) { setContext(EDITOR_ORCHESTRATION_KEY, eo); }
export function useEditorOrchestration(): EditorOrchestrationState {
  const eo = getContext<EditorOrchestrationState>(EDITOR_ORCHESTRATION_KEY);
  if (!eo) throw new Error('useEditorOrchestration() called outside provider — call provideEditorOrchestration() in a parent component');
  return eo;
}

/** Build a context Map for tests. selection and drag are required; additional context providers are optional. */
export function makeTestContext(
  selection: SelectionState,
  drag: DragState,
  opts?: { confirmDialog?: ConfirmDialogState; editorOrchestration?: EditorOrchestrationState },
): Map<string, unknown> {
  const m = new Map<string, unknown>();
  m.set(SELECTION_KEY, selection);
  m.set(DRAG_KEY, drag);
  if (opts?.confirmDialog) m.set(CONFIRM_DIALOG_KEY, opts.confirmDialog);
  if (opts?.editorOrchestration) m.set(EDITOR_ORCHESTRATION_KEY, opts.editorOrchestration);
  return m;
}
