import { describe, it, expect } from "vitest";
import { parseQuery, serializeQuery } from "./index";
import { REL_TOKEN_ORDER } from "./relations";
import { createAreaVocabulary } from "../areas";
import type { AreaVocabulary } from "../areas";
import type { NibFilter } from "../types";

// Round-trip a canonical string through parse → serialize.
const rt = (s: string) => serializeQuery(parseQuery(s));

describe("serializeQuery", () => {
  it("emits a single positive token for one value", () => {
    expect(serializeQuery({ filter: { status: ["todo"] } })).toBe("status:todo");
  });

  it("comma-joins multiple values within a field", () => {
    expect(serializeQuery({ filter: { status: ["todo", "in-progress"] } })).toBe("status:todo,in-progress");
  });

  it("orders enum values by declaration order, not input order (canonical-out)", () => {
    // STATUSES order is draft,todo,in-progress,... so todo precedes in-progress.
    expect(serializeQuery({ filter: { status: ["in-progress", "todo"] } })).toBe("status:todo,in-progress");
  });

  it("orders tag values alphabetically", () => {
    expect(serializeQuery({ filter: { tags: ["zebra", "apple"] } })).toBe("tags:apple,zebra");
  });

  it("emits the positive token before the negative for the same field", () => {
    expect(serializeQuery({ filter: { type: ["bug"], excludeType: ["task"] } })).toBe("type:bug -type:task");
  });

  it("emits fields in canonical order: type, priority, status, estimate, tags", () => {
    const filter: NibFilter = {
      tags: ["auth"],
      estimate: ["m"],
      status: ["todo"],
      priority: ["high"],
      type: ["bug"],
    };
    expect(serializeQuery({ filter })).toBe("type:bug priority:high status:todo estimate:m tags:auth");
  });

  it("emits search after the metadata tokens", () => {
    expect(serializeQuery({ filter: { status: ["todo"], search: "login" } })).toBe("status:todo login");
  });

  it("appends invalid tokens at the very end, after search", () => {
    expect(
      serializeQuery({ filter: { type: ["bug"], search: "login" }, invalidTokens: ["status:banana"] }),
    ).toBe("type:bug login status:banana");
  });

  it("returns an empty string for an empty filter", () => {
    expect(serializeQuery({ filter: {} })).toBe("");
  });
});

describe("serializeQuery — relationship + existence tokens (phase 5)", () => {
  it("emits a relationship-id scalar as field:id", () => {
    expect(serializeQuery({ filter: { blockingId: "tnib-9" } })).toBe("blocking:tnib-9");
  });

  it("uses hyphenated field-names for blocked-by / mentioned-by", () => {
    expect(serializeQuery({ filter: { blockedById: "tnib-1" } })).toBe("blocked-by:tnib-1");
    expect(serializeQuery({ filter: { mentionedById: "tnib-2" } })).toBe("mentioned-by:tnib-2");
  });

  it("emits an existence boolean as its fixed token", () => {
    expect(serializeQuery({ filter: { hasParent: true } })).toBe("has:parent");
    expect(serializeQuery({ filter: { isBlocked: true } })).toBe("is:blocked");
    // false is a SET value on the tri-state field, so it emits the no: spelling
    // rather than being omitted.
    expect(serializeQuery({ filter: { hasBlockedBy: false } })).toBe("no:blocked-by");
  });

  it("omits an existence field that is unset, but emits `no:` for an explicit false", () => {
    // The field is tri-state: undefined means "do not filter", false means
    // "filter for absence". Only the first is omitted — collapsing them would
    // reinstate the silent no-op the backend filter model just removed.
    expect(serializeQuery({ filter: {} })).toBe("");
    expect(serializeQuery({ filter: { hasParent: undefined } })).toBe("");
    expect(serializeQuery({ filter: { hasParent: false } })).toBe("no:parent");
  });

  it("places rel/existence tokens AFTER metadata and BEFORE free-text search", () => {
    const filter: NibFilter = { status: ["todo"], parentId: "x", search: "login" };
    expect(serializeQuery({ filter })).toBe("status:todo parent:x login");
  });

  it("orders rel/existence tokens by dimension: parent, blocking, blocked-by (+is:blocked), mentions, mentioned-by", () => {
    // Deliberately provide them out of canonical order to prove the fixed order wins.
    const filter: NibFilter = {
      mentionedById: "m2",
      isBlocked: true,
      blockingId: "b1",
      mentionsId: "m1",
      parentId: "p1",
    };
    expect(serializeQuery({ filter })).toBe(
      "parent:p1 blocking:b1 is:blocked mentions:m1 mentioned-by:m2",
    );
  });
});

describe("serializeQuery — hierarchy relationship tokens", () => {
  it("emits each hierarchy scalar as its own token", () => {
    expect(serializeQuery({ filter: { ancestorId: "tnib-1" } })).toBe("ancestor:tnib-1");
    expect(serializeQuery({ filter: { descendantId: "tnib-2" } })).toBe("descendant:tnib-2");
    expect(serializeQuery({ filter: { siblingId: "tnib-3" } })).toBe("sibling:tnib-3");
  });

  it("groups the hierarchy tokens with parent, before the blocking dimension", () => {
    // Placement in REL_TOKEN_ORDER is what makes the round-trip identity hold, so
    // pin it: the four hierarchy tokens form one contiguous block after parent's
    // has/no pair, and blocking still follows them. Supplied out of order on
    // purpose so only the fixed order can produce this string.
    const filter: NibFilter = {
      blockingId: "b1",
      siblingId: "s1",
      descendantId: "d1",
      ancestorId: "a1",
      hasParent: true,
      parentId: "p1",
    };
    expect(serializeQuery({ filter })).toBe(
      "parent:p1 has:parent ancestor:a1 descendant:d1 sibling:s1 blocking:b1",
    );
  });

  it("places the hierarchy tokens after metadata and before free-text search", () => {
    const filter: NibFilter = { status: ["todo"], ancestorId: "a1", search: "login" };
    expect(serializeQuery({ filter })).toBe("status:todo ancestor:a1 login");
  });
});

describe("serializeQuery — status group collapse", () => {
  // The include-list is the single source of truth, so a group token would
  // otherwise expand the moment the box lost focus. Collapsing an exact match
  // back to the group name is what keeps `status:open` typed, shared, and
  // re-read as `status:open`.
  it("collapses the exact open set to status:open", () => {
    expect(serializeQuery({ filter: { status: ["draft", "todo", "in-progress"] } })).toBe("status:open");
  });

  it("collapses the exact closed set to status:closed", () => {
    expect(serializeQuery({ filter: { status: ["deferred", "completed", "scrapped"] } })).toBe("status:closed");
  });

  it("collapses regardless of the order the members arrive in", () => {
    expect(serializeQuery({ filter: { status: ["in-progress", "draft", "todo"] } })).toBe("status:open");
  });

  // There is deliberately no `serializeQuery({ status: [...OPEN_STATUSES] })`
  // case here: STATUS_GROUPS.get("open") IS the OPEN_STATUSES reference, so such
  // a test compares the constant against itself and holds for whatever it
  // contains — it would keep passing with OPEN_STATUSES defined wrongly, while
  // the hardcoded case above correctly fails. The dropdown→box sync it was
  // meant to cover is asserted end-to-end in Toolbar.test.ts instead ("the Open
  // preset renders in the query box as status:open").

  it("collapses the exclude-list too", () => {
    expect(serializeQuery({ filter: { excludeStatus: ["deferred", "completed", "scrapped"] } })).toBe("-status:closed");
  });

  it("does NOT collapse a partial set", () => {
    expect(serializeQuery({ filter: { status: ["draft", "todo"] } })).toBe("status:draft,todo");
  });

  it("does NOT collapse a same-sized set that is not a group", () => {
    // Three statuses, like the open group has — but not those three. Matching a
    // group by size alone would mislabel this as `status:open`.
    expect(serializeQuery({ filter: { status: ["draft", "todo", "completed"] } })).toBe(
      "status:draft,todo,completed",
    );
  });

  it("collapses the group part of a superset and keeps the rest", () => {
    // The extra value must survive: emitting a bare `status:open` here would
    // silently drop `completed` from the filter.
    expect(serializeQuery({ filter: { status: ["draft", "todo", "in-progress", "completed"] } })).toBe(
      "status:open,completed",
    );
  });

  it("leaves a lone status alone — no group has a single member", () => {
    // `parked` was dropped for exactly this reason: a one-member group would
    // rewrite `status:deferred` into a second spelling of itself.
    expect(serializeQuery({ filter: { status: ["deferred"] } })).toBe("status:deferred");
  });

  it("does not collapse fields that have no groups", () => {
    expect(serializeQuery({ filter: { type: ["milestone", "epic", "bug", "feature", "task", "research"] } })).toBe(
      "type:milestone,epic,bug,feature,task,research",
    );
  });
});

describe("serializeQuery — status group canonicalization", () => {
  it("rewrites the spelled-out open set into the group name", () => {
    // Not the identity round-trip: `status:draft,todo,in-progress` is no longer
    // canonical form — `status:open` is the canonical spelling of that set.
    expect(rt("status:draft,todo,in-progress")).toBe("status:open");
    expect(rt("status:deferred,completed,scrapped")).toBe("status:closed");
  });

  it("keeps a partial set spelled out", () => {
    expect(rt("status:draft,todo")).toBe("status:draft,todo");
  });

  it("keeps BOTH group names when one token names two groups", () => {
    expect(rt("status:open,closed")).toBe("status:open,closed");
  });

  // The reported case. A group name has to survive alongside values outside it,
  // or the shorthand is lost the moment the box blurs and every shared `?q=`
  // link carries the spelled-out member list instead.
  it("keeps a group name beside a value outside the group", () => {
    expect(rt("status:open,deferred")).toBe("status:open,deferred");
    expect(rt("status:open,completed")).toBe("status:open,completed");
    expect(rt("status:draft,closed")).toBe("status:draft,closed");
  });

  // Ordering is by the lowest index a token covers, so the canonical form does
  // not depend on how the user happened to type it.
  it("canonicalizes to one spelling regardless of input order", () => {
    expect(rt("status:deferred,open")).toBe("status:open,deferred");
    expect(rt("status:completed,draft,in-progress,todo")).toBe("status:open,completed");
  });

  it("still spells out a partial group", () => {
    expect(rt("status:draft,todo")).toBe("status:draft,todo");
  });

  // Exclusion lists render through the same path, so the shorthand survives there too.
  it("collapses inside a negated token", () => {
    expect(rt("-status:open,deferred")).toBe("-status:open,deferred");
  });
});

describe("round-trip identity — serializeQuery(parseQuery(s)) === s", () => {
  // Every rel/existence spelling on its own, generated from REL_TOKEN_ORDER so a
  // token added there is covered without anyone remembering to hand-author a string
  // for it. These pin RECOGNITION ↔ SERIALIZATION per token. The multi-token strings
  // in `canonical` below stay hand-authored on purpose: they are what pins the
  // ORDER, and an expectation generated from the same array would agree with any
  // order the array happened to be in.
  const relSingles = REL_TOKEN_ORDER.map((t) => (t.kind === "id" ? `${t.name}:tnib-1` : t.token));

  it("covers every ordered rel/existence token", () => {
    // Guards the generated corpus against silently shrinking to nothing — an empty
    // `relSingles` would emit zero `it` blocks and the suite would still pass.
    expect(relSingles.length).toBe(REL_TOKEN_ORDER.length);
    expect(relSingles.length).toBeGreaterThan(10);
  });

  const canonical = [
    "",
    "type:bug",
    "type:bug,feature",
    "-type:task",
    "type:bug -type:task",
    "priority:high",
    "status:todo,in-progress",
    "-status:completed",
    // status group shortcuts survive the round-trip because serialize collapses
    // an exact member match back to the group name
    "status:open",
    "status:closed",
    "-status:open",
    "-status:closed",
    "status:open -status:draft",
    "estimate:m",
    "tags:apple,zebra",
    "-tags:wip",
    "type:bug priority:high status:todo estimate:m tags:auth",
    "type:bug login flow",
    "login flow",
    // a known-field token whose values are all empty (only commas) is kept as
    // free text, so it round-trips instead of vanishing.
    "type:,",
    "-type:,",
    // invalid tokens preserved through the round-trip
    "status:banana",
    "type:bug status:banana",
    "type:bug login status:banana",
    // a negated rel/existence token is parked as invalid (negation is metadata-only)
    // and preserved verbatim, so the user can see and edit what was rejected
    "-parent:tnib-1",
    "-ancestor:tnib-1",
    "-is:blocked",
    "type:bug login -ancestor:tnib-1",
    // every relationship-id scalar (incl. the hyphenated field-names) and every
    // existence/state boolean, one token per string — see `relSingles` above
    ...relSingles,
    // metadata + rel/existence + search interleaved, in canonical order
    "type:bug parent:tnib-1 has:blocking is:blocked mentions:tnib-2 login",
    // rel/existence "monster": every rel token in canonical dimension order
    // has: and no: for one dimension are two spellings of one tri-state field, so
    // a canonical string carries at most one of each pair — the old grammar let
    // you write "has:parent no:parent", which was self-contradictory. One of each
    // spelling appears below so both directions round-trip.
    "parent:tnib-1 has:parent blocking:tnib-2 no:blocking blocked-by:tnib-3 has:blocked-by is:blocked mentions:tnib-4 mentioned-by:tnib-5",
    // the same monster with the three hierarchy tokens in their canonical slot,
    // which only round-trips if REL_TOKEN_ORDER groups them right after parent
    "parent:tnib-1 has:parent ancestor:tnib-6 descendant:tnib-7 sibling:tnib-8 blocking:tnib-2 no:blocking blocked-by:tnib-3 has:blocked-by is:blocked mentions:tnib-4 mentioned-by:tnib-5 milestone:tnib-9 is:backlog",
    // hierarchy tokens interleaved with metadata + search, in canonical order
    "type:epic ancestor:tnib-1 descendant:tnib-2 sibling:tnib-3 login",
    // the assignment axis, which sits last in REL_TOKEN_ORDER: its id token before
    // its existence token, and the whole pair after every relationship dimension
    "milestone:tnib-1 is:backlog",
    "type:epic milestone:tnib-1 is:backlog login",
    "parent:tnib-1 mentioned-by:tnib-5 milestone:tnib-9 is:backlog",
    // the ownership axis, emitted after the whole rel/existence block and before
    // free text. `rt` supplies no vocabulary, so the value is kept unjudged — the
    // declared and undeclared vocabularies are round-tripped separately below.
    "area:web",
    "area:web/dashboard",
    "type:bug area:web",
    "area:web login",
    "type:epic milestone:tnib-1 is:backlog area:web login",
    "parent:tnib-1 area:web/dashboard words status:banana",
    // a negated area token is parked, so it round-trips from the sidecar
    "-area:web",
    "type:bug -area:web",
    // full monster: every field positive + negative, search, then two invalids
    "type:bug -type:task priority:high -priority:low status:todo -status:completed estimate:m -estimate:xl tags:auth -tags:wip login words status:banana -priority:pink",
  ];

  for (const s of canonical) {
    it(`round-trips ${JSON.stringify(s)}`, () => {
      expect(rt(s)).toBe(s);
    });
  }
});

describe("serializeQuery — the area token", () => {
  const READY: AreaVocabulary = createAreaVocabulary([
    { path: "web", name: "web", description: "", color: "", depth: 0 },
    { path: "web/dashboard", name: "dashboard", description: "", color: "", depth: 1 },
  ]);

  it("emits the path verbatim — no case folding, no escaping", () => {
    expect(serializeQuery({ filter: { area: "web/dashboard" } })).toBe("area:web/dashboard");
    expect(serializeQuery({ filter: { area: "Web" } })).toBe("area:Web");
  });

  it("emits it after the whole rel/existence block and before free text", () => {
    const filter: NibFilter = {
      search: "login",
      area: "web",
      milestone: "tnib-9",
      type: ["bug"],
    };
    expect(serializeQuery({ filter })).toBe("type:bug milestone:tnib-9 area:web login");
  });

  // The round-trip identity is per-vocabulary: parse must be asked with the same
  // one the string was canonicalized under, which is what the box does (it holds
  // one vocabulary and passes it to both sides).
  it("round-trips a declared path through a declared vocabulary", () => {
    for (const s of ["area:web", "type:bug area:web/dashboard login"]) {
      expect(serializeQuery(parseQuery(s, READY))).toBe(s);
    }
  });

  it("round-trips an undeclared path out of the invalid sidecar", () => {
    // Parked rather than filtered, so it comes back at the END — after free text —
    // which is that string's canonical form under this vocabulary.
    expect(serializeQuery(parseQuery("area:retired", READY))).toBe("area:retired");
    expect(serializeQuery(parseQuery("type:bug area:retired login", READY))).toBe(
      "type:bug login area:retired",
    );
    // And that form is a fixed point: parking it again changes nothing.
    expect(serializeQuery(parseQuery("type:bug login area:retired", READY))).toBe(
      "type:bug login area:retired",
    );
  });

  it("keeps a stored path through a reload, where no vocabulary has arrived yet", () => {
    // Preferences.setQuery parses with no vocabulary. Judging the token there
    // would drop a valid filter at a moment the user did not act.
    const stored = "type:bug area:web login";
    expect(serializeQuery(parseQuery(stored))).toBe(stored);
  });
});
