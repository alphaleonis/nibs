import { VIEW_LEVELS, DEFAULT_VIEW_LEVEL, MIN_DETAIL_PANEL_WIDTH, MIN_DETAIL_PANEL_HEIGHT, DETAIL_PANEL_POSITIONS, OPEN_DETAIL_GESTURES, BLOCKED_EMPHASES, REGION_BAND_MODES, THEMES, DEFAULT_THEME, FONT_SCALES } from "./types";
import type { FilterPreferences, RowDensity, ViewLevel, Theme, DetailPanelPosition, OpenDetailGesture, BlockedEmphasis, RegionBandMode, FontSize, NibFilter, TableSort } from "./types";
import { ALL_COLUMN_KEYS, ALWAYS_VISIBLE_KEYS, SORTABLE_COLUMN_KEYS } from "./columns";
import type { ColumnKey } from "./columns";
import { serializeQuery } from "./query";

const ALWAYS_VISIBLE_KEY_SET = new Set<ColumnKey>(ALWAYS_VISIBLE_KEYS);

// Duplicated verbatim by the pre-paint FOUC guard in index.html; exported so
// src/lib/fouc-guard.test.ts can assert the two stay in sync.
export const STORAGE_KEY = "nibs-filter-preferences";

const DEFAULTS: FilterPreferences = {
  query: "",
  viewLevel: DEFAULT_VIEW_LEVEL,
  theme: DEFAULT_THEME,
};

// Resolve the persisted filter to a canonical query STRING. Two formats are
// accepted so a returning user never loses their filter or crashes the load:
//   - New: `q` is already a query string — returned verbatim (Preferences
//     re-parses it, so a hand-edited/foreign value is tolerated downstream).
//   - Legacy: an older build persisted the structured `filter: NibFilter`
//     directly. It is serialized to the equivalent canonical string. This is a
//     FAITHFUL translation — a persisted `excludeStatus` becomes `-status:…`
//     (behaviorally identical to hiding those statuses), NOT rewritten into a
//     status include-list. That old include-list rewrite was for the retired
//     hide-completed toggle; folding it in here would mangle a `-status:X`
//     negation on reload (see nibs-grvv Phase-2 note).
// serializeQuery covers EVERY NibFilter field — the box owns the relationship and
// existence keys and the area path — so a legacy structured blob translates in
// full, with nothing dropped.
//
// PERSISTED-FORMAT NOTE — a status group token stores a RULE, not a set.
// serializeQuery collapses a group wherever all of its members are present, so a
// persisted `status:draft,todo,in-progress` is rewritten to `status:open` the
// next time it is saved (same for the `?q=` link built from it). What was an
// enumerated choice of three statuses becomes "everything not closed". Add a
// status to STATUSES and every stored or shared `status:open` widens to include
// it, and two clients on different versions resolve the same link differently.
//
// Collapse is NOT limited to an exact whole-list match, so this applies to more
// stored queries than it reads like: `status:draft,todo,in-progress,completed`
// persists as `status:open,completed` and widens later too. The rule is per
// group, not per token — a value outside every group (`completed` here) stays
// an enumerated choice and never widens.
// That is the intended behavior — group membership is derived on purpose (see
// constants.ts) — but it means the stored string is not a faithful record of
// what the user ticked. CLOSED_STATUSES / OPEN_STATUSES are pinned verbatim by
// filter.test.ts, so growing the vocabulary is a deliberate act, not a silent one.
function parseQueryField(parsed: Record<string, unknown>): string {
  if (typeof parsed.q === "string") return parsed.q;
  const legacy = parsed.filter;
  if (typeof legacy === "object" && legacy !== null && !Array.isArray(legacy)) {
    return serializeQuery({ filter: legacy as NibFilter });
  }
  return "";
}

const VALID_COLUMN_KEYS = new Set<string>(ALL_COLUMN_KEYS);

// One-time load migration for column-key renames (state → status, effort →
// estimate). Preferences persisted before a rename stored the column under its
// old key in the per-view visibility/order arrays, the per-view widths map, and
// the active tableSort's `field`. Rewrite each occurrence to the new key on the
// RAW parsed blob BEFORE the validators run, so the column keeps its persisted
// position, width, and sort — otherwise the now-unknown old key is dropped by the
// visibility/widths validators, appended out of place by parseColumnOrder, and an
// old tableSort field is rejected as invalid (sort silently lost).
const LEGACY_COLUMN_KEY_RENAMES: Record<string, string> = { state: "status", effort: "estimate" };

function renameLegacyColumnKey(key: string): string {
  return LEGACY_COLUMN_KEY_RENAMES[key] ?? key;
}

// Rename legacy keys in one persisted per-view ARRAY map (columnVisibility /
// columnOrder): each level's array has its string elements renamed in place.
function migratePerViewArray(raw: unknown): void {
  if (typeof raw !== "object" || raw === null) return;
  for (const level of Object.values(raw as Record<string, unknown>)) {
    if (!Array.isArray(level)) continue;
    for (let i = 0; i < level.length; i++) {
      if (typeof level[i] === "string") level[i] = renameLegacyColumnKey(level[i]);
    }
  }
}

// Rename legacy keys in the per-view WIDTHS map: each level is a {columnKey:
// width} object whose KEYS are renamed in place. A pre-existing entry under the
// new key wins (the legacy one is discarded) so a partial-migration blob can't
// clobber a real "status" width.
function migratePerViewWidths(raw: unknown): void {
  if (typeof raw !== "object" || raw === null) return;
  for (const level of Object.values(raw as Record<string, unknown>)) {
    if (typeof level !== "object" || level === null || Array.isArray(level)) continue;
    const widths = level as Record<string, unknown>;
    for (const [key, value] of Object.entries(widths)) {
      const renamed = renameLegacyColumnKey(key);
      if (renamed === key) continue;
      if (!(renamed in widths)) widths[renamed] = value;
      delete widths[key];
    }
  }
}

// Rename a legacy tableSort.field in place.
function migrateTableSortField(raw: unknown): void {
  if (typeof raw !== "object" || raw === null) return;
  const sort = raw as Record<string, unknown>;
  if (typeof sort.field === "string") sort.field = renameLegacyColumnKey(sort.field);
}

// Apply the legacy column-key renames across every persisted field that carries a
// column key. Mutates the freshly-parsed blob owned by loadPreferences.
function migrateLegacyColumnKeys(parsed: Record<string, unknown>): void {
  migratePerViewArray(parsed.columnVisibility);
  migratePerViewArray(parsed.columnOrder);
  migratePerViewWidths(parsed.columnWidths);
  migrateTableSortField(parsed.tableSort);
}

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

// Per-level validator for columnOrder: keep the persisted order of valid,
// non-duplicate column keys, then APPEND any ColumnKey that is missing (in
// canonical ALL_COLUMN_KEYS order) so a newly-added column still appears — the
// resolved order is (persisted valid ∪ missing-appended). Unknown/duplicate keys
// are dropped. A non-array (missing/garbage) level yields undefined so it is
// dropped and Preferences supplies the default order.
export function parseColumnOrder(raw: unknown): ColumnKey[] | undefined {
  if (!Array.isArray(raw)) return undefined;
  const seen = new Set<ColumnKey>();
  const ordered: ColumnKey[] = [];
  for (const v of raw) {
    if (typeof v === "string" && VALID_COLUMN_KEYS.has(v) && !seen.has(v as ColumnKey)) {
      seen.add(v as ColumnKey);
      ordered.push(v as ColumnKey);
    }
  }
  for (const key of ALL_COLUMN_KEYS) {
    if (!seen.has(key)) ordered.push(key);
  }
  return ordered;
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

const VALID_OPEN_DETAIL_GESTURES = new Set<string>(OPEN_DETAIL_GESTURES);

// Optional like detailPanelPosition: return undefined for missing/garbage so
// Preferences supplies the concrete default ("single", today's behavior).
function parseOpenDetailOn(raw: unknown): OpenDetailGesture | undefined {
  if (typeof raw === "string" && VALID_OPEN_DETAIL_GESTURES.has(raw)) return raw as OpenDetailGesture;
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

const VALID_REGION_BAND_MODES = new Set<string>(REGION_BAND_MODES);

// Optional like blockedEmphasis. A stored "always" from a build before the mode
// existed lands here as garbage and returns undefined, so such a session comes
// back on the current default rather than on a mode this build cannot draw.
function parseRegionBands(raw: unknown): RegionBandMode | undefined {
  if (typeof raw !== "string" || !VALID_REGION_BAND_MODES.has(raw)) return undefined;
  return raw as RegionBandMode;
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
    if (!raw) return { ...DEFAULTS };
    const parsed = JSON.parse(raw);
    if (typeof parsed !== "object" || parsed === null) return { ...DEFAULTS };
    migrateLegacyColumnKeys(parsed);
    return {
      query: parseQueryField(parsed),
      viewLevel: (VIEW_LEVELS as readonly string[]).includes(parsed.viewLevel)
        ? parsed.viewLevel
        : DEFAULTS.viewLevel,
      columnVisibility: parsePerViewMap(parsed.columnVisibility, validateVisibilityLevel),
      columnWidths: parsePerViewMap(parsed.columnWidths, validateWidthsLevel),
      columnOrder: parsePerViewMap(parsed.columnOrder, parseColumnOrder),
      detailPanelWidth: parseDetailPanelWidth(parsed.detailPanelWidth),
      detailPanelPosition: parseDetailPanelPosition(parsed.detailPanelPosition),
      openDetailOn: parseOpenDetailOn(parsed.openDetailOn),
      detailPanelHeight: parseDetailPanelHeight(parsed.detailPanelHeight),
      rowDensity: parseRowDensity(parsed.rowDensity),
      fontSize: parseFontSize(parsed.fontSize),
      blockedEmphasis: parseBlockedEmphasis(parsed.blockedEmphasis),
      regionBands: parseRegionBands(parsed.regionBands),
      theme: parseTheme(parsed.theme),
      previewOpen: parsePreviewOpen(parsed.previewOpen),
      tableSort: parseTableSort(parsed.tableSort),
    };
  } catch {
    return { ...DEFAULTS };
  }
}

export function savePreferences(prefs: FilterPreferences): void {
  try {
    // Persist the canonical query STRING under `q` (mirroring the `?q=` URL
    // param and marking the new format for loadPreferences); the remaining
    // preferences persist structured, exactly as before.
    const { query, ...rest } = prefs;
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ q: query, ...rest }));
  } catch {
    // Silently fail if localStorage is not available (SSR, privacy mode, etc.)
  }
}
