import type { NibFilter, ViewLevel, ColumnKey } from "./types";
import { ALL_COLUMN_KEYS, DEFAULT_COLUMN_WIDTHS } from "./types";
import type { Preferences } from "./preferences.svelte";

export function resolveFilter(prefs: Preferences | undefined, filter: NibFilter | undefined): NibFilter {
  return prefs?.filter ?? filter ?? {};
}

export function resolveViewLevel(prefs: Preferences | undefined, viewLevel: ViewLevel | undefined): ViewLevel {
  return prefs?.viewLevel ?? viewLevel ?? "none";
}

export function resolveVisibleColumns(prefs: Preferences | undefined, visibleColumns: ColumnKey[] | undefined): ColumnKey[] {
  return prefs?.visibleColumns ?? visibleColumns ?? [...ALL_COLUMN_KEYS];
}

export function resolveColumnWidths(prefs: Preferences | undefined, columnWidths: Record<ColumnKey, number> | undefined): Record<ColumnKey, number> {
  return prefs?.currentColumnWidths ?? columnWidths ?? { ...DEFAULT_COLUMN_WIDTHS };
}

export function emitFilter(prefs: Preferences | undefined, onchange: ((f: NibFilter) => void) | undefined, updated: NibFilter): void {
  if (prefs) {
    prefs.filter = updated;
  } else {
    onchange?.(updated);
  }
}
