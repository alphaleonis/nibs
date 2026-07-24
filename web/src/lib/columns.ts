// Pure column model — the "port" half of the table's ports-&-adapters column
// registry. ZERO Svelte dependency: this module is unit-testable under plain
// vitest (no jsdom). The Svelte snippet "adapters" that render each column's
// header/cell live in ColumnAdapters.svelte and are pinned to the ColumnKey
// union defined here, so the model and the renderers can never drift.
//
// Single source of truth: COLUMNS. The three legacy constants
// (DEFAULT_COLUMN_WIDTHS, DEFAULT_VISIBLE_COLUMNS, and — over in types.ts —
// DEFAULT_COLUMNS) are DERIVED from it, order-preserving, so a column's
// identity is declared exactly once.

import type { TreeTableNib, BlockedEmphasis } from "./types";

// Canonical column order. Consumers loop over this (filtered to the visible
// set) so the rendered th/td sequence follows a single ordering. Column
// reordering (nibs-46c1) will layer a per-view order on top; for now order is
// always this list.
export const ALL_COLUMN_KEYS = [
  "id",
  "parent",
  "type",
  "title",
  "state",
  "effort",
  "tags",
  "blocking",
  "blockedBy",
  "created",
  "modified",
] as const;

export type ColumnKey = (typeof ALL_COLUMN_KEYS)[number];

// The client-side flat-view date sort field. Structurally identical to today's
// FlatSortField ("created" | "modified"); kept as its own name here because the
// column model owns column *capabilities*. It widens as more columns become
// sortable (nibs-6grg).
export type SortKey = "created" | "modified";

export interface ColumnDef {
  key: ColumnKey;
  label: string;
  defaultWidth: number;
  // Cannot be toggled off in the Columns dropdown (title only, today).
  alwaysVisible: boolean;
  // Shown when a view has no persisted column configuration. Opt-in columns
  // (blocking / blockedBy / created) start hidden but remain toggleable.
  defaultVisible: boolean;
  // Column capabilities for the sort UI (nibs-6grg). Today only the date
  // columns are sortable; the click-to-sort header + aria-sort still live in
  // TreeTable's <th> shell and read these flags.
  sortable: boolean;
  sortKey: SortKey | null;
}

// The exact per-row inputs a cell adapter needs — mirrors TreeTableRow's
// per-row data. Cells are pure functions of this bag and read nothing from
// Svelte context (they cannot touch selection/drag).
export interface RowContext {
  nib: TreeTableNib;
  depth: number;
  parentNib: TreeTableNib | null;
  hasChildren: boolean;
  collapsed: boolean;
  blockedEmphasis: BlockedEmphasis;
}

// The single source of truth for every column's identity + capabilities. The
// `satisfies Record<ColumnKey, ColumnDef>` pins the key-set to the ColumnKey
// union: a missing or extra key is a compile error naming this file. The
// ColumnAdapters snippet map is pinned to the SAME union, so a column with a
// def but no renderer (or vice versa) also fails to compile.
export const COLUMNS = {
  id: { key: "id", label: "ID", defaultWidth: 100, alwaysVisible: false, defaultVisible: true, sortable: false, sortKey: null },
  parent: { key: "parent", label: "Parent", defaultWidth: 160, alwaysVisible: false, defaultVisible: true, sortable: false, sortKey: null },
  type: { key: "type", label: "Type", defaultWidth: 80, alwaysVisible: false, defaultVisible: true, sortable: false, sortKey: null },
  title: { key: "title", label: "Title", defaultWidth: 400, alwaysVisible: true, defaultVisible: true, sortable: false, sortKey: null },
  state: { key: "state", label: "State", defaultWidth: 120, alwaysVisible: false, defaultVisible: true, sortable: false, sortKey: null },
  effort: { key: "effort", label: "Effort", defaultWidth: 70, alwaysVisible: false, defaultVisible: true, sortable: false, sortKey: null },
  tags: { key: "tags", label: "Tags", defaultWidth: 150, alwaysVisible: false, defaultVisible: true, sortable: false, sortKey: null },
  blocking: { key: "blocking", label: "Blocking", defaultWidth: 90, alwaysVisible: false, defaultVisible: false, sortable: false, sortKey: null },
  blockedBy: { key: "blockedBy", label: "Blocked by", defaultWidth: 100, alwaysVisible: false, defaultVisible: false, sortable: false, sortKey: null },
  created: { key: "created", label: "Created", defaultWidth: 110, alwaysVisible: false, defaultVisible: false, sortable: true, sortKey: "created" },
  modified: { key: "modified", label: "Modified", defaultWidth: 110, alwaysVisible: false, defaultVisible: true, sortable: true, sortKey: "modified" },
} satisfies Record<ColumnKey, ColumnDef>;

// Derived, order-preserving. Was a hand-maintained record in types.ts.
export const DEFAULT_COLUMN_WIDTHS = Object.fromEntries(
  ALL_COLUMN_KEYS.map((k) => [k, COLUMNS[k].defaultWidth]),
) as Record<ColumnKey, number>;

// Derived, order-preserving. Columns shown when a view level has no persisted
// column configuration (opt-in columns excluded).
export const DEFAULT_VISIBLE_COLUMNS: ColumnKey[] = ALL_COLUMN_KEYS.filter((k) => COLUMNS[k].defaultVisible);

// The always-visible columns (title today). Order-preserving. Consumed by the
// persistence sanitizer to guarantee these survive a load/save round-trip.
export const ALWAYS_VISIBLE_KEYS: ColumnKey[] = ALL_COLUMN_KEYS.filter((k) => COLUMNS[k].alwaysVisible);
