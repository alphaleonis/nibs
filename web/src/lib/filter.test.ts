import { describe, it, expect } from "vitest";
import {
  matchesFilter,
  hasClientFilters,
  prepareFilter,
  isDragAllowed,
} from "./filter";
import { OPEN_STATUSES, CLOSED_STATUSES } from "./constants";
import {
  createAreaVocabulary,
  EMPTY_AREAS,
  LOADING_AREAS,
  UNAVAILABLE_AREAS,
} from "./areas";
import type { AreaNode, AreaVocabulary } from "./areas";
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

  // Exclusion (`-field:value` negation) — a nib whose field value is in the
  // exclude list is a non-match. One case per field proves each guard bites.
  it("excludeType: non-match when nib type is excluded", () => {
    expect(matchesFilter(makeNib({ type: "bug" }), { excludeType: ["bug"] })).toBe(false);
    expect(matchesFilter(makeNib({ type: "task" }), { excludeType: ["bug"] })).toBe(true);
  });

  it("excludePriority: non-match when nib priority is excluded", () => {
    expect(matchesFilter(makeNib({ priority: "high" }), { excludePriority: ["high"] })).toBe(false);
    expect(matchesFilter(makeNib({ priority: "low" }), { excludePriority: ["high"] })).toBe(true);
  });

  it("excludeStatus: non-match when nib status is excluded", () => {
    expect(matchesFilter(makeNib({ status: "completed" }), { excludeStatus: ["completed"] })).toBe(false);
    expect(matchesFilter(makeNib({ status: "todo" }), { excludeStatus: ["completed"] })).toBe(true);
  });

  it("excludeEstimate: non-match when nib estimate is excluded", () => {
    expect(matchesFilter(makeNib({ estimate: "xl" }), { excludeEstimate: ["xl"] })).toBe(false);
    expect(matchesFilter(makeNib({ estimate: "m" }), { excludeEstimate: ["xl"] })).toBe(true);
  });

  it("excludeTags: non-match when nib has ANY excluded tag (overlap rule)", () => {
    // any overlap between nib.tags and excludeTags removes the nib
    expect(matchesFilter(makeNib({ tags: ["wip", "auth"] }), { excludeTags: ["wip"] })).toBe(false);
    // no overlap → still a match
    expect(matchesFilter(makeNib({ tags: ["auth"] }), { excludeTags: ["wip"] })).toBe(true);
    // untagged nib is never excluded by a tag list
    expect(matchesFilter(makeNib({ tags: [] }), { excludeTags: ["wip"] })).toBe(true);
  });

  it("exclusion ANDs with includes: an include-matching nib is still removed by an exclusion", () => {
    const filter: NibFilter = { type: ["bug"], excludeStatus: ["completed"] };
    // matches the type include-list but its status is excluded → non-match
    expect(matchesFilter(makeNib({ type: "bug", status: "completed" }), filter)).toBe(false);
    // matches the include-list and dodges the exclusion → match
    expect(matchesFilter(makeNib({ type: "bug", status: "todo" }), filter)).toBe(true);
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

  it("returns true when any exclude filter is active", () => {
    expect(hasClientFilters({ excludeType: ["bug"] })).toBe(true);
    expect(hasClientFilters({ excludePriority: ["high"] })).toBe(true);
    expect(hasClientFilters({ excludeStatus: ["completed"] })).toBe(true);
    expect(hasClientFilters({ excludeEstimate: ["xl"] })).toBe(true);
    expect(hasClientFilters({ excludeTags: ["wip"] })).toBe(true);
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

  it("returns false for empty exclude arrays", () => {
    expect(hasClientFilters({ excludeType: [], excludeStatus: [] })).toBe(false);
  });
});

describe("prepareFilter", () => {
  it("returns original filter as serverFilter when no client filters are active", () => {
    const filter: NibFilter = { search: "hello" };
    const result = prepareFilter(filter, EMPTY_AREAS);

    expect(result.serverFilter).toBe(filter); // reference equality
    expect(result.clientFiltersActive).toBe(false);
    expect(result.matchesClient(makeNib())).toBe(true);
  });

  it("moves the status include-list out of serverFilter into the client-side filter", () => {
    const filter: NibFilter = {
      search: "hello",
      status: [...OPEN_STATUSES],
    };
    const result = prepareFilter(filter, EMPTY_AREAS);

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
    const result = prepareFilter(filter, EMPTY_AREAS);

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
    const result = prepareFilter(filter, EMPTY_AREAS);

    expect(result.serverFilter).toEqual({ search: "test" });
    expect(result.serverFilter).not.toHaveProperty("type");
    expect(result.serverFilter).not.toHaveProperty("priority");
    expect(result.serverFilter).not.toHaveProperty("status");
    expect(result.serverFilter).not.toHaveProperty("estimate");
    expect(result.serverFilter).not.toHaveProperty("tags");
    expect(result.clientFiltersActive).toBe(true);
  });

  it("strips exclude* fields from serverFilter and applies them client-side", () => {
    const filter: NibFilter = {
      search: "hello",
      excludeType: ["bug"],
      excludePriority: ["low"],
      excludeStatus: ["completed"],
      excludeEstimate: ["xl"],
      excludeTags: ["wip"],
    };
    const result = prepareFilter(filter, EMPTY_AREAS);

    // The exclusions are applied client-side (so an excluded ancestor of active
    // children is fetched and dimmed rather than dropped server-side), so none
    // are forwarded to the server.
    expect(result.serverFilter).toEqual({ search: "hello" });
    expect(result.serverFilter).not.toHaveProperty("excludeType");
    expect(result.serverFilter).not.toHaveProperty("excludePriority");
    expect(result.serverFilter).not.toHaveProperty("excludeStatus");
    expect(result.serverFilter).not.toHaveProperty("excludeEstimate");
    expect(result.serverFilter).not.toHaveProperty("excludeTags");
    expect(result.clientFiltersActive).toBe(true);
    expect(result.matchesClient(makeNib({ status: "completed" }))).toBe(false);
    expect(result.matchesClient(makeNib({ status: "todo" }))).toBe(true);
  });

  it("matchesClient returns true when nib matches the client-side filter", () => {
    const filter: NibFilter = { type: ["bug", "feature"] };
    const result = prepareFilter(filter, EMPTY_AREAS);

    expect(result.matchesClient(makeNib({ type: "bug" }))).toBe(true);
    expect(result.matchesClient(makeNib({ type: "feature" }))).toBe(true);
  });

  it("matchesClient returns false when nib does not match client-side filter", () => {
    const filter: NibFilter = { type: ["bug"], priority: ["high"] };
    const result = prepareFilter(filter, EMPTY_AREAS);

    // wrong type
    expect(result.matchesClient(makeNib({ type: "task", priority: "high" }))).toBe(false);
    // wrong priority
    expect(result.matchesClient(makeNib({ type: "bug", priority: "low" }))).toBe(false);
    // both match
    expect(result.matchesClient(makeNib({ type: "bug", priority: "high" }))).toBe(true);
  });

  it("matchesClient handles tags with OR logic (at least one tag matches)", () => {
    const filter: NibFilter = { tags: ["frontend", "backend"] };
    const result = prepareFilter(filter, EMPTY_AREAS);

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

// The `area` rule. A bad `area` fails the WHOLE query on the server
// (refuseUndeclaredArea, internal/graph/filters.go) instead of narrowing it, and
// unlike the id-valued fields that fail the same way it cannot be tagged
// NOT_FOUND — `FilterAreaError` implements no Unwrap — so the refusal blanks the
// table with a red error rather than reaching TreeTable's calm inline branch.
// The client holds the vocabulary, so it can pre-check: `prepareFilter` sends
// the value only on a "declared" answer.
//
// Reached DIRECTLY here, and only here, because nothing in the app can set
// `filter.area` yet: `QueryFilter` omits it, so neither a typed token, a `?q=`
// link, nor a persisted query string can produce one (storage.ts re-parses the
// stored string, and serializeQuery has no `area:` to write). The path is the
// filter's, not the box's, and this is what a restored or shared value will
// travel once nibs-gdvz gives it a token.
describe("prepareFilter area rule", () => {
  const declared: AreaNode[] = [
    { path: "web", name: "web", description: "", color: "", depth: 0 },
    { path: "web/dashboard", name: "dashboard", description: "", color: "", depth: 1 },
  ];
  const READY_AREAS = createAreaVocabulary(declared);

  // Every vocabulary a session can hold, crossed with every shape an `area`
  // value can arrive in. `sent` is the whole assertion: a value reaches
  // serverFilter iff this vocabulary declares it — except the empty string,
  // which is sent unconditionally so the server's own refusal of it is what the
  // user sees (see withSendableArea).
  const ALL_VOCABULARIES = [
    "pre-load",
    "config query failed",
    "project declares none",
    "declared vocabulary",
  ];
  const vocabularies: [string, AreaVocabulary][] = [
    ["pre-load", LOADING_AREAS],
    ["config query failed", UNAVAILABLE_AREAS],
    ["project declares none", EMPTY_AREAS],
    ["declared vocabulary", READY_AREAS],
  ];
  const values: [string, string, string[]][] = [
    // label, value, the vocabularies (by label) that send it
    ["a declared path", "web", ["declared vocabulary"]],
    ["a declared subpath", "web/dashboard", ["declared vocabulary"]],
    ["a retired path", "retired", []],
    ["the empty string", "", ALL_VOCABULARIES],
  ];

  for (const [vocabLabel, areas] of vocabularies) {
    for (const [valueLabel, value, sendingVocabs] of values) {
      const sent = sendingVocabs.includes(vocabLabel);
      it(`${sent ? "sends" : "withholds"} ${valueLabel} with a ${vocabLabel} vocabulary`, () => {
        const result = prepareFilter({ area: value }, areas);
        if (sent) {
          expect(result.serverFilter.area).toBe(value);
        } else {
          expect(result.serverFilter).not.toHaveProperty("area");
        }
      });
    }
  }

  it("leaves a filter carrying no area untouched, whatever the vocabulary answers", () => {
    for (const [, areas] of vocabularies) {
      const filter: NibFilter = { search: "hello" };
      const result = prepareFilter(filter, areas);
      // Reference equality: the no-client-filters fast path is unchanged for the
      // filters that carry no area at all, which is every filter today.
      expect(result.serverFilter).toBe(filter);
    }
  });

  it("withholds the area on the client-filter path too, and keeps the rest", () => {
    // The withheld value must not depend on which return path the filter takes:
    // an active client filter routes through the destructuring branch instead of
    // the fast path.
    const filter: NibFilter = { area: "retired", type: ["bug"], search: "hello" };
    const result = prepareFilter(filter, READY_AREAS);

    expect(result.serverFilter).toEqual({ search: "hello" });
    expect(result.clientFiltersActive).toBe(true);
    expect(result.matchesClient(makeNib({ type: "bug" }))).toBe(true);
    expect(result.matchesClient(makeNib({ type: "task" }))).toBe(false);
  });

  it("sends the empty string so the server's refusal of it stays loud", () => {
    // The server refuses `area: ""` BEFORE it tests membership, because reading
    // it as "unset" would drop the branch and widen the query to the whole store
    // (refuseUndeclaredArea). Withholding it here would perform that same
    // widening. Nothing in the app can produce one — the box does not recognize
    // an empty-valued token — so it can only arrive from client code, and that
    // is exactly the arrival that must not be silent.
    const filter: NibFilter = { area: "" };

    // Reference-identical on the fast path, like every other no-op answer.
    expect(prepareFilter(filter, READY_AREAS).serverFilter).toBe(filter);
    // The vocabulary is not consulted at all: an answer that never arrived
    // cannot make the empty string more or less refusable than it already is.
    expect(prepareFilter(filter, UNAVAILABLE_AREAS).serverFilter.area).toBe("");
  });

  it("does not mutate the filter it was handed", () => {
    // The caller keeps the unstripped filter — it is what the box serializes back
    // into `?q=` and localStorage, so a shared link must not lose the token to a
    // vocabulary that had not arrived yet.
    const filter: NibFilter = { area: "web", search: "hello" };
    prepareFilter(filter, LOADING_AREAS);

    expect(filter.area).toBe("web");
  });

  it("re-applies the area when the vocabulary lands", () => {
    // The "unknown" → "declared" transition, as the app makes it: TreeTable
    // re-derives `prepareFilter(resolvedFilter, viewSpine().areas)` when the
    // spine's vocabulary changes, which is the same filter asked twice.
    const filter: NibFilter = { area: "web" };

    expect(prepareFilter(filter, LOADING_AREAS).serverFilter).not.toHaveProperty("area");
    expect(prepareFilter(filter, READY_AREAS).serverFilter.area).toBe("web");
  });

  it("treats a failed config query like a pending one, not like a project with no areas", () => {
    // Both are empty vocabularies and both withhold — but for different reasons,
    // and only one of them is answering the question. UNAVAILABLE_AREAS says
    // "unknown" so the value survives to be sent if a vocabulary ever arrives,
    // where EMPTY_AREAS has answered: nothing is declared, so nothing is
    // declarable.
    expect(UNAVAILABLE_AREAS.validity("web")).toBe("unknown");
    expect(EMPTY_AREAS.validity("web")).toBe("undeclared");
    expect(prepareFilter({ area: "web" }, UNAVAILABLE_AREAS).serverFilter).not.toHaveProperty("area");
    expect(prepareFilter({ area: "web" }, EMPTY_AREAS).serverFilter).not.toHaveProperty("area");
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
