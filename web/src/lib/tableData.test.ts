import { describe, it, expect } from "vitest";
import { buildTableData } from "./tableData";
import type { TreeTableNib, NibFilter, ViewLevel } from "./types";

function makeTreeTableNib(overrides: Partial<TreeTableNib> = {}): TreeTableNib {
  return {
    id: "nibs-abc1",
    title: "Fix login bug",
    status: "in-progress",
    type: "bug",
    priority: "high",
    estimate: "m",
    tags: ["auth"],
    updatedAt: "2026-03-20T10:00:00Z",
    parentId: null,
    blockingIds: [],
    blockedByIds: [],
    ...overrides,
  };
}

const emptyFilter: NibFilter = {};
const noCollapsed = new Set<string>();

describe("buildTableData", () => {
  it("returns empty rows, tags, and parentIds for empty input", () => {
    const result = buildTableData([], emptyFilter, "milestones", noCollapsed);

    expect(result.rows).toEqual([]);
    expect(result.allTags).toEqual([]);
    expect(result.parentIds).toEqual(new Set());
  });

  it("returns single row for single milestone with no filter", () => {
    const nib = makeTreeTableNib({ id: "nibs-001", type: "milestone" });
    const result = buildTableData([nib], emptyFilter, "milestones", noCollapsed);

    expect(result.rows).toHaveLength(1);
    expect(result.rows[0].nib.id).toBe("nibs-001");
    expect(result.rows[0].depth).toBe(0);
    expect(result.rows[0].hasChildren).toBe(false);
    expect(result.rows[0].dimmed).toBe(false);
    expect(result.rows[0].parentNib).toBeNull();
  });

  it("resolves parent-child hierarchy with correct depths, parentNib, and hasChildren", () => {
    const milestone = makeTreeTableNib({ id: "nibs-001", type: "milestone", title: "Milestone" });
    const task = makeTreeTableNib({ id: "nibs-002", type: "task", title: "Task", parentId: "nibs-001" });
    const result = buildTableData([milestone, task], emptyFilter, "milestones", noCollapsed);

    expect(result.rows).toHaveLength(2);

    // Parent row
    expect(result.rows[0].nib.id).toBe("nibs-001");
    expect(result.rows[0].depth).toBe(0);
    expect(result.rows[0].hasChildren).toBe(true);
    expect(result.rows[0].parentNib).toBeNull();

    // Child row
    expect(result.rows[1].nib.id).toBe("nibs-002");
    expect(result.rows[1].depth).toBe(1);
    expect(result.rows[1].hasChildren).toBe(false);
    expect(result.rows[1].parentNib?.id).toBe("nibs-001");
  });

  it("collects allTags sorted and deduplicated across all nibs", () => {
    const nibs = [
      makeTreeTableNib({ id: "nibs-001", type: "milestone", tags: ["frontend", "auth"] }),
      makeTreeTableNib({ id: "nibs-002", type: "milestone", tags: ["auth", "backend", "api"] }),
      makeTreeTableNib({ id: "nibs-003", type: "milestone", tags: [] }),
    ];
    const result = buildTableData(nibs, emptyFilter, "milestones", noCollapsed);

    expect(result.allTags).toEqual(["api", "auth", "backend", "frontend"]);
  });

  it("parentIds contains IDs of nibs that have children, not leaf IDs", () => {
    const nibs = [
      makeTreeTableNib({ id: "nibs-001", type: "milestone" }),
      makeTreeTableNib({ id: "nibs-002", type: "epic", parentId: "nibs-001" }),
      makeTreeTableNib({ id: "nibs-003", type: "task", parentId: "nibs-002" }),
    ];
    const result = buildTableData(nibs, emptyFilter, "milestones", noCollapsed);

    expect(result.parentIds.has("nibs-001")).toBe(true);
    expect(result.parentIds.has("nibs-002")).toBe(true);
    expect(result.parentIds.has("nibs-003")).toBe(false);
    expect(result.parentIds.size).toBe(2);
  });

  it("advanced filter: matching nibs visible, non-matching ancestors dimmed", () => {
    const nibs = [
      makeTreeTableNib({ id: "nibs-001", type: "milestone", title: "Milestone" }),
      makeTreeTableNib({ id: "nibs-002", type: "bug", title: "Bug", parentId: "nibs-001" }),
    ];
    const bugFilter: NibFilter = { type: ["bug"] };
    const result = buildTableData(nibs, bugFilter, "milestones", noCollapsed);

    expect(result.rows).toHaveLength(2);

    // Milestone is an ancestor of a match, so visible but dimmed
    expect(result.rows[0].nib.id).toBe("nibs-001");
    expect(result.rows[0].dimmed).toBe(true);

    // Bug matches the filter, not dimmed
    expect(result.rows[1].nib.id).toBe("nibs-002");
    expect(result.rows[1].dimmed).toBe(false);
  });

  it("advanced filter: non-ancestors completely hidden from rows", () => {
    const nibs = [
      makeTreeTableNib({ id: "nibs-001", type: "milestone", title: "Milestone A" }),
      makeTreeTableNib({ id: "nibs-002", type: "bug", title: "Bug", parentId: "nibs-001" }),
      makeTreeTableNib({ id: "nibs-003", type: "milestone", title: "Milestone B (unrelated)" }),
      makeTreeTableNib({ id: "nibs-004", type: "task", title: "Task under B", parentId: "nibs-003" }),
    ];
    const bugFilter: NibFilter = { type: ["bug"] };
    const result = buildTableData(nibs, bugFilter, "milestones", noCollapsed);

    // Only Milestone A (ancestor) and Bug (match) should be visible
    const ids = result.rows.map(r => r.nib.id);
    expect(ids).toContain("nibs-001");
    expect(ids).toContain("nibs-002");
    expect(ids).not.toContain("nibs-003");
    expect(ids).not.toContain("nibs-004");
    expect(result.rows).toHaveLength(2);
  });

  it("collapsed node: children absent from rows", () => {
    const nibs = [
      makeTreeTableNib({ id: "nibs-001", type: "milestone", title: "Milestone" }),
      makeTreeTableNib({ id: "nibs-002", type: "task", title: "Task", parentId: "nibs-001" }),
    ];
    const collapsed = new Set(["nibs-001"]);
    const result = buildTableData(nibs, emptyFilter, "milestones", collapsed);

    expect(result.rows).toHaveLength(1);
    expect(result.rows[0].nib.id).toBe("nibs-001");
    // The parent still reports hasChildren even when collapsed
    expect(result.rows[0].hasChildren).toBe(true);
  });

  describe("view levels", () => {
    const hierarchyNibs = [
      makeTreeTableNib({ id: "nibs-001", type: "milestone", title: "Milestone" }),
      makeTreeTableNib({ id: "nibs-002", type: "epic", title: "Epic", parentId: "nibs-001" }),
      makeTreeTableNib({ id: "nibs-003", type: "feature", title: "Feature", parentId: "nibs-002" }),
      makeTreeTableNib({ id: "nibs-004", type: "task", title: "Task", parentId: "nibs-003" }),
    ];

    it("milestones view: milestones as roots with full subtrees", () => {
      const result = buildTableData(hierarchyNibs, emptyFilter, "milestones", noCollapsed);

      expect(result.rows[0].nib.id).toBe("nibs-001");
      expect(result.rows[0].depth).toBe(0);
      expect(result.rows[0].nib.type).toBe("milestone");
      // All descendants present
      expect(result.rows).toHaveLength(4);
    });

    it("backlog view: features/bugs as roots with task children only", () => {
      const result = buildTableData(hierarchyNibs, emptyFilter, "backlog", noCollapsed);

      // Feature becomes root at depth 0, with Task as child at depth 1
      expect(result.rows).toHaveLength(2);
      expect(result.rows[0].nib.id).toBe("nibs-003");
      expect(result.rows[0].nib.type).toBe("feature");
      expect(result.rows[0].depth).toBe(0);
      expect(result.rows[1].nib.id).toBe("nibs-004");
      expect(result.rows[1].nib.type).toBe("task");
      expect(result.rows[1].depth).toBe(1);
    });
  });
});
