import { describe, it, expect } from "vitest";
import { parseQuery } from "./index";
import type { ParsedQuery } from "./index";

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

    // --- negation is metadata-only: negated rel/existence → free text ---
    { name: "-blocking:x → free text (not a negation feature)", input: "-blocking:x", expected: { filter: { search: "-blocking:x" }, invalidTokens: [] } },
    { name: "-has:parent → free text", input: "-has:parent", expected: { filter: { search: "-has:parent" }, invalidTokens: [] } },
    { name: "-parent:x → free text", input: "-parent:x", expected: { filter: { search: "-parent:x" }, invalidTokens: [] } },

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
