import { describe, it, expect } from "vitest";
import {
  matchesFilter,
  hasClientFilters,
  prepareFilter,
  getStatusConflicts,
  resolveStatusConflicts,
  isDragAllowed,
} from "./filter";
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
    updatedAt: "2026-03-20T10:00:00Z",
    ...overrides,
  };
}

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

  it("returns false when nib status is in excludeStatus", () => {
    const nib = makeNib({ status: "completed" });
    expect(matchesFilter(nib, { excludeStatus: ["completed", "scrapped"] })).toBe(false);
  });

  it("returns true when nib status is not in excludeStatus", () => {
    const nib = makeNib({ status: "todo" });
    expect(matchesFilter(nib, { excludeStatus: ["completed", "scrapped"] })).toBe(true);
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

  it("returns true when excludeStatus is active", () => {
    expect(hasClientFilters({ excludeStatus: ["completed", "scrapped"] })).toBe(true);
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

  it("moves excludeStatus out of serverFilter into the client-side filter", () => {
    const filter: NibFilter = { search: "hello", excludeStatus: ["completed", "scrapped"] };
    const result = prepareFilter(filter);

    // excludeStatus is filtered client-side (so completed/scrapped ancestors of
    // active children can be fetched and dimmed rather than dropped server-side).
    expect(result.serverFilter).toEqual({ search: "hello" });
    expect(result.serverFilter).not.toHaveProperty("excludeStatus");
    expect(result.clientFiltersActive).toBe(true);
    expect(result.matchesClient(makeNib({ status: "completed" }))).toBe(false);
    expect(result.matchesClient(makeNib({ status: "todo" }))).toBe(true);
  });

  it("strips type from serverFilter when type filter is active", () => {
    const filter: NibFilter = { search: "hello", type: ["bug"], excludeStatus: ["completed"] };
    const result = prepareFilter(filter);

    expect(result.serverFilter).toEqual({ search: "hello" });
    expect(result.serverFilter).not.toHaveProperty("type");
    expect(result.serverFilter).not.toHaveProperty("excludeStatus");
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
      excludeStatus: ["scrapped"],
    };
    const result = prepareFilter(filter);

    expect(result.serverFilter).toEqual({ search: "test" });
    expect(result.serverFilter).not.toHaveProperty("type");
    expect(result.serverFilter).not.toHaveProperty("priority");
    expect(result.serverFilter).not.toHaveProperty("status");
    expect(result.serverFilter).not.toHaveProperty("estimate");
    expect(result.serverFilter).not.toHaveProperty("tags");
    expect(result.serverFilter).not.toHaveProperty("excludeStatus");
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

  it("automatically resolves status conflicts before splitting", () => {
    const filter: NibFilter = {
      status: ["todo", "completed"],
      excludeStatus: ["completed", "scrapped"],
      search: "test",
    };
    const result = prepareFilter(filter);

    // "completed" should be removed from the status filter since it conflicts
    // with excludeStatus — only "todo" remains as a client filter
    expect(result.clientFiltersActive).toBe(true);
    expect(result.matchesClient(makeNib({ status: "todo" }))).toBe(true);
    expect(result.matchesClient(makeNib({ status: "completed" }))).toBe(false);
  });

  it("resolves all status conflicts, keeping excludeStatus as the client filter", () => {
    const filter: NibFilter = {
      status: ["completed"],
      excludeStatus: ["completed", "scrapped"],
      search: "test",
    };
    const result = prepareFilter(filter);

    // All status values conflict, so the include status list is removed entirely,
    // but excludeStatus remains as an active client-side filter.
    expect(result.clientFiltersActive).toBe(true);
    expect(result.serverFilter).toEqual({ search: "test" });
    expect(result.matchesClient(makeNib({ status: "completed" }))).toBe(false);
    expect(result.matchesClient(makeNib({ status: "todo" }))).toBe(true);
  });
});

describe("getStatusConflicts", () => {
  it("returns empty array when no overlap between status and excludeStatus", () => {
    const filter: NibFilter = {
      status: ["todo", "in-progress"],
      excludeStatus: ["completed", "scrapped"],
    };

    expect(getStatusConflicts(filter)).toEqual([]);
  });

  it("returns conflicting values when status and excludeStatus overlap", () => {
    const filter: NibFilter = {
      status: ["completed"],
      excludeStatus: ["completed", "scrapped"],
    };

    expect(getStatusConflicts(filter)).toEqual(["completed"]);
  });

  it("returns multiple conflicts when several statuses overlap", () => {
    const filter: NibFilter = {
      status: ["completed", "scrapped", "todo"],
      excludeStatus: ["completed", "scrapped"],
    };

    expect(getStatusConflicts(filter)).toEqual(["completed", "scrapped"]);
  });

  it("returns empty array when status is undefined", () => {
    const filter: NibFilter = {
      excludeStatus: ["completed", "scrapped"],
    };

    expect(getStatusConflicts(filter)).toEqual([]);
  });

  it("returns empty array when excludeStatus is undefined", () => {
    const filter: NibFilter = {
      status: ["completed"],
    };

    expect(getStatusConflicts(filter)).toEqual([]);
  });

  it("returns empty array when both are undefined", () => {
    expect(getStatusConflicts({})).toEqual([]);
  });

  it("returns empty array when status is empty", () => {
    const filter: NibFilter = {
      status: [],
      excludeStatus: ["completed", "scrapped"],
    };

    expect(getStatusConflicts(filter)).toEqual([]);
  });

  it("returns empty array when excludeStatus is empty", () => {
    const filter: NibFilter = {
      status: ["completed"],
      excludeStatus: [],
    };

    expect(getStatusConflicts(filter)).toEqual([]);
  });
});

describe("resolveStatusConflicts", () => {
  it("returns filter unchanged when there are no conflicts", () => {
    const filter: NibFilter = {
      status: ["todo"],
      excludeStatus: ["completed", "scrapped"],
    };

    const result = resolveStatusConflicts(filter);
    expect(result).toBe(filter); // reference equality — no copy made
  });

  it("removes conflicting status values and keeps non-conflicting ones", () => {
    const filter: NibFilter = {
      status: ["todo", "completed"],
      excludeStatus: ["completed", "scrapped"],
    };

    const result = resolveStatusConflicts(filter);
    expect(result.status).toEqual(["todo"]);
    expect(result.excludeStatus).toEqual(["completed", "scrapped"]);
  });

  it("deletes status field entirely when all values conflict", () => {
    const filter: NibFilter = {
      status: ["completed", "scrapped"],
      excludeStatus: ["completed", "scrapped"],
      search: "test",
    };

    const result = resolveStatusConflicts(filter);
    expect(result).not.toHaveProperty("status");
    expect(result.search).toBe("test");
    expect(result.excludeStatus).toEqual(["completed", "scrapped"]);
  });

  it("handles undefined status gracefully", () => {
    const filter: NibFilter = {
      excludeStatus: ["completed"],
    };

    const result = resolveStatusConflicts(filter);
    expect(result).toBe(filter);
  });
});

describe("isDragAllowed", () => {
  it("returns false when search is active", () => {
    expect(isDragAllowed({ search: "hello" })).toBe(false);
  });

  it("returns false when client-side filters are active", () => {
    expect(isDragAllowed({ type: ["bug"] })).toBe(false);
    expect(isDragAllowed({ priority: ["high"] })).toBe(false);
    expect(isDragAllowed({ status: ["todo"] })).toBe(false);
    expect(isDragAllowed({ estimate: ["m"] })).toBe(false);
    expect(isDragAllowed({ tags: ["frontend"] })).toBe(false);
  });

  it("returns true when no search or client filters are active", () => {
    expect(isDragAllowed({})).toBe(true);
  });

  it("returns true when only excludeStatus is set (the 'hide completed' toggle)", () => {
    // excludeStatus is a client-side filter set when the "Include completed" toggle
    // is OFF (it is absent by default). It dims filtered-out ancestors in place and
    // removes filtered-out leaves rather than reordering rows — so drag must stay
    // allowed when it is the ONLY active client filter.
    expect(isDragAllowed({ excludeStatus: ["completed", "scrapped"] })).toBe(true);
  });

  it("returns false when a real client filter is combined with excludeStatus", () => {
    expect(isDragAllowed({ type: ["bug"], excludeStatus: ["completed", "scrapped"] })).toBe(false);
    expect(isDragAllowed({ status: ["todo"], excludeStatus: ["completed", "scrapped"] })).toBe(false);
  });
});
