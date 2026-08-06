import { describe, it, expect } from "vitest";
import { ALL_COLUMN_KEYS, DEFAULT_COLUMNS, DEFAULT_VISIBLE_COLUMNS, DEFAULT_COLUMN_WIDTHS } from "./types";

describe("column config: blocking / blockedBy columns", () => {
  it("ALL_COLUMN_KEYS includes blocking and blockedBy at the end", () => {
    expect(ALL_COLUMN_KEYS).toContain("blocking");
    expect(ALL_COLUMN_KEYS).toContain("blockedBy");
    // Appended after the original 7 columns, preserving canonical order.
    // created / modified follow the relation columns.
    expect(ALL_COLUMN_KEYS).toEqual([
      "id",
      "parent",
      "type",
      "title",
      "status",
      "estimate",
      "tags",
      "blocking",
      "blockedBy",
      "created",
      "modified",
    ]);
  });

  it("DEFAULT_COLUMNS exposes both new columns with their labels, not always-visible", () => {
    const blocking = DEFAULT_COLUMNS.find((c) => c.key === "blocking");
    const blockedBy = DEFAULT_COLUMNS.find((c) => c.key === "blockedBy");

    expect(blocking).toMatchObject({ key: "blocking", label: "Blocking", alwaysVisible: false });
    expect(blockedBy).toMatchObject({ key: "blockedBy", label: "Blocked by", alwaysVisible: false });
  });

  it("DEFAULT_VISIBLE_COLUMNS excludes the opt-in relation columns but keeps the original defaults plus modified", () => {
    expect(DEFAULT_VISIBLE_COLUMNS).not.toContain("blocking");
    expect(DEFAULT_VISIBLE_COLUMNS).not.toContain("blockedBy");
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

  it("DEFAULT_COLUMN_WIDTHS provides widths for the new columns", () => {
    expect(DEFAULT_COLUMN_WIDTHS.blocking).toBe(90);
    expect(DEFAULT_COLUMN_WIDTHS.blockedBy).toBe(100);
  });
});

describe("column config: created / modified date columns", () => {
  it("DEFAULT_COLUMNS exposes created (opt-in) and modified (default-visible) with their labels", () => {
    const created = DEFAULT_COLUMNS.find((c) => c.key === "created");
    const modified = DEFAULT_COLUMNS.find((c) => c.key === "modified");

    // created is opt-in (defaultVisible: false); modified is on by default (no flag).
    expect(created).toMatchObject({ key: "created", label: "Created", alwaysVisible: false, defaultVisible: false });
    expect(modified).toMatchObject({ key: "modified", label: "Modified", alwaysVisible: false });
    expect(modified?.defaultVisible).toBeUndefined();
  });

  it("DEFAULT_VISIBLE_COLUMNS includes modified but not created", () => {
    expect(DEFAULT_VISIBLE_COLUMNS).toContain("modified");
    expect(DEFAULT_VISIBLE_COLUMNS).not.toContain("created");
  });

  it("DEFAULT_COLUMN_WIDTHS provides widths for created and modified", () => {
    expect(DEFAULT_COLUMN_WIDTHS.created).toBe(110);
    expect(DEFAULT_COLUMN_WIDTHS.modified).toBe(110);
  });
});
