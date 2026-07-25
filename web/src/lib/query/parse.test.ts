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
