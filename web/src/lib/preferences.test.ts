import { describe, it, expect, vi, beforeEach } from "vitest";

// Set up localStorage mock before importing the module under test
const store: Record<string, string> = {};
const mockStorage = {
  getItem: vi.fn((key: string) => store[key] ?? null),
  setItem: vi.fn((key: string, value: string) => { store[key] = value; }),
  removeItem: vi.fn((key: string) => { delete store[key]; }),
};
Object.defineProperty(globalThis, "localStorage", { value: mockStorage, writable: true });

import { Preferences } from "./preferences.svelte";
import { ALL_COLUMN_KEYS, DEFAULT_COLUMN_WIDTHS, DEFAULT_DETAIL_PANEL_WIDTH, MIN_DETAIL_PANEL_WIDTH } from "./types";

describe("Preferences", () => {
  beforeEach(() => {
    for (const key of Object.keys(store)) delete store[key];
    vi.clearAllMocks();
  });

  it("loads initial values from localStorage via loadPreferences", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: { search: "hello" },
      viewLevel: "epics",
    });

    const prefs = new Preferences();
    expect(prefs.filter).toEqual({ search: "hello" });
    expect(prefs.viewLevel).toBe("epics");
  });

  it("uses defaults when localStorage is empty", () => {
    const prefs = new Preferences();
    expect(prefs.filter).toEqual({});
    expect(prefs.viewLevel).toBe("milestones");
    expect(prefs.columnVisibility).toEqual({});
    expect(prefs.columnWidths).toEqual({});
  });

  it("save() persists current state to localStorage", () => {
    const prefs = new Preferences();
    prefs.filter = { search: "saved" };
    prefs.viewLevel = "epics";
    prefs.save();

    const stored = JSON.parse(store["nibs-filter-preferences"]);
    expect(stored.filter).toEqual({ search: "saved" });
    expect(stored.viewLevel).toBe("epics");
  });

  it("visibleColumns returns ALL_COLUMN_KEYS when no per-viewLevel override", () => {
    const prefs = new Preferences();
    expect(prefs.visibleColumns).toEqual([...ALL_COLUMN_KEYS]);
  });

  it("visibleColumns returns per-viewLevel columns when set", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      columnVisibility: { milestones: ["id", "title", "state"] },
    });

    const prefs = new Preferences();
    expect(prefs.visibleColumns).toEqual(["id", "title", "state"]);
  });

  it("currentColumnWidths returns defaults when no overrides", () => {
    const prefs = new Preferences();
    expect(prefs.currentColumnWidths).toEqual(DEFAULT_COLUMN_WIDTHS);
  });

  it("currentColumnWidths merges per-viewLevel overrides with defaults", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      columnWidths: { milestones: { id: 200, title: 600 } },
    });

    const prefs = new Preferences();
    expect(prefs.currentColumnWidths.id).toBe(200);
    expect(prefs.currentColumnWidths.title).toBe(600);
    // Non-overridden columns keep defaults
    expect(prefs.currentColumnWidths.type).toBe(DEFAULT_COLUMN_WIDTHS.type);
    expect(prefs.currentColumnWidths.state).toBe(DEFAULT_COLUMN_WIDTHS.state);
  });

  it("setColumnWidth updates per-viewLevel width without saving", () => {
    const prefs = new Preferences();
    // Clear any setItem calls from constructor
    mockStorage.setItem.mockClear();

    prefs.setColumnWidth("id", 250);

    // Should update the underlying columnWidths for the current viewLevel
    expect(prefs.columnWidths.milestones?.id).toBe(250);
    // Should NOT have saved to localStorage
    expect(mockStorage.setItem).not.toHaveBeenCalled();
  });

  it("flushColumnWidths saves immediately", () => {
    const prefs = new Preferences();
    prefs.setColumnWidth("title", 500);
    mockStorage.setItem.mockClear();

    prefs.flushColumnWidths();

    expect(mockStorage.setItem).toHaveBeenCalledTimes(1);
    const stored = JSON.parse(store["nibs-filter-preferences"]);
    expect(stored.columnWidths.milestones.title).toBe(500);
  });

  it("detailPanelWidth defaults to DEFAULT_DETAIL_PANEL_WIDTH when nothing persisted", () => {
    const prefs = new Preferences();
    expect(prefs.detailPanelWidth).toBe(DEFAULT_DETAIL_PANEL_WIDTH);
  });

  it("detailPanelWidth returns persisted value when available", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      filter: {},
      viewLevel: "milestones",
      detailPanelWidth: 550,
    });
    const prefs = new Preferences();
    expect(prefs.detailPanelWidth).toBe(550);
  });

  it("setDetailPanelWidth updates width without triggering save", () => {
    const prefs = new Preferences();
    mockStorage.setItem.mockClear();

    prefs.setDetailPanelWidth(600);

    expect(prefs.detailPanelWidth).toBe(600);
    expect(mockStorage.setItem).not.toHaveBeenCalled();
  });

  it("setDetailPanelWidth clamps to MIN/MAX bounds", () => {
    const prefs = new Preferences();

    prefs.setDetailPanelWidth(50);
    expect(prefs.detailPanelWidth).toBe(MIN_DETAIL_PANEL_WIDTH);

    prefs.setDetailPanelWidth(9999);
    expect(prefs.detailPanelWidth).toBe(9999);
  });

  it("setDetailPanelWidth ignores invalid values", () => {
    const prefs = new Preferences();
    prefs.setDetailPanelWidth(500);

    prefs.setDetailPanelWidth(NaN);
    expect(prefs.detailPanelWidth).toBe(500);

    prefs.setDetailPanelWidth(Infinity);
    expect(prefs.detailPanelWidth).toBe(500);

    prefs.setDetailPanelWidth(-100);
    expect(prefs.detailPanelWidth).toBe(500);

    prefs.setDetailPanelWidth(0);
    expect(prefs.detailPanelWidth).toBe(500);
  });

  it("flushDetailPanelWidth persists width to localStorage", () => {
    const prefs = new Preferences();
    prefs.setDetailPanelWidth(700);
    mockStorage.setItem.mockClear();

    prefs.flushDetailPanelWidth();

    expect(mockStorage.setItem).toHaveBeenCalledTimes(1);
    const stored = JSON.parse(store["nibs-filter-preferences"]);
    expect(stored.detailPanelWidth).toBe(700);
  });

});
