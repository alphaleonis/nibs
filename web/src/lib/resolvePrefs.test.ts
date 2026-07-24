import { describe, it, expect, vi } from "vitest";
import {
  resolveFilter,
  resolveViewLevel,
  resolveVisibleColumns,
  resolveColumnWidths,
  resolveFlatSort,
  emitFilter,
  emitFlatSort,
} from "./resolvePrefs";
import { DEFAULT_COLUMN_WIDTHS, DEFAULT_VISIBLE_COLUMNS } from "./types";
import type { NibFilter, ViewLevel, ColumnKey, FlatSort } from "./types";
import type { Preferences } from "./preferences.svelte.ts";

function makePrefs(overrides: Partial<Preferences> = {}): Preferences {
  return {
    filter: { search: "default" },
    viewLevel: "epics" as ViewLevel,
    visibleColumns: ["id", "title"] as ColumnKey[],
    currentColumnWidths: { id: 200, parent: 200, type: 200, title: 500, state: 200, effort: 200, tags: 200 } as Record<ColumnKey, number>,
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
    const cols: ColumnKey[] = ["type", "state"];
    expect(resolveVisibleColumns(undefined, cols)).toEqual(["type", "state"]);
  });

  it("returns the default-visible columns when both are undefined (new opt-in columns excluded)", () => {
    expect(resolveVisibleColumns(undefined, undefined)).toEqual([...DEFAULT_VISIBLE_COLUMNS]);
    expect(resolveVisibleColumns(undefined, undefined)).not.toContain("blocking");
    expect(resolveVisibleColumns(undefined, undefined)).not.toContain("blockedBy");
  });
});

describe("resolveColumnWidths", () => {
  it("returns prefs.currentColumnWidths when prefs is defined", () => {
    const widths = { id: 200, parent: 200, type: 200, title: 500, state: 200, effort: 200, tags: 200 } as Record<ColumnKey, number>;
    const prefs = makePrefs({ currentColumnWidths: widths });
    expect(resolveColumnWidths(prefs, undefined)).toEqual(widths);
  });

  it("returns columnWidths prop when prefs is undefined", () => {
    const widths = { id: 50, parent: 50, type: 50, title: 50, state: 50, effort: 50, tags: 50 } as Record<ColumnKey, number>;
    expect(resolveColumnWidths(undefined, widths)).toEqual(widths);
  });

  it("returns default column widths when both are undefined", () => {
    expect(resolveColumnWidths(undefined, undefined)).toEqual({ ...DEFAULT_COLUMN_WIDTHS });
  });
});

describe("resolveFlatSort", () => {
  const asc: FlatSort = { field: "modified", direction: "asc" };
  const desc: FlatSort = { field: "created", direction: "desc" };

  it("returns prefs.flatSort when prefs is defined (prop ignored)", () => {
    const prefs = makePrefs({ flatSort: asc });
    expect(resolveFlatSort(prefs, desc)).toEqual(asc);
  });

  it("returns null when prefs.flatSort is null even if a non-null prop is supplied", () => {
    // The regression this guards: null is nullish, so `prefs?.flatSort ?? prop`
    // would leak the prop's sort. Branching on prefs presence keeps the persisted
    // "off" state authoritative.
    const prefs = makePrefs({ flatSort: null });
    expect(resolveFlatSort(prefs, desc)).toBeNull();
  });

  it("returns the flatSort prop when prefs is undefined", () => {
    expect(resolveFlatSort(undefined, desc)).toEqual(desc);
  });

  it("returns null when both prefs and prop are undefined", () => {
    expect(resolveFlatSort(undefined, undefined)).toBeNull();
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

describe("emitFlatSort", () => {
  const asc: FlatSort = { field: "modified", direction: "asc" };

  it("writes prefs.flatSort when prefs is defined (callback not called)", () => {
    const prefs = makePrefs({ flatSort: null });
    const onchange = vi.fn();
    emitFlatSort(prefs, onchange, asc);
    expect(prefs.flatSort).toEqual(asc);
    expect(onchange).not.toHaveBeenCalled();
  });

  it("writes null to prefs.flatSort when turning the sort off", () => {
    // The write path must persist the "off" state, not skip it — the read path
    // (resolveFlatSort) treats a present prefs as authoritative including null.
    const prefs = makePrefs({ flatSort: asc });
    emitFlatSort(prefs, undefined, null);
    expect(prefs.flatSort).toBeNull();
  });

  it("calls onchange when prefs is undefined", () => {
    const onchange = vi.fn();
    emitFlatSort(undefined, onchange, asc);
    expect(onchange).toHaveBeenCalledWith(asc);
  });

  it("does nothing when both prefs and onchange are undefined", () => {
    // Should not throw
    emitFlatSort(undefined, undefined, null);
  });
});
