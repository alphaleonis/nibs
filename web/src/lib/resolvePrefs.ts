import type { NibFilter, ViewLevel, ColumnKey, FlatSort } from "./types";
import { DEFAULT_COLUMN_WIDTHS, DEFAULT_VISIBLE_COLUMNS } from "./types";
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

// Unlike the sibling resolvers, flatSort's "off" state is literally `null`, which
// is nullish — so `prefs?.flatSort ?? flatSort` would fall through to the prop
// whenever the persisted preference is legitimately off. Branch on prefs presence
// (matching the write path) so a supplied prefs always wins, null included.
export function resolveFlatSort(prefs: Preferences | undefined, flatSort: FlatSort | null | undefined): FlatSort | null {
  return prefs ? prefs.flatSort : (flatSort ?? null);
}

export function emitFilter(prefs: Preferences | undefined, onchange: ((f: NibFilter) => void) | undefined, updated: NibFilter): void {
  if (prefs) {
    prefs.filter = updated;
  } else {
    onchange?.(updated);
  }
}

// Mirror of emitFilter for the flat-sort write path: with prefs present, write
// through to the persisted preference (null = off included); otherwise emit via
// the callback. Keeps the read (resolveFlatSort) and write paths symmetric.
export function emitFlatSort(prefs: Preferences | undefined, onchange: ((s: FlatSort | null) => void) | undefined, sort: FlatSort | null): void {
  if (prefs) {
    prefs.flatSort = sort;
  } else {
    onchange?.(sort);
  }
}
