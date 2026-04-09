import { VIEW_LEVELS, ALL_COLUMN_KEYS, DEFAULT_COLUMNS, MIN_DETAIL_PANEL_WIDTH, MAX_DETAIL_PANEL_WIDTH } from "./types";
import type { ColumnKey, FilterPreferences, RowDensity, ViewLevel } from "./types";

const ALWAYS_VISIBLE_KEYS = new Set<ColumnKey>(
  DEFAULT_COLUMNS.filter(c => c.alwaysVisible).map(c => c.key),
);

const STORAGE_KEY = "nibs-filter-preferences";

const DEFAULTS: FilterPreferences = {
  filter: {},
  viewLevel: "milestones",
};

function parseColumnVisibility(
  raw: unknown,
): Partial<Record<ViewLevel, ColumnKey[]>> | undefined {
  if (typeof raw !== "object" || raw === null) return undefined;
  const result: Partial<Record<ViewLevel, ColumnKey[]>> = {};
  const validKeys = new Set<string>(ALL_COLUMN_KEYS);
  for (const level of VIEW_LEVELS) {
    const arr = (raw as Record<string, unknown>)[level];
    if (Array.isArray(arr)) {
      const filtered = arr.filter(
        (v): v is ColumnKey => typeof v === "string" && validKeys.has(v),
      );
      // Ensure alwaysVisible columns are always present
      for (const key of ALWAYS_VISIBLE_KEYS) {
        if (!filtered.includes(key)) {
          filtered.push(key);
        }
      }
      if (filtered.length > 0) {
        result[level] = filtered;
      }
    }
  }
  return Object.keys(result).length > 0 ? result : undefined;
}

function parseColumnWidths(
  raw: unknown,
): Partial<Record<ViewLevel, Partial<Record<ColumnKey, number>>>> | undefined {
  if (typeof raw !== "object" || raw === null) return undefined;
  const result: Partial<Record<ViewLevel, Partial<Record<ColumnKey, number>>>> = {};
  const validKeys = new Set<string>(ALL_COLUMN_KEYS);
  for (const level of VIEW_LEVELS) {
    const obj = (raw as Record<string, unknown>)[level];
    if (typeof obj === "object" && obj !== null && !Array.isArray(obj)) {
      const widths: Partial<Record<ColumnKey, number>> = {};
      let count = 0;
      for (const [key, value] of Object.entries(obj as Record<string, unknown>)) {
        if (validKeys.has(key) && typeof value === "number" && value > 0 && isFinite(value)) {
          widths[key as ColumnKey] = value;
          count++;
        }
      }
      if (count > 0) {
        result[level] = widths;
      }
    }
  }
  return Object.keys(result).length > 0 ? result : undefined;
}

function parseDetailPanelWidth(raw: unknown): number | undefined {
  if (typeof raw !== "number" || !isFinite(raw) || raw <= 0) return undefined;
  return Math.max(MIN_DETAIL_PANEL_WIDTH, Math.min(MAX_DETAIL_PANEL_WIDTH, raw));
}

const VALID_ROW_DENSITIES = new Set<string>(["compact", "comfortable"]);

function parseRowDensity(raw: unknown): RowDensity | undefined {
  if (typeof raw !== "string" || !VALID_ROW_DENSITIES.has(raw)) return undefined;
  return raw as RowDensity;
}

export function loadPreferences(): FilterPreferences {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return { ...DEFAULTS, filter: { ...DEFAULTS.filter } };
    const parsed = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null) return { ...DEFAULTS, filter: { ...DEFAULTS.filter } };
    return {
      filter: typeof parsed.filter === "object" && parsed.filter !== null
        ? parsed.filter
        : { ...DEFAULTS.filter },
      viewLevel: (VIEW_LEVELS as readonly string[]).includes(parsed.viewLevel)
        ? parsed.viewLevel
        : DEFAULTS.viewLevel,
      columnVisibility: parseColumnVisibility(parsed.columnVisibility),
      columnWidths: parseColumnWidths(parsed.columnWidths),
      detailPanelWidth: parseDetailPanelWidth(parsed.detailPanelWidth),
      rowDensity: parseRowDensity(parsed.rowDensity),
    };
  } catch {
    return { ...DEFAULTS, filter: { ...DEFAULTS.filter } };
  }
}

export function savePreferences(prefs: FilterPreferences): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(prefs));
  } catch {
    // Silently fail if localStorage is not available (SSR, privacy mode, etc.)
  }
}
