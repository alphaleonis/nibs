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
import type { RowSection } from "./tableData";

// Canonical column order. Consumers loop over this (filtered to the visible
// set) so the rendered th/td sequence follows a single ordering. Column
// reordering (nibs-46c1) will layer a per-view order on top; for now order is
// always this list.
export const ALL_COLUMN_KEYS = [
  "id",
  "parent",
  "type",
  "title",
  "status",
  "estimate",
  "tags",
  "blocking",
  "blockedBy",
  "created",
  "modified",
] as const;

export type ColumnKey = (typeof ALL_COLUMN_KEYS)[number];

// The client-side table click-to-sort field. A column's sort field equals its
// own key, so the sortable ColumnKey subset IS the field union — this is the
// SINGLE source of that union (types.ts `SortField` is a re-export). Every
// column is sortable today (see COLUMNS below), so this equals ColumnKey; the
// RUNTIME-authoritative gate is `SORTABLE_COLUMN_KEYS` (derived from
// COLUMNS[].sortable), which parseTableSort validates against, so a column later
// marked `sortable:false` is rejected at load even while the type still lists it.
export type SortKey = ColumnKey;

export interface ColumnDef {
  key: ColumnKey;
  label: string;
  defaultWidth: number;
  // Cannot be toggled off in the Columns dropdown (title only, today).
  alwaysVisible: boolean;
  // Shown when a view has no persisted column configuration. Opt-in columns
  // (blocking / blockedBy / created) start hidden but remain toggleable.
  defaultVisible: boolean;
  // Column capabilities for the sort UI. Every column is sortable in every view;
  // the click-to-sort header + aria-sort live in TreeTable's <th> shell and read
  // these flags. A sortable column's `sortKey` equals its own `key`.
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
  /**
   * The section this row DRAWS, or null for the rows that draw none — which is
   * most of them, and every row of an ungrouped view.
   *
   * The only channel a FABRICATED section row has for what it IS: it names no
   * nib, and `nib` is a placeholder carrying the section's label with empty
   * strings around it. A section a real nib heads answers here too, and its
   * cells still come from that nib.
   */
  drawsSection: RowSection | null;
}

// The single source of truth for every column's identity + capabilities. The
// `satisfies Record<ColumnKey, ColumnDef>` pins the key-set to the ColumnKey
// union: a missing or extra key is a compile error naming this file. The
// ColumnAdapters snippet map is pinned to the SAME union, so a column with a
// def but no renderer (or vice versa) also fails to compile.
export const COLUMNS = {
  id: { key: "id", label: "ID", defaultWidth: 100, alwaysVisible: false, defaultVisible: true, sortable: true, sortKey: "id" },
  parent: { key: "parent", label: "Parent", defaultWidth: 160, alwaysVisible: false, defaultVisible: true, sortable: true, sortKey: "parent" },
  type: { key: "type", label: "Type", defaultWidth: 80, alwaysVisible: false, defaultVisible: true, sortable: true, sortKey: "type" },
  title: { key: "title", label: "Title", defaultWidth: 400, alwaysVisible: true, defaultVisible: true, sortable: true, sortKey: "title" },
  status: { key: "status", label: "Status", defaultWidth: 120, alwaysVisible: false, defaultVisible: true, sortable: true, sortKey: "status" },
  estimate: { key: "estimate", label: "Estimate", defaultWidth: 70, alwaysVisible: false, defaultVisible: true, sortable: true, sortKey: "estimate" },
  tags: { key: "tags", label: "Tags", defaultWidth: 150, alwaysVisible: false, defaultVisible: true, sortable: true, sortKey: "tags" },
  blocking: { key: "blocking", label: "Blocking", defaultWidth: 90, alwaysVisible: false, defaultVisible: false, sortable: true, sortKey: "blocking" },
  blockedBy: { key: "blockedBy", label: "Blocked by", defaultWidth: 100, alwaysVisible: false, defaultVisible: false, sortable: true, sortKey: "blockedBy" },
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

// Runtime-authoritative sortable set, derived from COLUMNS[].sortable and
// order-preserving. The single source consumed downstream: parseTableSort
// (storage.ts) validates persisted sort fields against it, and TreeTable renders
// a click-to-sort header for each. A column's sort field equals its own key.
export const SORTABLE_COLUMN_KEYS: ColumnKey[] = ALL_COLUMN_KEYS.filter((k) => COLUMNS[k].sortable);

// The always-visible columns (title today). Order-preserving. Consumed by the
// persistence sanitizer to guarantee these survive a load/save round-trip.
export const ALWAYS_VISIBLE_KEYS: ColumnKey[] = ALL_COLUMN_KEYS.filter((k) => COLUMNS[k].alwaysVisible);
