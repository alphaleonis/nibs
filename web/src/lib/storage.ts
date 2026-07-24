import { VIEW_LEVELS, MIN_DETAIL_PANEL_WIDTH, MIN_DETAIL_PANEL_HEIGHT, DETAIL_PANEL_POSITIONS, BLOCKED_EMPHASES, THEMES, DEFAULT_THEME, FONT_SCALES } from "./types";
import type { FilterPreferences, RowDensity, ViewLevel, Theme, DetailPanelPosition, BlockedEmphasis, FontSize, NibFilter, TableSort } from "./types";
import { ALL_COLUMN_KEYS, ALWAYS_VISIBLE_KEYS, SORTABLE_COLUMN_KEYS } from "./columns";
import type { ColumnKey } from "./columns";
import { STATUSES } from "./constants";

const ALWAYS_VISIBLE_KEY_SET = new Set<ColumnKey>(ALWAYS_VISIBLE_KEYS);

// Duplicated verbatim by the pre-paint FOUC guard in index.html; exported so
// src/lib/fouc-guard.test.ts can assert the two stay in sync.
export const STORAGE_KEY = "nibs-filter-preferences";

const DEFAULTS: FilterPreferences = {
  filter: {},
  viewLevel: "none",
  theme: DEFAULT_THEME,
};

// Sanitize/migrate a persisted filter. The legacy `excludeStatus` field (the
// retired standalone hide-completed negative filter) is folded into
// the single `status` include-list: with no explicit include-list present it is
// translated to the equivalent one (every status except the excluded ones,
// STATUSES order preserved); otherwise it is dropped, since `status` is now the
// single source of truth for status visibility. Never crashes on old state.
function parseFilter(raw: unknown): NibFilter {
  if (typeof raw !== "object" || raw === null) return {};
  const source = raw as Record<string, unknown> & { excludeStatus?: unknown };
  const { excludeStatus, ...rest } = source;
  const filter = rest as NibFilter;

  const hasStatus = Array.isArray(filter.status) && filter.status.length > 0;
  if (!hasStatus && Array.isArray(excludeStatus) && excludeStatus.length > 0) {
    const excluded = new Set(excludeStatus.filter((s): s is string => typeof s === "string"));
    filter.status = STATUSES.filter((s) => !excluded.has(s));
  }
  return filter;
}

const VALID_COLUMN_KEYS = new Set<string>(ALL_COLUMN_KEYS);

// Shared per-view map parser: one VIEW_LEVELS loop, with the concern-specific
// per-level validator injected. A level is included only when the validator
// returns a value; the whole map collapses to undefined when no level survives
// (so an absent/garbage field stays undefined and Preferences supplies defaults).
export function parsePerViewMap<T>(
  raw: unknown,
  validateLevel: (raw: unknown) => T | undefined,
): Partial<Record<ViewLevel, T>> | undefined {
  if (typeof raw !== "object" || raw === null) return undefined;
  const result: Partial<Record<ViewLevel, T>> = {};
  for (const level of VIEW_LEVELS) {
    const value = validateLevel((raw as Record<string, unknown>)[level]);
    if (value !== undefined) {
      result[level] = value;
    }
  }
  return Object.keys(result).length > 0 ? result : undefined;
}

// Per-level validator for columnVisibility: keep valid column keys, always
// re-add the alwaysVisible columns (title today) so they survive a round-trip.
// A non-array (missing/garbage) level yields undefined so it is dropped.
function validateVisibilityLevel(raw: unknown): ColumnKey[] | undefined {
  if (!Array.isArray(raw)) return undefined;
  const filtered = raw.filter(
    (v): v is ColumnKey => typeof v === "string" && VALID_COLUMN_KEYS.has(v),
  );
  for (const key of ALWAYS_VISIBLE_KEY_SET) {
    if (!filtered.includes(key)) {
      filtered.push(key);
    }
  }
  return filtered.length > 0 ? filtered : undefined;
}

// Per-level validator for columnWidths: keep valid column keys mapped to
// positive finite numbers. A non-object (or array) level yields undefined.
function validateWidthsLevel(raw: unknown): Partial<Record<ColumnKey, number>> | undefined {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) return undefined;
  const widths: Partial<Record<ColumnKey, number>> = {};
  let count = 0;
  for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
    if (VALID_COLUMN_KEYS.has(key) && typeof value === "number" && value > 0 && isFinite(value)) {
      widths[key as ColumnKey] = value;
      count++;
    }
  }
  return count > 0 ? widths : undefined;
}

function parseDetailPanelWidth(raw: unknown): number | undefined {
  if (typeof raw !== "number" || !isFinite(raw) || raw <= 0) return undefined;
  return Math.max(MIN_DETAIL_PANEL_WIDTH, raw);
}

const VALID_DETAIL_PANEL_POSITIONS = new Set<string>(DETAIL_PANEL_POSITIONS);

// Optional like detailPanelWidth/rowDensity: return undefined for
// missing/garbage so Preferences supplies the concrete default.
function parseDetailPanelPosition(raw: unknown): DetailPanelPosition | undefined {
  if (typeof raw === "string" && VALID_DETAIL_PANEL_POSITIONS.has(raw)) return raw as DetailPanelPosition;
  return undefined;
}

function parseDetailPanelHeight(raw: unknown): number | undefined {
  if (typeof raw !== "number" || !isFinite(raw) || raw <= 0) return undefined;
  return Math.max(MIN_DETAIL_PANEL_HEIGHT, raw);
}

const VALID_ROW_DENSITIES = new Set<string>(["compact", "comfortable"]);

function parseRowDensity(raw: unknown): RowDensity | undefined {
  if (typeof raw !== "string" || !VALID_ROW_DENSITIES.has(raw)) return undefined;
  return raw as RowDensity;
}

const VALID_FONT_SIZES = new Set<string>(Object.keys(FONT_SCALES));

// Optional like rowDensity: return undefined for missing/garbage so Preferences
// supplies the concrete default (medium).
function parseFontSize(raw: unknown): FontSize | undefined {
  if (typeof raw !== "string" || !VALID_FONT_SIZES.has(raw)) return undefined;
  return raw as FontSize;
}

const VALID_BLOCKED_EMPHASES = new Set<string>(BLOCKED_EMPHASES);

// Optional like rowDensity: return undefined for missing/garbage so Preferences
// supplies the concrete default.
function parseBlockedEmphasis(raw: unknown): BlockedEmphasis | undefined {
  if (typeof raw !== "string" || !VALID_BLOCKED_EMPHASES.has(raw)) return undefined;
  return raw as BlockedEmphasis;
}

// Optional like rowDensity/blockedEmphasis: return undefined for
// missing/garbage so Preferences supplies the concrete default.
function parsePreviewOpen(raw: unknown): boolean | undefined {
  return typeof raw === "boolean" ? raw : undefined;
}

// The full sortable-field set is single-sourced in columns.ts (derived from
// COLUMNS[].sortable). A persisted field naming a column that was removed or made
// non-sortable falls out of this set and is treated as off (no unset-vs-off
// ambiguity), so old preferences never crash or pin a sort to a gone column.
const VALID_TABLE_SORT_FIELDS = new Set<string>(SORTABLE_COLUMN_KEYS);
const VALID_TABLE_SORT_DIRECTIONS = new Set<string>(["asc", "desc"]);

// Optional like blockedEmphasis: return the object only when BOTH field and
// direction are valid enums; else undefined so Preferences treats it as off
// (null). An absent/invalid tableSort means "no sort" — no unset-vs-off
// ambiguity.
function parseTableSort(raw: unknown): TableSort | undefined {
  if (typeof raw !== "object" || raw === null) return undefined;
  const { field, direction } = raw as Record<string, unknown>;
  if (
    typeof field === "string" && VALID_TABLE_SORT_FIELDS.has(field) &&
    typeof direction === "string" && VALID_TABLE_SORT_DIRECTIONS.has(direction)
  ) {
    return { field: field as TableSort["field"], direction: direction as TableSort["direction"] };
  }
  return undefined;
}

const VALID_THEMES = new Set<string>(THEMES.map(t => t.value));

// Validate a persisted theme against the known set, falling back to the default
// for missing/garbage/unknown values. (Unlike the *optional* prefs above which
// return undefined, theme always resolves to a concrete value.)
export function parseTheme(raw: unknown): Theme {
  if (typeof raw === "string" && VALID_THEMES.has(raw)) return raw as Theme;
  return DEFAULT_THEME;
}

export function loadPreferences(): FilterPreferences {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (!raw) return { ...DEFAULTS, filter: { ...DEFAULTS.filter } };
    const parsed = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null) return { ...DEFAULTS, filter: { ...DEFAULTS.filter } };
    return {
      filter: parseFilter(parsed.filter),
      viewLevel: (VIEW_LEVELS as readonly string[]).includes(parsed.viewLevel)
        ? parsed.viewLevel
        : DEFAULTS.viewLevel,
      columnVisibility: parsePerViewMap(parsed.columnVisibility, validateVisibilityLevel),
      columnWidths: parsePerViewMap(parsed.columnWidths, validateWidthsLevel),
      detailPanelWidth: parseDetailPanelWidth(parsed.detailPanelWidth),
      detailPanelPosition: parseDetailPanelPosition(parsed.detailPanelPosition),
      detailPanelHeight: parseDetailPanelHeight(parsed.detailPanelHeight),
      rowDensity: parseRowDensity(parsed.rowDensity),
      fontSize: parseFontSize(parsed.fontSize),
      blockedEmphasis: parseBlockedEmphasis(parsed.blockedEmphasis),
      theme: parseTheme(parsed.theme),
      previewOpen: parsePreviewOpen(parsed.previewOpen),
      tableSort: parseTableSort(parsed.tableSort),
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
