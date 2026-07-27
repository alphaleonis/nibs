import { describe, it, expect, vi } from "vitest";
import {
  resolveFilter,
  resolveViewLevel,
  resolveVisibleColumns,
  resolveColumnWidths,
  resolveColumnOrder,
  resolveTableSort,
  emitFilter,
  emitTableSort,
  emitColumnOrder,
} from "./resolvePrefs";
import { ALL_COLUMN_KEYS, DEFAULT_COLUMN_WIDTHS, DEFAULT_VISIBLE_COLUMNS } from "./types";
import type { NibFilter, ViewLevel, ColumnKey, TableSort } from "./types";
import type { Preferences } from "./preferences.svelte.ts";

function makePrefs(overrides: Partial<Preferences> = {}): Preferences {
  return {
    filter: { search: "default" },
    viewLevel: "epics" as ViewLevel,
    visibleColumns: ["id", "title"] as ColumnKey[],
    currentColumnWidths: { id: 200, parent: 200, type: 200, title: 500, status: 200, estimate: 200, tags: 200 } as Record<ColumnKey, number>,
    currentColumnOrder: [...ALL_COLUMN_KEYS] as ColumnKey[],
    ...overrides,
  } as Preferences;
}

describe("resolveFilter", () => {
  it("returns prefs.filter when prefs is defined", () => {
    const prefs = makePrefs({ filter: { search: "hello" } });
    expect(resolveFilter(prefs, { search: "fallback" })).toEqual({ search: "hello" });
  });

  it("returns filter prop when prefs is undefined", () => {
    expect(resolveFilter(undefined, { search: "fallback" })).toEqual({ search: "fallback" });
  });

  it("returns empty object when both are undefined", () => {
    expect(resolveFilter(undefined, undefined)).toEqual({});
  });
});

describe("resolveViewLevel", () => {
  it("returns prefs.viewLevel when prefs is defined", () => {
    const prefs = makePrefs({ viewLevel: "features" as ViewLevel });
    expect(resolveViewLevel(prefs, "milestones")).toBe("features");
  });

  it("returns viewLevel prop when prefs is undefined", () => {
    expect(resolveViewLevel(undefined, "epics")).toBe("epics");
  });

  it("returns 'none' when both are undefined", () => {
    expect(resolveViewLevel(undefined, undefined)).toBe("none");
  });
});

describe("resolveVisibleColumns", () => {
  it("returns prefs.visibleColumns when prefs is defined", () => {
    const prefs = makePrefs({ visibleColumns: ["id", "title"] as ColumnKey[] });
    expect(resolveVisibleColumns(prefs, undefined)).toEqual(["id", "title"]);
  });

  it("returns visibleColumns prop when prefs is undefined", () => {
    const cols: ColumnKey[] = ["type", "status"];
    expect(resolveVisibleColumns(undefined, cols)).toEqual(["type", "status"]);
  });

  it("returns the default-visible columns when both are undefined (new opt-in columns excluded)", () => {
    expect(resolveVisibleColumns(undefined, undefined)).toEqual([...DEFAULT_VISIBLE_COLUMNS]);
    expect(resolveVisibleColumns(undefined, undefined)).not.toContain("blocking");
    expect(resolveVisibleColumns(undefined, undefined)).not.toContain("blockedBy");
  });
});

describe("resolveColumnWidths", () => {
  it("returns prefs.currentColumnWidths when prefs is defined", () => {
    const widths = { id: 200, parent: 200, type: 200, title: 500, status: 200, estimate: 200, tags: 200 } as Record<ColumnKey, number>;
    const prefs = makePrefs({ currentColumnWidths: widths });
    expect(resolveColumnWidths(prefs, undefined)).toEqual(widths);
  });

  it("returns columnWidths prop when prefs is undefined", () => {
    const widths = { id: 50, parent: 50, type: 50, title: 50, status: 50, estimate: 50, tags: 50 } as Record<ColumnKey, number>;
    expect(resolveColumnWidths(undefined, widths)).toEqual(widths);
  });

  it("returns default column widths when both are undefined", () => {
    expect(resolveColumnWidths(undefined, undefined)).toEqual({ ...DEFAULT_COLUMN_WIDTHS });
  });
});

describe("resolveColumnOrder", () => {
  it("returns prefs.currentColumnOrder when prefs is defined", () => {
    const perm = [...ALL_COLUMN_KEYS].reverse();
    const prefs = makePrefs({ currentColumnOrder: perm });
    expect(resolveColumnOrder(prefs, undefined)).toEqual(perm);
  });

  it("returns the columnOrder prop when prefs is undefined", () => {
    const order: ColumnKey[] = ["status", "title", "id"];
    expect(resolveColumnOrder(undefined, order)).toEqual(["status", "title", "id"]);
  });

  it("returns the canonical ALL_COLUMN_KEYS order when both are undefined", () => {
    expect(resolveColumnOrder(undefined, undefined)).toEqual([...ALL_COLUMN_KEYS]);
  });
});

describe("emitColumnOrder", () => {
  const perm: ColumnKey[] = [...ALL_COLUMN_KEYS].reverse();

  it("writes prefs.order.setLevel for the current view when prefs is defined", () => {
    const setLevel = vi.fn();
    const prefs = { viewLevel: "epics" as ViewLevel, order: { setLevel } } as unknown as Preferences;
    const onchange = vi.fn();
    emitColumnOrder(prefs, onchange, perm);
    expect(setLevel).toHaveBeenCalledWith("epics", perm);
    expect(onchange).not.toHaveBeenCalled();
  });

  it("calls onchange when prefs is undefined", () => {
    const onchange = vi.fn();
    emitColumnOrder(undefined, onchange, perm);
    expect(onchange).toHaveBeenCalledWith(perm);
  });

  it("does nothing when both prefs and onchange are undefined", () => {
    emitColumnOrder(undefined, undefined, perm);
  });
});

describe("resolveTableSort", () => {
  const asc: TableSort = { field: "modified", direction: "asc" };
  const desc: TableSort = { field: "created", direction: "desc" };

  it("returns prefs.tableSort when prefs is defined (prop ignored)", () => {
    const prefs = makePrefs({ tableSort: asc });
    expect(resolveTableSort(prefs, desc)).toEqual(asc);
  });

  it("returns null when prefs.tableSort is null even if a non-null prop is supplied", () => {
    // The regression this guards: null is nullish, so `prefs?.tableSort ?? prop`
    // would leak the prop's sort. Branching on prefs presence keeps the persisted
    // "off" state authoritative.
    const prefs = makePrefs({ tableSort: null });
    expect(resolveTableSort(prefs, desc)).toBeNull();
  });

  it("returns the tableSort prop when prefs is undefined", () => {
    expect(resolveTableSort(undefined, desc)).toEqual(desc);
  });

  it("returns null when both prefs and prop are undefined", () => {
    expect(resolveTableSort(undefined, undefined)).toBeNull();
  });
});

describe("emitFilter", () => {
  it("mutates prefs.filter when prefs is defined", () => {
    const prefs = makePrefs({ filter: { search: "old" } });
    const updated: NibFilter = { search: "new" };
    emitFilter(prefs, undefined, updated);
    expect(prefs.filter).toEqual({ search: "new" });
  });

  it("calls onchange when prefs is undefined", () => {
    const onchange = vi.fn();
    const updated: NibFilter = { search: "test" };
    emitFilter(undefined, onchange, updated);
    expect(onchange).toHaveBeenCalledWith({ search: "test" });
  });

  it("does nothing when both prefs and onchange are undefined", () => {
    // Should not throw
    emitFilter(undefined, undefined, { search: "test" });
  });
});

describe("emitTableSort", () => {
  const asc: TableSort = { field: "modified", direction: "asc" };

  it("writes prefs.tableSort when prefs is defined (callback not called)", () => {
    const prefs = makePrefs({ tableSort: null });
    const onchange = vi.fn();
    emitTableSort(prefs, onchange, asc);
    expect(prefs.tableSort).toEqual(asc);
    expect(onchange).not.toHaveBeenCalled();
  });

  it("writes null to prefs.tableSort when turning the sort off", () => {
    // The write path must persist the "off" state, not skip it — the read path
    // (resolveTableSort) treats a present prefs as authoritative including null.
    const prefs = makePrefs({ tableSort: asc });
    emitTableSort(prefs, undefined, null);
    expect(prefs.tableSort).toBeNull();
  });

  it("calls onchange when prefs is undefined", () => {
    const onchange = vi.fn();
    emitTableSort(undefined, onchange, asc);
    expect(onchange).toHaveBeenCalledWith(asc);
  });

  it("does nothing when both prefs and onchange are undefined", () => {
    // Should not throw
    emitTableSort(undefined, undefined, null);
  });
});
