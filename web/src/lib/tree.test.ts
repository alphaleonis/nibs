import { describe, it, expect } from "vitest";
import { buildTree, buildViewTree } from "./tree";
import type { TreeNib, TreeTableNib } from "./types";

function makeTreeNib(overrides: Partial<TreeNib> = {}): TreeNib {
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
    ...overrides,
  };
}

function makeTreeTableNib(overrides: Partial<TreeTableNib> = {}): TreeTableNib {
  return {
    ...makeTreeNib(overrides),
    blockingIds: [],
    blockedByIds: [],
    ...overrides,
  };
}

describe("buildTree", () => {
  it("returns empty array for empty input", () => {
    const result = buildTree([]);
    expect(result).toEqual([]);
  });

  it("returns root nodes (no parent) at top level", () => {
    const nibs: TreeNib[] = [
      makeTreeNib({ id: "nibs-001", title: "Root task" }),
      makeTreeNib({ id: "nibs-002", title: "Another root" }),
    ];

    const result = buildTree(nibs);

    expect(result).toHaveLength(2);
    expect(result[0].nib.id).toBe("nibs-001");
    expect(result[0].depth).toBe(0);
    expect(result[0].children).toEqual([]);
    expect(result[1].nib.id).toBe("nibs-002");
    expect(result[1].depth).toBe(0);
    expect(result[1].children).toEqual([]);
  });

  it("nests children under their parent nodes", () => {
    const nibs: TreeNib[] = [
      makeTreeNib({ id: "nibs-001", title: "Epic", type: "epic" }),
      makeTreeNib({ id: "nibs-002", title: "Task under epic", type: "task", parentId: "nibs-001" }),
    ];

    const result = buildTree(nibs);

    expect(result).toHaveLength(1);
    expect(result[0].nib.id).toBe("nibs-001");
    expect(result[0].children).toHaveLength(1);
    expect(result[0].children[0].nib.id).toBe("nibs-002");
    expect(result[0].children[0].depth).toBe(1);
    expect(result[0].children[0].children).toEqual([]);
  });

  it("handles multi-level hierarchy (grandchildren)", () => {
    const nibs: TreeNib[] = [
      makeTreeNib({ id: "nibs-001", title: "Milestone", type: "milestone" }),
      makeTreeNib({ id: "nibs-002", title: "Epic", type: "epic", parentId: "nibs-001" }),
      makeTreeNib({ id: "nibs-003", title: "Task", type: "task", parentId: "nibs-002" }),
    ];

    const result = buildTree(nibs);

    expect(result).toHaveLength(1);
    expect(result[0].nib.id).toBe("nibs-001");
    expect(result[0].depth).toBe(0);

    const epic = result[0].children[0];
    expect(epic.nib.id).toBe("nibs-002");
    expect(epic.depth).toBe(1);

    const task = epic.children[0];
    expect(task.nib.id).toBe("nibs-003");
    expect(task.depth).toBe(2);
    expect(task.children).toEqual([]);
  });

  it("handles orphans (parentId references missing nib) as roots", () => {
    const nibs: TreeNib[] = [
      makeTreeNib({ id: "nibs-001", title: "Root" }),
      makeTreeNib({ id: "nibs-002", title: "Orphan", parentId: "nibs-missing" }),
    ];

    const result = buildTree(nibs);

    expect(result).toHaveLength(2);
    expect(result[0].nib.id).toBe("nibs-001");
    expect(result[0].depth).toBe(0);
    expect(result[1].nib.id).toBe("nibs-002");
    expect(result[1].depth).toBe(0);
  });

  it("computes correct depth when children appear before parents", () => {
    const nibs: TreeNib[] = [
      makeTreeNib({ id: "nibs-003", title: "Task", type: "task", parentId: "nibs-002" }),
      makeTreeNib({ id: "nibs-002", title: "Epic", type: "epic", parentId: "nibs-001" }),
      makeTreeNib({ id: "nibs-001", title: "Milestone", type: "milestone" }),
    ];

    const result = buildTree(nibs);

    expect(result).toHaveLength(1);
    expect(result[0].depth).toBe(0);
    expect(result[0].children[0].depth).toBe(1);
    expect(result[0].children[0].children[0].depth).toBe(2);
  });

  it("preserves input order among siblings", () => {
    const nibs: TreeNib[] = [
      makeTreeNib({ id: "nibs-parent", title: "Parent" }),
      makeTreeNib({ id: "nibs-c1", title: "First child", parentId: "nibs-parent" }),
      makeTreeNib({ id: "nibs-c2", title: "Second child", parentId: "nibs-parent" }),
      makeTreeNib({ id: "nibs-c3", title: "Third child", parentId: "nibs-parent" }),
    ];

    const result = buildTree(nibs);

    expect(result).toHaveLength(1);
    const children = result[0].children;
    expect(children).toHaveLength(3);
    expect(children[0].nib.id).toBe("nibs-c1");
    expect(children[1].nib.id).toBe("nibs-c2");
    expect(children[2].nib.id).toBe("nibs-c3");
  });
});

describe("buildViewTree", () => {
  describe("milestones view", () => {
    it("returns empty array when no milestones exist", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Standalone task", type: "task" }),
        makeTreeNib({ id: "nibs-002", title: "A bug", type: "bug" }),
      ];

      const result = buildViewTree(nibs, "milestones");
      expect(result).toHaveLength(0);
    });

    it("promotes milestones to roots with their full subtrees", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Milestone A", type: "milestone" }),
        makeTreeNib({ id: "nibs-002", title: "Epic under A", type: "epic", parentId: "nibs-001" }),
        makeTreeNib({ id: "nibs-003", title: "Task under epic", type: "task", parentId: "nibs-002" }),
        makeTreeNib({ id: "nibs-004", title: "Standalone task", type: "task" }),
      ];

      const result = buildViewTree(nibs, "milestones");

      // Only the milestone should be a root; standalone task is discarded
      expect(result).toHaveLength(1);
      expect(result[0].nib.id).toBe("nibs-001");
      expect(result[0].depth).toBe(0);

      // Subtree is preserved
      expect(result[0].children).toHaveLength(1);
      expect(result[0].children[0].nib.id).toBe("nibs-002");
      expect(result[0].children[0].depth).toBe(1);
      expect(result[0].children[0].children).toHaveLength(1);
      expect(result[0].children[0].children[0].nib.id).toBe("nibs-003");
      expect(result[0].children[0].children[0].depth).toBe(2);
    });

    it("keeps nested milestone inside parent milestone subtree (no duplication)", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Parent milestone", type: "milestone" }),
        makeTreeNib({ id: "nibs-002", title: "Child milestone", type: "milestone", parentId: "nibs-001" }),
        makeTreeNib({ id: "nibs-003", title: "Task under child", type: "task", parentId: "nibs-002" }),
      ];

      const result = buildViewTree(nibs, "milestones");

      // Only the outer milestone is a root
      expect(result).toHaveLength(1);
      expect(result[0].nib.id).toBe("nibs-001");

      // The child milestone stays in subtree
      expect(result[0].children).toHaveLength(1);
      expect(result[0].children[0].nib.id).toBe("nibs-002");
      expect(result[0].children[0].children).toHaveLength(1);
      expect(result[0].children[0].children[0].nib.id).toBe("nibs-003");
    });
  });

  describe("epics view", () => {
    it("promotes epics to roots with their subtrees, discarding milestones and standalone items", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Milestone", type: "milestone" }),
        makeTreeNib({ id: "nibs-002", title: "Epic A", type: "epic", parentId: "nibs-001" }),
        makeTreeNib({ id: "nibs-003", title: "Task under epic", type: "task", parentId: "nibs-002" }),
        makeTreeNib({ id: "nibs-004", title: "Standalone task", type: "task" }),
      ];

      const result = buildViewTree(nibs, "epics");

      // Only Epic A should be a root; milestone container and standalone task discarded
      expect(result).toHaveLength(1);
      expect(result[0].nib.id).toBe("nibs-002");
      expect(result[0].depth).toBe(0);
      expect(result[0].children).toHaveLength(1);
      expect(result[0].children[0].nib.id).toBe("nibs-003");
      expect(result[0].children[0].depth).toBe(1);
    });

    it("keeps nested epic inside parent epic subtree (no duplication)", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Parent epic", type: "epic" }),
        makeTreeNib({ id: "nibs-002", title: "Child epic", type: "epic", parentId: "nibs-001" }),
        makeTreeNib({ id: "nibs-003", title: "Task under child", type: "task", parentId: "nibs-002" }),
      ];

      const result = buildViewTree(nibs, "epics");

      // Only the outer epic is a root
      expect(result).toHaveLength(1);
      expect(result[0].nib.id).toBe("nibs-001");

      // The child epic stays in subtree
      expect(result[0].children).toHaveLength(1);
      expect(result[0].children[0].nib.id).toBe("nibs-002");
      expect(result[0].children[0].children).toHaveLength(1);
      expect(result[0].children[0].children[0].nib.id).toBe("nibs-003");
    });
  });

  describe("backlog view", () => {
    it("shows features/bugs as roots with only their task children", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Milestone", type: "milestone" }),
        makeTreeNib({ id: "nibs-002", title: "Epic", type: "epic", parentId: "nibs-001" }),
        makeTreeNib({ id: "nibs-003", title: "Feature A", type: "feature", parentId: "nibs-002" }),
        makeTreeNib({ id: "nibs-004", title: "Task 1", type: "task", parentId: "nibs-003" }),
        makeTreeNib({ id: "nibs-005", title: "Bug B", type: "bug" }),
        makeTreeNib({ id: "nibs-006", title: "Task 2", type: "task", parentId: "nibs-005" }),
      ];

      const result = buildViewTree(nibs, "backlog");

      // Feature A and Bug B should be roots
      expect(result).toHaveLength(2);
      expect(result[0].nib.id).toBe("nibs-003");
      expect(result[0].nib.type).toBe("feature");
      expect(result[0].depth).toBe(0);
      expect(result[0].children).toHaveLength(1);
      expect(result[0].children[0].nib.id).toBe("nibs-004");
      expect(result[0].children[0].depth).toBe(1);

      expect(result[1].nib.id).toBe("nibs-005");
      expect(result[1].nib.type).toBe("bug");
      expect(result[1].depth).toBe(0);
      expect(result[1].children).toHaveLength(1);
      expect(result[1].children[0].nib.id).toBe("nibs-006");
      expect(result[1].children[0].depth).toBe(1);
    });

    it("includes only task-type children under backlog roots, not nested features/bugs", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Feature A", type: "feature" }),
        makeTreeNib({ id: "nibs-002", title: "Task 1", type: "task", parentId: "nibs-001" }),
        makeTreeNib({ id: "nibs-003", title: "Sub-bug", type: "bug", parentId: "nibs-001" }),
        makeTreeNib({ id: "nibs-004", title: "Task under sub-bug", type: "task", parentId: "nibs-003" }),
      ];

      const result = buildViewTree(nibs, "backlog");

      // Feature A is a root with only Task 1 as child
      const featureA = result.find(r => r.nib.id === "nibs-001")!;
      expect(featureA).toBeDefined();
      expect(featureA.children).toHaveLength(1);
      expect(featureA.children[0].nib.id).toBe("nibs-002");

      // Sub-bug is also promoted to its own root with its task
      const subBug = result.find(r => r.nib.id === "nibs-003")!;
      expect(subBug).toBeDefined();
      expect(subBug.children).toHaveLength(1);
      expect(subBug.children[0].nib.id).toBe("nibs-004");
    });

    it("places orphaned tasks under a virtual 'Unparented' root node", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Feature A", type: "feature" }),
        makeTreeNib({ id: "nibs-002", title: "Task under feature", type: "task", parentId: "nibs-001" }),
        makeTreeNib({ id: "nibs-003", title: "Orphan task 1", type: "task" }),
        makeTreeNib({ id: "nibs-004", title: "Orphan task 2", type: "task" }),
      ];

      const result = buildViewTree(nibs, "backlog");

      // Feature A + Unparented group
      expect(result).toHaveLength(2);

      const featureA = result.find(r => r.nib.id === "nibs-001")!;
      expect(featureA).toBeDefined();
      expect(featureA.children).toHaveLength(1);

      const unparented = result.find(r => r.nib.id === "__unparented__")!;
      expect(unparented).toBeDefined();
      expect(unparented.nib.title).toBe("Unparented");
      expect(unparented.nib.type).toBe("");
      expect(unparented.depth).toBe(0);
      expect(unparented.children).toHaveLength(2);
      expect(unparented.children[0].nib.id).toBe("nibs-003");
      expect(unparented.children[0].depth).toBe(1);
      expect(unparented.children[1].nib.id).toBe("nibs-004");
      expect(unparented.children[1].depth).toBe(1);
    });

    it("virtual Unparented nib includes blockingIds and blockedByIds when T is TreeTableNib", () => {
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "nibs-001", title: "Orphan task", type: "task" }),
      ];

      const result = buildViewTree<TreeTableNib>(nibs, "backlog");

      // The virtual "Unparented" nib should have blockingIds and blockedByIds arrays
      const unparented = result.find(r => r.nib.id === "__unparented__")!;
      expect(unparented).toBeDefined();
      expect(unparented.nib.blockingIds).toEqual([]);
      expect(unparented.nib.blockedByIds).toEqual([]);
    });

    it("does not create Unparented node when all tasks have feature/bug parents", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Feature A", type: "feature" }),
        makeTreeNib({ id: "nibs-002", title: "Task 1", type: "task", parentId: "nibs-001" }),
      ];

      const result = buildViewTree(nibs, "backlog");

      expect(result).toHaveLength(1);
      expect(result[0].nib.id).toBe("nibs-001");
      expect(result.find(r => r.nib.id === "__unparented__")).toBeUndefined();
    });
  });
});
