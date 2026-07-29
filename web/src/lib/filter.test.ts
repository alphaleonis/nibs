import { describe, it, expect } from "vitest";
import {
  matchesFilter,
  hasClientFilters,
  prepareFilter,
  isDragAllowed,
} from "./filter";
import { OPEN_STATUSES, CLOSED_STATUSES } from "./constants";
import type { NibSummary, NibFilter } from "./types";

function makeNib(overrides: Partial<NibSummary> = {}): NibSummary {
  return {
    id: "nibs-001",
    title: "Test nib",
    status: "todo",
    type: "task",
    priority: "normal",
    estimate: "m",
    tags: [],
    createdAt: "2026-03-15T10:00:00Z",
    updatedAt: "2026-03-20T10:00:00Z",
    ...overrides,
  };
}

describe("status presets", () => {
  it("Open is the complement of the closed set", () => {
    expect(OPEN_STATUSES).toEqual(["draft", "todo", "in-progress"]);
  });

  it("deferred is closed, so there is no second preset to distinguish", () => {
    // This is what collapsed the two presets into one: "open" and "not
    // finished" used to name different sets, and now they do not.
    expect(CLOSED_STATUSES).toEqual(["deferred", "completed", "scrapped"]);
    expect(OPEN_STATUSES).not.toContain("deferred");
  });

  it("the Open include-list hides every closed status via matchesFilter", () => {
    const filter: NibFilter = { status: [...OPEN_STATUSES] };
    expect(matchesFilter(makeNib({ status: "todo" }), filter)).toBe(true);
    expect(matchesFilter(makeNib({ status: "deferred" }), filter)).toBe(false);
    expect(matchesFilter(makeNib({ status: "completed" }), filter)).toBe(false);
    expect(matchesFilter(makeNib({ status: "scrapped" }), filter)).toBe(false);
  });

});

describe("matchesFilter", () => {
  it("returns true when nib type matches one of the filter types", () => {
    const nib = makeNib({ type: "bug" });
    expect(matchesFilter(nib, { type: ["bug", "feature"] })).toBe(true);
  });

  it("returns false when nib type does not match filter types", () => {
    const nib = makeNib({ type: "task" });
    expect(matchesFilter(nib, { type: ["bug", "feature"] })).toBe(false);
  });

  it("returns true when filter is empty (matches everything)", () => {
    const nib = makeNib();
    expect(matchesFilter(nib, {})).toBe(true);
  });

  it("requires all active filter fields to match (AND logic)", () => {
    const nib = makeNib({ type: "bug", priority: "low" });
    expect(matchesFilter(nib, { type: ["bug"], priority: ["high"] })).toBe(false);
    expect(matchesFilter(nib, { type: ["bug"], priority: ["low"] })).toBe(true);
  });

  it("matches when nib tags include at least one of the filter tags", () => {
    const nib = makeNib({ tags: ["frontend", "auth"] });
    expect(matchesFilter(nib, { tags: ["backend", "frontend"] })).toBe(true);
  });

  it("does not match when nib has no matching tags", () => {
    const nib = makeNib({ tags: ["auth"] });
    expect(matchesFilter(nib, { tags: ["backend", "frontend"] })).toBe(false);
  });

  it("matches estimate filter", () => {
    const nib = makeNib({ estimate: "xl" });
    expect(matchesFilter(nib, { estimate: ["l", "xl"] })).toBe(true);
    expect(matchesFilter(nib, { estimate: ["s", "m"] })).toBe(false);
  });

  it("matches status filter", () => {
    const nib = makeNib({ status: "in-progress" });
    expect(matchesFilter(nib, { status: ["in-progress", "todo"] })).toBe(true);
    expect(matchesFilter(nib, { status: ["completed"] })).toBe(false);
  });
});

describe("hasClientFilters", () => {
  it("returns true when type filter is active", () => {
    expect(hasClientFilters({ type: ["bug"] })).toBe(true);
  });

  it("returns true when priority filter is active", () => {
    expect(hasClientFilters({ priority: ["high"] })).toBe(true);
  });

  it("returns true when estimate filter is active", () => {
    expect(hasClientFilters({ estimate: ["s"] })).toBe(true);
  });

  it("returns true when tags filter is active", () => {
    expect(hasClientFilters({ tags: ["frontend"] })).toBe(true);
  });

  it("returns true when status filter is active", () => {
    expect(hasClientFilters({ status: ["todo"] })).toBe(true);
  });

  it("returns false for search-only filter (not advanced)", () => {
    expect(hasClientFilters({ search: "test" })).toBe(false);
  });

  it("returns false for empty filter", () => {
    expect(hasClientFilters({})).toBe(false);
  });

  it("returns false for empty arrays", () => {
    expect(hasClientFilters({ type: [], priority: [] })).toBe(false);
  });
});

describe("prepareFilter", () => {
  it("returns original filter as serverFilter when no client filters are active", () => {
    const filter: NibFilter = { search: "hello" };
    const result = prepareFilter(filter);

    expect(result.serverFilter).toBe(filter); // reference equality
    expect(result.clientFiltersActive).toBe(false);
    expect(result.matchesClient(makeNib())).toBe(true);
  });

  it("moves the status include-list out of serverFilter into the client-side filter", () => {
    const filter: NibFilter = {
      search: "hello",
      status: [...OPEN_STATUSES],
    };
    const result = prepareFilter(filter);

    // status is filtered client-side (so completed/scrapped ancestors of active
    // children can be fetched and dimmed rather than dropped server-side).
    expect(result.serverFilter).toEqual({ search: "hello" });
    expect(result.serverFilter).not.toHaveProperty("status");
    expect(result.clientFiltersActive).toBe(true);
    expect(result.matchesClient(makeNib({ status: "completed" }))).toBe(false);
    expect(result.matchesClient(makeNib({ status: "todo" }))).toBe(true);
  });

  it("strips type from serverFilter when type filter is active", () => {
    const filter: NibFilter = { search: "hello", type: ["bug"], status: ["todo"] };
    const result = prepareFilter(filter);

    expect(result.serverFilter).toEqual({ search: "hello" });
    expect(result.serverFilter).not.toHaveProperty("type");
    expect(result.serverFilter).not.toHaveProperty("status");
    expect(result.clientFiltersActive).toBe(true);
  });

  it("strips all client-side fields when multiple are active", () => {
    const filter: NibFilter = {
      search: "test",
      type: ["bug"],
      priority: ["high"],
      status: ["todo"],
      estimate: ["m"],
      tags: ["frontend"],
    };
    const result = prepareFilter(filter);

    expect(result.serverFilter).toEqual({ search: "test" });
    expect(result.serverFilter).not.toHaveProperty("type");
    expect(result.serverFilter).not.toHaveProperty("priority");
    expect(result.serverFilter).not.toHaveProperty("status");
    expect(result.serverFilter).not.toHaveProperty("estimate");
    expect(result.serverFilter).not.toHaveProperty("tags");
    expect(result.clientFiltersActive).toBe(true);
  });

  it("matchesClient returns true when nib matches the client-side filter", () => {
    const filter: NibFilter = { type: ["bug", "feature"] };
    const result = prepareFilter(filter);

    expect(result.matchesClient(makeNib({ type: "bug" }))).toBe(true);
    expect(result.matchesClient(makeNib({ type: "feature" }))).toBe(true);
  });

  it("matchesClient returns false when nib does not match client-side filter", () => {
    const filter: NibFilter = { type: ["bug"], priority: ["high"] };
    const result = prepareFilter(filter);

    // wrong type
    expect(result.matchesClient(makeNib({ type: "task", priority: "high" }))).toBe(false);
    // wrong priority
    expect(result.matchesClient(makeNib({ type: "bug", priority: "low" }))).toBe(false);
    // both match
    expect(result.matchesClient(makeNib({ type: "bug", priority: "high" }))).toBe(true);
  });

  it("matchesClient handles tags with OR logic (at least one tag matches)", () => {
    const filter: NibFilter = { tags: ["frontend", "backend"] };
    const result = prepareFilter(filter);

    // has one matching tag
    expect(result.matchesClient(makeNib({ tags: ["frontend", "auth"] }))).toBe(true);
    // has both matching tags
    expect(result.matchesClient(makeNib({ tags: ["frontend", "backend"] }))).toBe(true);
    // no matching tags
    expect(result.matchesClient(makeNib({ tags: ["auth", "db"] }))).toBe(false);
    // empty tags
    expect(result.matchesClient(makeNib({ tags: [] }))).toBe(false);
  });
});

describe("isDragAllowed", () => {
  it("returns false when search is active", () => {
    expect(isDragAllowed({ search: "hello" })).toBe(false);
  });

  it("returns true when a hide-filter is active (filters never reorder rows)", () => {
    // Hide-filters (type/priority/status/estimate/tags) keep matching nibs in tree
    // order, dim ancestors in place, and only remove non-matching leaves — they
    // never reorder rows. Anchor-based reorder-on-drop stays well-defined, so drag
    // must remain allowed for every hide-filter (including the "Open" status preset).
    expect(isDragAllowed({ type: ["bug"] })).toBe(true);
    expect(isDragAllowed({ priority: ["high"] })).toBe(true);
    expect(isDragAllowed({ status: [...OPEN_STATUSES] })).toBe(true);
    expect(isDragAllowed({ estimate: ["m"] })).toBe(true);
    expect(isDragAllowed({ tags: ["frontend"] })).toBe(true);
  });

  it("returns true when hide-filters are combined in any mix", () => {
    expect(isDragAllowed({ type: ["bug"], status: [...OPEN_STATUSES] })).toBe(true);
    expect(
      isDragAllowed({
        type: ["bug"],
        priority: ["high"],
        status: ["todo"],
        estimate: ["m"],
        tags: ["frontend"],
      }),
    ).toBe(true);
  });

  it("returns false when search is combined with hide-filters", () => {
    expect(isDragAllowed({ search: "hello", type: ["bug"] })).toBe(false);
  });

  it("returns true when the filter is empty", () => {
    expect(isDragAllowed({})).toBe(true);
  });
});
