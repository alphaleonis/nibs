import type { NibFilter, ViewLevel, ColumnKey, TableSort } from "./types";
import { ALL_COLUMN_KEYS, DEFAULT_COLUMN_WIDTHS, DEFAULT_VISIBLE_COLUMNS, DEFAULT_VIEW_LEVEL } from "./types";
import type { Preferences } from "./preferences.svelte";
// Type-only, so this module stays rune-free.
import type { TreeViewState } from "./treeView.svelte";

export function resolveFilter(prefs: Preferences | undefined, filter: NibFilter | undefined): NibFilter {
  return prefs?.filter ?? filter ?? {};
}

export function resolveViewLevel(prefs: Preferences | undefined, viewLevel: ViewLevel | undefined): ViewLevel {
  return prefs?.viewLevel ?? viewLevel ?? DEFAULT_VIEW_LEVEL;
}

export function resolveVisibleColumns(prefs: Preferences | undefined, visibleColumns: ColumnKey[] | undefined): ColumnKey[] {
  return prefs?.visibleColumns ?? visibleColumns ?? [...DEFAULT_VISIBLE_COLUMNS];
}

export function resolveColumnWidths(prefs: Preferences | undefined, columnWidths: Record<ColumnKey, number> | undefined): Record<ColumnKey, number> {
  return prefs?.currentColumnWidths ?? columnWidths ?? { ...DEFAULT_COLUMN_WIDTHS };
}

export function resolveColumnOrder(prefs: Preferences | undefined, columnOrder: ColumnKey[] | undefined): ColumnKey[] {
  return prefs?.currentColumnOrder ?? columnOrder ?? [...ALL_COLUMN_KEYS];
}

// Branch on prefs rather than `??`: tableSort's off state is null, which `??`
// would fall through to the prop.
export function resolveTableSort(prefs: Preferences | undefined, tableSort: TableSort | null | undefined): TableSort | null {
  return prefs ? prefs.tableSort : (tableSort ?? null);
}

export function emitFilter(prefs: Preferences | undefined, onchange: ((f: NibFilter) => void) | undefined, updated: NibFilter): void {
  if (prefs) {
    prefs.filter = updated;
  } else {
    onchange?.(updated);
  }
}

export function emitTableSort(prefs: Preferences | undefined, onchange: ((s: TableSort | null) => void) | undefined, sort: TableSort | null): void {
  if (prefs) {
    prefs.tableSort = sort;
  } else {
    onchange?.(sort);
  }
}

// Not exported: change the view through switchViewLevel, which records the switch.
function emitViewLevel(prefs: Preferences | undefined, onchange: ((v: ViewLevel) => void) | undefined, viewLevel: ViewLevel): void {
  if (prefs) {
    prefs.viewLevel = viewLevel;
  } else {
    onchange?.(viewLevel);
  }
}

/**
 * Switch the view level. Change the view only through this.
 *
 * The switch is recorded on `treeView` so TreeTable reconciles selection and
 * focus against the new view's rows: a grouping lens can hide a row that would
 * otherwise stay selected and a bulk-action target off screen.
 *
 * `from` is the caller's resolved current level; a caller without prefs has no
 * other way to supply it. `treeView` is optional for a control rendered without a
 * table; the write still happens.
 */
export function switchViewLevel(
  prefs: Preferences | undefined,
  onchange: ((v: ViewLevel) => void) | undefined,
  treeView: TreeViewState | undefined,
  from: ViewLevel,
  to: ViewLevel,
): void {
  // Re-picking the active level is a no-op; recording it would prune the
  // selection and reset the scroll position.
  if (from === to) return;
  treeView?.beginTransition(from, to);
  emitViewLevel(prefs, onchange, to);
}

export function emitColumnOrder(prefs: Preferences | undefined, onchange: ((order: ColumnKey[]) => void) | undefined, order: ColumnKey[]): void {
  if (prefs) {
    prefs.order.setLevel(prefs.viewLevel, order);
  } else {
    onchange?.(order);
  }
}
