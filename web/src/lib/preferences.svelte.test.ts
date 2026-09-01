import { describe, it, expect, vi, beforeEach } from "vitest";
import { flushSync } from "svelte";

// localStorage mock, mirroring preferences.test.ts, installed before importing
// the module under test.
const store: Record<string, string> = {};
const mockStorage = {
  getItem: vi.fn((key: string) => store[key] ?? null),
  setItem: vi.fn((key: string, value: string) => { store[key] = value; }),
  removeItem: vi.fn((key: string) => { delete store[key]; }),
};
Object.defineProperty(globalThis, "localStorage", { value: mockStorage, writable: true });

import { Preferences } from "./preferences.svelte";

function withRoot(fn: () => void): () => void {
  return $effect.root(fn);
}

// The save-timing split is the load-bearing structural property: visibility is a
// "auto" per-view map (tracked by the auto-save $effect); widths is a "flush" map
// (never tracked, persisted only via flushColumnWidths on pointerup). Exercising
// the real $effect (inside an effect root) proves the split, not just the methods.
describe("Preferences — per-view save-timing split (auto vs flush)", () => {
  beforeEach(() => {
    for (const key of Object.keys(store)) delete store[key];
    vi.clearAllMocks();
  });

  it("a width change does NOT auto-save (until flushColumnWidths); a visibility change DOES auto-save", () => {
    let prefs!: Preferences;
    const dispose = withRoot(() => {
      prefs = new Preferences();
    });
    // Run the auto-save effect's initial (skipped) pass so later saves are real.
    flushSync();
    mockStorage.setItem.mockClear();

    // Width drag mutates the flush-mode map — the auto-save effect must stay silent.
    prefs.setColumnWidth("id", 250);
    flushSync();
    expect(mockStorage.setItem).not.toHaveBeenCalled();

    // Pointerup flush is the ONLY path that persists a width.
    prefs.flushColumnWidths();
    expect(mockStorage.setItem).toHaveBeenCalledTimes(1);
    const afterFlush = JSON.parse(store["nibs-filter-preferences"]);
    expect(afterFlush.columnWidths.milestones.id).toBe(250);
    mockStorage.setItem.mockClear();

    // A visibility change mutates the auto-mode map — the effect MUST fire and save.
    prefs.visibility.setLevel(prefs.viewLevel, ["id", "title"]);
    flushSync();
    expect(mockStorage.setItem).toHaveBeenCalledTimes(1);
    const afterToggle = JSON.parse(store["nibs-filter-preferences"]);
    expect(afterToggle.columnVisibility.milestones).toEqual(["id", "title"]);
    mockStorage.setItem.mockClear();

    // An order change mutates the third auto-mode map (columnOrder) — same as
    // visibility, the effect MUST fire and persist immediately (no flush needed).
    prefs.order.setLevel(prefs.viewLevel, ["title", "id"]);
    flushSync();
    expect(mockStorage.setItem).toHaveBeenCalledTimes(1);
    const afterReorder = JSON.parse(store["nibs-filter-preferences"]);
    expect(afterReorder.columnOrder.milestones).toEqual(["title", "id"]);

    dispose();
  });
});

describe("Preferences — openDetailOn", () => {
  beforeEach(() => {
    for (const key of Object.keys(store)) delete store[key];
    vi.clearAllMocks();
  });

  it("defaults to 'single' when nothing is persisted", () => {
    const prefs = new Preferences();
    expect(prefs.openDetailOn).toBe("single");
  });

  it("hydrates from the persisted value", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      q: "",
      viewLevel: "none",
      openDetailOn: "double",
    });
    const prefs = new Preferences();
    expect(prefs.openDetailOn).toBe("double");
  });

  it("falls back to 'single' for a bogus persisted value", () => {
    store["nibs-filter-preferences"] = JSON.stringify({
      q: "",
      viewLevel: "none",
      openDetailOn: "triple",
    });
    const prefs = new Preferences();
    expect(prefs.openDetailOn).toBe("single");
  });

  it("is included in save()", () => {
    const prefs = new Preferences();
    prefs.openDetailOn = "double";
    prefs.save();
    expect(JSON.parse(store["nibs-filter-preferences"]).openDetailOn).toBe("double");
  });

  it("auto-saves on change (discrete toggle, not flush-saved)", () => {
    let prefs!: Preferences;
    const dispose = withRoot(() => {
      prefs = new Preferences();
    });
    // Run the auto-save effect's initial (skipped) pass so later saves are real.
    flushSync();
    mockStorage.setItem.mockClear();

    prefs.openDetailOn = "double";
    flushSync();

    expect(mockStorage.setItem).toHaveBeenCalledTimes(1);
    expect(JSON.parse(store["nibs-filter-preferences"]).openDetailOn).toBe("double");

    dispose();
  });
});
