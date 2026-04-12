import { describe, it, expect, beforeEach, vi } from "vitest";
import { savePreferences, loadPreferences } from "./storage";
import { MIN_DETAIL_PANEL_WIDTH } from "./types";

const store: Record<string, string> = {};
const mockStorage = {
  getItem: vi.fn((key: string) => store[key] ?? null),
  setItem: vi.fn((key: string, value: string) => { store[key] = value; }),
  removeItem: vi.fn((key: string) => { delete store[key]; }),
};

Object.defineProperty(globalThis, "localStorage", { value: mockStorage, writable: true });

describe("storage", () => {
  beforeEach(() => {
    for (const key of Object.keys(store)) delete store[key];
    vi.clearAllMocks();
  });

  it("saves and loads filter preferences with viewLevel", () => {
    savePreferences({ filter: { search: "test" }, viewLevel: "epics" });
    const loaded = loadPreferences();
    expect(loaded).toEqual({ filter: { search: "test" }, viewLevel: "epics" });
  });

  it("returns defaults when localStorage is empty", () => {
    const loaded = loadPreferences();
    expect(loaded).toEqual({ filter: {}, viewLevel: "milestones" });
  });

  it("returns defaults when localStorage has corrupt JSON", () => {
    store["nibs-filter-preferences"] = "not valid json{{{";
    const loaded = loadPreferences();
    expect(loaded).toEqual({ filter: {}, viewLevel: "milestones" });
  });

  it("returns defaults when localStorage has non-object value", () => {
    store["nibs-filter-preferences"] = '"just a string"';
    const loaded = loadPreferences();
    expect(loaded).toEqual({ filter: {}, viewLevel: "milestones" });
  });

  it("gracefully falls back to default when old viewMode value is stored", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: { search: "old" },
      viewMode: "hierarchy",
    });
    const loaded = loadPreferences();
    expect(loaded).toEqual({ filter: { search: "old" }, viewLevel: "milestones" });
  });

  it("gracefully handles unknown viewLevel value", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "unknown-value",
    });
    const loaded = loadPreferences();
    expect(loaded).toEqual({ filter: {}, viewLevel: "milestones" });
  });

  it("enforces alwaysVisible columns in stored columnVisibility", () => {
    // Store column visibility that omits 'title' (which is alwaysVisible)
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      columnVisibility: {
        milestones: ["id", "type"],
      },
    });
    const loaded = loadPreferences();
    expect(loaded.columnVisibility?.milestones).toContain("title");
    expect(loaded.columnVisibility?.milestones).toContain("id");
    expect(loaded.columnVisibility?.milestones).toContain("type");
  });

  it("does not duplicate alwaysVisible columns if already present", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      columnVisibility: {
        milestones: ["id", "title", "type"],
      },
    });
    const loaded = loadPreferences();
    const titleCount = loaded.columnVisibility?.milestones?.filter(k => k === "title").length;
    expect(titleCount).toBe(1);
  });

  it("saves and loads column widths per view level", () => {
    savePreferences({
      filter: {},
      viewLevel: "milestones",
      columnWidths: {
        milestones: { id: 120, title: 500 },
      },
    });
    const loaded = loadPreferences();
    expect(loaded.columnWidths).toEqual({
      milestones: { id: 120, title: 500 },
    });
  });

  it("strips invalid column keys from columnWidths", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      columnWidths: {
        milestones: { id: 120, bogus: 200 },
      },
    });
    const loaded = loadPreferences();
    expect(loaded.columnWidths?.milestones).toEqual({ id: 120 });
    expect(loaded.columnWidths?.milestones).not.toHaveProperty("bogus");
  });

  it("strips non-positive and non-finite width values", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "epics",
      columnWidths: {
        epics: { id: -10, title: 0, type: Infinity, state: "not a number", effort: 80 },
      },
    });
    const loaded = loadPreferences();
    expect(loaded.columnWidths?.epics).toEqual({ effort: 80 });
  });

  it("returns undefined columnWidths when stored value is not an object", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      columnWidths: "invalid",
    });
    const loaded = loadPreferences();
    expect(loaded.columnWidths).toBeUndefined();
  });

  it("ignores view levels with array values in columnWidths", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      columnWidths: {
        milestones: [100, 200],
      },
    });
    const loaded = loadPreferences();
    expect(loaded.columnWidths).toBeUndefined();
  });

  it("saves and loads detailPanelWidth", () => {
    savePreferences({
      filter: {},
      viewLevel: "milestones",
      detailPanelWidth: 500,
    });
    const loaded = loadPreferences();
    expect(loaded.detailPanelWidth).toBe(500);
  });

  it("returns undefined detailPanelWidth when not set", () => {
    savePreferences({ filter: {}, viewLevel: "milestones" });
    const loaded = loadPreferences();
    expect(loaded.detailPanelWidth).toBeUndefined();
  });

  it("clamps detailPanelWidth to MIN/MAX bounds on load", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      detailPanelWidth: 50,
    });
    expect(loadPreferences().detailPanelWidth).toBe(MIN_DETAIL_PANEL_WIDTH);

    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      detailPanelWidth: 9999,
    });
    expect(loadPreferences().detailPanelWidth).toBe(9999);
  });

  it("strips invalid detailPanelWidth values", () => {
    const cases: [string, unknown][] = [
      ["negative", -100],
      ["zero", 0],
      ["Infinity", Infinity],
      ["NaN", NaN],
      ["string", "400"],
      ["null", null],
      ["object", { width: 400 }],
    ];
    for (const [label, value] of cases) {
      store["nibs-filter-preferences"] = JSON.stringify({
        filter: {},
        viewLevel: "milestones",
        detailPanelWidth: value,
      });
      const loaded = loadPreferences();
      expect(loaded.detailPanelWidth, `should strip ${label}`).toBeUndefined();
    }
  });
});
