import { describe, it, expect } from "vitest";
import {
  ALL_COLUMN_KEYS,
  COLUMNS,
  DEFAULT_COLUMN_WIDTHS,
  DEFAULT_VISIBLE_COLUMNS,
  ALWAYS_VISIBLE_KEYS,
  SORTABLE_COLUMN_KEYS,
  type ColumnKey,
} from "./columns";

// Pure model tests — no jsdom, no Svelte. columns.ts must stay Svelte-free so it
// can be reasoned about (and tested) as plain data.

describe("columns model", () => {
  it("COLUMNS has exactly one entry per ColumnKey, keyed to itself", () => {
    expect(Object.keys(COLUMNS).sort()).toEqual([...ALL_COLUMN_KEYS].sort());
    for (const key of ALL_COLUMN_KEYS) {
      expect(COLUMNS[key].key).toBe(key);
    }
  });

  it("labels reproduce today's column labels exactly", () => {
    const labels = ALL_COLUMN_KEYS.map((k) => COLUMNS[k].label);
    expect(labels).toEqual([
      "ID",
      "Parent",
      "Type",
      "Title",
      "Status",
      "Estimate",
      "Tags",
      "Milestone",
      "Area",
      "Blocking",
      "Blocked by",
      "Created",
      "Modified",
    ]);
  });

  it("DEFAULT_COLUMN_WIDTHS derives from COLUMNS and matches the legacy record exactly", () => {
    // The exact record that used to be hand-maintained in types.ts.
    expect(DEFAULT_COLUMN_WIDTHS).toEqual({
      id: 100,
      parent: 160,
      type: 80,
      title: 400,
      status: 120,
      estimate: 70,
      tags: 150,
      milestone: 160,
      area: 140,
      blocking: 90,
      blockedBy: 100,
      created: 110,
      modified: 110,
    });
    // Single-sourced: every derived width equals its ColumnDef.defaultWidth.
    for (const key of ALL_COLUMN_KEYS) {
      expect(DEFAULT_COLUMN_WIDTHS[key]).toBe(COLUMNS[key].defaultWidth);
    }
  });

  it("DEFAULT_VISIBLE_COLUMNS keeps the on-by-default columns in canonical order, opt-ins excluded", () => {
    expect(DEFAULT_VISIBLE_COLUMNS).toEqual([
      "id",
      "parent",
      "type",
      "title",
      "status",
      "estimate",
      "tags",
      "modified",
    ]);
  });

  it("the opt-in columns (milestone, area, blocking, blockedBy, created) are hidden by default", () => {
    const optIn: ColumnKey[] = ["milestone", "area", "blocking", "blockedBy", "created"];
    for (const key of optIn) {
      expect(COLUMNS[key].defaultVisible).toBe(false);
      expect(DEFAULT_VISIBLE_COLUMNS).not.toContain(key);
    }
  });

  it("every column is sortable, and each column's sortKey is itself", () => {
    for (const key of ALL_COLUMN_KEYS) {
      const def = COLUMNS[key];
      expect(def.sortable).toBe(true);
      expect(def.sortKey).toBe(key);
    }
  });

  it("SORTABLE_COLUMN_KEYS is the sortable subset in canonical order (all columns today)", () => {
    expect(SORTABLE_COLUMN_KEYS).toEqual(ALL_COLUMN_KEYS.filter((k) => COLUMNS[k].sortable));
    expect(SORTABLE_COLUMN_KEYS).toEqual([...ALL_COLUMN_KEYS]);
  });

  // The two assignment axes are columns because neither can ever appear as an
  // ancestor in a parent tree: a milestone is a waypoint outside the parent
  // graph, and an area is a declared path rather than a nib at all. They start
  // hidden because the table is `table-layout: fixed` on a computed width, so a
  // default-on column costs every view its share of that width — including the
  // two views that already spine on the axis and would repeat their own section
  // header in a column.
  it("both assignment axes are registered columns that start hidden", () => {
    for (const key of ["milestone", "area"] as const) {
      expect(ALL_COLUMN_KEYS).toContain(key);
      expect(DEFAULT_VISIBLE_COLUMNS).not.toContain(key);
      expect(COLUMNS[key].defaultVisible).toBe(false);
      // Hidden by default, but reachable: still toggleable and still sortable.
      expect(COLUMNS[key].alwaysVisible).toBe(false);
      expect(COLUMNS[key].sortable).toBe(true);
      expect(SORTABLE_COLUMN_KEYS).toContain(key);
    }
  });

  it("title is the only always-visible column", () => {
    expect(ALWAYS_VISIBLE_KEYS).toEqual(["title"]);
    expect(COLUMNS.title.alwaysVisible).toBe(true);
    for (const key of ALL_COLUMN_KEYS) {
      if (key !== "title") expect(COLUMNS[key].alwaysVisible).toBe(false);
    }
  });
});
