// Type-only: tree.ts imports this module, so a value import would be a runtime cycle.
import type { SectionMeta } from "./tree";

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
  // Hierarchy predicates name the relationship the MATCHED nib holds toward the
  // id: `ancestorId` selects the target's descendants, `descendantId` its
  // ancestor chain, `siblingId` nibs sharing its parent (a parentless target
  // selects the other roots). The target itself is excluded, but with free text
  // the server re-adds every match's ancestors, which the tree rendering needs.
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
  // `milestone` is DIRECT assignment (that milestone's queue). `noMilestone` is
  // DERIVED membership: true keeps the backlog, false its complement. The query
  // box spells them `milestone:<id>` and `is:backlog`; the false half has no
  // spelling there.
  milestone?: string;
  noMilestone?: boolean;
  // The area path and every area declared beneath it (`webhooks` is not within
  // `web`). The server refuses an undeclared value, so what a list query may
  // send is `withSendableArea`'s decision (filter.ts).
  area?: string;
}

// Compile-time guard: the hand-written NibFilter and the generated one must have
// EQUAL key sets. The filter reaches urql as a variable, so no excess-property
// check runs on it and a misspelled key would be silently ignored by the server;
// a one-way `extends` would miss exactly that.
//
// The hand-written type stays because consumers rely on its optional `T?` fields,
// where the generated ones are `T | null | undefined`.
import type { NibFilter as GeneratedNibFilter } from "./gql/graphql";

type _ClientKeysExistOnGenerated = keyof NibFilter extends keyof GeneratedNibFilter ? true : never;
const _clientKeysCheck: _ClientKeysExistOnGenerated = true;
void _clientKeysCheck;

type _GeneratedKeysExistOnClient = keyof GeneratedNibFilter extends keyof NibFilter ? true : never;
const _generatedKeysCheck: _GeneratedKeysExistOnClient = true;
void _generatedKeysCheck;

// The assignment axes live here, not on TreeTableNib: `buildViewTree` is generic
// over `T extends TreeNib`, and a grouping lens reads them.
export interface TreeNib extends NibSummary {
  parentId: string | null;
  /** Milestone assignment, verbatim as stored; empty when unassigned. */
  milestone: string;
  /** Fractional position within the assigned milestone's queue; empty when the
   *  nib has never been placed in one. */
  milestoneOrder: string;
  /** Area assignment, verbatim as stored; empty when unassigned. */
  area: string;
}

export interface TreeTableNib extends TreeNib {
  blockingIds: string[];
  blockedByIds: string[];
  /** The content etag, for the table's batch mutations to send as ifMatch. Not on
   *  NibSummary because leaner queries (the typeahead) do not select it. */
  etag: string;
}

export interface TreeNode<T extends TreeNib = TreeNib> {
  nib: T;
  children: TreeNode<T>[];
  depth: number;
  /** Present exactly on a grouped view's section container nodes, carrying every
   *  section fact at once. */
  section?: SectionMeta;
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

export const VIEW_LEVELS = ["none", "flat", "milestones", "epics", "features", "areas"] as const;
export type ViewLevel = (typeof VIEW_LEVELS)[number];

/** The view used when no preference is stored. */
export const DEFAULT_VIEW_LEVEL: ViewLevel = "milestones";

/** The hierarchical view. Use this, not DEFAULT_VIEW_LEVEL, when you mean the tree. */
export const TREE_VIEW_LEVEL: ViewLevel = "none";

/** The user-facing name of each view, for any module that names one. The icons
 *  stay in Toolbar.svelte, because they import lucide components. */
export const VIEW_LEVEL_LABELS: Record<ViewLevel, string> = {
  none: "Tree",
  flat: "Flat",
  milestones: "Milestones",
  epics: "Epics",
  features: "Features & Bugs",
  areas: "Areas",
};

// Client-side table sort; absent means manual `order`. Flat sorts the list; the
// other views sort siblings and keep the nesting.
export type SortField = SortKey;
export type SortDirection = "asc" | "desc";
export interface TableSort {
  field: SortField;
  direction: SortDirection;
}

// The column model is defined in columns.ts and re-exported here.
import { COLUMNS, ALL_COLUMN_KEYS, DEFAULT_COLUMN_WIDTHS, DEFAULT_VISIBLE_COLUMNS } from "./columns";
import type { ColumnKey, SortKey } from "./columns";
export { ALL_COLUMN_KEYS, DEFAULT_COLUMN_WIDTHS, DEFAULT_VISIBLE_COLUMNS };
export type { ColumnKey, SortKey };

export interface ColumnConfig {
  key: ColumnKey;
  label: string;
  alwaysVisible: boolean;
  // Omitted means visible by default; false for opt-in columns.
  defaultVisible?: boolean;
}

// Derived from COLUMNS in order. A default-visible column omits `defaultVisible`.
export const DEFAULT_COLUMNS: ColumnConfig[] = ALL_COLUMN_KEYS.map((key) => {
  const c = COLUMNS[key];
  const config: ColumnConfig = { key: c.key, label: c.label, alwaysVisible: c.alwaysVisible };
  if (!c.defaultVisible) config.defaultVisible = false;
  return config;
});

export const DEFAULT_DETAIL_PANEL_WIDTH = 400;
export const MIN_DETAIL_PANEL_WIDTH = 200;
export const MAX_DETAIL_PANEL_PERCENT = 75;
// The unresized pane size as a percent of the container, for both dock orientations.
export const DEFAULT_DETAIL_PANEL_PERCENT = 40;

// The detail panel docks RIGHT of or BELOW the table. MAX_DETAIL_PANEL_PERCENT
// applies to both; the min and default sizes are per axis.
export const DETAIL_PANEL_POSITIONS = ["right", "bottom"] as const;
export type DetailPanelPosition = (typeof DETAIL_PANEL_POSITIONS)[number];
export const DEFAULT_DETAIL_PANEL_POSITION: DetailPanelPosition = "right";
export const DEFAULT_DETAIL_PANEL_HEIGHT = 300;
export const MIN_DETAIL_PANEL_HEIGHT = 150;

// Which row gesture opens the nib in the detail panel. With "double", a single
// click selects and focuses the row without opening it.
export const OPEN_DETAIL_GESTURES = ["single", "double"] as const;
export type OpenDetailGesture = (typeof OPEN_DETAIL_GESTURES)[number];
export const DEFAULT_OPEN_DETAIL_ON: OpenDetailGesture = "single";

export type RowDensity = "compact" | "comfortable";

// Global font-size preference, applied through the root `--font-scale` variable.
// app.css scales the type tokens and Tailwind's `--text-*` ladder by it; TreeTable's
// `--row-pad-y`, ActiveNibView's title, ui/button's `sm` size and ui/dropdown-menu's
// `max-w` cap read it directly. Root font-size, rem and spacing are unscaled.
export type FontSize = "small" | "medium" | "large";
export const DEFAULT_FONT_SIZE: FontSize = "medium";
export const FONT_SCALES: Record<FontSize, number> = { small: 0.9, medium: 1, large: 1.15 };

// Whether the editor's side-by-side Preview pane is shown. Persisted.
export const DEFAULT_PREVIEW_OPEN = true;

// When ordering-region bands are drawn. Their color tells a queue seam from a
// parent seam, which only matters during a drop.
export const REGION_BAND_MODES = ["on-drag", "never"] as const;
export type RegionBandMode = (typeof REGION_BAND_MODES)[number];
export const DEFAULT_REGION_BAND_MODE: RegionBandMode = "on-drag";

// How the "blocked" state is emphasized in the tree row and ActiveNibView header:
//   subtle   → the bare lock icon
//   pill     → tinted "Blocked" pill
//   pill-dim → pill + the whole table row dimmed
export const BLOCKED_EMPHASES = ["subtle", "pill", "pill-dim"] as const;
export type BlockedEmphasis = (typeof BLOCKED_EMPHASES)[number];
export const DEFAULT_BLOCKED_EMPHASIS: BlockedEmphasis = "pill";

// Maps a BlockedEmphasis to RelationBadge's `variant`. No default arm, so a new
// emphasis fails to compile here. Row dimming is handled at the row.
export function blockedVariantFor(e: BlockedEmphasis): "icon" | "pill" {
  switch (e) {
    case "subtle":
      return "icon";
    case "pill":
    case "pill-dim":
      return "pill";
  }
}

// Palettes selectable from Settings, applied through `data-theme` on <html>.
// "midnight" has no override block: it is app.css's bare :root values.
//
// `dark` owns the light/dark axis: applyTheme() (theme.ts) and index.html's
// pre-paint FOUC guard toggle the `.dark` class from it, and app.css binds
// Tailwind's `dark:` variant to that class independently of `data-theme`.
export const THEMES = [
  { value: "graphite", label: "Graphite", dark: true },
  { value: "midnight", label: "Midnight", dark: true },
  { value: "dracula", label: "Dracula", dark: true },
  { value: "daylight", label: "Daylight", dark: false },
] as const;

export type Theme = (typeof THEMES)[number]["value"];

// Duplicated in index.html's FOUC guard; fouc-guard.test.ts holds them equal.
export const DEFAULT_THEME: Theme = "graphite";

export interface FilterPreferences {
  // The canonical query STRING (the `?q=` form), not a NibFilter; parseQuery
  // derives the structured filter from it.
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
  regionBands?: RegionBandMode;
  theme?: Theme;
  previewOpen?: boolean;
  tableSort?: TableSort;
}
