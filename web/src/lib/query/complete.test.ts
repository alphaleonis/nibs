import { describe, it, expect } from "vitest";
import { getCompletion } from "./index";
import {
  createAreaVocabulary,
  EMPTY_AREAS,
  LOADING_AREAS,
  UNAVAILABLE_AREAS,
} from "../areas";
import type { AreaVocabulary } from "../areas";

// Helper: complete with the caret at the end of the text.
const at = (text: string, tags: string[] = []) => getCompletion(text, text.length, tags);

describe("getCompletion — field names", () => {
  it("suggests field names for a partial token", () => {
    const c = at("ty");
    expect(c?.kind).toBe("field");
    expect(c?.items).toEqual(["type"]);
  });

  it("prefix-matches (t → type, tags)", () => {
    expect(at("t")?.items).toEqual(["type", "tags"]);
  });

  it("inserts the field with a trailing colon", () => {
    const c = at("ty");
    expect(c?.apply("type")).toEqual({ text: "type:", caret: 5 });
  });

  it("keeps the negation prefix when completing a field name", () => {
    const c = at("-ty");
    expect(c?.kind).toBe("field");
    expect(c?.apply("type")).toEqual({ text: "-type:", caret: 6 });
  });

  it("returns null when no field name matches (it is a search word)", () => {
    expect(at("zzz")).toBeNull();
  });

  it("returns null for an empty token", () => {
    expect(getCompletion("", 0)).toBeNull();
    expect(getCompletion("type:bug ", 9)).toBeNull();
  });
});

describe("getCompletion — enum values", () => {
  it("suggests all values right after the colon", () => {
    const c = at("type:");
    expect(c?.kind).toBe("value");
    expect(c?.items).toEqual(["milestone", "epic", "bug", "feature", "task", "research"]);
  });

  it("substring-filters values by the partial", () => {
    const c = at("status:in");
    expect(c?.items).toEqual(["in-progress"]);
  });

  it("inserts the value, preserving the field", () => {
    const c = at("type:bu");
    expect(c?.apply("bug")).toEqual({ text: "type:bug", caret: 8 });
  });

  it("completes the segment after the last comma and excludes already-chosen values", () => {
    const c = at("status:todo,in");
    expect(c?.items).toEqual(["in-progress"]);
    expect(c?.apply("in-progress")).toEqual({ text: "status:todo,in-progress", caret: 23 });
  });

  it("does not re-suggest a value already present earlier in the token", () => {
    const c = at("type:bug,");
    expect(c?.items).not.toContain("bug");
    expect(c?.items).toContain("feature");
  });

  it("returns null for an unknown field", () => {
    expect(at("title:fo")).toBeNull();
  });
});

describe("getCompletion — tags", () => {
  it("suggests from availableTags, substring-filtered", () => {
    const c = at("tags:fr", ["frontend", "backend"]);
    expect(c?.kind).toBe("tag");
    expect(c?.items).toEqual(["frontend"]);
  });

  it("returns null when no available tag matches", () => {
    expect(at("tags:zzz", ["frontend"])).toBeNull();
  });

  it("inserts the chosen tag", () => {
    const c = at("tags:", ["frontend", "backend"]);
    expect(c?.apply("backend")).toEqual({ text: "tags:backend", caret: 12 });
  });
});

describe("getCompletion — status group shortcuts", () => {
  it("offers the group names ahead of the concrete statuses", () => {
    const c = at("status:");
    expect(c?.kind).toBe("value");
    expect(c?.items).toEqual([
      "open",
      "closed",
      "draft",
      "todo",
      "in-progress",
      "deferred",
      "completed",
      "scrapped",
    ]);
  });

  it("substring-filters the group names like any other value", () => {
    expect(at("status:ope")?.items).toEqual(["open"]);
    expect(at("status:clos")?.items).toEqual(["closed"]);
  });

  it("inserts the chosen group name", () => {
    expect(at("status:op")?.apply("open")).toEqual({ text: "status:open", caret: 11 });
  });

  it("does not re-suggest a group already chosen in the token", () => {
    expect(at("status:open,")?.items).not.toContain("open");
  });

  it("offers no group names for a field that has none", () => {
    expect(at("type:")?.items).toEqual(["milestone", "epic", "bug", "feature", "task", "research"]);
  });
});

// The completable vocabulary is the whole query language, not just the five
// metadata facets: the relationship-id field names and the `has`/`no`/`is`
// existence words complete too. The pool is derived from `relations.ts`, so these
// expectations track the grammar rather than a hand-copied list.
describe("getCompletion — relationship and existence field names", () => {
  it("completes relationship-id field names by prefix", () => {
    expect(at("blo")?.items).toEqual(["blocking", "blocked-by"]);
    expect(at("ment")?.items).toEqual(["mentions", "mentioned-by"]);
  });

  it("completes the existence words", () => {
    expect(at("ha")?.items).toEqual(["has"]);
    expect(at("n")?.items).toEqual(["no"]);
    expect(at("is")?.items).toEqual(["is"]);
  });

  it("inserts a relationship field name with a trailing colon", () => {
    expect(at("blo")?.apply("blocked-by")).toEqual({ text: "blocked-by:", caret: 11 });
  });

  it("inserts an existence word with a trailing colon", () => {
    expect(at("ha")?.apply("has")).toEqual({ text: "has:", caret: 4 });
  });
});

describe("getCompletion — existence token values", () => {
  it("offers the has/no dimensions that have a server predicate", () => {
    const c = at("has:");
    expect(c?.kind).toBe("value");
    expect(c?.items).toEqual(["parent", "blocking", "blocked-by"]);
    expect(at("no:")?.items).toEqual(["parent", "blocking", "blocked-by"]);
  });

  // `is:` carries the two dimensions that have no `no:` twin — a state and the
  // assignment axis — so the word completes to both.
  it("offers the twinless dimensions for is:", () => {
    expect(at("is:")?.items).toEqual(["blocked", "backlog"]);
  });

  it("substring-filters the is: dimensions apart", () => {
    expect(at("is:back")?.items).toEqual(["backlog"]);
    expect(at("is:blo")?.items).toEqual(["blocked"]);
  });

  it("substring-filters existence values", () => {
    expect(at("has:block")?.items).toEqual(["blocking", "blocked-by"]);
    expect(at("no:by")?.items).toEqual(["blocked-by"]);
  });

  it("inserts the chosen existence value", () => {
    expect(at("has:blo")?.apply("blocking")).toEqual({ text: "has:blocking", caret: 12 });
  });

  it("returns null when no existence value matches", () => {
    expect(at("is:zzz")).toBeNull();
    expect(at("has:mentions")).toBeNull();
  });
});

// Negation is metadata-only: `relations.ts` disqualifies a leading `-` from
// rel/existence recognition, so offering those names after `-` would suggest
// tokens the parser parks as invalid.
describe("getCompletion — negation stays metadata-only", () => {
  it("offers only the five metadata names after a bare `-`", () => {
    expect(getCompletion("-", 1)?.items).toEqual(["type", "priority", "status", "estimate", "tags"]);
  });

  it("does not complete a negated relationship or existence name", () => {
    expect(at("-blo")).toBeNull();
    expect(at("-ha")).toBeNull();
  });

  it("does not complete values for a negated existence token", () => {
    expect(at("-has:")).toBeNull();
  });
});

// Explicit trigger (Ctrl+Space): only an EXPLICIT request turns an empty token
// into the full field list. Without the flag an empty token stays `null`, because
// the Toolbar refreshes on focus and would otherwise pop a dropdown over the table
// every time the empty box takes focus.
describe("getCompletion — explicit trigger", () => {
  const ALL_FIELDS = [
    "type",
    "priority",
    "status",
    "estimate",
    "tags",
    "parent",
    "ancestor",
    "descendant",
    "sibling",
    "blocking",
    "blocked-by",
    "mentions",
    "mentioned-by",
    "milestone",
    "has",
    "no",
    "is",
    "area",
  ];

  it("lists the whole vocabulary for an empty token", () => {
    const c = getCompletion("", 0, [], { explicit: true });
    expect(c?.kind).toBe("field");
    expect(c?.items).toEqual(ALL_FIELDS);
  });

  it("lists the whole vocabulary at a caret after a space", () => {
    expect(getCompletion("type:bug ", 9, [], { explicit: true })?.items).toEqual(ALL_FIELDS);
  });

  it("still returns null for an empty token without the flag", () => {
    expect(getCompletion("", 0, [], { explicit: false })).toBeNull();
    expect(getCompletion("", 0)).toBeNull();
    expect(getCompletion("type:bug ", 9)).toBeNull();
  });

  it("offers only the metadata names for an empty negated token", () => {
    expect(getCompletion("-", 1, [], { explicit: true })?.items).toEqual([
      "type",
      "priority",
      "status",
      "estimate",
      "tags",
    ]);
  });

  it("behaves exactly like typing mid-token — the flag only bypasses the empty-token case", () => {
    expect(getCompletion("ty", 2, [], { explicit: true })?.items).toEqual(at("ty")?.items);
    expect(getCompletion("status:in", 9, [], { explicit: true })?.items).toEqual(["in-progress"]);
  });

  it("separates the insert from a token the caret is jammed against", () => {
    // Caret at 0 of a real query: without the separator the insert would merge into
    // `status:todo`, yielding the single token `type:status:todo` and losing a facet.
    const c = getCompletion("status:todo tags:wip", 0, [], { explicit: true });
    expect(c?.apply("type")).toEqual({ text: "type: status:todo tags:wip", caret: 5 });
  });

  it("adds no separator when the caret already has whitespace or nothing after it", () => {
    expect(getCompletion("ty", 2)?.apply("type")).toEqual({ text: "type:", caret: 5 });
    expect(getCompletion("ty tags:wip", 2)?.apply("type")).toEqual({
      text: "type: tags:wip",
      caret: 5,
    });
  });
});

describe("getCompletion — area paths", () => {
  const declared = [
    { path: "web", name: "web", description: "", color: "", depth: 0 },
    { path: "web/dashboard", name: "dashboard", description: "", color: "", depth: 1 },
    { path: "webhooks", name: "webhooks", description: "", color: "", depth: 0 },
    { path: "docs", name: "docs", description: "", color: "", depth: 0 },
  ];
  const READY = createAreaVocabulary(declared);
  const complete = (text: string, areas?: AreaVocabulary) =>
    getCompletion(text, text.length, [], { areas });

  it("offers the field name itself", () => {
    expect(complete("ar", READY)?.items).toEqual(["area"]);
  });

  it("does not offer it after a minus — negation has no area spelling", () => {
    expect(complete("-ar", READY)).toBeNull();
  });

  it("offers every declared path, in declaration order, for a bare `area:`", () => {
    const c = complete("area:", READY);
    expect(c?.kind).toBe("value");
    expect(c?.items).toEqual(["web", "web/dashboard", "webhooks", "docs"]);
  });

  it("substring-matches the partial, keeping declaration order", () => {
    expect(complete("area:web", READY)?.items).toEqual(["web", "web/dashboard", "webhooks"]);
    expect(complete("area:dash", READY)?.items).toEqual(["web/dashboard"]);
  });

  it("matches case-insensitively while inserting the declared spelling", () => {
    const c = complete("area:WEB", READY);
    expect(c?.items).toEqual(["web", "web/dashboard", "webhooks"]);
    expect(c?.apply("web/dashboard")).toEqual({ text: "area:web/dashboard", caret: 18 });
  });

  it("replaces only the typed run, leaving the rest of the query alone", () => {
    const text = "type:bug area:da tags:wip";
    const c = getCompletion(text, 16, [], { areas: READY });
    expect(c?.apply("web/dashboard")).toEqual({
      text: "type:bug area:web/dashboard tags:wip",
      caret: 27,
    });
  });

  it("offers nothing when no path matches", () => {
    expect(complete("area:zzz", READY)).toBeNull();
  });

  // The three vocabularies that cannot answer. Offering nothing is the whole
  // behavior: a menu built from a vocabulary that has not arrived would either be
  // empty anyway or, worse, assert the project declares none.
  it("offers nothing while the vocabulary is loading, unavailable, or absent", () => {
    for (const areas of [LOADING_AREAS, UNAVAILABLE_AREAS, undefined]) {
      expect(complete("area:", areas)).toBeNull();
      expect(complete("area:web", areas)).toBeNull();
    }
  });

  it("offers nothing for a project that declares none", () => {
    expect(complete("area:", EMPTY_AREAS)).toBeNull();
  });

  it("still offers the field name when the vocabulary cannot answer", () => {
    // The token is part of the language whether or not this project uses it; what
    // the vocabulary gates is the VALUE list.
    expect(complete("ar", LOADING_AREAS)?.items).toEqual(["area"]);
    expect(complete("ar")?.items).toEqual(["area"]);
  });

  // `validateAreaNodes` (internal/config/areas.go) rejects only an empty name, one
  // with padding, and one containing "/" — so a declared path may carry interior
  // whitespace, and the grammar splits on it with no quoting to escape it.
  // Accepting `Web UI` would produce `area:Web` plus a bare `UI` in free text: the
  // area filter lost, drag reorder disabled, and the chip naming the wrong token.
  describe("paths the grammar cannot express", () => {
    const SPACEY = createAreaVocabulary([
      { path: "web", name: "web", description: "", color: "", depth: 0 },
      { path: "Web UI", name: "Web UI", description: "", color: "", depth: 0 },
      { path: "Web UI/panel", name: "panel", description: "", color: "", depth: 1 },
      { path: "docs", name: "docs", description: "", color: "", depth: 0 },
    ]);

    it("does not offer a path containing whitespace", () => {
      expect(complete("area:", SPACEY)?.items).toEqual(["web", "docs"]);
    });

    it("offers nothing at all when every match carries whitespace", () => {
      expect(complete("area:panel", SPACEY)).toBeNull();
      expect(complete("area:Web U", SPACEY)).toBeNull();
    });

    it("still offers the typeable siblings of an untypeable match", () => {
      expect(complete("area:we", SPACEY)?.items).toEqual(["web"]);
    });
  });
});
