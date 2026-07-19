import { describe, it, expect } from "vitest";
import { ALL_COLUMN_KEYS, DEFAULT_COLUMNS, DEFAULT_VISIBLE_COLUMNS, DEFAULT_COLUMN_WIDTHS } from "./types";

describe("column config: blocking / blockedBy columns", () => {
  it("ALL_COLUMN_KEYS includes blocking and blockedBy at the end", () => {
    expect(ALL_COLUMN_KEYS).toContain("blocking");
    expect(ALL_COLUMN_KEYS).toContain("blockedBy");
    // Appended after the original 7 columns, preserving canonical order.
    expect(ALL_COLUMN_KEYS).toEqual([
      "id",
      "parent",
      "type",
      "title",
      "state",
      "effort",
      "tags",
      "blocking",
      "blockedBy",
    ]);
  });

  it("DEFAULT_COLUMNS exposes both new columns with their labels, not always-visible", () => {
    const blocking = DEFAULT_COLUMNS.find((c) => c.key === "blocking");
    const blockedBy = DEFAULT_COLUMNS.find((c) => c.key === "blockedBy");

    expect(blocking).toMatchObject({ key: "blocking", label: "Blocking", alwaysVisible: false });
    expect(blockedBy).toMatchObject({ key: "blockedBy", label: "Blocked by", alwaysVisible: false });
  });

  it("DEFAULT_VISIBLE_COLUMNS excludes the two new columns but keeps the original defaults", () => {
    expect(DEFAULT_VISIBLE_COLUMNS).not.toContain("blocking");
    expect(DEFAULT_VISIBLE_COLUMNS).not.toContain("blockedBy");
    expect(DEFAULT_VISIBLE_COLUMNS).toEqual([
      "id",
      "parent",
      "type",
      "title",
      "state",
      "effort",
      "tags",
    ]);
  });

  it("DEFAULT_COLUMN_WIDTHS provides widths for the new columns", () => {
    expect(DEFAULT_COLUMN_WIDTHS.blocking).toBe(90);
    expect(DEFAULT_COLUMN_WIDTHS.blockedBy).toBe(100);
  });
});
