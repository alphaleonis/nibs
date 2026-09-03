import { describe, it, expect } from "vitest";
import { parseQuery } from "./index";
import type { ParsedQuery } from "./index";
import { createAreaVocabulary, EMPTY_AREAS, LOADING_AREAS, UNAVAILABLE_AREAS } from "../areas";
import type { AreaVocabulary } from "../areas";

describe("parseQuery — metadata grammar", () => {
  const cases: { name: string; input: string; expected: ParsedQuery }[] = [
    // --- all five fields, positive ---
    { name: "type token", input: "type:bug", expected: { filter: { type: ["bug"] }, invalidTokens: [] } },
    { name: "priority token", input: "priority:high", expected: { filter: { priority: ["high"] }, invalidTokens: [] } },
    { name: "status token", input: "status:todo", expected: { filter: { status: ["todo"] }, invalidTokens: [] } },
    { name: "estimate token", input: "estimate:m", expected: { filter: { estimate: ["m"] }, invalidTokens: [] } },
    { name: "tags token", input: "tags:frontend", expected: { filter: { tags: ["frontend"] }, invalidTokens: [] } },

    // --- negation (5 enum fields → exclude*) ---
    { name: "-type", input: "-type:bug", expected: { filter: { excludeType: ["bug"] }, invalidTokens: [] } },
    { name: "-priority", input: "-priority:high", expected: { filter: { excludePriority: ["high"] }, invalidTokens: [] } },
    { name: "-status", input: "-status:completed", expected: { filter: { excludeStatus: ["completed"] }, invalidTokens: [] } },
    { name: "-estimate", input: "-estimate:xl", expected: { filter: { excludeEstimate: ["xl"] }, invalidTokens: [] } },
    { name: "-tags", input: "-tags:wip", expected: { filter: { excludeTags: ["wip"] }, invalidTokens: [] } },

    // --- comma multi-value (OR within a field) ---
    {
      name: "comma multi-value status",
      input: "status:todo,in-progress",
      expected: { filter: { status: ["todo", "in-progress"] }, invalidTokens: [] },
    },
    {
      name: "comma multi-value type (three)",
      input: "type:bug,feature,task",
      expected: { filter: { type: ["bug", "feature", "task"] }, invalidTokens: [] },
    },
    {
      name: "comma multi-value negation",
      input: "-tags:wip,later",
      expected: { filter: { excludeTags: ["wip", "later"] }, invalidTokens: [] },
    },

    // --- repeated-token union + dedupe ---
    {
      name: "repeated tokens union",
      input: "status:todo status:in-progress",
      expected: { filter: { status: ["todo", "in-progress"] }, invalidTokens: [] },
    },
    { name: "repeated identical token dedupes", input: "type:bug type:bug", expected: { filter: { type: ["bug"] }, invalidTokens: [] } },
    { name: "comma dedupe within one token", input: "status:todo,todo", expected: { filter: { status: ["todo"] }, invalidTokens: [] } },
    {
      name: "mixed comma + repeat union (lenient-in)",
      input: "type:bug,feature type:task",
      expected: { filter: { type: ["bug", "feature", "task"] }, invalidTokens: [] },
    },

    // --- positive + negative on the same field coexist ---
    {
      name: "positive and negative same field",
      input: "type:bug -type:task",
      expected: { filter: { type: ["bug"], excludeType: ["task"] }, invalidTokens: [] },
    },

    // --- case-insensitive field + value, normalized to lowercase ---
    { name: "uppercase field + value", input: "TYPE:BUG", expected: { filter: { type: ["bug"] }, invalidTokens: [] } },
    { name: "mixed-case field + value", input: "Priority:High", expected: { filter: { priority: ["high"] }, invalidTokens: [] } },
    { name: "mixed-case negation", input: "-STATUS:Completed", expected: { filter: { excludeStatus: ["completed"] }, invalidTokens: [] } },

    // --- tags are pattern-checked, NOT enum-validated ---
    {
      name: "a never-seen tag is still valid (any tag string allowed)",
      input: "tags:never-seen-before",
      expected: { filter: { tags: ["never-seen-before"] }, invalidTokens: [] },
    },
    {
      name: "tag failing the lowercase-hyphen pattern is invalid",
      input: "tags:1bad",
      expected: { filter: {}, invalidTokens: ["tags:1bad"] },
    },
    {
      name: "tag with an underscore fails the pattern",
      input: "tags:has_underscore",
      expected: { filter: {}, invalidTokens: ["tags:has_underscore"] },
    },

    // --- invalid enum value: excluded from filter, preserved in sidecar ---
    { name: "invalid status value preserved", input: "status:banana", expected: { filter: {}, invalidTokens: ["status:banana"] } },
    {
      name: "invalid value alongside a valid token",
      input: "type:bug status:banana",
      expected: { filter: { type: ["bug"] }, invalidTokens: ["status:banana"] },
    },
    {
      name: "mixed valid + invalid inside one comma token (per-value split)",
      input: "type:bug,banana",
      expected: { filter: { type: ["bug"] }, invalidTokens: ["type:banana"] },
    },
    {
      name: "invalid negated value preserved with its minus",
      input: "-priority:pink",
      expected: { filter: {}, invalidTokens: ["-priority:pink"] },
    },
    {
      name: "invalid value is lowercased in the sidecar",
      input: "STATUS:Banana",
      expected: { filter: {}, invalidTokens: ["status:banana"] },
    },
    {
      name: "duplicate invalid tokens dedupe in the sidecar",
      input: "status:banana status:banana",
      expected: { filter: {}, invalidTokens: ["status:banana"] },
    },

    // --- unknown field → free-text search (Bleve handles it incidentally) ---
    { name: "unknown field falls through to search", input: "title:foo", expected: { filter: { search: "title:foo" }, invalidTokens: [] } },
    { name: "Bleve body field falls through", input: "body:bar", expected: { filter: { search: "body:bar" }, invalidTokens: [] } },
    {
      name: "negated unknown field keeps its minus in search",
      input: "-title:foo",
      expected: { filter: { search: "-title:foo" }, invalidTokens: [] },
    },

    // --- bare words → search ---
    { name: "bare words", input: "login page", expected: { filter: { search: "login page" }, invalidTokens: [] } },
    { name: "collapses runs of whitespace in search", input: "login    flow", expected: { filter: { search: "login flow" }, invalidTokens: [] } },

    // --- field: with empty value is not a token (kept as free text) ---
    { name: "empty value is free text", input: "status:", expected: { filter: { search: "status:" }, invalidTokens: [] } },
    { name: "empty typed field value is free text", input: "type:", expected: { filter: { search: "type:" }, invalidTokens: [] } },

    // --- trailing/empty comma segments are dropped ---
    { name: "trailing comma segment dropped", input: "type:bug,", expected: { filter: { type: ["bug"] }, invalidTokens: [] } },

    // --- a known-field token whose values are ALL empty (only commas) is NOT lost:
    // it is preserved verbatim as free text (reachable by editing `type:bug,` down
    // to `type:,`). Absent this, the token contributes nothing and silently vanishes.
    { name: "field token with only empty comma values is preserved as free text", input: "type:,", expected: { filter: { search: "type:," }, invalidTokens: [] } },
    { name: "negated field token with only empty comma values is preserved", input: "-type:,", expected: { filter: { search: "-type:," }, invalidTokens: [] } },
    { name: "field token with multiple empty comma values is preserved", input: "status:,,", expected: { filter: { search: "status:,," }, invalidTokens: [] } },

    // --- mixed everything ---
    {
      name: "mixed fields + exclude + search",
      input: "type:bug login -tags:wip",
      expected: { filter: { type: ["bug"], excludeTags: ["wip"], search: "login" }, invalidTokens: [] },
    },

    // --- empty / whitespace ---
    { name: "empty string", input: "", expected: { filter: {}, invalidTokens: [] } },
    { name: "whitespace only", input: "   ", expected: { filter: {}, invalidTokens: [] } },
  ];

  for (const { name, input, expected } of cases) {
    it(name, () => {
      expect(parseQuery(input)).toEqual(expected);
    });
  }

  it("omits absent metadata keys entirely (field-absent vs empty distinguishable)", () => {
    const { filter } = parseQuery("login");
    expect(filter).not.toHaveProperty("type");
    expect(filter).not.toHaveProperty("status");
    expect(filter).not.toHaveProperty("excludeType");
  });

  it("omits the search key when only metadata tokens are present", () => {
    expect(parseQuery("type:bug")).not.toHaveProperty("filter.search");
    expect(parseQuery("type:bug").filter).not.toHaveProperty("search");
  });
});

describe("parseQuery — relationship + existence tokens (phase 5)", () => {
  const cases: { name: string; input: string; expected: ParsedQuery }[] = [
    // --- all five relationship-id tokens → the correct scalar field ---
    { name: "blocking:<id>", input: "blocking:tnib-1", expected: { filter: { blockingId: "tnib-1" }, invalidTokens: [] } },
    { name: "blocked-by:<id> (hyphenated)", input: "blocked-by:tnib-1", expected: { filter: { blockedById: "tnib-1" }, invalidTokens: [] } },
    { name: "parent:<id>", input: "parent:tnib-1", expected: { filter: { parentId: "tnib-1" }, invalidTokens: [] } },
    { name: "mentions:<id>", input: "mentions:tnib-1", expected: { filter: { mentionsId: "tnib-1" }, invalidTokens: [] } },
    { name: "mentioned-by:<id> (hyphenated)", input: "mentioned-by:tnib-1", expected: { filter: { mentionedById: "tnib-1" }, invalidTokens: [] } },

    // --- all seven existence tokens → the correct tri-state value ---
    // has:/no: for one dimension write the SAME field with opposite values;
    // they are two spellings of one filter, not two filters.
    { name: "has:parent", input: "has:parent", expected: { filter: { hasParent: true }, invalidTokens: [] } },
    { name: "no:parent", input: "no:parent", expected: { filter: { hasParent: false }, invalidTokens: [] } },
    { name: "has:blocking", input: "has:blocking", expected: { filter: { hasBlocking: true }, invalidTokens: [] } },
    { name: "no:blocking", input: "no:blocking", expected: { filter: { hasBlocking: false }, invalidTokens: [] } },
    { name: "has:blocked-by (hyphenated)", input: "has:blocked-by", expected: { filter: { hasBlockedBy: true }, invalidTokens: [] } },
    { name: "no:blocked-by (hyphenated)", input: "no:blocked-by", expected: { filter: { hasBlockedBy: false }, invalidTokens: [] } },
    { name: "is:blocked", input: "is:blocked", expected: { filter: { isBlocked: true }, invalidTokens: [] } },

    // --- case-insensitive field-names + lowercased id values ---
    { name: "uppercase rel field + value lowercased", input: "PARENT:TNIB-ABC", expected: { filter: { parentId: "tnib-abc" }, invalidTokens: [] } },
    { name: "uppercase existence token", input: "HAS:PARENT", expected: { filter: { hasParent: true }, invalidTokens: [] } },

    // --- scalar last-wins: a repeated same-kind token OVERWRITES ---
    { name: "repeated parent overwrites (last wins)", input: "parent:a parent:b", expected: { filter: { parentId: "b" }, invalidTokens: [] } },
    { name: "repeated blocking overwrites (last wins)", input: "blocking:x blocking:y", expected: { filter: { blockingId: "y" }, invalidTokens: [] } },

    // --- invalid existence subjects → free text (no such field invented) ---
    { name: "has:mentions is not a field → free text", input: "has:mentions", expected: { filter: { search: "has:mentions" }, invalidTokens: [] } },
    { name: "no:mentions is not a field → free text", input: "no:mentions", expected: { filter: { search: "no:mentions" }, invalidTokens: [] } },
    { name: "has:mentioned-by is not a field → free text", input: "has:mentioned-by", expected: { filter: { search: "has:mentioned-by" }, invalidTokens: [] } },
    { name: "is:<other> is not a field → free text", input: "is:foo", expected: { filter: { search: "is:foo" }, invalidTokens: [] } },

    // --- negation is metadata-only: a negated rel/existence token is PARKED as
    // invalid, not routed to free text. Free text reaches the server's Bleve query
    // string, where `-parent:x` is valid syntax over a field Bleve does not index —
    // the MUST-NOT clause excludes nothing and the query degrades to match-all, so
    // the user gets the whole dataset back with no signal the token did nothing.
    // Parking flags it in the box and round-trips it verbatim instead.
    { name: "-blocking:x → invalid (not a negation feature)", input: "-blocking:x", expected: { filter: {}, invalidTokens: ["-blocking:x"] } },
    { name: "-has:parent → invalid", input: "-has:parent", expected: { filter: {}, invalidTokens: ["-has:parent"] } },
    { name: "-parent:x → invalid", input: "-parent:x", expected: { filter: {}, invalidTokens: ["-parent:x"] } },
    { name: "-blocked-by:x → invalid (hyphenated field-name)", input: "-blocked-by:x", expected: { filter: {}, invalidTokens: ["-blocked-by:x"] } },
    { name: "-mentions:x → invalid", input: "-mentions:x", expected: { filter: {}, invalidTokens: ["-mentions:x"] } },
    { name: "-mentioned-by:x → invalid", input: "-mentioned-by:x", expected: { filter: {}, invalidTokens: ["-mentioned-by:x"] } },
    { name: "-no:parent → invalid", input: "-no:parent", expected: { filter: {}, invalidTokens: ["-no:parent"] } },
    { name: "-is:blocked → invalid", input: "-is:blocked", expected: { filter: {}, invalidTokens: ["-is:blocked"] } },
    { name: "negated rel token is lowercased when parked", input: "-PARENT:TNIB-ABC", expected: { filter: {}, invalidTokens: ["-parent:tnib-abc"] } },
    // A negated token that is NOT a rel/existence spelling still falls to free
    // text — parking is scoped to tokens the grammar would otherwise recognize.
    { name: "-has:mentions → free text (no such existence token)", input: "-has:mentions", expected: { filter: { search: "-has:mentions" }, invalidTokens: [] } },
    { name: "-parent: (empty value) → free text", input: "-parent:", expected: { filter: { search: "-parent:" }, invalidTokens: [] } },
    // The valid tokens around a parked one still apply, so the results narrow —
    // they just do not carry the exclusion the user asked for.
    {
      name: "a parked negation leaves the rest of the query working",
      input: "type:bug -ancestor:tnib-1 login",
      expected: { filter: { type: ["bug"], search: "login" }, invalidTokens: ["-ancestor:tnib-1"] },
    },

    // --- empty value is not a rel token (kept as free text, like metadata) ---
    { name: "blocking: with empty value is free text", input: "blocking:", expected: { filter: { search: "blocking:" }, invalidTokens: [] } },
    { name: "parent: with empty value is free text", input: "parent:", expected: { filter: { search: "parent:" }, invalidTokens: [] } },

    // --- rel-id value is scalar: NOT comma-split ---
    { name: "comma in a rel-id value is taken whole (scalar)", input: "parent:a,b", expected: { filter: { parentId: "a,b" }, invalidTokens: [] } },

    // --- interaction with metadata + free text in one query ---
    {
      name: "metadata + existence + rel-id + free text together",
      input: "type:bug has:parent blocking:tnib-1 login",
      expected: { filter: { type: ["bug"], hasParent: true, blockingId: "tnib-1", search: "login" }, invalidTokens: [] },
    },
    {
      name: "rel-id and its existence sibling coexist",
      input: "parent:tnib-1 has:parent",
      expected: { filter: { parentId: "tnib-1", hasParent: true }, invalidTokens: [] },
    },
  ];

  for (const { name, input, expected } of cases) {
    it(name, () => {
      expect(parseQuery(input)).toEqual(expected);
    });
  }

  it("does not touch the invalid-token sidecar for rel/existence tokens", () => {
    // Rel-id values are pattern-only (any non-empty value); they never populate
    // invalidTokens the way a bad enum value does.
    expect(parseQuery("blocking:whatever").invalidTokens).toEqual([]);
  });
});

// The assignment axis, whose two tokens land on two differently-shaped fields:
// `milestone` is a scalar id read as DIRECT assignment, `noMilestone` a tri-state
// read over DERIVED membership. Only the true half of the tri-state is typeable,
// which is what keeps the grammar's `no:` prefix meaning false everywhere.
describe("parseQuery — assignment axis tokens", () => {
  const cases: { name: string; input: string; expected: ParsedQuery }[] = [
    { name: "milestone:<id>", input: "milestone:tnib-1", expected: { filter: { milestone: "tnib-1" }, invalidTokens: [] } },
    { name: "milestone field-name and id are lowercased", input: "MILESTONE:TNIB-ABC", expected: { filter: { milestone: "tnib-abc" }, invalidTokens: [] } },
    { name: "repeated milestone overwrites (last wins)", input: "milestone:a milestone:b", expected: { filter: { milestone: "b" }, invalidTokens: [] } },
    {
      name: "is:backlog writes noMilestone TRUE — the backlog is the nibs no plan covers",
      input: "is:backlog",
      expected: { filter: { noMilestone: true }, invalidTokens: [] },
    },
    { name: "is:backlog is case-insensitive", input: "IS:BACKLOG", expected: { filter: { noMilestone: true }, invalidTokens: [] } },
    // `noMilestone` has no `has:`/`no:` pair, so those spellings stay unrecognized
    // and reach free text like `has:mentions` does.
    { name: "has:milestone is not a token → free text", input: "has:milestone", expected: { filter: { search: "has:milestone" }, invalidTokens: [] } },
    { name: "no:milestone is not a token → free text", input: "no:milestone", expected: { filter: { search: "no:milestone" }, invalidTokens: [] } },
    // Empty value: the id grammar requires a non-empty one, so the whole token is
    // free text rather than an assignment filter set to "" (which the server
    // refuses as malformed).
    { name: "milestone: with empty value is free text", input: "milestone:", expected: { filter: { search: "milestone:" }, invalidTokens: [] } },
    { name: "-milestone: (negated, empty value) → free text", input: "-milestone:", expected: { filter: { search: "-milestone:" }, invalidTokens: [] } },
    // Negation is metadata-only here as everywhere else in the rel grammar.
    { name: "-milestone:x → invalid", input: "-milestone:x", expected: { filter: {}, invalidTokens: ["-milestone:x"] } },
    { name: "-is:backlog → invalid", input: "-is:backlog", expected: { filter: {}, invalidTokens: ["-is:backlog"] } },
    {
      name: "both axis tokens coexist with metadata and free text",
      input: "type:epic milestone:tnib-1 is:backlog login",
      expected: {
        filter: { type: ["epic"], milestone: "tnib-1", noMilestone: true, search: "login" },
        invalidTokens: [],
      },
    },
  ];

  for (const { name, input, expected } of cases) {
    it(name, () => {
      expect(parseQuery(input)).toEqual(expected);
    });
  }
});

describe("parseQuery — hierarchy relationship tokens", () => {
  // Directions mirror the server `NibFilter` fields exactly: `ancestorId: X` keeps
  // nibs with X in their parent chain (X's descendants), `descendantId: X` keeps
  // nibs with X in their subtree (X's ancestor chain), `siblingId: X` keeps nibs
  // sharing X's parent. The token name states the relationship the MATCHED nib
  // holds toward the supplied id, same as `parent:`/`blocking:`/`mentions:`.
  const cases: { name: string; input: string; expected: ParsedQuery }[] = [
    // --- each token routes to its own scalar field, NOT to free-text search ---
    { name: "ancestor:<id>", input: "ancestor:tnib-1", expected: { filter: { ancestorId: "tnib-1" }, invalidTokens: [] } },
    { name: "descendant:<id>", input: "descendant:tnib-1", expected: { filter: { descendantId: "tnib-1" }, invalidTokens: [] } },
    { name: "sibling:<id>", input: "sibling:tnib-1", expected: { filter: { siblingId: "tnib-1" }, invalidTokens: [] } },

    // --- case-insensitive field-name, lowercased id value ---
    { name: "uppercase hierarchy field + value lowercased", input: "ANCESTOR:TNIB-ABC", expected: { filter: { ancestorId: "tnib-abc" }, invalidTokens: [] } },

    // --- scalar last-wins on repeat, like every other rel-id token ---
    { name: "repeated sibling overwrites (last wins)", input: "sibling:a sibling:b", expected: { filter: { siblingId: "b" }, invalidTokens: [] } },

    // --- NO existence spellings: the server has no has/no predicate for these, so
    // the tokens must fall through to free text rather than invent a filter field.
    { name: "has:ancestor is not a field → free text", input: "has:ancestor", expected: { filter: { search: "has:ancestor" }, invalidTokens: [] } },
    { name: "no:ancestor is not a field → free text", input: "no:ancestor", expected: { filter: { search: "no:ancestor" }, invalidTokens: [] } },
    { name: "has:descendant is not a field → free text", input: "has:descendant", expected: { filter: { search: "has:descendant" }, invalidTokens: [] } },
    { name: "has:sibling is not a field → free text", input: "has:sibling", expected: { filter: { search: "has:sibling" }, invalidTokens: [] } },
    { name: "no:sibling is not a field → free text", input: "no:sibling", expected: { filter: { search: "no:sibling" }, invalidTokens: [] } },

    // --- negation is metadata-only: parked as invalid rather than sent to Bleve,
    // where `-ancestor:x` silently matches everything (see the phase-5 block above)
    { name: "-ancestor:x → invalid", input: "-ancestor:x", expected: { filter: {}, invalidTokens: ["-ancestor:x"] } },
    { name: "-descendant:x → invalid", input: "-descendant:x", expected: { filter: {}, invalidTokens: ["-descendant:x"] } },
    { name: "-sibling:x → invalid", input: "-sibling:x", expected: { filter: {}, invalidTokens: ["-sibling:x"] } },

    // --- empty value is not a token ---
    { name: "descendant: with empty value is free text", input: "descendant:", expected: { filter: { search: "descendant:" }, invalidTokens: [] } },

    // --- all three coexist with metadata, the other rel tokens, and free text ---
    {
      name: "hierarchy tokens combine with metadata, parent, and free text",
      input: "type:epic parent:tnib-0 ancestor:tnib-1 descendant:tnib-2 sibling:tnib-3 login",
      expected: {
        filter: {
          type: ["epic"],
          parentId: "tnib-0",
          ancestorId: "tnib-1",
          descendantId: "tnib-2",
          siblingId: "tnib-3",
          search: "login",
        },
        invalidTokens: [],
      },
    },
  ];

  for (const { name, input, expected } of cases) {
    it(name, () => {
      expect(parseQuery(input)).toEqual(expected);
    });
  }
});

describe("parseQuery — status group shortcuts", () => {
  // `status:open` / `status:closed` are shorthands the CLI already accepts
  // (`nibs list -s open`). They expand to their member statuses at parse time,
  // so NibFilter never carries a group name — only concrete statuses reach the
  // backend.
  const cases: { name: string; input: string; expected: ParsedQuery }[] = [
    {
      name: "status:open expands to the open statuses",
      input: "status:open",
      expected: { filter: { status: ["draft", "todo", "in-progress"] }, invalidTokens: [] },
    },
    {
      name: "status:closed expands to the closed statuses",
      input: "status:closed",
      expected: { filter: { status: ["deferred", "completed", "scrapped"] }, invalidTokens: [] },
    },
    {
      name: "group names are case-insensitive like every other value",
      input: "STATUS:Open",
      expected: { filter: { status: ["draft", "todo", "in-progress"] }, invalidTokens: [] },
    },
    {
      name: "a negated group expands into the exclude list",
      input: "-status:closed",
      expected: { filter: { excludeStatus: ["deferred", "completed", "scrapped"] }, invalidTokens: [] },
    },
    {
      name: "a group unions with concrete values in the same token, deduped",
      input: "status:open,todo,completed",
      expected: { filter: { status: ["draft", "todo", "in-progress", "completed"] }, invalidTokens: [] },
    },
    {
      name: "the two groups partition the statuses, so together they are all of them",
      input: "status:open,closed",
      expected: {
        filter: { status: ["draft", "todo", "in-progress", "deferred", "completed", "scrapped"] },
        invalidTokens: [],
      },
    },
    {
      name: "a group unions across repeated tokens",
      input: "status:open status:completed",
      expected: { filter: { status: ["draft", "todo", "in-progress", "completed"] }, invalidTokens: [] },
    },
    {
      name: "groups are a status-only vocabulary — `open` is not a type",
      input: "type:open",
      expected: { filter: {}, invalidTokens: ["type:open"] },
    },
    {
      name: "there is no `parked` group — it would only spell `status:deferred`",
      input: "status:parked",
      expected: { filter: {}, invalidTokens: ["status:parked"] },
    },
  ];

  for (const { name, input, expected } of cases) {
    it(name, () => {
      expect(parseQuery(input)).toEqual(expected);
    });
  }

  it("does not resolve an Object prototype key as a group name", () => {
    // Group lookup must not walk the prototype chain: `constructor` is not a
    // status, so it belongs in the invalid sidecar like any other bad value.
    expect(parseQuery("status:constructor")).toEqual({
      filter: {},
      invalidTokens: ["status:constructor"],
    });
  });
});

// --- The area token ------------------------------------------------------------

describe("parseQuery — the area token", () => {
  const declared = [
    { path: "web", name: "web", description: "", color: "", depth: 0 },
    { path: "web/dashboard", name: "dashboard", description: "", color: "", depth: 1 },
    { path: "webhooks", name: "webhooks", description: "", color: "", depth: 0 },
  ];
  const READY = createAreaVocabulary(declared);

  // The routing table, over both axes at once: what the token looks like × what
  // the vocabulary answers. `filter.area` is written on "declared" AND "unknown";
  // only an answered "undeclared" parks.
  const cases: {
    name: string;
    input: string;
    areas: AreaVocabulary | undefined;
    expected: ParsedQuery;
  }[] = [
    {
      name: "a declared path reaches the filter",
      input: "area:web",
      areas: READY,
      expected: { filter: { area: "web" }, invalidTokens: [] },
    },
    {
      name: "a declared nested path reaches the filter unwidened",
      input: "area:web/dashboard",
      areas: READY,
      expected: { filter: { area: "web/dashboard" }, invalidTokens: [] },
    },
    {
      name: "an undeclared path is parked, like status:banana",
      input: "area:retired",
      areas: READY,
      expected: { filter: {}, invalidTokens: ["area:retired"] },
    },
    {
      name: "a project declaring no areas parks every path",
      input: "area:web",
      areas: EMPTY_AREAS,
      expected: { filter: {}, invalidTokens: ["area:web"] },
    },
    {
      name: "a pre-load vocabulary keeps the value rather than judging it",
      input: "area:web",
      areas: LOADING_AREAS,
      expected: { filter: { area: "web" }, invalidTokens: [] },
    },
    {
      name: "a failed config query keeps it too",
      input: "area:retired",
      areas: UNAVAILABLE_AREAS,
      expected: { filter: { area: "retired" }, invalidTokens: [] },
    },
    {
      name: "no vocabulary at all keeps it — this is Preferences.setQuery on load",
      input: "area:retired",
      areas: undefined,
      expected: { filter: { area: "retired" }, invalidTokens: [] },
    },
    {
      name: "the value keeps its case, because the server's lookup is case-sensitive",
      input: "area:Web",
      areas: undefined,
      expected: { filter: { area: "Web" }, invalidTokens: [] },
    },
    {
      name: "and a mis-cased path is refused once a vocabulary can say so",
      input: "area:Web",
      areas: READY,
      expected: { filter: {}, invalidTokens: ["area:Web"] },
    },
    {
      name: "the field name is normalized",
      input: "AREA:web",
      areas: READY,
      expected: { filter: { area: "web" }, invalidTokens: [] },
    },
    {
      name: "negation is parked whole — there is no excludeArea",
      input: "-area:web",
      areas: READY,
      expected: { filter: {}, invalidTokens: ["-area:web"] },
    },
    {
      name: "a negated declared path is parked for its negation, not its value",
      input: "-area:web",
      areas: undefined,
      expected: { filter: {}, invalidTokens: ["-area:web"] },
    },
    {
      name: "an empty value is free text, as `type:` is",
      input: "area:",
      areas: READY,
      expected: { filter: { search: "area:" }, invalidTokens: [] },
    },
    {
      name: "a bare `area` is a search word",
      input: "area",
      areas: READY,
      expected: { filter: { search: "area" }, invalidTokens: [] },
    },
    {
      name: "an unknown field that starts with it stays free text",
      input: "areas:web",
      areas: READY,
      expected: { filter: { search: "areas:web" }, invalidTokens: [] },
    },
    {
      name: "the last of two area tokens wins, like a relationship id",
      input: "area:web area:webhooks",
      areas: READY,
      expected: { filter: { area: "webhooks" }, invalidTokens: [] },
    },
    {
      name: "a declared and an undeclared path coexist: one filters, one is flagged",
      input: "area:web area:retired",
      areas: READY,
      expected: { filter: { area: "web" }, invalidTokens: ["area:retired"] },
    },
    {
      name: "it composes with every other token kind",
      input: "type:bug parent:tnib-1 has:parent area:web login",
      areas: READY,
      expected: {
        filter: {
          type: ["bug"],
          parentId: "tnib-1",
          hasParent: true,
          area: "web",
          search: "login",
        },
        invalidTokens: [],
      },
    },
  ];

  for (const { name, input, areas, expected } of cases) {
    it(name, () => {
      expect(parseQuery(input, areas)).toEqual(expected);
    });
  }

  it("takes the value whole — no comma split, unlike a metadata facet", () => {
    // `type:a,b` is two values; an area path is a scalar, and a comma is a legal
    // character in an area NAME (config.validateAreaNodes forbids only `/`).
    expect(parseQuery("area:a,b").filter.area).toBe("a,b");
  });

  it("does not widen a declared ancestor to its subtree", () => {
    // Downward closure is filterByAreaWithin's, server-side. The token carries the
    // one path the user typed, and `webhooks` is not in that subtree at all.
    expect(parseQuery("area:web", READY).filter.area).toBe("web");
    expect(READY.subtreeOf("web").map((n) => n.path)).toEqual(["web", "web/dashboard"]);
  });
});
