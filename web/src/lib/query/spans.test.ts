import { describe, it, expect } from "vitest";
import { parseQuery, tokenizeSpans } from "./index";
import type { Span } from "./index";
import { REL_TOKEN_ORDER } from "./relations";
import {
  createAreaVocabulary,
  EMPTY_AREAS,
  LOADING_AREAS,
  UNAVAILABLE_AREAS,
} from "../areas";
import type { AreaVocabulary } from "../areas";

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

    // --- relationship-id tokens: recognized, so they get the same field / colon /
    // value shape as a metadata token. The id value is colored `value` (not a
    // separate "unchecked" kind) because non-empty is the ONLY condition the
    // grammar imposes on it, and the token already passed that by being recognized.
    {
      name: "parent:tnib-1 splits into field, colon, value",
      input: "parent:tnib-1",
      expected: [
        { start: 0, end: 6, kind: "field" },
        { start: 6, end: 7, kind: "operator" },
        { start: 7, end: 13, kind: "value" },
      ],
    },
    {
      name: "ancestor:tnib-1 splits into field, colon, value",
      input: "ancestor:tnib-1",
      expected: [
        { start: 0, end: 8, kind: "field" },
        { start: 8, end: 9, kind: "operator" },
        { start: 9, end: 15, kind: "value" },
      ],
    },
    {
      name: "descendant:tnib-1 splits into field, colon, value",
      input: "descendant:tnib-1",
      expected: [
        { start: 0, end: 10, kind: "field" },
        { start: 10, end: 11, kind: "operator" },
        { start: 11, end: 17, kind: "value" },
      ],
    },
    {
      name: "sibling:tnib-1 splits into field, colon, value",
      input: "sibling:tnib-1",
      expected: [
        { start: 0, end: 7, kind: "field" },
        { start: 7, end: 8, kind: "operator" },
        { start: 8, end: 14, kind: "value" },
      ],
    },
    {
      name: "blocking:tnib-1 splits into field, colon, value",
      input: "blocking:tnib-1",
      expected: [
        { start: 0, end: 8, kind: "field" },
        { start: 8, end: 9, kind: "operator" },
        { start: 9, end: 15, kind: "value" },
      ],
    },
    // A hyphenated rel field-name is one field span — the hyphen is part of the
    // name, not punctuation. `FIELD_TOKEN`'s `[A-Za-z]+` group cannot match these,
    // which is exactly why recognition runs through `recognizeRelationship`.
    {
      name: "blocked-by:tnib-1 keeps the hyphenated name in one field span",
      input: "blocked-by:tnib-1",
      expected: [
        { start: 0, end: 10, kind: "field" },
        { start: 10, end: 11, kind: "operator" },
        { start: 11, end: 17, kind: "value" },
      ],
    },
    {
      name: "mentions:tnib-1 splits into field, colon, value",
      input: "mentions:tnib-1",
      expected: [
        { start: 0, end: 8, kind: "field" },
        { start: 8, end: 9, kind: "operator" },
        { start: 9, end: 15, kind: "value" },
      ],
    },
    {
      name: "mentioned-by:tnib-1 keeps the hyphenated name in one field span",
      input: "mentioned-by:tnib-1",
      expected: [
        { start: 0, end: 12, kind: "field" },
        { start: 12, end: 13, kind: "operator" },
        { start: 13, end: 19, kind: "value" },
      ],
    },
    // A rel id value is SCALAR — no comma split — so a comma inside it is part of
    // the value, not an operator. This mirrors `parseQuery`, which stores the
    // literal remainder after the first colon.
    {
      name: "parent:tnib-1,tnib-2 keeps the comma inside the scalar id value",
      input: "parent:tnib-1,tnib-2",
      expected: [
        { start: 0, end: 6, kind: "field" },
        { start: 6, end: 7, kind: "operator" },
        { start: 7, end: 20, kind: "value" },
      ],
    },
    {
      name: "Parent:TNIB-1 (uppercase) still classifies as field + value",
      input: "Parent:TNIB-1",
      expected: [
        { start: 0, end: 6, kind: "field" },
        { start: 6, end: 7, kind: "operator" },
        { start: 7, end: 13, kind: "value" },
      ],
    },

    // --- existence tokens: `has`/`no`/`is` are the field span, the dimension is
    // the value span. Unlike a rel id, this value IS validated — the legal set is
    // the closed `EXISTENCE_TOKENS` vocabulary.
    {
      name: "has:parent splits into field, colon, value",
      input: "has:parent",
      expected: [
        { start: 0, end: 3, kind: "field" },
        { start: 3, end: 4, kind: "operator" },
        { start: 4, end: 10, kind: "value" },
      ],
    },
    {
      name: "no:parent splits into field, colon, value",
      input: "no:parent",
      expected: [
        { start: 0, end: 2, kind: "field" },
        { start: 2, end: 3, kind: "operator" },
        { start: 3, end: 9, kind: "value" },
      ],
    },
    {
      name: "has:blocking splits into field, colon, value",
      input: "has:blocking",
      expected: [
        { start: 0, end: 3, kind: "field" },
        { start: 3, end: 4, kind: "operator" },
        { start: 4, end: 12, kind: "value" },
      ],
    },
    {
      name: "no:blocked-by splits into field, colon, value",
      input: "no:blocked-by",
      expected: [
        { start: 0, end: 2, kind: "field" },
        { start: 2, end: 3, kind: "operator" },
        { start: 3, end: 13, kind: "value" },
      ],
    },
    {
      name: "is:blocked splits into field, colon, value",
      input: "is:blocked",
      expected: [
        { start: 0, end: 2, kind: "field" },
        { start: 2, end: 3, kind: "operator" },
        { start: 3, end: 10, kind: "value" },
      ],
    },
    {
      name: "milestone:tnib-1 colors like any other id-valued token",
      input: "milestone:tnib-1",
      expected: [
        { start: 0, end: 9, kind: "field" },
        { start: 9, end: 10, kind: "operator" },
        { start: 10, end: 16, kind: "value" },
      ],
    },
    {
      name: "is:backlog splits into field, colon, value",
      input: "is:backlog",
      expected: [
        { start: 0, end: 2, kind: "field" },
        { start: 2, end: 3, kind: "operator" },
        { start: 3, end: 10, kind: "value" },
      ],
    },

    // --- a negated rel/existence token renders `invalid` (whole token). This is
    // the ONE place the overlay gained a red underline: `parseQuery` parks these in
    // `invalidTokens`, which the box already shows in its "Unrecognized:" chip, so
    // leaving them muted made the overlay disagree with a warning already on screen.
    // The WHOLE token is marked because the whole token is what gets parked — the
    // defect is the negation, not the value, so underlining only the value would
    // point at the wrong characters.
    { name: "-ancestor:tnib-1 (negated rel) is invalid, whole token", input: "-ancestor:tnib-1", expected: [{ start: 0, end: 16, kind: "invalid" }] },
    { name: "-parent:tnib-1 (negated rel) is invalid, whole token", input: "-parent:tnib-1", expected: [{ start: 0, end: 14, kind: "invalid" }] },
    { name: "-is:blocked (negated existence) is invalid, whole token", input: "-is:blocked", expected: [{ start: 0, end: 11, kind: "invalid" }] },
    { name: "-has:parent (negated existence) is invalid, whole token", input: "-has:parent", expected: [{ start: 0, end: 11, kind: "invalid" }] },
    // A negated token that is NOT a rel/existence spelling is still free text —
    // `recognizeRelationship` returns undefined, and Bleve's `-` syntax is the point.
    { name: "-has:mentions (not a spelling) stays free text", input: "-has:mentions", expected: [{ start: 0, end: 13, kind: "freetext" }] },

    // --- near-misses stay free text: an unknown field, and a rel name with no value ---
    { name: "foo:bar (unknown field) is free text", input: "foo:bar", expected: [{ start: 0, end: 7, kind: "freetext" }] },
    { name: "parent: (rel name, empty value) is free text", input: "parent:", expected: [{ start: 0, end: 7, kind: "freetext" }] },
    { name: "is:foo (not an existence spelling) is free text", input: "is:foo", expected: [{ start: 0, end: 6, kind: "freetext" }] },
    { name: "has:ancestor (no such predicate) is free text", input: "has:ancestor", expected: [{ start: 0, end: 12, kind: "freetext" }] },

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

// Driven from the vocabulary itself rather than a hand-listed set, so a token
// added to `REL_TOKEN_ORDER` is automatically held to these rules. A spelling the
// parser accepts but the overlay greys out is the exact defect this pins.
describe("tokenizeSpans — every rel/existence spelling is recognized", () => {
  const spellings = REL_TOKEN_ORDER.map((spec) =>
    spec.kind === "id" ? `${spec.name}:tnib-1` : spec.token,
  );

  for (const token of spellings) {
    it(`${token} opens with a field span covering its name`, () => {
      const spans = tokenizeSpans(token);
      expect(spans[0].kind).toBe("field");
      // The field span is the name up to (not including) the first colon.
      expect(token.slice(spans[0].start, spans[0].end)).toBe(token.slice(0, token.indexOf(":")));
      // Never a single free-text blob — that is the bug being fixed.
      expect(spans).not.toEqual([{ start: 0, end: token.length, kind: "freetext" }]);
    });

    it(`${token} produces no invalid span (it is not negated)`, () => {
      expect(tokenizeSpans(token).some((s) => s.kind === "invalid")).toBe(false);
    });

    it(`${token} produces no freetext span (the parser recognizes it)`, () => {
      expect(tokenizeSpans(token).some((s) => s.kind === "freetext")).toBe(false);
    });

    it(`-${token} (negated) is one invalid span over the whole token`, () => {
      expect(tokenizeSpans(`-${token}`)).toEqual([
        { start: 0, end: token.length + 1, kind: "invalid" },
      ]);
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
    // Rel/existence shapes: hyphenated names, scalar values holding a comma or an
    // extra colon, the negated (invalid) forms, and the near-misses that stay free
    // text. These are the token shapes whose classification this file just changed —
    // the tiling invariant has to survive all of them.
    "parent:tnib-1 has:parent is:blocked",
    "blocked-by:tnib-1 no:blocked-by mentioned-by:tnib-2",
    "blocking:tnib-1 no:blocking has:blocking mentions:tnib-2",
    "-ancestor:tnib-1 -is:blocked -has:parent",
    "-has:mentions foo:bar parent: is:foo",
    "parent:tnib-1,tnib-2 sibling:tnib:3",
    "  type:bug   -parent:tnib-1   has:parent  ",
    "Parent:TNIB-1 HAS:PARENT Is:Blocked",
    "milestone:tnib-1 is:backlog -milestone:tnib-2 milestone: has:milestone",
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

describe("tokenizeSpans — the area token", () => {
  const READY = createAreaVocabulary([
    { path: "web", name: "web", description: "", color: "", depth: 0 },
    { path: "web/dashboard", name: "dashboard", description: "", color: "", depth: 1 },
  ]);

  // The coloring axis, cell by cell. The value span's kind is decided by the same
  // `isRefusedArea` call `parseQuery` routes on, so a token cannot be parked as
  // invalid while rendering as accepted, or vice versa.
  const cases: {
    name: string;
    input: string;
    areas: AreaVocabulary | undefined;
    expected: Span[];
  }[] = [
    {
      name: "a declared path colors like any other value",
      input: "area:web",
      areas: READY,
      expected: [
        { start: 0, end: 4, kind: "field" },
        { start: 4, end: 5, kind: "operator" },
        { start: 5, end: 8, kind: "value" },
      ],
    },
    {
      name: "the `/` in a nested path is part of the value, not an operator",
      input: "area:web/dashboard",
      areas: READY,
      expected: [
        { start: 0, end: 4, kind: "field" },
        { start: 4, end: 5, kind: "operator" },
        { start: 5, end: 18, kind: "value" },
      ],
    },
    {
      name: "an undeclared path underlines just the value",
      input: "area:retired",
      areas: READY,
      expected: [
        { start: 0, end: 4, kind: "field" },
        { start: 4, end: 5, kind: "operator" },
        { start: 5, end: 12, kind: "invalid" },
      ],
    },
    {
      name: "a pre-load vocabulary renders neutral, never invalid-red",
      input: "area:retired",
      areas: LOADING_AREAS,
      expected: [
        { start: 0, end: 4, kind: "field" },
        { start: 4, end: 5, kind: "operator" },
        { start: 5, end: 12, kind: "value" },
      ],
    },
    {
      name: "a failed config query renders neutral too",
      input: "area:retired",
      areas: UNAVAILABLE_AREAS,
      expected: [
        { start: 0, end: 4, kind: "field" },
        { start: 4, end: 5, kind: "operator" },
        { start: 5, end: 12, kind: "value" },
      ],
    },
    {
      name: "no vocabulary renders neutral, matching what the parser kept",
      input: "area:retired",
      areas: undefined,
      expected: [
        { start: 0, end: 4, kind: "field" },
        { start: 4, end: 5, kind: "operator" },
        { start: 5, end: 12, kind: "value" },
      ],
    },
    {
      name: "a negated token is invalid across its whole width",
      input: "-area:web",
      areas: READY,
      expected: [{ start: 0, end: 9, kind: "invalid" }],
    },
    {
      name: "an empty value is free text, as the parser treats it",
      input: "area:",
      areas: READY,
      expected: [{ start: 0, end: 5, kind: "freetext" }],
    },
  ];

  for (const { name, input, areas, expected } of cases) {
    it(name, () => {
      expect(tokenizeSpans(input, areas)).toEqual(expected);
    });
  }

  it("keeps the tiling invariant with an area token in the string", () => {
    const text = "type:bug area:web/dashboard -area:retired login";
    for (const areas of [READY, LOADING_AREAS, undefined]) {
      const spans = tokenizeSpans(text, areas);
      expect(spans.map((s) => text.slice(s.start, s.end)).join("")).toBe(text);
      for (const s of spans) expect(s.end).toBeGreaterThan(s.start);
    }
  });

  it("colors exactly what the parser routed, for every vocabulary", () => {
    // The anti-drift property, asserted rather than described: a value span iff
    // the parser wrote the filter, an invalid span iff it parked the token.
    for (const areas of [READY, EMPTY_AREAS, LOADING_AREAS, UNAVAILABLE_AREAS, undefined]) {
      for (const text of ["area:web", "area:retired", "area:web/dashboard"]) {
        const parked = parseQuery(text, areas).invalidTokens.length > 0;
        const kinds = tokenizeSpans(text, areas).map((s) => s.kind);
        expect(kinds.includes("invalid"), `${text} / ${areas?.status ?? "none"}`).toBe(parked);
      }
    }
  });
});
