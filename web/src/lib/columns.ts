// Pure column model with no Svelte dependency. The header and cell renderers
// live in ColumnAdapters.svelte, pinned to the same ColumnKey union. COLUMNS is
// the single source; the defaults below and types.ts DEFAULT_COLUMNS derive from it.

import type { TreeTableNib, BlockedEmphasis } from "./types";
import type { RowSection } from "./tableData";

// Canonical column order: the default per-view order, and the order missing keys
// are appended in when a persisted order is loaded.
export const ALL_COLUMN_KEYS = [
  "id",
  "parent",
  "type",
  "title",
  "status",
  "estimate",
  "tags",
  "milestone",
  "area",
  "blocking",
  "blockedBy",
  "created",
  "modified",
] as const;

export type ColumnKey = (typeof ALL_COLUMN_KEYS)[number];

// A column's sort field is its own key. At runtime SORTABLE_COLUMN_KEYS, not this
// type, decides which fields a persisted sort may name.
export type SortKey = ColumnKey;

export interface ColumnDef {
  key: ColumnKey;
  label: string;
  defaultWidth: number;
  // Cannot be toggled off in the Columns dropdown.
  alwaysVisible: boolean;
  // Shown when a view has no persisted column configuration.
  defaultVisible: boolean;
  // TableHeader offers click-to-sort when `sortable`. `sortKey` equals `key`.
  sortable: boolean;
  sortKey: SortKey | null;
}

// Per-row inputs to a cell adapter. Cells are pure functions of this bag and read
// nothing from Svelte context.
export interface RowContext {
  nib: TreeTableNib;
  depth: number;
  parentNib: TreeTableNib | null;
  /**
   * The milestone this row's `milestone` assignment resolves to, or null when it
   * is unassigned or names a nib the table does not hold or that is not a
   * milestone. Read `nib.milestone` to tell unassigned apart.
   */
  milestoneNib: TreeTableNib | null;
  hasChildren: boolean;
  collapsed: boolean;
  blockedEmphasis: BlockedEmphasis;
  /**
   * The section this row draws, or null for most rows. A fabricated section row
   * names no nib, so this is its only record of what it is.
   */
  drawsSection: RowSection | null;
}

// `satisfies` pins the key set to ColumnKey, as ColumnAdapters' renderer map is,
// so a column missing from either is a compile error.
export const COLUMNS = {
  id: { key: "id", label: "ID", defaultWidth: 100, alwaysVisible: false, defaultVisible: true, sortable: true, sortKey: "id" },
  parent: { key: "parent", label: "Parent", defaultWidth: 160, alwaysVisible: false, defaultVisible: true, sortable: true, sortKey: "parent" },
  type: { key: "type", label: "Type", defaultWidth: 80, alwaysVisible: false, defaultVisible: true, sortable: true, sortKey: "type" },
  title: { key: "title", label: "Title", defaultWidth: 400, alwaysVisible: true, defaultVisible: true, sortable: true, sortKey: "title" },
  status: { key: "status", label: "Status", defaultWidth: 120, alwaysVisible: false, defaultVisible: true, sortable: true, sortKey: "status" },
  estimate: { key: "estimate", label: "Estimate", defaultWidth: 70, alwaysVisible: false, defaultVisible: true, sortable: true, sortKey: "estimate" },
  tags: { key: "tags", label: "Tags", defaultWidth: 150, alwaysVisible: false, defaultVisible: true, sortable: true, sortKey: "tags" },
  milestone: { key: "milestone", label: "Milestone", defaultWidth: 160, alwaysVisible: false, defaultVisible: false, sortable: true, sortKey: "milestone" },
  area: { key: "area", label: "Area", defaultWidth: 140, alwaysVisible: false, defaultVisible: false, sortable: true, sortKey: "area" },
  blocking: { key: "blocking", label: "Blocking", defaultWidth: 90, alwaysVisible: false, defaultVisible: false, sortable: true, sortKey: "blocking" },
  blockedBy: { key: "blockedBy", label: "Blocked by", defaultWidth: 100, alwaysVisible: false, defaultVisible: false, sortable: true, sortKey: "blockedBy" },
  created: { key: "created", label: "Created", defaultWidth: 110, alwaysVisible: false, defaultVisible: false, sortable: true, sortKey: "created" },
  modified: { key: "modified", label: "Modified", defaultWidth: 110, alwaysVisible: false, defaultVisible: true, sortable: true, sortKey: "modified" },
} satisfies Record<ColumnKey, ColumnDef>;

export const DEFAULT_COLUMN_WIDTHS = Object.fromEntries(
  ALL_COLUMN_KEYS.map((k) => [k, COLUMNS[k].defaultWidth]),
) as Record<ColumnKey, number>;

export const DEFAULT_VISIBLE_COLUMNS: ColumnKey[] = ALL_COLUMN_KEYS.filter((k) => COLUMNS[k].defaultVisible);

export const SORTABLE_COLUMN_KEYS: ColumnKey[] = ALL_COLUMN_KEYS.filter((k) => COLUMNS[k].sortable);

export const ALWAYS_VISIBLE_KEYS: ColumnKey[] = ALL_COLUMN_KEYS.filter((k) => COLUMNS[k].alwaysVisible);
