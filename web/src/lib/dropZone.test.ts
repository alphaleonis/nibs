import { describe, it, expect } from "vitest";
import {
  computeDropZone,
  isValidParent,
  isValidDropTarget,
  isValidCrossParentDrop,
  collectDescendantIds,
} from "./dropZone";
import type { RowData } from "./tableData";
import type { TreeTableNib } from "./types";

function makeNib(overrides: Partial<TreeTableNib> = {}): TreeTableNib {
  return {
    id: "nib-001",
    title: "Test",
    status: "todo",
    type: "task",
    priority: "normal",
    estimate: "",
    tags: [],
    updatedAt: "",
    parentId: null,
    blockingIds: [],
    blockedByIds: [],
    ...overrides,
  };
}

// Omit `nib` from the base Partial<RowData> before re-adding it: intersecting
// `Partial<RowData>` (nib: TreeTableNib) with `{ nib?: Partial<TreeTableNib> }`
// would AND the two nib types, forcing callers to pass a full TreeTableNib.
function makeRow(overrides: Partial<Omit<RowData, "nib">> & { nib?: Partial<TreeTableNib> } = {}): RowData {
  const { nib: nibOverrides, ...rowOverrides } = overrides;
  return {
    nib: makeNib(nibOverrides),
    depth: 0,
    hasChildren: false,
    dimmed: false,
    parentNib: null,
    displayParentId: null,
    ...rowOverrides,
  };
}

describe("computeDropZone", () => {
  const rect = { top: 100, height: 40 } as DOMRect;

  it("returns 'before' when cursor is in the top 30%", () => {
    expect(computeDropZone(100, rect)).toBe("before"); // 0%
    expect(computeDropZone(105, rect)).toBe("before"); // 12.5%
    expect(computeDropZone(111, rect)).toBe("before"); // 27.5%
  });

  it("returns 'reparent' when cursor is in the middle 40%", () => {
    expect(computeDropZone(112, rect)).toBe("reparent"); // 30%
    expect(computeDropZone(120, rect)).toBe("reparent"); // 50%
    expect(computeDropZone(127, rect)).toBe("reparent"); // 67.5%
  });

  it("returns 'after' when cursor is in the bottom 30%", () => {
    expect(computeDropZone(129, rect)).toBe("after"); // 72.5%
    expect(computeDropZone(135, rect)).toBe("after"); // 87.5%
    expect(computeDropZone(140, rect)).toBe("after"); // 100%
  });
});

describe("isValidParent", () => {
  it("task can be child of epic", () => {
    expect(isValidParent("task", "epic")).toBe(true);
  });

  it("task can be child of feature", () => {
    expect(isValidParent("task", "feature")).toBe(true);
  });

  it("epic can be child of milestone", () => {
    expect(isValidParent("epic", "milestone")).toBe(true);
  });

  it("milestone cannot be child of epic", () => {
    expect(isValidParent("milestone", "epic")).toBe(false);
  });

  it("task cannot be child of task", () => {
    expect(isValidParent("task", "task")).toBe(false);
  });

  it("epic cannot be child of feature", () => {
    expect(isValidParent("epic", "feature")).toBe(false);
  });
});

describe("isValidDropTarget", () => {
  it("cannot drop on self", () => {
    const target = { id: "nib-001", type: "epic", parentId: null };
    expect(isValidDropTarget(["task"], target, "reparent", ["nib-001"], new Set())).toBe(false);
  });

  it("cannot drop on own descendant", () => {
    const target = { id: "nib-002", type: "epic", parentId: "nib-001" };
    const descendants = new Set(["nib-002"]);
    expect(isValidDropTarget(["milestone"], target, "reparent", ["nib-001"], descendants)).toBe(false);
  });

  it("valid reparent: task to epic", () => {
    const target = { id: "nib-002", type: "epic", parentId: null };
    expect(isValidDropTarget(["task"], target, "reparent", ["nib-001"], new Set())).toBe(true);
  });

  it("invalid reparent: task to task (leaf type)", () => {
    const target = { id: "nib-002", type: "task", parentId: null };
    expect(isValidDropTarget(["task"], target, "reparent", ["nib-001"], new Set())).toBe(false);
  });

  it("invalid reparent: epic to feature (not a valid child)", () => {
    const target = { id: "nib-002", type: "feature", parentId: null };
    expect(isValidDropTarget(["epic"], target, "reparent", ["nib-001"], new Set())).toBe(false);
  });

  it("multi-type reparent: all must be valid children", () => {
    const target = { id: "nib-003", type: "epic", parentId: null };
    // task and bug are valid children of epic
    expect(isValidDropTarget(["task", "bug"], target, "reparent", ["nib-001", "nib-002"], new Set())).toBe(true);
    // task and epic are NOT both valid children of epic (epic is not)
    expect(isValidDropTarget(["task", "epic"], target, "reparent", ["nib-001", "nib-002"], new Set())).toBe(false);
  });

  it("before/after zones pass validation when not on self/descendant", () => {
    const target = { id: "nib-002", type: "task", parentId: "nib-parent" };
    expect(isValidDropTarget(["task"], target, "before", ["nib-001"], new Set())).toBe(true);
    expect(isValidDropTarget(["task"], target, "after", ["nib-001"], new Set())).toBe(true);
  });

  it("synthetic bucket rows are never valid drop targets (any zone)", () => {
    const bucket = { id: "__no_epic__", type: "", parentId: null };
    expect(isValidDropTarget(["task"], bucket, "before", [], new Set())).toBe(false);
    expect(isValidDropTarget(["task"], bucket, "after", [], new Set())).toBe(false);
    expect(isValidDropTarget(["task"], bucket, "reparent", [], new Set())).toBe(false);
  });
});

describe("collectDescendantIds", () => {
  it("collects direct children", () => {
    const rows: RowData[] = [
      makeRow({ nib: { id: "nib-001", parentId: null, type: "epic" }, depth: 0 }),
      makeRow({ nib: { id: "nib-002", parentId: "nib-001", type: "task" }, depth: 1 }),
      makeRow({ nib: { id: "nib-003", parentId: "nib-001", type: "task" }, depth: 1 }),
    ];
    const result = collectDescendantIds(["nib-001"], rows);
    expect(result.has("nib-002")).toBe(true);
    expect(result.has("nib-003")).toBe(true);
    expect(result.size).toBe(2);
  });

  it("collects nested descendants", () => {
    const rows: RowData[] = [
      makeRow({ nib: { id: "nib-001", parentId: null, type: "milestone" }, depth: 0 }),
      makeRow({ nib: { id: "nib-002", parentId: "nib-001", type: "epic" }, depth: 1 }),
      makeRow({ nib: { id: "nib-003", parentId: "nib-002", type: "task" }, depth: 2 }),
    ];
    const result = collectDescendantIds(["nib-001"], rows);
    expect(result.has("nib-002")).toBe(true);
    expect(result.has("nib-003")).toBe(true);
    expect(result.size).toBe(2);
  });

  it("does not include unrelated nibs", () => {
    const rows: RowData[] = [
      makeRow({ nib: { id: "nib-001", parentId: null, type: "epic" }, depth: 0 }),
      makeRow({ nib: { id: "nib-002", parentId: "nib-001", type: "task" }, depth: 1 }),
      makeRow({ nib: { id: "nib-003", parentId: null, type: "epic" }, depth: 0 }),
      makeRow({ nib: { id: "nib-004", parentId: "nib-003", type: "task" }, depth: 1 }),
    ];
    const result = collectDescendantIds(["nib-001"], rows);
    expect(result.has("nib-002")).toBe(true);
    expect(result.has("nib-003")).toBe(false);
    expect(result.has("nib-004")).toBe(false);
    expect(result.size).toBe(1);
  });

  it("handles multiple dragged items", () => {
    const rows: RowData[] = [
      makeRow({ nib: { id: "nib-001", parentId: null, type: "epic" }, depth: 0 }),
      makeRow({ nib: { id: "nib-002", parentId: "nib-001", type: "task" }, depth: 1 }),
      makeRow({ nib: { id: "nib-003", parentId: null, type: "epic" }, depth: 0 }),
      makeRow({ nib: { id: "nib-004", parentId: "nib-003", type: "task" }, depth: 1 }),
    ];
    const result = collectDescendantIds(["nib-001", "nib-003"], rows);
    expect(result.has("nib-002")).toBe(true);
    expect(result.has("nib-004")).toBe(true);
    expect(result.size).toBe(2);
  });

  it("returns empty set when no descendants", () => {
    const rows: RowData[] = [
      makeRow({ nib: { id: "nib-001", parentId: null, type: "task" }, depth: 0 }),
      makeRow({ nib: { id: "nib-002", parentId: null, type: "task" }, depth: 0 }),
    ];
    const result = collectDescendantIds(["nib-001"], rows);
    expect(result.size).toBe(0);
  });
});

describe("isValidCrossParentDrop", () => {
  it("allows any type at root level (null parent)", () => {
    expect(isValidCrossParentDrop(["milestone"], null)).toBe(true);
    expect(isValidCrossParentDrop(["task"], null)).toBe(true);
    expect(isValidCrossParentDrop(["epic", "task"], null)).toBe(true);
  });

  it("validates type hierarchy against parent type", () => {
    // epic can contain: feature, task, bug
    expect(isValidCrossParentDrop(["task"], "epic")).toBe(true);
    expect(isValidCrossParentDrop(["feature"], "epic")).toBe(true);
    expect(isValidCrossParentDrop(["milestone"], "epic")).toBe(false);
  });

  it("requires all dragged types to be valid children", () => {
    expect(isValidCrossParentDrop(["task", "bug"], "epic")).toBe(true);
    expect(isValidCrossParentDrop(["task", "epic"], "milestone")).toBe(false);
  });
});

