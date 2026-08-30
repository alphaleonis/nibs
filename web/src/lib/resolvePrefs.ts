import type { NibFilter, ViewLevel, ColumnKey, TableSort } from "./types";
import { ALL_COLUMN_KEYS, DEFAULT_COLUMN_WIDTHS, DEFAULT_VISIBLE_COLUMNS, DEFAULT_VIEW_LEVEL } from "./types";
import type { Preferences } from "./preferences.svelte";
// Type-only, so this module keeps its rune-free posture — it has no runtime
// imports and every consumer can reach it.
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

// The full per-view column order (all keys). Falls back to the canonical
// ALL_COLUMN_KEYS order when neither prefs nor prop supplies one.
export function resolveColumnOrder(prefs: Preferences | undefined, columnOrder: ColumnKey[] | undefined): ColumnKey[] {
  return prefs?.currentColumnOrder ?? columnOrder ?? [...ALL_COLUMN_KEYS];
}

// Unlike the sibling resolvers, tableSort's "off" state is literally `null`, which
// is nullish — so `prefs?.tableSort ?? tableSort` would fall through to the prop
// whenever the persisted preference is legitimately off. Branch on prefs presence
// (matching the write path) so a supplied prefs always wins, null included.
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

// Mirror of emitFilter for the table-sort write path: with prefs present, write
// through to the persisted preference (null = off included); otherwise emit via
// the callback. Keeps the read (resolveTableSort) and write paths symmetric.
export function emitTableSort(prefs: Preferences | undefined, onchange: ((s: TableSort | null) => void) | undefined, sort: TableSort | null): void {
  if (prefs) {
    prefs.tableSort = sort;
  } else {
    onchange?.(sort);
  }
}

// View-level write path, mirroring emitFilter/emitTableSort. Deliberately NOT
// exported: `switchViewLevel` is the only way to change the view, so a caller
// cannot take the write without the reconcile that has to accompany it.
function emitViewLevel(prefs: Preferences | undefined, onchange: ((v: ViewLevel) => void) | undefined, viewLevel: ViewLevel): void {
  if (prefs) {
    prefs.viewLevel = viewLevel;
  } else {
    onchange?.(viewLevel);
  }
}

/**
 * Switch the view — the ONE way the level changes, for every control that offers
 * it (the toolbar's picker, the blocked-drag toast's remedy).
 *
 * A grouping lens is lossless in work items but not in rows: it hides a container
 * ranked above its tier while descending into it, so a milestone selected in the
 * Tree view has no row at all under the Epics lens and would otherwise stay
 * selected, focused, and a legal bulk-action target off screen. The write alone
 * cannot say that happened — `prefs.viewLevel` records which view is on screen,
 * never that it just changed or from what — so the switch is recorded on the tree
 * view for TreeTable's applier to reconcile against the incoming view's rows.
 *
 * `from` is the caller's RESOLVED current level (prefs or prop), not re-derived
 * here: a component holding only the prop contract has no prefs to read it back
 * from, and guessing would make the no-op check below wrong exactly there.
 *
 * `treeView` is optional because a control can render without a table to
 * reconcile — Toolbar does, in its own tests. The write still happens; only the
 * reconcile has nobody to run it.
 */
export function switchViewLevel(
  prefs: Preferences | undefined,
  onchange: ((v: ViewLevel) => void) | undefined,
  treeView: TreeViewState | undefined,
  from: ViewLevel,
  to: ViewLevel,
): void {
  // The picker offers every level including the active one. Re-picking it is not
  // an event: recording a transition would prune a selection nothing invalidated
  // and throw away the scroll position for no reason.
  if (from === to) return;
  treeView?.beginTransition(from, to);
  emitViewLevel(prefs, onchange, to);
}

// Column-reorder write path: with prefs present, write the new full order for the
// current view (auto-saved via the `order` per-view map); otherwise emit via the
// callback. Mirrors the visibility write (Toolbar's prefs.visibility.setLevel).
export function emitColumnOrder(prefs: Preferences | undefined, onchange: ((order: ColumnKey[]) => void) | undefined, order: ColumnKey[]): void {
  if (prefs) {
    prefs.order.setLevel(prefs.viewLevel, order);
  } else {
    onchange?.(order);
  }
}
