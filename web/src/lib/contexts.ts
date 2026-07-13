import { setContext, getContext } from 'svelte';
import type { SelectionState } from './selection.svelte';
import type { DragState } from './drag.svelte';
import { TreeViewState } from './treeView.svelte';
import type { ConfirmDialogState } from './composables/useConfirmDialog.svelte';
import type { HistoryNav } from './composables/useHistoryNav.svelte';
import type { ActiveView } from './composables/useActiveView.svelte';

export const SELECTION_KEY = 'nibs:selection';
export const DRAG_KEY = 'nibs:drag';
export const TREE_VIEW_KEY = 'nibs:tree-view';
export const CONFIRM_DIALOG_KEY = 'nibs:confirm-dialog';
export const HISTORY_NAV_KEY = 'nibs:history-nav';
export const ACTIVE_VIEW_KEY = 'nibs:active-view';

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

export function provideTreeView(t: TreeViewState) { setContext(TREE_VIEW_KEY, t); }
export function useTreeView(): TreeViewState {
  const t = getContext<TreeViewState>(TREE_VIEW_KEY);
  if (!t) throw new Error('useTreeView() called outside provider — call provideTreeView() in a parent component');
  return t;
}

export function provideHistoryNav(n: HistoryNav) { setContext(HISTORY_NAV_KEY, n); }
export function useHistoryNav(): HistoryNav {
  const n = getContext<HistoryNav>(HISTORY_NAV_KEY);
  if (!n) throw new Error('useHistoryNav() called outside provider — call provideHistoryNav() in a parent component');
  return n;
}

export function provideConfirmDialog(cd: ConfirmDialogState) { setContext(CONFIRM_DIALOG_KEY, cd); }
export function useConfirmDialog(): ConfirmDialogState {
  const cd = getContext<ConfirmDialogState>(CONFIRM_DIALOG_KEY);
  if (!cd) throw new Error('useConfirmDialog() called outside provider — call provideConfirmDialog() in a parent component');
  return cd;
}

export function provideActiveView(v: ActiveView) { setContext(ACTIVE_VIEW_KEY, v); }
export function useActiveView(): ActiveView {
  const v = getContext<ActiveView>(ACTIVE_VIEW_KEY);
  if (!v) throw new Error('useActiveView() called outside provider — call provideActiveView() in a parent component');
  return v;
}

/** Build a context Map for tests. selection and drag are required; additional context providers are optional. */
export function makeTestContext(
  selection: SelectionState,
  drag: DragState,
  opts?: {
    treeView?: TreeViewState;
    confirmDialog?: ConfirmDialogState;
    historyNav?: HistoryNav;
    activeView?: ActiveView;
  },
): Map<string, unknown> {
  const m = new Map<string, unknown>();
  m.set(SELECTION_KEY, selection);
  m.set(DRAG_KEY, drag);
  // Always provide a tree-view so components that read collapse state work in
  // tests without extra setup.
  m.set(TREE_VIEW_KEY, opts?.treeView ?? new TreeViewState());
  if (opts?.confirmDialog) m.set(CONFIRM_DIALOG_KEY, opts.confirmDialog);
  // Always provide an active-view so components that open/sync the unified nib
  // view (TreeTable rows, RowContextMenu) work in tests without extra setup.
  // Default mirrors selection: `open` selects, `requestClose` closes, and the
  // guard-bypass `syncTo`/transitions are no-ops (selection is the observable).
  m.set(ACTIVE_VIEW_KEY, opts?.activeView ?? {
    state: { kind: 'closed' },
    form: null,
    detail: null,
    isOpen: false,
    presentation: 'docked',
    typePicker: null,
    blocksHistoryNav: false,
    open: async (id: string) => { selection.select(id); },
    expand: () => {},
    collapse: () => {},
    startCreate: async () => {},
    startCreateChild: async () => {},
    chooseType: async () => {},
    cancelType: () => {},
    save: async () => undefined,
    requestClose: async () => { selection.close(); },
    syncTo: () => {},
    dispose: () => {},
  } satisfies ActiveView);
  // Always provide a history-nav so components that read it work in tests without extra setup.
  // Default is a select-only stub that mirrors selection without touching browser history.
  m.set(HISTORY_NAV_KEY, opts?.historyNav ?? {
    navigateToNib: (id: string) => selection.select(id),
    closePanel: () => selection.close(),
    replaceClosed: () => {},
    handlePopState: () => {},
    syncFromUrl: () => {},
  } satisfies HistoryNav);
  return m;
}
