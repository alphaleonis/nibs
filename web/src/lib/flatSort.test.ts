import { describe, it, expect } from "vitest";
import { applyFlatSort, nextFlatSort } from "./flatSort";
import type { SortableRow } from "./flatSort";
import type { FlatSort } from "./types";

// A full SortableRow with neutral defaults; each test overrides only the field
// under test. `id` doubles as the tracking key in result assertions.
function row(overrides: Partial<SortableRow> = {}): SortableRow {
  return {
    id: "nibs-0001",
    title: "",
    status: "todo",
    type: "task",
    estimate: "",
    tags: [],
    createdAt: "",
    updatedAt: "",
    parentId: null,
    blockingIds: [],
    blockedByIds: [],
    ...overrides,
  };
}

const ids = (rows: SortableRow[]) => rows.map((r) => r.id);

describe("applyFlatSort — off / identity", () => {
  it("returns the SAME array reference (not a copy) when sort is null", () => {
    const input = [row({ id: "a" }), row({ id: "b" })];
    expect(applyFlatSort(input, null)).toBe(input);
  });

  it("does not mutate the input array", () => {
    const input = [row({ id: "c", title: "c" }), row({ id: "a", title: "a" }), row({ id: "b", title: "b" })];
    const before = ids(input);
    applyFlatSort(input, { field: "title", direction: "asc" });
    expect(ids(input)).toEqual(before);
  });
});

describe("applyFlatSort — text columns", () => {
  it("title: case-insensitive ascending", () => {
    const rows = [row({ id: "banana", title: "banana" }), row({ id: "apple", title: "Apple" }), row({ id: "cherry", title: "cherry" })];
    expect(ids(applyFlatSort(rows, { field: "title", direction: "asc" }))).toEqual(["apple", "banana", "cherry"]);
  });

  it("title: case-insensitive descending", () => {
    const rows = [row({ id: "banana", title: "banana" }), row({ id: "apple", title: "Apple" }), row({ id: "cherry", title: "cherry" })];
    expect(ids(applyFlatSort(rows, { field: "title", direction: "desc" }))).toEqual(["cherry", "banana", "apple"]);
  });

  it("title: blank/whitespace titles sort LAST in both directions", () => {
    const rows = [row({ id: "blank", title: "   " }), row({ id: "zed", title: "zed" }), row({ id: "aaa", title: "aaa" })];
    expect(ids(applyFlatSort(rows, { field: "title", direction: "asc" }))).toEqual(["aaa", "zed", "blank"]);
    expect(ids(applyFlatSort(rows, { field: "title", direction: "desc" }))).toEqual(["zed", "aaa", "blank"]);
  });

  it("id: lexicographic ascending / descending", () => {
    const rows = [row({ id: "nibs-0003" }), row({ id: "nibs-0001" }), row({ id: "nibs-0002" })];
    expect(ids(applyFlatSort(rows, { field: "id", direction: "asc" }))).toEqual(["nibs-0001", "nibs-0002", "nibs-0003"]);
    expect(ids(applyFlatSort(rows, { field: "id", direction: "desc" }))).toEqual(["nibs-0003", "nibs-0002", "nibs-0001"]);
  });
});

describe("applyFlatSort — enum columns (canonical rank, not string order)", () => {
  it("type: canonical rank ascending (milestone → epic → bug → feature → task → research)", () => {
    const rows = [
      row({ id: "task", type: "task" }),
      row({ id: "milestone", type: "milestone" }),
      row({ id: "feature", type: "feature" }),
      row({ id: "bug", type: "bug" }),
      row({ id: "epic", type: "epic" }),
      row({ id: "research", type: "research" }),
    ];
    expect(ids(applyFlatSort(rows, { field: "type", direction: "asc" }))).toEqual([
      "milestone", "epic", "bug", "feature", "task", "research",
    ]);
  });

  it("type: descending reverses the canonical rank", () => {
    const rows = [
      row({ id: "bug", type: "bug" }),
      row({ id: "milestone", type: "milestone" }),
      row({ id: "research", type: "research" }),
    ];
    expect(ids(applyFlatSort(rows, { field: "type", direction: "desc" }))).toEqual(["research", "bug", "milestone"]);
  });

  it("state: STATUSES order ascending (draft → todo → in-progress → deferred → completed → scrapped)", () => {
    const rows = [
      row({ id: "completed", status: "completed" }),
      row({ id: "draft", status: "draft" }),
      row({ id: "in-progress", status: "in-progress" }),
      row({ id: "scrapped", status: "scrapped" }),
      row({ id: "todo", status: "todo" }),
      row({ id: "deferred", status: "deferred" }),
    ];
    expect(ids(applyFlatSort(rows, { field: "state", direction: "asc" }))).toEqual([
      "draft", "todo", "in-progress", "deferred", "completed", "scrapped",
    ]);
  });

  it("state: descending reverses STATUSES order", () => {
    const rows = [row({ id: "todo", status: "todo" }), row({ id: "draft", status: "draft" }), row({ id: "scrapped", status: "scrapped" })];
    expect(ids(applyFlatSort(rows, { field: "state", direction: "desc" }))).toEqual(["scrapped", "todo", "draft"]);
  });

  it("effort: ESTIMATES order ascending with NO estimate last", () => {
    const rows = [
      row({ id: "l", estimate: "l" }),
      row({ id: "none", estimate: "" }),
      row({ id: "s", estimate: "s" }),
      row({ id: "xl", estimate: "xl" }),
      row({ id: "m", estimate: "m" }),
    ];
    expect(ids(applyFlatSort(rows, { field: "effort", direction: "asc" }))).toEqual(["s", "m", "l", "xl", "none"]);
  });

  it("effort: descending keeps NO estimate last (empties never lead)", () => {
    const rows = [
      row({ id: "l", estimate: "l" }),
      row({ id: "none", estimate: "" }),
      row({ id: "s", estimate: "s" }),
      row({ id: "xl", estimate: "xl" }),
    ];
    expect(ids(applyFlatSort(rows, { field: "effort", direction: "desc" }))).toEqual(["xl", "l", "s", "none"]);
  });
});

describe("applyFlatSort — date columns", () => {
  const a = row({ id: "a", createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-03-10T00:00:00Z" });
  const b = row({ id: "b", createdAt: "2026-02-01T00:00:00Z", updatedAt: "2026-01-05T00:00:00Z" });
  const c = row({ id: "c", createdAt: "2026-03-01T00:00:00Z", updatedAt: "2026-02-20T00:00:00Z" });

  it("created ascending / descending", () => {
    expect(ids(applyFlatSort([c, a, b], { field: "created", direction: "asc" }))).toEqual(["a", "b", "c"]);
    expect(ids(applyFlatSort([a, c, b], { field: "created", direction: "desc" }))).toEqual(["c", "b", "a"]);
  });

  it("modified (updatedAt) ascending / descending", () => {
    expect(ids(applyFlatSort([a, b, c], { field: "modified", direction: "asc" }))).toEqual(["b", "c", "a"]);
    expect(ids(applyFlatSort([a, b, c], { field: "modified", direction: "desc" }))).toEqual(["a", "c", "b"]);
  });

  it("empty / unparseable timestamps sort LAST in both directions", () => {
    const g1 = row({ id: "g1", createdAt: "2026-01-01T00:00:00Z" });
    const g2 = row({ id: "g2", createdAt: "2026-02-01T00:00:00Z" });
    const empty = row({ id: "empty", createdAt: "" });
    const bad = row({ id: "bad", createdAt: "not-a-date" });
    expect(ids(applyFlatSort([empty, g2, bad, g1], { field: "created", direction: "asc" }))).toEqual(["g1", "g2", "empty", "bad"]);
    expect(ids(applyFlatSort([empty, g1, bad, g2], { field: "created", direction: "desc" }))).toEqual(["g2", "g1", "empty", "bad"]);
  });
});

describe("applyFlatSort — relation-count columns", () => {
  it("blocking: by count ascending (zero is a real low value, not empty)", () => {
    const rows = [
      row({ id: "two", blockingIds: ["x", "y"] }),
      row({ id: "zero", blockingIds: [] }),
      row({ id: "one", blockingIds: ["x"] }),
    ];
    expect(ids(applyFlatSort(rows, { field: "blocking", direction: "asc" }))).toEqual(["zero", "one", "two"]);
    expect(ids(applyFlatSort(rows, { field: "blocking", direction: "desc" }))).toEqual(["two", "one", "zero"]);
  });

  it("blockedBy: by count ascending / descending", () => {
    const rows = [
      row({ id: "one", blockedByIds: ["x"] }),
      row({ id: "three", blockedByIds: ["x", "y", "z"] }),
      row({ id: "zero", blockedByIds: [] }),
    ];
    expect(ids(applyFlatSort(rows, { field: "blockedBy", direction: "asc" }))).toEqual(["zero", "one", "three"]);
    expect(ids(applyFlatSort(rows, { field: "blockedBy", direction: "desc" }))).toEqual(["three", "one", "zero"]);
  });
});

describe("applyFlatSort — tags column", () => {
  it("tags: by first tag alphabetical, no tags LAST in both directions", () => {
    const rows = [
      row({ id: "zebra", tags: ["zebra"] }),
      row({ id: "apple", tags: ["apple", "x"] }),
      row({ id: "none", tags: [] }),
    ];
    expect(ids(applyFlatSort(rows, { field: "tags", direction: "asc" }))).toEqual(["apple", "zebra", "none"]);
    expect(ids(applyFlatSort(rows, { field: "tags", direction: "desc" }))).toEqual(["zebra", "apple", "none"]);
  });
});

describe("applyFlatSort — parent column", () => {
  // The parent nibs live in the same list; sorting by parent uses each row's
  // PARENT nib title. Parent nibs have no parent → empty → last.
  const p1 = row({ id: "p1", title: "Zeta" });
  const p2 = row({ id: "p2", title: "Alpha" });
  const c1 = row({ id: "c1", parentId: "p1" }); // parent title "Zeta"
  const c2 = row({ id: "c2", parentId: "p2" }); // parent title "Alpha"
  const orphan = row({ id: "orphan", parentId: null });
  const dangling = row({ id: "dangling", parentId: "missing" }); // parent not in list

  it("sorts by parent nib title ascending, no/unknown parent LAST", () => {
    const out = ids(applyFlatSort([p1, c1, orphan, c2, dangling, p2], { field: "parent", direction: "asc" }));
    // Resolved parent titles: c2=Alpha, c1=Zeta; then empties (p1, orphan, dangling, p2) in stable input order.
    expect(out).toEqual(["c2", "c1", "p1", "orphan", "dangling", "p2"]);
  });

  it("sorts by parent nib title descending, empties still LAST", () => {
    const out = ids(applyFlatSort([p1, c1, orphan, c2, p2], { field: "parent", direction: "desc" }));
    // Zeta before Alpha descending; empties (p1, orphan, p2) unchanged at the end.
    expect(out).toEqual(["c1", "c2", "p1", "orphan", "p2"]);
  });
});

describe("applyFlatSort — stable tiebreak on equal keys", () => {
  it("equal non-empty keys keep incoming order in both directions", () => {
    const rows = [row({ id: "x", type: "bug" }), row({ id: "y", type: "bug" }), row({ id: "z", type: "bug" })];
    expect(ids(applyFlatSort(rows, { field: "type", direction: "asc" }))).toEqual(["x", "y", "z"]);
    expect(ids(applyFlatSort(rows, { field: "type", direction: "desc" }))).toEqual(["x", "y", "z"]);
  });

  it("equal empty keys keep incoming order", () => {
    const rows = [row({ id: "x", tags: [] }), row({ id: "y", tags: [] }), row({ id: "z", tags: [] })];
    expect(ids(applyFlatSort(rows, { field: "tags", direction: "asc" }))).toEqual(["x", "y", "z"]);
    expect(ids(applyFlatSort(rows, { field: "tags", direction: "desc" }))).toEqual(["x", "y", "z"]);
  });
});

describe("nextFlatSort — tri-state cycle (field-agnostic)", () => {
  it("goes from off to ascending on the clicked field", () => {
    expect(nextFlatSort(null, "created")).toEqual({ field: "created", direction: "asc" });
    expect(nextFlatSort(null, "type")).toEqual({ field: "type", direction: "asc" });
  });

  it("cycles ascending → descending on the same field", () => {
    expect(nextFlatSort({ field: "state", direction: "asc" }, "state")).toEqual({ field: "state", direction: "desc" });
  });

  it("cycles descending → off on the same field", () => {
    expect(nextFlatSort({ field: "state", direction: "desc" }, "state")).toBeNull();
  });

  it("starts a NEW field at ascending regardless of the current field's direction", () => {
    expect(nextFlatSort({ field: "created", direction: "desc" }, "tags")).toEqual({ field: "tags", direction: "asc" });
    expect(nextFlatSort({ field: "modified", direction: "asc" }, "id")).toEqual({ field: "id", direction: "asc" });
  });

  it("completes the full asc → desc → off cycle for one field", () => {
    let s: FlatSort | null = null;
    s = nextFlatSort(s, "effort");
    expect(s).toEqual({ field: "effort", direction: "asc" });
    s = nextFlatSort(s, "effort");
    expect(s).toEqual({ field: "effort", direction: "desc" });
    s = nextFlatSort(s, "effort");
    expect(s).toBeNull();
  });
});
