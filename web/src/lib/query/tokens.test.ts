import { describe, it, expect } from "vitest";
import { tokenSegments, tokenGroups } from "./index";
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

// The backdrop draws one chip per structured token, so it needs the spans grouped
// by token AND a flag for which runs earned a chip. `tokenSegments` above is the
// coarser view the click layer uses; both come from this one grouping.
describe("tokenGroups — spans grouped per token, with the chip flag", () => {
  it("marks a field:value token structured and carries its spans", () => {
    const groups = tokenGroups("type:bug");
    expect(groups).toHaveLength(1);
    expect(groups[0].kind).toBe("token");
    expect(groups[0].structured).toBe(true);
    expect(groups[0].spans.map((s) => s.kind)).toEqual(["field", "operator", "value"]);
  });

  it("marks a bare word unstructured so it gets no chip", () => {
    const groups = tokenGroups("login");
    expect(groups[0].structured).toBe(false);
    expect(groups[0].spans.map((s) => s.kind)).toEqual(["freetext"]);
  });

  // A known field with a bad value is still structure — it chips, and the wavy
  // underline on the value is what marks it wrong.
  it("marks a known field with an invalid value structured", () => {
    const groups = tokenGroups("status:banana");
    expect(groups[0].structured).toBe(true);
    expect(groups[0].spans.map((s) => s.kind)).toEqual(["field", "operator", "invalid"]);
  });

  // A parked whole-token invalid (negated relationship) has no field span. It must
  // NOT be chipped, or it would be dressed as a working token while doing nothing.
  it("does not mark a parked whole-token invalid as structured", () => {
    const groups = tokenGroups("-ancestor:tnib-1");
    expect(groups[0].spans.map((s) => s.kind)).toEqual(["invalid"]);
    expect(groups[0].structured).toBe(false);
  });

  it("never marks a gap structured", () => {
    const groups = tokenGroups("type:bug login");
    expect(groups.filter((g) => g.kind === "gap").every((g) => !g.structured)).toBe(true);
  });

  // Grouping must not disturb the offsets the backdrop aligns on: the spans it
  // hands out still tile the whole string in order.
  it("preserves the tiling identity across the grouped spans", () => {
    const inputs = [
      "type:bug,feature -tags:wip status:banana login flow",
      "  spaced   out  tokens  ",
      "blocking:tnib-1 has:parent -ancestor:tnib-2",
    ];
    for (const input of inputs) {
      const spans = tokenGroups(input).flatMap((g) => g.spans);
      expect(spans[0].start).toBe(0);
      expect(spans[spans.length - 1].end).toBe(input.length);
      for (let i = 1; i < spans.length; i++) expect(spans[i].start).toBe(spans[i - 1].end);
      expect(spans.map((s) => input.slice(s.start, s.end)).join("")).toBe(input);
    }
  });
});

// The backdrop recesses only the VALUE RUN into a darker well — the field name and
// its colon stay on the plain surface. The run has to span the whole value region
// rather than each value span, or a comma splits one token into two visual pills.
describe("tokenGroups — where the value run starts", () => {
  it("starts the run just after the field's colon", () => {
    const g = tokenGroups("type:bug")[0];
    expect(g.spans.map((s) => s.kind)).toEqual(["field", "operator", "value"]);
    expect(g.valueRunStart).toBe(2);
  });

  // The whole point of the run: it must cover the comma so the fill is continuous.
  it("covers commas and every following value", () => {
    const g = tokenGroups("status:todo,in-progress")[0];
    expect(g.spans.map((s) => s.kind)).toEqual(["field", "operator", "value", "operator", "value"]);
    expect(g.valueRunStart).toBe(2);
    expect(g.spans.slice(g.valueRunStart).map((s) => s.kind)).toEqual([
      "value",
      "operator",
      "value",
    ]);
  });

  it("covers a known field's invalid value", () => {
    const g = tokenGroups("status:banana")[0];
    expect(g.spans.slice(g.valueRunStart).map((s) => s.kind)).toEqual(["invalid"]);
  });

  it("negation stays outside the run — the minus belongs to the field", () => {
    const g = tokenGroups("-type:bug")[0];
    expect(g.spans[0].kind).toBe("field");
    expect(g.valueRunStart).toBe(2);
  });

  // No run means no well. A bare word has no structure to mark, and a parked
  // whole-token invalid must keep its wavy underline without being dressed up.
  it("reports no run for a bare word, a parked invalid, or a gap", () => {
    expect(tokenGroups("login")[0].valueRunStart).toBe(-1);
    expect(tokenGroups("-ancestor:tnib-1")[0].valueRunStart).toBe(-1);
    expect(tokenGroups("a b")[1].valueRunStart).toBe(-1);
  });

  it("keeps the run a contiguous tail of the token's spans", () => {
    for (const q of ["type:bug,feature", "has:parent", "status:draft,todo login"]) {
      for (const g of tokenGroups(q)) {
        if (g.valueRunStart < 0) continue;
        expect(g.valueRunStart).toBeGreaterThan(0);
        expect(g.valueRunStart).toBeLessThan(g.spans.length);
        // head + run reconstruct the group exactly
        const head = g.spans.slice(0, g.valueRunStart);
        const run = g.spans.slice(g.valueRunStart);
        expect([...head, ...run]).toEqual(g.spans);
      }
    }
  });
});
