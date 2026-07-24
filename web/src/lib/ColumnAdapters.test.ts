import { render } from "@testing-library/svelte";
import { describe, it, expect } from "vitest";
import ColumnAdapters, {
  columnAdapters,
  assertColumnParity,
  type ColumnAdapters as ColumnAdaptersMap,
} from "./ColumnAdapters.svelte";
import { ALL_COLUMN_KEYS, COLUMNS } from "./columns";

// Parity: the compile-time `satisfies Record<ColumnKey, …>` pins on both the
// COLUMNS model and the columnAdapters map already; these are the runtime
// belt-and-suspenders the RFC asks for.

describe("column adapters parity", () => {
  it("every ColumnKey has both a ColumnDef and a ColumnRenderer (header + cell)", () => {
    for (const key of ALL_COLUMN_KEYS) {
      // ColumnDef side.
      expect(COLUMNS[key]).toBeDefined();
      expect(COLUMNS[key].key).toBe(key);
      // ColumnRenderer side.
      const renderer = columnAdapters[key];
      expect(renderer).toBeDefined();
      expect(typeof renderer.header).toBe("function");
      expect(typeof renderer.cell).toBe("function");
    }
    // No stray adapter keys beyond the model.
    expect(Object.keys(columnAdapters).sort()).toEqual([...ALL_COLUMN_KEYS].sort());
  });

  it("assertColumnParity passes for the real adapter map", () => {
    expect(() => assertColumnParity(columnAdapters)).not.toThrow();
  });

  it("assertColumnParity bites: it throws, naming the column, when a renderer is missing", () => {
    const broken = { ...columnAdapters } as Record<string, unknown>;
    delete broken.blocking;
    expect(() => assertColumnParity(broken as ColumnAdaptersMap)).toThrow(/blocking/);
  });

  it("assertColumnParity bites when a renderer lacks a cell snippet", () => {
    const broken = { ...columnAdapters, tags: { header: columnAdapters.tags.header } } as ColumnAdaptersMap;
    expect(() => assertColumnParity(broken)).toThrow(/tags/);
  });

  it("mounts as a provider (builds the real map and runs the DEV parity backstop) without throwing", () => {
    expect(() => render(ColumnAdapters)).not.toThrow();
  });
});
