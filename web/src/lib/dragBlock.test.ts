import { describe, it, expect, afterEach } from "vitest";
import { dragBlockFor } from "./dragBlock";
import { VIEW_LEVEL_LABELS } from "./types";
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

  // The flat block names two views the user can see — the one reorder is off in,
  // and the one its action switches to — so both have to be READ from the label
  // map the toolbar menu renders. Asserting the literal strings cannot tell a
  // derived name from a hand-written one that happens to agree today, so these
  // rewrite the map and require the block to follow.
  describe("names views the way the toolbar does", () => {
    const original = { ...VIEW_LEVEL_LABELS };
    afterEach(() => {
      Object.assign(VIEW_LEVEL_LABELS, original);
    });

    it("follows a renamed default view in the action label", () => {
      VIEW_LEVEL_LABELS.none = "Outline";
      expect(dragBlockFor({}, "flat", null)?.actionLabel).toBe("Switch to Outline");
    });

    it("follows a renamed flat view in the message", () => {
      VIEW_LEVEL_LABELS.flat = "Listing";
      expect(dragBlockFor({}, "flat", null)?.message).toBe("Reordering is off in the Listing view");
    });
  });
});
