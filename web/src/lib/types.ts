export interface NibSummary {
  id: string;
  title: string;
  status: string;
  type: string;
  priority: string;
  estimate: string;
  tags: string[];
  createdAt: string;
  updatedAt: string;
}

export interface NibFilter {
  search?: string;
  status?: string[];
  excludeStatus?: string[];
  type?: string[];
  excludeType?: string[];
  priority?: string[];
  excludePriority?: string[];
  estimate?: string[];
  excludeEstimate?: string[];
  tags?: string[];
  excludeTags?: string[];
  hasParent?: boolean;
  parentId?: string;
  // Hierarchy predicates. Each names the relationship the MATCHED nib holds toward
  // the supplied id, so `ancestorId` selects that nib's descendants and
  // `descendantId` selects its ancestor chain. The target itself is excluded by the
  // filter. When the query also carries free text, the server re-adds every match's
  // ancestors afterwards, so an ancestorId target reappears and a siblingId query
  // also brings in the shared parent — that completion is what the tree rendering
  // relies on, not a bug.
  // siblingId selects nibs sharing the target's parent; a parentless target selects
  // the other root nibs, matching `nibs rel --rel siblings`.
  ancestorId?: string;
  descendantId?: string;
  siblingId?: string;
  hasBlocking?: boolean;
  blockingId?: string;
  isBlocked?: boolean;
  hasBlockedBy?: boolean;
  blockedById?: string;
  mentionsId?: string;
  mentionedById?: string;
}

// Compile-time guard binding the hand-written NibFilter above to the codegen'd
// one, so the two key sets cannot drift.
//
// The filter reaches the wire as a variable (`variables: { filter }` in
// useTableData.svelte.ts), not an object literal, so TypeScript's
// excess-property check never runs on it: an extra or misspelled client-side key
// would type-check, ship, and be silently ignored by the server.
//
// BOTH directions are required. A one-way `extends` is satisfied by extra
// properties, so it would miss exactly that misspelling case; the reverse
// direction catches a key the schema gained that the client never picked up.
//
// The hand-written type is kept rather than replaced by the generated one
// because the generated fields are spelled `T | null | undefined` where these
// are optional `T?`, which every consumer (prefs.filter, QueryFilter,
// parse/serialize) relies on. `import type` keeps this erased at compile time.
import type { NibFilter as GeneratedNibFilter } from "./gql/graphql";

type _ClientKeysExistOnGenerated = keyof NibFilter extends keyof GeneratedNibFilter ? true : never;
const _clientKeysCheck: _ClientKeysExistOnGenerated = true;
void _clientKeysCheck;

type _GeneratedKeysExistOnClient = keyof GeneratedNibFilter extends keyof NibFilter ? true : never;
const _generatedKeysCheck: _GeneratedKeysExistOnClient = true;
void _generatedKeysCheck;

export interface TreeNib extends NibSummary {
  parentId: string | null;
}

export interface TreeTableNib extends TreeNib {
  blockingIds: string[];
  blockedByIds: string[];
}

export interface TreeNode<T extends TreeNib = TreeNib> {
  nib: T;
  children: TreeNode<T>[];
  depth: number;
}

/**
 * Subtree expand/collapse actions for a row, supplied by TreeTable (which owns
 * the collapse state via TreeViewState) to the row context menu through the
 * `onrowcontextmenu` callback. `hasChildren` gates whether the menu shows the
 * options at all.
 */
export interface RowSubtreeActions {
  hasChildren: boolean;
  /** Fully expand this row and every descendant. */
  expandChildren: () => void;
  /** Collapse this row and every descendant (re-expanding reveals one level). */
  collapseChildren: () => void;
}

export const VIEW_LEVELS = ["none", "flat", "milestones", "epics", "features"] as const;
export type ViewLevel = (typeof VIEW_LEVELS)[number];

// Client-side table sort. Absent/null means "off" (manual `order` sequence).
// Applied in every view: a flat sorted list in Flat, sibling-sort (siblings,
// roots, grouping-bucket items, and promoted group headers reordered, nesting
// preserved) in the Tree + grouping-lens views. The field union is
// single-sourced as `SortKey` in columns.ts (the sortable ColumnKey subset);
// `SortField` re-exports it so the many "./types" importers keep working without
// duplicating the set.
export type SortField = SortKey;
export type SortDirection = "asc" | "desc";
export interface TableSort {
  field: SortField;
  direction: SortDirection;
}

// The column model lives in columns.ts (pure, zero Svelte dependency). These are
// re-exported here so the many existing importers of "./types" keep working; the
// canonical definitions are single-sourced in columns.ts.
import { COLUMNS, ALL_COLUMN_KEYS, DEFAULT_COLUMN_WIDTHS, DEFAULT_VISIBLE_COLUMNS } from "./columns";
import type { ColumnKey, SortKey } from "./columns";
export { ALL_COLUMN_KEYS, DEFAULT_COLUMN_WIDTHS, DEFAULT_VISIBLE_COLUMNS };
export type { ColumnKey, SortKey };

export interface ColumnConfig {
  key: ColumnKey;
  label: string;
  alwaysVisible: boolean;
  // Omitted ⇒ visible by default. Set false for opt-in columns that start hidden
  // (e.g. blocking / blockedBy) but remain toggleable in the Columns dropdown.
  defaultVisible?: boolean;
}

// Derived from COLUMNS, order-preserving. Feeds the Columns dropdown config and
// the persistence layer. Preserves the legacy convention that a default-visible
// column omits `defaultVisible` (present-and-false only for opt-in columns), so
// existing consumers/tests that key off its presence stay valid.
export const DEFAULT_COLUMNS: ColumnConfig[] = ALL_COLUMN_KEYS.map((key) => {
  const c = COLUMNS[key];
  const config: ColumnConfig = { key: c.key, label: c.label, alwaysVisible: c.alwaysVisible };
  if (!c.defaultVisible) config.defaultVisible = false;
  return config;
});

export const DEFAULT_DETAIL_PANEL_WIDTH = 400;
export const MIN_DETAIL_PANEL_WIDTH = 200;
export const MAX_DETAIL_PANEL_PERCENT = 75;
// Size the detail pane opens at when the user hasn't resized it — a percent of
// the container, so the default stays screen-relative instead of a fixed px that
// looks narrow on large displays. Applies to both dock orientations.
export const DEFAULT_DETAIL_PANEL_PERCENT = 40;

// The detail panel can dock at the RIGHT (table on the left, preview on the
// right) or the BOTTOM (table on top, preview below). Bottom keeps full table
// width when many columns are shown. MAX_DETAIL_PANEL_PERCENT is reused for both
// orientations; only the min/default sizes differ per axis.
export const DETAIL_PANEL_POSITIONS = ["right", "bottom"] as const;
export type DetailPanelPosition = (typeof DETAIL_PANEL_POSITIONS)[number];
export const DEFAULT_DETAIL_PANEL_POSITION: DetailPanelPosition = "right";
export const DEFAULT_DETAIL_PANEL_HEIGHT = 300;
export const MIN_DETAIL_PANEL_HEIGHT = 150;

// Which mouse gesture on a table row opens the nib in the detail panel. With
// "double", a plain single click selects and focuses the row WITHOUT opening the
// panel (so a stray click never replaces what the user is reading) and the open
// moves to the double-click path. "single" is the default because it keeps the
// open gesture and the row styling every existing profile already has.
// Post-mutation cleanup is narrower in BOTH modes, independently of this
// preference: a delete/archive now closes the detail panel only when it took out
// the nib the panel was showing (see clearAfterMutation in actionTarget.ts),
// where it used to close it unconditionally. That is reachable in "single" too,
// because plain arrow-key nav moves focus — and with it the action target —
// without moving the panel.
export const OPEN_DETAIL_GESTURES = ["single", "double"] as const;
export type OpenDetailGesture = (typeof OPEN_DETAIL_GESTURES)[number];
export const DEFAULT_OPEN_DETAIL_ON: OpenDetailGesture = "single";

export type RowDensity = "compact" | "comfortable";

// Global font-size preference. Scales the whole UI type scale from one root CSS
// variable (`--font-scale`). app.css multiplies the semantic type-size tokens
// and Tailwind's raw `--text-*` ladder by it; several components then read it
// directly for sizes no token covers — TreeTable's `--row-pad-y`,
// ActiveNibView's title, ui/button's `sm` size, and the `max-w` cap on
// ui/dropdown-menu's container primitives. Root font-size, the rem unit and the
// spacing scale stay untouched. Row height and that dropdown cap are the two
// dimensions beyond type that do move, so each box keeps tracking the text
// inside it. Decoupled from RowDensity, which still picks the BASE row padding.
export type FontSize = "small" | "medium" | "large";
// Default is Medium = 1.0 so existing users see no change.
export const DEFAULT_FONT_SIZE: FontSize = "medium";
// The multiplier each size feeds into `--font-scale`.
export const FONT_SCALES: Record<FontSize, number> = { small: 0.9, medium: 1, large: 1.15 };

// Whether the editor's side-by-side Preview pane is shown while editing a nib
// body. Persisted so the on/off choice survives remounts (docked↔expanded) and
// sessions. Defaults to on.
export const DEFAULT_PREVIEW_OPEN = true;

// How the "blocked" state is emphasized in the tree row + ActiveNibView header:
//   subtle   → the bare lock icon
//   pill     → tinted "Blocked" pill (default)
//   pill-dim → pill + the whole table row dimmed
export const BLOCKED_EMPHASES = ["subtle", "pill", "pill-dim"] as const;
export type BlockedEmphasis = (typeof BLOCKED_EMPHASES)[number];
export const DEFAULT_BLOCKED_EMPHASIS: BlockedEmphasis = "pill";

// Maps a BlockedEmphasis to the RelationBadge presentational `variant`. Exhaustive
// switch (no default) so adding a new emphasis is a compile-time error here until
// its variant is decided — the single source of truth for both the tree row and
// the ActiveNibView header. Row dimming (`pill-dim`) is handled separately at the
// row level; it is not a badge concern.
export function blockedVariantFor(e: BlockedEmphasis): "icon" | "pill" {
  switch (e) {
    case "subtle":
      return "icon";
    case "pill":
    case "pill-dim":
      return "pill";
  }
}

// Curated palettes selectable from the Settings sheet. The chosen palette is
// selected via the `data-theme` attribute on <html>. "midnight" is the original
// near-black palette (the bare :root values in app.css) and intentionally has no
// override block. "graphite" is the default for fresh profiles.
//
// Each entry carries a `dark` flag that OWNS the light/dark axis.
// The theme seam toggles the `.dark` class on <html> from this flag: applyTheme()
// (theme.ts) and the pre-paint FOUC guard (index.html) both set `.dark` to match
// the active theme's `dark` value. This matters because app.css wires Tailwind's
// `dark:` variant to that class (`@custom-variant dark (&:is(.dark *))`) —
// INDEPENDENT of `data-theme`. Shipped shadcn components use `dark:` utilities
// (e.g. dark:bg-input/30); driving `.dark` from the flag lets a LIGHT palette
// switch those utilities off so the app renders light, not just re-tint chrome.
// "daylight" is the first light entry (dark: false); the three original palettes
// stay dark (dark: true).
export const THEMES = [
  { value: "graphite", label: "Graphite", dark: true },
  { value: "midnight", label: "Midnight", dark: true },
  { value: "dracula", label: "Dracula", dark: true },
  { value: "daylight", label: "Daylight", dark: false },
] as const;

export type Theme = (typeof THEMES)[number]["value"];

// Also duplicated by index.html's pre-paint FOUC guard (`var t = "graphite"`),
// kept in sync by src/lib/fouc-guard.test.ts.
export const DEFAULT_THEME: Theme = "graphite";

export interface FilterPreferences {
  // The filter is persisted as a canonical query STRING (the same human-readable
  // form shared via the `?q=` URL param), NOT as a structured NibFilter. The
  // structured filter + its invalid-token sidecar are DERIVED from this via
  // parseQuery; serializeQuery renders them back. Only the query moves to string
  // form — every other preference below stays personal and structured.
  query: string;
  viewLevel: ViewLevel;
  columnVisibility?: Partial<Record<ViewLevel, ColumnKey[]>>;
  columnWidths?: Partial<Record<ViewLevel, Partial<Record<ColumnKey, number>>>>;
  columnOrder?: Partial<Record<ViewLevel, ColumnKey[]>>;
  detailPanelWidth?: number;
  detailPanelPosition?: DetailPanelPosition;
  openDetailOn?: OpenDetailGesture;
  detailPanelHeight?: number;
  rowDensity?: RowDensity;
  fontSize?: FontSize;
  blockedEmphasis?: BlockedEmphasis;
  theme?: Theme;
  previewOpen?: boolean;
  tableSort?: TableSort;
}
