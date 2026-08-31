import { describe, it, expect, afterEach } from "vitest";
import { GATE_REASONS, adjacencyReflectsOrdering, dragBlockFor } from "./dragBlock";
import type { DragBlockReason } from "./dragBlock";
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

// The region band's gate. It agrees with `dragBlockFor === null` on every input
// today — all three gates are adjacency gates — and is asked separately so a
// fourth gate added for another reason (a read-only mode, a connection state)
// cannot delete every band in views where adjacency still holds.
describe("adjacencyReflectsOrdering", () => {
  it.each([
    { name: "nothing blocking", filter: {} as NibFilter, level: "none" as const, sort: null, expected: true },
    { name: "the flat view", filter: {} as NibFilter, level: "flat" as const, sort: null, expected: false },
    { name: "an active search", filter: { search: "api" } as NibFilter, level: "none" as const, sort: null, expected: false },
    {
      name: "a client sort",
      filter: {} as NibFilter,
      level: "none" as const,
      sort: { field: "title", direction: "asc" } as const,
      expected: false,
    },
  ])("is $expected for $name", ({ filter, level, sort, expected }) => {
    expect(adjacencyReflectsOrdering(filter, level, sort)).toBe(expected);
  });

  it("answers alongside the drag gate on every gate shipped today", () => {
    // The equivalence the band relies on, asserted rather than assumed — and the
    // thing that stops being true the moment a non-adjacency gate is added, at
    // which point this test is the place the divergence has to be stated.
    const inputs = [
      { filter: {} as NibFilter, level: "none" as const, sort: null },
      { filter: {} as NibFilter, level: "flat" as const, sort: null },
      { filter: { search: "api" } as NibFilter, level: "none" as const, sort: null },
      { filter: {} as NibFilter, level: "none" as const, sort: { field: "title", direction: "asc" } as const },
    ];
    for (const { filter, level, sort } of inputs) {
      expect(adjacencyReflectsOrdering(filter, level, sort)).toBe(dragBlockFor(filter, level, sort) === null);
    }
  });
});

describe("the gate table", () => {
  it("has exactly one gate per reason, so none is declared and never asked", () => {
    // adjacencyReflectsOrdering asks EVERY gate rather than the precedence
    // winner, which is only meaningful if every reason has a gate to ask. A
    // reason with no gate would be unreachable; two gates sharing one would make
    // the message depend on order in a way `dragBlockFor`'s doc does not admit.
    const reasons: DragBlockReason[] = ["flat", "search", "sort"];
    expect([...GATE_REASONS].sort()).toEqual([...reasons].sort());
    expect(new Set(GATE_REASONS).size).toBe(GATE_REASONS.length);
  });

  it("reports adjacency broken when a LOWER-precedence gate is the one that breaks it", () => {
    // The masking case: two gates active at once. `dragBlockFor` reports only
    // the winner, so a predicate reading just that winner answers for one gate
    // and speaks for all. Every gate breaks adjacency today, so this passes
    // either way now — it is here to fail the day a non-adjacency gate is added
    // ABOVE one that does, which is the shape a read-only or connection gate
    // would take.
    const searchedAndFlat = { search: "api" } as NibFilter;
    expect(dragBlockFor(searchedAndFlat, "flat", null)?.reason).toBe("flat");
    expect(adjacencyReflectsOrdering(searchedAndFlat, "flat", null)).toBe(false);

    const sortedAndSearched = { search: "api" } as NibFilter;
    const sort = { field: "title", direction: "asc" } as const;
    expect(dragBlockFor(sortedAndSearched, "none", sort)?.reason).toBe("search");
    expect(adjacencyReflectsOrdering(sortedAndSearched, "none", sort)).toBe(false);
  });
});
