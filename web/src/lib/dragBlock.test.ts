import { describe, it, expect } from "vitest";
import { dragBlockFor } from "./dragBlock";
import type { NibFilter } from "./types";

describe("dragBlockFor", () => {
  it("returns null when nothing blocks drag-reorder", () => {
    expect(dragBlockFor({}, "none", null)).toBeNull();
  });

  it("does not block on token filters, which preserve row order", () => {
    const filter: NibFilter = { type: ["bug"], status: ["todo"], tags: ["web-ui"] };
    expect(dragBlockFor(filter, "milestones", null)).toBeNull();
  });

  it("names the sorted column when a sort is active", () => {
    const block = dragBlockFor({}, "none", { field: "title", direction: "asc" });
    expect(block).toEqual({
      reason: "sort",
      message: "Reordering is off while sorted by Title",
      actionLabel: "Clear sort",
    });
  });

  it("uses the column's display label, not its key", () => {
    const block = dragBlockFor({}, "none", { field: "blockedBy", direction: "desc" });
    expect(block?.message).toBe("Reordering is off while sorted by Blocked by");
  });

  it("blocks on a free-text search", () => {
    const block = dragBlockFor({ search: "api" }, "none", null);
    expect(block).toEqual({
      reason: "search",
      message: "Reordering is off while a search is active",
      actionLabel: "Clear search",
    });
  });

  it("blocks in the Flat view", () => {
    const block = dragBlockFor({}, "flat", null);
    expect(block).toEqual({
      reason: "flat",
      message: "Reordering is off in the Flat view",
      actionLabel: "Switch to Tree",
    });
  });

  // Precedence is deterministic so the toast and its action can never disagree
  // about which gate they refer to. Flat outranks the rest because reorder has no
  // meaning at all in that view; search outranks sort for the same reason.
  it("reports Flat ahead of a search or sort when several gates are active", () => {
    const block = dragBlockFor({ search: "api" }, "flat", { field: "title", direction: "asc" });
    expect(block?.reason).toBe("flat");
  });

  it("reports the search ahead of a sort when both are active", () => {
    const block = dragBlockFor({ search: "api" }, "none", { field: "title", direction: "asc" });
    expect(block?.reason).toBe("search");
  });
});
