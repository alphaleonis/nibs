import { describe, it, expect } from "vitest";
import { tokenizeSpans } from "./index";
import type { Span } from "./index";

describe("tokenizeSpans — per-token classification", () => {
  const cases: { name: string; input: string; expected: Span[] }[] = [
    // --- empty / whitespace ---
    { name: "empty string", input: "", expected: [] },
    { name: "whitespace only", input: "   ", expected: [{ start: 0, end: 3, kind: "whitespace" }] },

    // --- a single positive field token: field / operator / value ---
    {
      name: "type:bug splits into field, colon, value",
      input: "type:bug",
      expected: [
        { start: 0, end: 4, kind: "field" },
        { start: 4, end: 5, kind: "operator" },
        { start: 5, end: 8, kind: "value" },
      ],
    },

    // --- negation is part of the field-name span ---
    {
      name: "-type:bug keeps the minus inside the field span",
      input: "-type:bug",
      expected: [
        { start: 0, end: 5, kind: "field" },
        { start: 5, end: 6, kind: "operator" },
        { start: 6, end: 9, kind: "value" },
      ],
    },

    // --- comma multi-value: each value classified individually, commas are operators ---
    {
      name: "type:bug,feature classifies each value, comma is operator",
      input: "type:bug,feature",
      expected: [
        { start: 0, end: 4, kind: "field" },
        { start: 4, end: 5, kind: "operator" },
        { start: 5, end: 8, kind: "value" },
        { start: 8, end: 9, kind: "operator" },
        { start: 9, end: 16, kind: "value" },
      ],
    },

    // --- mixed valid + invalid inside one comma token (per-value) ---
    {
      name: "type:bug,banana marks only the bad value invalid",
      input: "type:bug,banana",
      expected: [
        { start: 0, end: 4, kind: "field" },
        { start: 4, end: 5, kind: "operator" },
        { start: 5, end: 8, kind: "value" },
        { start: 8, end: 9, kind: "operator" },
        { start: 9, end: 15, kind: "invalid" },
      ],
    },

    // --- known field + invalid value → the value span is `invalid` ---
    {
      name: "status:banana marks the value invalid",
      input: "status:banana",
      expected: [
        { start: 0, end: 6, kind: "field" },
        { start: 6, end: 7, kind: "operator" },
        { start: 7, end: 13, kind: "invalid" },
      ],
    },

    // --- unknown field → the whole token is free text ---
    { name: "title:foo (unknown field) is free text", input: "title:foo", expected: [{ start: 0, end: 9, kind: "freetext" }] },

    // --- relationship tokens are not metadata fields, so the highlighter treats
    // the whole token as free text and never marks it `invalid` — the red wavy
    // underline is reserved for a KNOWN field's rejected value. The three hierarchy
    // tokens share that treatment with parent:/blocking:/mentions:.
    //
    // CHARACTERIZATION, NOT A GUARD: these four cases record what the highlighter
    // does today, not what it should do. A recognized relationship token rendering
    // identically to unrecognized noise is the tracked display gap `nibs-kjug`;
    // when that lands, these expectations change rather than break.
    { name: "ancestor:tnib-1 is free text, never invalid", input: "ancestor:tnib-1", expected: [{ start: 0, end: 15, kind: "freetext" }] },
    { name: "descendant:tnib-1 is free text, never invalid", input: "descendant:tnib-1", expected: [{ start: 0, end: 17, kind: "freetext" }] },
    { name: "sibling:tnib-1 is free text, never invalid", input: "sibling:tnib-1", expected: [{ start: 0, end: 14, kind: "freetext" }] },
    { name: "parent:tnib-1 is free text too (same rel-token treatment)", input: "parent:tnib-1", expected: [{ start: 0, end: 13, kind: "freetext" }] },

    // --- bare words → free text, separated by whitespace gaps ---
    {
      name: "bare words are free text with a whitespace gap",
      input: "login page",
      expected: [
        { start: 0, end: 5, kind: "freetext" },
        { start: 5, end: 6, kind: "whitespace" },
        { start: 6, end: 10, kind: "freetext" },
      ],
    },

    // --- field: with empty value is NOT a token (mirrors parseQuery → free text) ---
    { name: "type: (empty value) is free text", input: "type:", expected: [{ start: 0, end: 5, kind: "freetext" }] },

    // --- a known-field token whose values are all empty commas is free text ---
    { name: "type:, (only commas) is free text", input: "type:,", expected: [{ start: 0, end: 6, kind: "freetext" }] },

    // --- tags are pattern-checked, not enum-validated ---
    {
      name: "tags:never-seen is a valid value (pattern check)",
      input: "tags:never-seen",
      expected: [
        { start: 0, end: 4, kind: "field" },
        { start: 4, end: 5, kind: "operator" },
        { start: 5, end: 15, kind: "value" },
      ],
    },
    {
      name: "tags:1bad fails the tag pattern → invalid",
      input: "tags:1bad",
      expected: [
        { start: 0, end: 4, kind: "field" },
        { start: 4, end: 5, kind: "operator" },
        { start: 5, end: 9, kind: "invalid" },
      ],
    },

    // --- case-insensitive field/value: still classified as valid value ---
    {
      name: "TYPE:BUG (uppercase) still classifies as field + value",
      input: "TYPE:BUG",
      expected: [
        { start: 0, end: 4, kind: "field" },
        { start: 4, end: 5, kind: "operator" },
        { start: 5, end: 8, kind: "value" },
      ],
    },

    // --- a full multi-token query (the screenshot query) ---
    {
      name: "type:bug status:banana login (valid + invalid + free text)",
      input: "type:bug status:banana login",
      expected: [
        { start: 0, end: 4, kind: "field" },
        { start: 4, end: 5, kind: "operator" },
        { start: 5, end: 8, kind: "value" },
        { start: 8, end: 9, kind: "whitespace" },
        { start: 9, end: 15, kind: "field" },
        { start: 15, end: 16, kind: "operator" },
        { start: 16, end: 22, kind: "invalid" },
        { start: 22, end: 23, kind: "whitespace" },
        { start: 23, end: 28, kind: "freetext" },
      ],
    },

    // --- status group shortcuts are legal values, not errors ---
    {
      name: "status:open colors the group name as a value, not invalid",
      input: "status:open",
      expected: [
        { start: 0, end: 6, kind: "field" },
        { start: 6, end: 7, kind: "operator" },
        { start: 7, end: 11, kind: "value" },
      ],
    },
    {
      name: "-status:closed keeps the negated group name a value",
      input: "-status:closed",
      expected: [
        { start: 0, end: 7, kind: "field" },
        { start: 7, end: 8, kind: "operator" },
        { start: 8, end: 14, kind: "value" },
      ],
    },
    {
      name: "status:open,banana marks only the bad value invalid",
      input: "status:open,banana",
      expected: [
        { start: 0, end: 6, kind: "field" },
        { start: 6, end: 7, kind: "operator" },
        { start: 7, end: 11, kind: "value" },
        { start: 11, end: 12, kind: "operator" },
        { start: 12, end: 18, kind: "invalid" },
      ],
    },
    {
      name: "type:open is invalid — groups are a status-only vocabulary",
      input: "type:open",
      expected: [
        { start: 0, end: 4, kind: "field" },
        { start: 4, end: 5, kind: "operator" },
        { start: 5, end: 9, kind: "invalid" },
      ],
    },
  ];

  for (const { name, input, expected } of cases) {
    it(name, () => {
      expect(tokenizeSpans(input)).toEqual(expected);
    });
  }
});

describe("tokenizeSpans — full contiguous coverage", () => {
  // The backdrop renders every character in its exact position, so the spans MUST
  // tile the whole string with no gaps and no overlaps. This guard bites if a
  // classifier ever drops a character (e.g. a stray comma or empty segment).
  const inputs = [
    "",
    "   ",
    "type:bug",
    "-type:bug",
    "type:bug,feature,task",
    "type:bug,banana",
    "status:banana",
    "status:open",
    "-status:closed",
    "status:open,banana",
    "title:foo",
    "login page",
    "type:",
    "type:,",
    "status:,,",
    "  leading and   inner   spaces  ",
    "-tags:wip,later status:todo login flow",
    "type:bug status:banana login",
    "type:epic ancestor:tnib-1 descendant:tnib-2 sibling:tnib-3 login",
    "TYPE:BUG Priority:High -STATUS:Completed",
    "tags:frontend,1bad,backend",
  ];

  for (const input of inputs) {
    it(`tiles ${JSON.stringify(input)} contiguously and reconstructs it`, () => {
      const spans = tokenizeSpans(input);
      if (input.length === 0) {
        expect(spans).toEqual([]);
        return;
      }
      // Starts at 0, ends at length, each span begins where the previous ended.
      expect(spans[0].start).toBe(0);
      expect(spans[spans.length - 1].end).toBe(input.length);
      for (let i = 1; i < spans.length; i++) {
        expect(spans[i].start).toBe(spans[i - 1].end);
      }
      // Each span is non-empty and the concatenation reproduces the input exactly.
      for (const s of spans) expect(s.end).toBeGreaterThan(s.start);
      expect(spans.map((s) => input.slice(s.start, s.end)).join("")).toBe(input);
    });
  }
});
