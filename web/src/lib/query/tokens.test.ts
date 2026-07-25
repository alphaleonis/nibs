import { describe, it, expect } from "vitest";
import { tokenSegments, removeTokenRange } from "./index";
import type { TokenSegment } from "./index";

describe("tokenSegments — token/gap grouping", () => {
  const cases: { name: string; input: string; expected: TokenSegment[] }[] = [
    { name: "empty string", input: "", expected: [] },
    { name: "whitespace only", input: "   ", expected: [{ kind: "gap", start: 0, end: 3 }] },
    {
      name: "a single multi-part token groups field+op+value into one segment",
      input: "type:bug",
      expected: [{ kind: "token", start: 0, end: 8 }],
    },
    {
      name: "invalid value still groups into one token segment",
      input: "status:banana",
      expected: [{ kind: "token", start: 0, end: 13 }],
    },
    {
      name: "two tokens separated by a gap",
      input: "type:bug login",
      expected: [
        { kind: "token", start: 0, end: 8 },
        { kind: "gap", start: 8, end: 9 },
        { kind: "token", start: 9, end: 14 },
      ],
    },
    {
      name: "valid + invalid + free text (the screenshot query)",
      input: "type:bug status:banana login",
      expected: [
        { kind: "token", start: 0, end: 8 },
        { kind: "gap", start: 8, end: 9 },
        { kind: "token", start: 9, end: 22 },
        { kind: "gap", start: 22, end: 23 },
        { kind: "token", start: 23, end: 28 },
      ],
    },
    {
      name: "leading/trailing and inner multi-space gaps are preserved",
      input: "  a   b  ",
      expected: [
        { kind: "gap", start: 0, end: 2 },
        { kind: "token", start: 2, end: 3 },
        { kind: "gap", start: 3, end: 6 },
        { kind: "token", start: 6, end: 7 },
        { kind: "gap", start: 7, end: 9 },
      ],
    },
  ];

  for (const { name, input, expected } of cases) {
    it(name, () => {
      expect(tokenSegments(input)).toEqual(expected);
    });
  }

  it("tiles the whole string contiguously (reconstructs the input)", () => {
    const inputs = [
      "type:bug,feature -tags:wip status:banana login flow",
      "  spaced   out  tokens  ",
      "blocking:tnib-1 has:parent",
    ];
    for (const input of inputs) {
      const segs = tokenSegments(input);
      expect(segs[0].start).toBe(0);
      expect(segs[segs.length - 1].end).toBe(input.length);
      for (let i = 1; i < segs.length; i++) expect(segs[i].start).toBe(segs[i - 1].end);
      expect(segs.map((s) => input.slice(s.start, s.end)).join("")).toBe(input);
    }
  });
});

describe("removeTokenRange — splice + whitespace collapse", () => {
  // Each case removes the token at [start, end) and checks the collapsed result.
  const cases: { name: string; input: string; start: number; end: number; expected: string }[] = [
    {
      name: "first token: no leading space is left",
      input: "type:bug status:todo login",
      start: 0,
      end: 8,
      expected: "status:todo login",
    },
    {
      name: "middle token: exactly one space between the neighbors",
      input: "type:bug priority:high status:todo",
      start: 9,
      end: 22,
      expected: "type:bug status:todo",
    },
    {
      name: "last token: no trailing space is left",
      input: "type:bug status:todo",
      start: 9,
      end: 20,
      expected: "type:bug",
    },
    {
      name: "only token: yields the empty string",
      input: "type:bug",
      start: 0,
      end: 8,
      expected: "",
    },
    {
      name: "an invalid token adjacent to a valid one collapses like any other",
      input: "type:bug status:banana",
      start: 9,
      end: 22,
      expected: "type:bug",
    },
    {
      name: "removing the valid token before an invalid one leaves the invalid alone",
      input: "type:bug status:banana",
      start: 0,
      end: 8,
      expected: "status:banana",
    },
  ];

  for (const { name, input, start, end, expected } of cases) {
    it(name, () => {
      expect(removeTokenRange(input, start, end)).toBe(expected);
    });
  }
});
