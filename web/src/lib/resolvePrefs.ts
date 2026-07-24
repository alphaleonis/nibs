import type { NibFilter, ViewLevel, ColumnKey, TableSort } from "./types";
import { ALL_COLUMN_KEYS, DEFAULT_COLUMN_WIDTHS, DEFAULT_VISIBLE_COLUMNS } from "./types";
import type { Preferences } from "./preferences.svelte";

export function resolveFilter(prefs: Preferences | undefined, filter: NibFilter | undefined): NibFilter {
  return prefs?.filter ?? filter ?? {};
}

export function resolveViewLevel(prefs: Preferences | undefined, viewLevel: ViewLevel | undefined): ViewLevel {
  return prefs?.viewLevel ?? viewLevel ?? "none";
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
