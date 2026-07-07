import { describe, it, expect, vi } from "vitest";
import {
  resolveFilter,
  resolveViewLevel,
  resolveVisibleColumns,
  resolveColumnWidths,
  emitFilter,
} from "./resolvePrefs";
import { ALL_COLUMN_KEYS, DEFAULT_COLUMN_WIDTHS } from "./types";
import type { NibFilter, ViewLevel, ColumnKey } from "./types";
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

  it("returns all column keys when both are undefined", () => {
    expect(resolveVisibleColumns(undefined, undefined)).toEqual([...ALL_COLUMN_KEYS]);
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
