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
    expect(afterFlush.columnWidths.none.id).toBe(250);
    mockStorage.setItem.mockClear();

    // A visibility change mutates the auto-mode map — the effect MUST fire and save.
    prefs.visibility.setLevel(prefs.viewLevel, ["id", "title"]);
    flushSync();
    expect(mockStorage.setItem).toHaveBeenCalledTimes(1);
    const afterToggle = JSON.parse(store["nibs-filter-preferences"]);
    expect(afterToggle.columnVisibility.none).toEqual(["id", "title"]);

    dispose();
  });
});
