export interface NibSummary {
  id: string;
  title: string;
  status: string;
  type: string;
  priority: string;
  estimate: string;
  tags: string[];
  updatedAt: string;
}

export interface NibFilter {
  search?: string;
  status?: string[];
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
  hasBlocking?: boolean;
  blockingId?: string;
  isBlocked?: boolean;
  hasBlockedBy?: boolean;
  blockedById?: string;
  noParent?: boolean;
  noBlocking?: boolean;
  noBlockedBy?: boolean;
}

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

export const VIEW_LEVELS = ["none", "milestones", "epics", "features"] as const;
export type ViewLevel = (typeof VIEW_LEVELS)[number];

export const ALL_COLUMN_KEYS = ["id", "parent", "type", "title", "state", "effort", "tags", "blocking", "blockedBy"] as const;
export type ColumnKey = (typeof ALL_COLUMN_KEYS)[number];

export interface ColumnConfig {
  key: ColumnKey;
  label: string;
  alwaysVisible: boolean;
  // Omitted ⇒ visible by default. Set false for opt-in columns that start hidden
  // (e.g. blocking / blockedBy) but remain toggleable in the Columns dropdown.
  defaultVisible?: boolean;
}

export const DEFAULT_COLUMNS: ColumnConfig[] = [
  { key: "id", label: "ID", alwaysVisible: false },
  { key: "parent", label: "Parent", alwaysVisible: false },
  { key: "type", label: "Type", alwaysVisible: false },
  { key: "title", label: "Title", alwaysVisible: true },
  { key: "state", label: "State", alwaysVisible: false },
  { key: "effort", label: "Effort", alwaysVisible: false },
  { key: "tags", label: "Tags", alwaysVisible: false },
  { key: "blocking", label: "Blocking", alwaysVisible: false, defaultVisible: false },
  { key: "blockedBy", label: "Blocked by", alwaysVisible: false, defaultVisible: false },
];

// Columns shown when a view level has no persisted column configuration. Opt-in
// columns (defaultVisible: false) are excluded; everything else is on by default.
export const DEFAULT_VISIBLE_COLUMNS: ColumnKey[] = DEFAULT_COLUMNS.filter((c) => c.defaultVisible !== false).map((c) => c.key);

export const DEFAULT_COLUMN_WIDTHS: Record<ColumnKey, number> = {
  id: 100,
  parent: 160,
  type: 80,
  title: 400,
  state: 120,
  effort: 70,
  tags: 150,
  blocking: 90,
  blockedBy: 100,
};

export const DEFAULT_DETAIL_PANEL_WIDTH = 400;
export const MIN_DETAIL_PANEL_WIDTH = 200;
export const MAX_DETAIL_PANEL_PERCENT = 75;
// Size the detail pane opens at when the user hasn't resized it — a percent of
// the container, so the default stays screen-relative instead of a fixed px that
// looks narrow on large displays (nibs-lcyo). Applies to both dock orientations.
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

export type RowDensity = "compact" | "comfortable";

// Global font-size preference. Scales the whole UI type scale from one root CSS
// variable (`--font-scale`) applied ONLY to the semantic type-size tokens, so
// layout rem/spacing/row-density stay untouched. Decoupled from RowDensity.
export type FontSize = "small" | "medium" | "large";
// Default is Medium = 1.0 so existing users see no change.
export const DEFAULT_FONT_SIZE: FontSize = "medium";
// The multiplier each size feeds into `--font-scale`.
export const FONT_SCALES: Record<FontSize, number> = { small: 0.9, medium: 1, large: 1.15 };

// Whether the editor's side-by-side Preview pane is shown while editing a nib
// body. Persisted so the on/off choice survives remounts (docked↔expanded) and
// sessions. Defaults to on (matches the original local-state default).
export const DEFAULT_PREVIEW_OPEN = true;

// How the "blocked" state is emphasized in the tree row + ActiveNibView header:
//   subtle   → the bare lock icon
//   pill     → tinted "Blocked" pill (default)
//   pill-dim → pill + the whole table row dimmed
export const BLOCKED_EMPHASES = ["subtle", "pill", "pill-dim"] as const;
export type BlockedEmphasis = (typeof BLOCKED_EMPHASES)[number];
export const DEFAULT_BLOCKED_EMPHASIS: BlockedEmphasis = "pill";

// Maps a BlockedEmphasis to the BlockedBadge presentational `variant`. Exhaustive
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
// Each entry carries a `dark` flag that OWNS the light/dark axis (nibs-fen5).
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
  filter: NibFilter;
  viewLevel: ViewLevel;
  columnVisibility?: Partial<Record<ViewLevel, ColumnKey[]>>;
  columnWidths?: Partial<Record<ViewLevel, Partial<Record<ColumnKey, number>>>>;
  detailPanelWidth?: number;
  detailPanelPosition?: DetailPanelPosition;
  detailPanelHeight?: number;
  rowDensity?: RowDensity;
  fontSize?: FontSize;
  blockedEmphasis?: BlockedEmphasis;
  theme?: Theme;
  previewOpen?: boolean;
}
