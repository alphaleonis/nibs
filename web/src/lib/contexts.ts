import { setContext, getContext } from 'svelte';
import type { SelectionState } from './selection.svelte';
import type { DragState } from './drag.svelte';
import { TreeViewState } from './treeView.svelte';
import { DEFAULT_VIEW_LEVEL } from './types';
import type { ConfirmDialogState } from './composables/useConfirmDialog.svelte';
import type { HistoryNav } from './composables/useHistoryNav.svelte';
import type { ActiveView } from './composables/useActiveView.svelte';
import type { ConnectionRecovery } from './connectionRecovery';
import { COLUMN_ADAPTERS_KEY, columnAdapters } from './ColumnAdapters.svelte';
import { EMPTY_SPINE } from './viewSpine';
import type { ViewSpine } from './viewSpine';
import type { MilestoneOption } from './milestones';

export const SELECTION_KEY = 'nibs:selection';
export const DRAG_KEY = 'nibs:drag';
export const TREE_VIEW_KEY = 'nibs:tree-view';
export const CONFIRM_DIALOG_KEY = 'nibs:confirm-dialog';
export const HISTORY_NAV_KEY = 'nibs:history-nav';
export const ACTIVE_VIEW_KEY = 'nibs:active-view';
export const CONNECTION_KEY = 'nibs:connection';
export const VIEW_SPINE_KEY = 'nibs:view-spine';
export const MILESTONES_KEY = 'nibs:milestones';
export const CONFIG_RETRY_KEY = 'nibs:config-retry';

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

/**
 * The view spine, as a getter: its identity changes when the areas vocabulary
 * arrives, so a value captured at setup would stay stale.
 */
export function provideViewSpine(get: () => ViewSpine) { setContext(VIEW_SPINE_KEY, get); }
export function useViewSpine(): () => ViewSpine {
  const g = getContext<() => ViewSpine>(VIEW_SPINE_KEY);
  if (!g) throw new Error('useViewSpine() called outside provider — call provideViewSpine() in a parent component');
  return g;
}

/**
 * The milestones a nib can be assigned to, as a getter: the list arrives after
 * first paint and grows as milestones are created.
 */
export function provideMilestones(get: () => readonly MilestoneOption[]) { setContext(MILESTONES_KEY, get); }
export function useMilestones(): () => readonly MilestoneOption[] {
  const g = getContext<() => readonly MilestoneOption[]>(MILESTONES_KEY);
  // Optional: without a provider, no milestone is offered.
  return g ?? (() => []);
}

export function provideConnection(c: ConnectionRecovery) { setContext(CONNECTION_KEY, c); }
/**
 * The live-socket recovery handle, or undefined outside a provider, where there
 * is no reconnect to re-read after.
 */
export function useConnection(): ConnectionRecovery | undefined {
  return getContext<ConnectionRecovery | undefined>(CONNECTION_KEY);
}

/**
 * Re-run the store's config query, for the dead end a failed one leaves. Undefined
 * outside a provider: offer no retry.
 */
export function provideConfigRetry(retry: () => void) { setContext(CONFIG_RETRY_KEY, retry); }
export function useConfigRetry(): (() => void) | undefined {
  return getContext<(() => void) | undefined>(CONFIG_RETRY_KEY);
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
    viewSpine?: ViewSpine;
    milestones?: readonly MilestoneOption[];
    configRetry?: () => void;
  },
): Map<string, unknown> {
  const m = new Map<string, unknown>();
  m.set(SELECTION_KEY, selection);
  m.set(DRAG_KEY, drag);
  // Provided as the app's <ColumnAdapters> does, so table components render
  // without wrapping.
  m.set(COLUMN_ADAPTERS_KEY, columnAdapters);
  // Default: a spine declaring no areas.
  const spine = opts?.viewSpine ?? EMPTY_SPINE;
  m.set(VIEW_SPINE_KEY, () => spine);
  // Default: no milestones.
  const milestones = opts?.milestones ?? [];
  m.set(MILESTONES_KEY, () => milestones);
  m.set(TREE_VIEW_KEY, opts?.treeView ?? new TreeViewState(DEFAULT_VIEW_LEVEL));
  // Only when supplied; omitting it tests the no-retry path.
  if (opts?.configRetry) m.set(CONFIG_RETRY_KEY, opts.configRetry);
  if (opts?.confirmDialog) m.set(CONFIRM_DIALOG_KEY, opts.confirmDialog);
  // Default stub: `open` selects, `requestClose` closes, the rest are no-ops.
  m.set(ACTIVE_VIEW_KEY, opts?.activeView ?? {
    state: { kind: 'closed' },
    form: null,
    detail: null,
    isOpen: false,
    presentation: 'docked',
    typePicker: null,
    blocksHistoryNav: false,
    savePending: false,
    externalApplied: 0,
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
    // From a `closed` state the real noteMissing always returns "stale".
    noteMissing: () => "stale",
    invalidateDetailSeed: () => {},
    dispose: () => {},
  } satisfies ActiveView);
  // Default stub mirrors selection without touching browser history.
  m.set(HISTORY_NAV_KEY, opts?.historyNav ?? {
    navigateToNib: (id: string) => selection.select(id),
    closePanel: () => selection.close(),
    replaceClosed: () => {},
    handlePopState: () => {},
    syncFromUrl: () => {},
  } satisfies HistoryNav);
  return m;
}
