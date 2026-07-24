import { describe, it, expect } from "vitest";
import { applyFlatSort, nextFlatSort } from "./flatSort";
import type { FlatSort } from "./types";

interface DateNib {
  id: string;
  createdAt: string;
  updatedAt: string;
}

function nib(id: string, createdAt: string, updatedAt: string): DateNib {
  return { id, createdAt, updatedAt };
}

describe("applyFlatSort", () => {
  const a = nib("a", "2026-01-01T00:00:00Z", "2026-03-10T00:00:00Z");
  const b = nib("b", "2026-02-01T00:00:00Z", "2026-01-05T00:00:00Z");
  const c = nib("c", "2026-03-01T00:00:00Z", "2026-02-20T00:00:00Z");

  it("returns the SAME array reference (not a copy) when sort is null", () => {
    const input = [a, b, c];
    expect(applyFlatSort(input, null)).toBe(input);
  });

  it("sorts by createdAt ascending", () => {
    const sort: FlatSort = { field: "created", direction: "asc" };
    expect(applyFlatSort([c, a, b], sort).map((n) => n.id)).toEqual(["a", "b", "c"]);
  });

  it("sorts by createdAt descending", () => {
    const sort: FlatSort = { field: "created", direction: "desc" };
    expect(applyFlatSort([a, c, b], sort).map((n) => n.id)).toEqual(["c", "b", "a"]);
  });

  it("sorts by updatedAt (modified) ascending", () => {
    const sort: FlatSort = { field: "modified", direction: "asc" };
    // updatedAt: a=Mar10, b=Jan05, c=Feb20 → b, c, a
    expect(applyFlatSort([a, b, c], sort).map((n) => n.id)).toEqual(["b", "c", "a"]);
  });

  it("sorts by updatedAt (modified) descending", () => {
    const sort: FlatSort = { field: "modified", direction: "desc" };
    expect(applyFlatSort([a, b, c], sort).map((n) => n.id)).toEqual(["a", "c", "b"]);
  });

  it("does not mutate the input array", () => {
    const input = [c, a, b];
    const before = input.map((n) => n.id);
    applyFlatSort(input, { field: "created", direction: "asc" });
    expect(input.map((n) => n.id)).toEqual(before);
  });

  it("keeps incoming order as a stable tiebreak for equal timestamps", () => {
    // Three nibs share one createdAt; their relative input order must survive.
    const x = nib("x", "2026-05-01T00:00:00Z", "2026-05-01T00:00:00Z");
    const y = nib("y", "2026-05-01T00:00:00Z", "2026-05-01T00:00:00Z");
    const z = nib("z", "2026-05-01T00:00:00Z", "2026-05-01T00:00:00Z");
    const asc = applyFlatSort([x, y, z], { field: "created", direction: "asc" });
    expect(asc.map((n) => n.id)).toEqual(["x", "y", "z"]);
    const desc = applyFlatSort([x, y, z], { field: "created", direction: "desc" });
    // Equal timestamps → stable → input order preserved even descending.
    expect(desc.map((n) => n.id)).toEqual(["x", "y", "z"]);
  });

  it("sorts empty / unparseable timestamps last in both directions", () => {
    const good1 = nib("g1", "2026-01-01T00:00:00Z", "2026-01-01T00:00:00Z");
    const good2 = nib("g2", "2026-02-01T00:00:00Z", "2026-02-01T00:00:00Z");
    const empty = nib("empty", "", "");
    const bad = nib("bad", "not-a-date", "not-a-date");

    const asc = applyFlatSort([empty, good2, bad, good1], { field: "created", direction: "asc" });
    // Valid ascending first (g1, g2), invalids last in stable input order (empty, bad).
    expect(asc.map((n) => n.id)).toEqual(["g1", "g2", "empty", "bad"]);

    const desc = applyFlatSort([empty, good1, bad, good2], { field: "created", direction: "desc" });
    // Valid descending first (g2, g1), invalids still last.
    expect(desc.map((n) => n.id)).toEqual(["g2", "g1", "empty", "bad"]);
  });
});

describe("nextFlatSort", () => {
  it("goes from off to ascending on the clicked field", () => {
    expect(nextFlatSort(null, "created")).toEqual({ field: "created", direction: "asc" });
    expect(nextFlatSort(null, "modified")).toEqual({ field: "modified", direction: "asc" });
  });

  it("cycles ascending → descending on the same field", () => {
    expect(nextFlatSort({ field: "created", direction: "asc" }, "created")).toEqual({
      field: "created",
      direction: "desc",
    });
  });

  it("cycles descending → off on the same field", () => {
    expect(nextFlatSort({ field: "created", direction: "desc" }, "created")).toBeNull();
  });

  it("starts a NEW field at ascending regardless of the current field's direction", () => {
    expect(nextFlatSort({ field: "created", direction: "desc" }, "modified")).toEqual({
      field: "modified",
      direction: "asc",
    });
    expect(nextFlatSort({ field: "modified", direction: "asc" }, "created")).toEqual({
      field: "created",
      direction: "asc",
    });
  });

  it("completes the full asc → desc → off cycle for one field", () => {
    let s: FlatSort | null = null;
    s = nextFlatSort(s, "modified");
    expect(s).toEqual({ field: "modified", direction: "asc" });
    s = nextFlatSort(s, "modified");
    expect(s).toEqual({ field: "modified", direction: "desc" });
    s = nextFlatSort(s, "modified");
    expect(s).toBeNull();
  });
});
