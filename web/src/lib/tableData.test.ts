import { describe, it, expect } from "vitest";
import { buildTableData } from "./tableData";
import { isBucketId } from "./tree";
import { applySort } from "./tableSort";
import { typeRank } from "./typeHierarchy";
import { OPEN_PLUS_DEFERRED_STATUSES } from "./constants";
import type { TreeTableNib, NibFilter, TableSort } from "./types";

function makeTreeTableNib(overrides: Partial<TreeTableNib> = {}): TreeTableNib {
  return {
    id: "nibs-abc1",
    title: "Fix login bug",
    status: "in-progress",
    type: "bug",
    priority: "high",
    estimate: "m",
    tags: ["auth"],
    createdAt: "2026-03-15T10:00:00Z",
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

  // The "hide completed" behavior is now expressed as the "Open + deferred" status
  // include-list (everything except completed + scrapped), not a separate
  // excludeStatus field. A non-matching ancestor of an active child is
  // still dimmed in place rather than dropped.
  describe("status include-list client filter (hide completed)", () => {
    it("dims a completed parent with an active child instead of dropping it", () => {
      const nibs = [
        makeTreeTableNib({ id: "nibs-001", type: "milestone", status: "completed", title: "Done milestone" }),
        makeTreeTableNib({ id: "nibs-002", type: "task", status: "in-progress", title: "Active task", parentId: "nibs-001" }),
      ];
      const filter: NibFilter = { status: [...OPEN_PLUS_DEFERRED_STATUSES] };
      const result = buildTableData(nibs, filter, "milestones", noCollapsed);

      // Both rows present: the excluded (completed) parent survives as a dimmed
      // ancestor of its active child, keeping the child visible.
      expect(result.rows).toHaveLength(2);

      const parent = result.rows.find(r => r.nib.id === "nibs-001")!;
      const child = result.rows.find(r => r.nib.id === "nibs-002")!;
      expect(parent).toBeDefined();
      expect(parent.dimmed).toBe(true);
      expect(child).toBeDefined();
      expect(child.dimmed).toBe(false);
    });

    it("hides a completed leaf with no active descendants", () => {
      const nibs = [
        makeTreeTableNib({ id: "nibs-001", type: "milestone", status: "in-progress", title: "Active milestone" }),
        makeTreeTableNib({ id: "nibs-002", type: "task", status: "completed", title: "Done leaf", parentId: "nibs-001" }),
      ];
      const filter: NibFilter = { status: [...OPEN_PLUS_DEFERRED_STATUSES] };
      const result = buildTableData(nibs, filter, "milestones", noCollapsed);

      const ids = result.rows.map(r => r.nib.id);
      expect(ids).toContain("nibs-001");
      expect(ids).not.toContain("nibs-002");

      // The active milestone matches the filter directly, so it is not dimmed.
      const parent = result.rows.find(r => r.nib.id === "nibs-001")!;
      expect(parent.dimmed).toBe(false);
    });

    it("epics lens: dims completed epic, keeps active child, never dims the bucket header", () => {
      const nibs = [
        makeTreeTableNib({ id: "E1", type: "epic", status: "completed", title: "Done epic" }),
        makeTreeTableNib({ id: "T1", type: "task", status: "in-progress", title: "Active task under epic", parentId: "E1" }),
        // A loose active task with no epic ancestor lands in the synthetic
        // "No epic" bucket, so a bucket header row is emitted.
        makeTreeTableNib({ id: "T2", type: "task", status: "in-progress", title: "Loose active task" }),
      ];
      const filter: NibFilter = { status: [...OPEN_PLUS_DEFERRED_STATUSES] };
      const result = buildTableData(nibs, filter, "epics", noCollapsed);

      // Completed epic survives as a dimmed ancestor of its active child.
      const epic = result.rows.find(r => r.nib.id === "E1")!;
      expect(epic).toBeDefined();
      expect(epic.dimmed).toBe(true);

      // Active child is visible and not dimmed.
      const child = result.rows.find(r => r.nib.id === "T1")!;
      expect(child).toBeDefined();
      expect(child.dimmed).toBe(false);

      // The synthetic "No epic" bucket header is a structural container, never a
      // real nib in matchingIds, so it must never be dimmed. (RED before the
      // flatten isBucketId guard: bucket ids are absent from matchingIds, so the
      // old `!matchingIds.has(id)` marked this row dimmed:true.)
      const bucket = result.rows.find(r => isBucketId(r.nib.id));
      expect(bucket).toBeDefined();
      expect(bucket!.dimmed).toBe(false);
    });
  });

  // The `-status:completed` negation routes to NibFilter.excludeStatus. Like the
  // positive status include-list, the exclusion is applied CLIENT-side so an
  // excluded ancestor of active children is kept + dimmed rather than dropped
  // server-side (which would orphan the children and detach the tree). Reverting
  // the filter.ts change (excludeStatus removed from CLIENT_FIELDS / matchesFilter
  // / hasClientFilters) makes hasClientFilters return false here, so Stage-4
  // dimming never runs and `parent.dimmed` is false — this test bites.
  describe("excludeStatus client filter (`-status:completed` negation)", () => {
    it("dims an excluded (completed) parent with an active child, keeping the child nested", () => {
      const nibs = [
        makeTreeTableNib({ id: "nibs-001", type: "milestone", status: "completed", title: "Done milestone" }),
        makeTreeTableNib({ id: "nibs-002", type: "task", status: "in-progress", title: "Active task", parentId: "nibs-001" }),
      ];
      const filter: NibFilter = { excludeStatus: ["completed"] };
      const result = buildTableData(nibs, filter, "milestones", noCollapsed);

      // The excluded parent survives as a dimmed ancestor of its active child,
      // rather than being dropped (which would re-root the orphaned child).
      expect(result.rows).toHaveLength(2);

      const parent = result.rows.find(r => r.nib.id === "nibs-001")!;
      const child = result.rows.find(r => r.nib.id === "nibs-002")!;
      expect(parent).toBeDefined();
      expect(parent.dimmed).toBe(true);
      // Tree stays attached: the child is still nested under (not promoted above)
      // its dimmed parent.
      expect(child).toBeDefined();
      expect(child.dimmed).toBe(false);
      expect(child.depth).toBe(1);
      expect(child.parentNib?.id).toBe("nibs-001");
    });

    it("hides an excluded leaf with no active descendants", () => {
      const nibs = [
        makeTreeTableNib({ id: "nibs-001", type: "milestone", status: "in-progress", title: "Active milestone" }),
        makeTreeTableNib({ id: "nibs-002", type: "task", status: "completed", title: "Done leaf", parentId: "nibs-001" }),
      ];
      const filter: NibFilter = { excludeStatus: ["completed"] };
      const result = buildTableData(nibs, filter, "milestones", noCollapsed);

      const ids = result.rows.map(r => r.nib.id);
      expect(ids).toContain("nibs-001");
      expect(ids).not.toContain("nibs-002");
      // The active milestone dodges the exclusion, so it is not dimmed.
      expect(result.rows.find(r => r.nib.id === "nibs-001")!.dimmed).toBe(false);
    });
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

    it("never dims the 'No X' bucket header under an active client filter", () => {
      // A type filter (a real client filter) is active, so Stage-4 dimming runs.
      // T2 (matching) lands in the "No epic" bucket. The bucket is a structural
      // container, never a real nib in matchingIds, so it must not be dimmed.
      const result = buildTableData(nibs, { type: ["task"] }, "epics", noCollapsed);
      const bucket = result.rows.find(r => isBucketId(r.nib.id));
      expect(bucket).toBeDefined();
      expect(bucket!.dimmed).toBe(false);
    });
  });

  describe("flat view + client filter (no ancestor context)", () => {
    // Flat puts every nib at depth 0 with no nesting, so a non-matching ancestor
    // pulled in "for context" would render as a stray, unindented dimmed row with
    // no visual link to the match that caused it. The ancestor walk must be gated
    // off in flat view.
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "E1", type: "epic", title: "Epic" }),
      makeTreeTableNib({ id: "T1", type: "task", title: "Task", parentId: "E1" }),
    ];

    it("renders ONLY the matching rows — no dimmed ancestor", () => {
      const result = buildTableData(nibs, { type: ["task"] }, "flat", noCollapsed);
      const ids = result.rows.map(r => r.nib.id);

      expect(ids).toEqual(["T1"]);
      expect(ids).not.toContain("E1");
      // The one visible row is the direct match, never dimmed.
      expect(result.rows[0].dimmed).toBe(false);
    });

    it("with no client filter, flat still shows every nib as an ungrouped depth-0 root", () => {
      const result = buildTableData(nibs, emptyFilter, "flat", noCollapsed);
      const ids = result.rows.map(r => r.nib.id);

      expect(ids).toContain("E1");
      expect(ids).toContain("T1");
      expect(result.rows.every(r => r.depth === 0)).toBe(true);
      expect(result.rows.every(r => !r.dimmed)).toBe(true);
    });

    it("tree (none) view still keeps the non-matching ancestor as a dimmed row", () => {
      // The flat gate must NOT change nested views: the ancestor context that
      // makes a matching descendant reachable in a tree stays intact.
      const result = buildTableData(nibs, { type: ["task"] }, "none", noCollapsed);

      const epic = result.rows.find(r => r.nib.id === "E1")!;
      const task = result.rows.find(r => r.nib.id === "T1")!;
      expect(epic).toBeDefined();
      expect(epic.dimmed).toBe(true);
      expect(task).toBeDefined();
      expect(task.dimmed).toBe(false);
    });

    it("grouping lens (epics) + filter: bucket/ancestor handling unchanged (loose match still rendered under its bucket)", () => {
      // A grouping lens is not flat: its buckets and ancestor context are
      // preserved. A loose matching task still surfaces under its "No epic" bucket.
      const loose: TreeTableNib[] = [
        makeTreeTableNib({ id: "E1", type: "epic", title: "Epic" }),
        makeTreeTableNib({ id: "T1", type: "task", title: "Task under epic", parentId: "E1" }),
        makeTreeTableNib({ id: "T2", type: "task", title: "Orphan task" }),
      ];
      const result = buildTableData(loose, { type: ["task"] }, "epics", noCollapsed);
      const ids = result.rows.map(r => r.nib.id);

      expect(ids).toContain("T1");
      expect(ids).toContain("T2");
      expect(ids.some(id => isBucketId(id))).toBe(true);
      // Epic ancestor of the matching T1 stays as a dimmed context row.
      const epic = result.rows.find(r => r.nib.id === "E1")!;
      expect(epic).toBeDefined();
      expect(epic.dimmed).toBe(true);
    });
  });

  describe("displayParentId (view-tree display position)", () => {
    it("none lens: root has null display parent, child points to its parent id", () => {
      const nibs = [
        makeTreeTableNib({ id: "nibs-001", type: "milestone", title: "Root" }),
        makeTreeTableNib({ id: "nibs-002", type: "task", title: "Child", parentId: "nibs-001" }),
      ];
      const result = buildTableData(nibs, emptyFilter, "none", noCollapsed);

      const root = result.rows.find(r => r.nib.id === "nibs-001")!;
      const child = result.rows.find(r => r.nib.id === "nibs-002")!;
      expect(root.displayParentId).toBeNull();
      expect(child.displayParentId).toBe("nibs-001");
    });

    it("none lens: a dangling-parent item re-roots (displayParentId null, nib.parentId non-null)", () => {
      // Real parent id points at a nib not in the set, so buildTree makes it a
      // root. Its DISPLAY parent is null even though nib.parentId is set.
      const nibs = [
        makeTreeTableNib({ id: "nibs-001", type: "task", title: "Orphan", parentId: "missing-xyz" }),
      ];
      const result = buildTableData(nibs, emptyFilter, "none", noCollapsed);

      const orphan = result.rows.find(r => r.nib.id === "nibs-001")!;
      expect(orphan.nib.parentId).toBe("missing-xyz");
      expect(orphan.displayParentId).toBeNull();
    });

    describe("grouping lens (epics) reparenting", () => {
      // M1(milestone, above tier → hidden) → E1(epic) → F1(feature). T1 is a loose
      // task that lands in the synthetic "No epic" bucket.
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "M1", type: "milestone", title: "Milestone" }),
        makeTreeTableNib({ id: "E1", type: "epic", title: "Epic", parentId: "M1" }),
        makeTreeTableNib({ id: "F1", type: "feature", title: "Feature", parentId: "E1" }),
        makeTreeTableNib({ id: "T1", type: "task", title: "Loose task" }),
      ];

      it("promoted header re-roots: displayParentId null though nib.parentId is set (the crux)", () => {
        const result = buildTableData(nibs, emptyFilter, "epics", noCollapsed);

        // E1's real parent M1 is hidden by the lens, so E1 is promoted to a
        // top-level header: its DISPLAY parent is root even though nib.parentId
        // still points at the hidden milestone.
        const header = result.rows.find(r => r.nib.id === "E1")!;
        expect(header.nib.parentId).toBe("M1");
        expect(header.displayParentId).toBeNull();
      });

      it("a child under a promoted header points to that header", () => {
        const result = buildTableData(nibs, emptyFilter, "epics", noCollapsed);

        const child = result.rows.find(r => r.nib.id === "F1")!;
        expect(child.displayParentId).toBe("E1");
      });

      it("a loose bucket item inherits the bucket's own display parent (null), never the synthetic bucket id", () => {
        const result = buildTableData(nibs, emptyFilter, "epics", noCollapsed);

        // The bucket itself re-roots to null (it is a top-level display node).
        const bucket = result.rows.find(r => isBucketId(r.nib.id))!;
        expect(bucket).toBeDefined();
        expect(bucket.displayParentId).toBeNull();

        // RowData.displayParentId invariant: NEVER a synthetic bucket id. A loose
        // bucket item inherits the bucket's OWN display parent (null here) rather
        // than the unusable bucket id, so consumers can use it directly as a
        // backend parentId without an isBucketId guard.
        const item = result.rows.find(r => r.nib.id === "T1")!;
        expect(item.displayParentId).toBeNull();
        expect(isBucketId(item.displayParentId ?? "")).toBe(false);
      });

      it("two loose siblings in one bucket resolve to the identical display parent (symmetric property)", () => {
        // Two loose tasks with no grouping ancestor both fall into the single
        // "No epic" bucket. flatten() threads ONE displayParentId to every child
        // of a node, so both siblings get the identical value (null — the bucket's
        // OWN display parent). This is the structural home of the symmetric
        // same-container property: a reorder between two
        // such siblings reads sourceParentId === targetParentId → a same-parent
        // reorder, never a re-root. The EQUAL assertion pins that symmetric
        // property (both siblings share one container); the toBeNull assertion is
        // the one that catches a producer-fix revert — under a revert both would
        // still be EQUAL, but at the synthetic bucket id, so equality alone is not
        // the discriminating check.
        const twoLoose: TreeTableNib[] = [
          makeTreeTableNib({ id: "L1", type: "task", title: "Loose task 1" }),
          makeTreeTableNib({ id: "L2", type: "task", title: "Loose task 2" }),
        ];
        const result = buildTableData(twoLoose, emptyFilter, "epics", noCollapsed);

        const l1 = result.rows.find(r => r.nib.id === "L1")!;
        const l2 = result.rows.find(r => r.nib.id === "L2")!;
        expect(l1.displayParentId).toBe(l2.displayParentId);
        expect(l1.displayParentId).toBeNull();
      });
    });
  });
});

// The load-bearing premise of nibs-6grg: sorting `allNibs` BEFORE buildTableData
// yields sibling-sort in the nested views and a flat sorted list in Flat, because
// buildTree/buildViewTree preserve sibling input order within each group. These
// tests exercise that seam directly (applySort → buildTableData), independent of
// the Svelte component.
describe("buildTableData — sibling-sort from a pre-sorted array", () => {
  // Two milestone roots (input Z-before-A), one carrying two child tasks (input
  // Zeta-before-Alpha). A global-flat title order would be Alpha, Root A, Root Z,
  // Zeta — interleaving a child ahead of a root. Sibling-sort must instead keep
  // children nested under their parent while ordering each sibling group.
  const nestedRoots: TreeTableNib[] = [
    makeTreeTableNib({ id: "m2", title: "Root Z", type: "milestone" }),
    makeTreeTableNib({ id: "m1", title: "Root A", type: "milestone" }),
    makeTreeTableNib({ id: "c2", title: "Zeta", type: "task", parentId: "m1" }),
    makeTreeTableNib({ id: "c1", title: "Alpha", type: "task", parentId: "m1" }),
  ];

  it("none view: roots AND children reorder by the field, nesting/depths preserved", () => {
    const sorted = applySort(nestedRoots, { field: "title", direction: "asc" });
    const result = buildTableData(sorted, emptyFilter, "none", noCollapsed);

    expect(result.rows.map((r) => r.nib.id)).toEqual(["m1", "c1", "c2", "m2"]);
    // Structure intact: roots at depth 0, the two children nested at depth 1.
    expect(result.rows.map((r) => r.depth)).toEqual([0, 1, 1, 0]);
    expect(result.rows.find((r) => r.nib.id === "c1")!.parentNib?.id).toBe("m1");
    expect(result.rows.find((r) => r.nib.id === "c2")!.parentNib?.id).toBe("m1");
  });

  it("none view: an UNSORTED array keeps the manual input order (control for the sort)", () => {
    const result = buildTableData(nestedRoots, emptyFilter, "none", noCollapsed);
    // No sort applied → input order, nested.
    expect(result.rows.map((r) => r.nib.id)).toEqual(["m2", "m1", "c2", "c1"]);
  });

  it("flat view: a pre-sorted array yields a flat sorted list (every row depth 0)", () => {
    const sorted = applySort(nestedRoots, { field: "title", direction: "asc" });
    const result = buildTableData(sorted, emptyFilter, "flat", noCollapsed);
    // Flat has no nesting: pure title order, all depth 0.
    expect(result.rows.map((r) => r.nib.id)).toEqual(["c1", "m1", "m2", "c2"]);
    expect(result.rows.every((r) => r.depth === 0)).toBe(true);
  });

  it("grouping lens (milestones): promoted headers AND bucket items both reorder", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "m2", title: "M-Zulu", type: "milestone" }),
      makeTreeTableNib({ id: "m1", title: "M-Alpha", type: "milestone" }),
      makeTreeTableNib({ id: "t2", title: "T-Zulu", type: "task" }),
      makeTreeTableNib({ id: "t1", title: "T-Alpha", type: "task" }),
    ];
    const sorted = applySort(nibs, { field: "title", direction: "asc" });
    const result = buildTableData(sorted, emptyFilter, "milestones", noCollapsed);

    // Promoted milestone headers reorder (M-Alpha before M-Zulu)...
    expect(result.rows[0].nib.id).toBe("m1");
    expect(result.rows[1].nib.id).toBe("m2");
    // ...then the synthetic "No milestone" bucket...
    expect(isBucketId(result.rows[2].nib.id)).toBe(true);
    // ...whose loose items also reorder (T-Alpha before T-Zulu).
    expect(result.rows[3].nib.id).toBe("t1");
    expect(result.rows[4].nib.id).toBe("t2");
  });
});

// nibs-2lqm: in the epics/features lenses the pre-sort of `allNibs` alone is NOT
// enough — promoted headers descend through a HIDDEN higher-tier ancestor, so
// they come out grouped by that ancestor's position. Threading the active sort
// into buildTableData (→ buildViewTree's node comparator) orders them GLOBALLY.
// When no sort is passed, the grouped order is preserved (the guard bites).
describe("buildTableData — global promoted-header ordering under an active sort (nibs-2lqm)", () => {
  const titleAsc: TableSort = { field: "title", direction: "asc" };
  const titleDesc: TableSort = { field: "title", direction: "desc" };

  describe("epics lens: two epics under DIFFERENT hidden milestones", () => {
    // Even after pre-sorting by title, the milestones sort ahead and each epic
    // trails its milestone, so DFS yields the GROUPED order [Zebra, Apple].
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "m1", title: "Mmm1", type: "milestone" }),
      makeTreeTableNib({ id: "eZ", title: "Zebra", type: "epic", parentId: "m1" }),
      makeTreeTableNib({ id: "m2", title: "Mmm2", type: "milestone" }),
      makeTreeTableNib({ id: "eA", title: "Apple", type: "epic", parentId: "m2" }),
    ];

    it("no sort arg → grouped DFS header order [Zebra, Apple] (control for the bug)", () => {
      const sorted = applySort(nibs, titleAsc);
      const result = buildTableData(sorted, emptyFilter, "epics", noCollapsed);
      expect(result.rows.map((r) => r.nib.id)).toEqual(["eZ", "eA"]);
    });

    it("active sort → GLOBAL header order [Apple, Zebra]", () => {
      const sorted = applySort(nibs, titleAsc);
      const result = buildTableData(sorted, emptyFilter, "epics", noCollapsed, titleAsc);
      expect(result.rows.map((r) => r.nib.id)).toEqual(["eA", "eZ"]);
    });

    it("active sort desc → GLOBAL header order reverses [Zebra, Apple]", () => {
      const sorted = applySort(nibs, titleDesc);
      const result = buildTableData(sorted, emptyFilter, "epics", noCollapsed, titleDesc);
      expect(result.rows.map((r) => r.nib.id)).toEqual(["eZ", "eA"]);
    });
  });

  describe("features lens: two features under DIFFERENT hidden epics", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "e1", title: "Eee1", type: "epic" }),
      makeTreeTableNib({ id: "fZ", title: "Zebra", type: "feature", parentId: "e1" }),
      makeTreeTableNib({ id: "e2", title: "Eee2", type: "epic" }),
      makeTreeTableNib({ id: "fA", title: "Apple", type: "feature", parentId: "e2" }),
    ];

    it("no sort arg → grouped DFS header order [Zebra, Apple] (control)", () => {
      const sorted = applySort(nibs, titleAsc);
      const result = buildTableData(sorted, emptyFilter, "features", noCollapsed);
      expect(result.rows.map((r) => r.nib.id)).toEqual(["fZ", "fA"]);
    });

    it("active sort → GLOBAL header order [Apple, Zebra]", () => {
      const sorted = applySort(nibs, titleAsc);
      const result = buildTableData(sorted, emptyFilter, "features", noCollapsed, titleAsc);
      expect(result.rows.map((r) => r.nib.id)).toEqual(["fA", "fZ"]);
    });

    it("active sort desc → GLOBAL header order reverses [Zebra, Apple]", () => {
      // The titleDesc pre-sort reshuffles the DFS grouping to [fA, fZ]; only the
      // threaded desc re-sort corrects it to [fZ, fA] (bites on revert).
      const sorted = applySort(nibs, titleDesc);
      const result = buildTableData(sorted, emptyFilter, "features", noCollapsed, titleDesc);
      expect(result.rows.map((r) => r.nib.id)).toEqual(["fZ", "fA"]);
    });
  });

  describe("bucket items from DISTINCT hidden parents also globally order", () => {
    // Two loose tasks under two different milestones → the "No epic" bucket.
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "m1", title: "Mmm1", type: "milestone" }),
      makeTreeTableNib({ id: "tZ", title: "Zebra", type: "task", parentId: "m1" }),
      makeTreeTableNib({ id: "m2", title: "Mmm2", type: "milestone" }),
      makeTreeTableNib({ id: "tA", title: "Apple", type: "task", parentId: "m2" }),
    ];

    it("no sort arg → grouped DFS bucket-item order [Zebra, Apple] (control)", () => {
      const sorted = applySort(nibs, titleAsc);
      const result = buildTableData(sorted, emptyFilter, "epics", noCollapsed);
      const itemIds = result.rows
        .filter((r) => !isBucketId(r.nib.id))
        .map((r) => r.nib.id);
      expect(itemIds).toEqual(["tZ", "tA"]);
    });

    it("active sort → bucket items globally ordered [Apple, Zebra]", () => {
      const sorted = applySort(nibs, titleAsc);
      const result = buildTableData(sorted, emptyFilter, "epics", noCollapsed, titleAsc);
      // Rows: the "No epic" bucket header, then its two items in global order.
      const bucket = result.rows.find((r) => isBucketId(r.nib.id))!;
      expect(bucket).toBeDefined();
      const itemIds = result.rows
        .filter((r) => !isBucketId(r.nib.id))
        .map((r) => r.nib.id);
      expect(itemIds).toEqual(["tA", "tZ"]);
    });
  });

  describe("epics lens: headers sort by their hidden parent's title (parent field / byId path)", () => {
    // `parent` is the only extractor that resolves via byId.get(parentId).title —
    // the title tests never touch it. Each epic's real parent is a HIDDEN
    // milestone, so a parent-field sort must order the promoted headers by their
    // milestone-parent's title, exercising the buildViewTree re-sort's nibMap→byId
    // lookup. A regression in that map construction would leave the headers in
    // grouped DFS order [eA, eZ] and fail this test.
    const parentAsc: TableSort = { field: "parent", direction: "asc" };
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "m1", title: "M-Zebra", type: "milestone" }),
      makeTreeTableNib({ id: "eA", title: "Apple", type: "epic", parentId: "m1" }),
      makeTreeTableNib({ id: "m2", title: "M-Apple", type: "milestone" }),
      makeTreeTableNib({ id: "eZ", title: "Zebra", type: "epic", parentId: "m2" }),
    ];

    it("active parent sort → headers ordered by hidden parent title [eZ, eA]", () => {
      const sorted = applySort(nibs, parentAsc);
      const result = buildTableData(sorted, emptyFilter, "epics", noCollapsed, parentAsc);
      // eZ's parent "M-Apple" sorts before eA's parent "M-Zebra".
      expect(result.rows.map((r) => r.nib.id)).toEqual(["eZ", "eA"]);
    });
  });

  it("milestones lens: the sort param reorders an UNSORTED array (wiring check)", () => {
    // Feed a raw, unsorted array straight into buildTableData (no applySort). The
    // milestone headers and the "No milestone" bucket's loose items come out of
    // `classify` in raw input order [m2, m1] / [t2, t1]; only the threaded sort
    // re-sorts them to [m1, m2] / [t1, t2]. This FAILS if `sort` isn't threaded.
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "m2", title: "M-Zulu", type: "milestone" }),
      makeTreeTableNib({ id: "m1", title: "M-Alpha", type: "milestone" }),
      makeTreeTableNib({ id: "t2", title: "T-Zulu", type: "task" }),
      makeTreeTableNib({ id: "t1", title: "T-Alpha", type: "task" }),
    ];
    const result = buildTableData(nibs, emptyFilter, "milestones", noCollapsed, titleAsc);
    expect(result.rows.map((r) => r.nib.id)).toEqual(["m1", "m2", "__no_milestone__", "t1", "t2"]);
  });
});
