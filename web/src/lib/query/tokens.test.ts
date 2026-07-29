import { describe, it, expect } from "vitest";
import { tokenSegments } from "./index";
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
