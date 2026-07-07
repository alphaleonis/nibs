import { describe, it, expect } from "vitest";
import { buildTableData } from "./tableData";
import { isBucketId } from "./tree";
import { typeRank } from "./typeHierarchy";
import type { TreeTableNib, NibFilter } from "./types";

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

    it("none view: full tree, nothing hidden", () => {
      const result = buildTableData(hierarchyNibs, emptyFilter, "none", noCollapsed);

      expect(result.rows.map(r => r.nib.id)).toEqual(["nibs-001", "nibs-002", "nibs-003", "nibs-004"]);
      expect(result.rows.map(r => r.depth)).toEqual([0, 1, 2, 3]);
    });

    it("milestones view: milestones as roots with full subtrees", () => {
      const result = buildTableData(hierarchyNibs, emptyFilter, "milestones", noCollapsed);

      expect(result.rows[0].nib.id).toBe("nibs-001");
      expect(result.rows[0].depth).toBe(0);
      expect(result.rows[0].nib.type).toBe("milestone");
      // All descendants present
      expect(result.rows).toHaveLength(4);
    });

    it("features view: features/bugs as headers with their full subtrees; milestone/epic rows hidden", () => {
      const result = buildTableData(hierarchyNibs, emptyFilter, "features", noCollapsed);

      // Feature becomes header at depth 0, with Task as child at depth 1
      expect(result.rows).toHaveLength(2);
      expect(result.rows[0].nib.id).toBe("nibs-003");
      expect(result.rows[0].nib.type).toBe("feature");
      expect(result.rows[0].depth).toBe(0);
      expect(result.rows[1].nib.id).toBe("nibs-004");
      expect(result.rows[1].nib.type).toBe("task");
      expect(result.rows[1].depth).toBe(1);
    });
  });

  describe("completeness invariant (messy hierarchy)", () => {
    // Every type at root and mis-nested (tier-skipping / orphaned), plus a dangling
    // parentId. No hierarchy inversions, so rank comparisons are well-defined.
    const messyFixture: TreeTableNib[] = [
      makeTreeTableNib({ id: "m1", type: "milestone" }),
      makeTreeTableNib({ id: "e1", type: "epic", parentId: "m1" }),
      makeTreeTableNib({ id: "f1", type: "feature", parentId: "e1" }),
      makeTreeTableNib({ id: "b1", type: "bug", parentId: "f1" }),
      makeTreeTableNib({ id: "t1", type: "task", parentId: "b1" }),
      makeTreeTableNib({ id: "r1", type: "research", parentId: "f1" }),
      makeTreeTableNib({ id: "r3", type: "research", parentId: "e1" }),
      makeTreeTableNib({ id: "b2", type: "bug", parentId: "m1" }),
      makeTreeTableNib({ id: "m2", type: "milestone" }),
      makeTreeTableNib({ id: "e2", type: "epic" }),
      makeTreeTableNib({ id: "t2", type: "task", parentId: "e2" }),
      makeTreeTableNib({ id: "f2", type: "feature" }),
      makeTreeTableNib({ id: "t3", type: "task", parentId: "f2" }),
      makeTreeTableNib({ id: "t4", type: "task" }),
      makeTreeTableNib({ id: "r2", type: "research" }),
      makeTreeTableNib({ id: "t5", type: "task", parentId: "missing-parent-xyz" }),
    ];

    // Derived from the single source of truth (typeRank), not hardcoded, so a
    // future TYPE_RANK change can't silently desync this completeness sweep.
    const lensRanks: Record<"milestones" | "epics" | "features", number> = {
      milestones: typeRank("milestone"),
      epics: typeRank("epic"),
      features: typeRank("feature"),
    };

    for (const lens of ["milestones", "epics", "features"] as const) {
      it(`${lens}: every rank<=gRank nib is a row exactly once; rank>gRank nibs are not rows`, () => {
        const gRank = lensRanks[lens];
        // Expand everything (no collapse) so all rows are emitted
        const result = buildTableData(messyFixture, emptyFilter, lens, noCollapsed);

        const rowIds = result.rows.map(r => r.nib.id).filter(id => !isBucketId(id));
        const counts = new Map<string, number>();
        for (const id of rowIds) counts.set(id, (counts.get(id) ?? 0) + 1);

        for (const nib of messyFixture) {
          if (typeRank(nib.type) <= gRank) {
            expect(counts.get(nib.id), `${nib.id} (${nib.type}) should be a row exactly once`).toBe(1);
          } else {
            expect(counts.get(nib.id), `${nib.id} (${nib.type}) should not be a row`).toBeUndefined();
          }
        }
      });
    }
  });

  describe("buckets", () => {
    // nibs-001 (loose feature) carries a nested task so the count assertion below
    // can distinguish direct-child counting (correct: 2) from recursive descendant
    // counting (would be 3).
    const looseNibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-001", type: "feature", title: "Loose feature" }),
      makeTreeTableNib({ id: "nibs-002", type: "bug", title: "Loose bug" }),
      makeTreeTableNib({ id: "nibs-003", type: "task", title: "Task under loose feature", parentId: "nibs-001" }),
    ];

    it("bucket row passes isBucketId, includes a count in its title, and is collapsible", () => {
      const expanded = buildTableData(looseNibs, emptyFilter, "epics", noCollapsed);

      const bucketRow = expanded.rows.find(r => isBucketId(r.nib.id))!;
      expect(bucketRow).toBeDefined();
      expect(bucketRow.nib.id).toBe("__no_epic__");
      expect(bucketRow.nib.title).toBe("No epic (2)");
      expect(bucketRow.hasChildren).toBe(true);
      // Expanded: bucket + its 2 direct children + the nested task under the feature
      expect(expanded.rows).toHaveLength(4);

      // The bucket is a collapsible container, so "Collapse All" (which uses
      // parentIds) must include it — otherwise the bucket stays expanded (#3).
      expect(expanded.parentIds.has("__no_epic__")).toBe(true);

      // Collapsing the bucket omits its children but still reports hasChildren
      const collapsed = buildTableData(looseNibs, emptyFilter, "epics", new Set(["__no_epic__"]));
      expect(collapsed.rows).toHaveLength(1);
      expect(collapsed.rows[0].nib.id).toBe("__no_epic__");
      expect(collapsed.rows[0].hasChildren).toBe(true);
    });
  });

  describe("advanced filter with grouping-lens buckets", () => {
    // E1(epic) → F1(feature) → T1(task) under an epic header; T2(task) is a loose
    // orphan that lands in the synthetic "No epic" bucket.
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "E1", type: "epic", title: "Epic" }),
      makeTreeTableNib({ id: "F1", type: "feature", title: "Feature", parentId: "E1" }),
      makeTreeTableNib({ id: "T1", type: "task", title: "Task under feature", parentId: "F1" }),
      makeTreeTableNib({ id: "T2", type: "task", title: "Orphan task" }),
    ];

    it("does not drop a matching item that lives inside a 'No X' bucket", () => {
      const result = buildTableData(nibs, { type: ["task"] }, "epics", noCollapsed);
      const ids = result.rows.map(r => r.nib.id);

      // Both matching tasks survive: T1 under the epic header, T2 in the bucket.
      expect(ids).toContain("T1");
      expect(ids).toContain("T2");
      // The synthetic bucket must be rendered as the container for T2.
      expect(ids.some(id => isBucketId(id))).toBe(true);
    });

    it("renders loose matches when the filter matches ONLY bucket items (table not empty)", () => {
      // Fixture where the ONLY filter-matching nib is loose (lands in the bucket)
      // and nothing sits under a header. Without the bucket being marked visible,
      // flatten() skips the bucket and its child, rendering an empty table.
      const looseOnly = buildTableData(
        [makeTreeTableNib({ id: "T2", type: "task", title: "Orphan task" })],
        { type: ["task"] },
        "epics",
        noCollapsed,
      );
      const looseIds = looseOnly.rows.map(r => r.nib.id);
      expect(looseIds).toContain("T2");
      expect(looseIds.some(id => isBucketId(id))).toBe(true);
    });
  });
});
