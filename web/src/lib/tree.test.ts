import { describe, it, expect } from "vitest";
import { buildTree, buildViewTree, buildShapedViewTree, viewShapeFor, isSyntheticRowId, holdsChildrenByDisplay, containingSectionRowId, collectDescendantIds, BUCKET_IDS } from "./tree";
import type { GroupingLens, Placement, ViewShape } from "./tree";
import { makeNibComparator } from "./tableSort";
import { milestoneOf, resolvedMilestoneId } from "./membership";
import { typeRank } from "./typeHierarchy";
import type { TreeNib, TreeTableNib, TreeNode, ViewLevel, TableSort } from "./types";
import { VIEW_LEVELS } from "./types";

function makeTreeNib(overrides: Partial<TreeNib> = {}): TreeNib {
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
    milestone: "",
    milestoneOrder: "",
    ...overrides,
  };
}

function makeTreeTableNib(overrides: Partial<TreeTableNib> = {}): TreeTableNib {
  return {
    ...makeTreeNib(overrides),
    blockingIds: [],
    etag: "etag-test",
    blockedByIds: [],
    ...overrides,
  };
}

/** Collect all nib ids appearing anywhere in a forest (depth-first). */
function collectIds<T extends TreeNib>(nodes: TreeNode<T>[]): string[] {
  const ids: string[] = [];
  for (const node of nodes) {
    ids.push(node.nib.id);
    ids.push(...collectIds(node.children));
  }
  return ids;
}

/**
 * A deliberately messy hierarchy: every type present at root and mis-nested
 * (tier-skipping and orphaned), plus a nib whose parentId points to a missing
 * nib. No hierarchy inversions (a container is never nested under a lower tier),
 * so the completeness invariant below is well-defined.
 */
const MESSY_FIXTURE: TreeNib[] = [
  makeTreeNib({ id: "m1", type: "milestone" }),
  makeTreeNib({ id: "e1", type: "epic", parentId: "m1" }),
  makeTreeNib({ id: "f1", type: "feature", parentId: "e1" }),
  makeTreeNib({ id: "b1", type: "bug", parentId: "f1" }),
  makeTreeNib({ id: "t1", type: "task", parentId: "b1" }),
  makeTreeNib({ id: "r1", type: "research", parentId: "f1" }),
  makeTreeNib({ id: "r3", type: "research", parentId: "e1" }),
  makeTreeNib({ id: "b2", type: "bug", parentId: "m1" }), // bug directly under milestone (tier skip)
  makeTreeNib({ id: "m2", type: "milestone" }),            // empty milestone
  makeTreeNib({ id: "e2", type: "epic" }),                 // orphan epic at root
  makeTreeNib({ id: "t2", type: "task", parentId: "e2" }), // task directly under epic
  makeTreeNib({ id: "f2", type: "feature" }),              // orphan feature at root
  makeTreeNib({ id: "t3", type: "task", parentId: "f2" }),
  makeTreeNib({ id: "t4", type: "task" }),                 // orphan task at root
  makeTreeNib({ id: "r2", type: "research" }),             // orphan research at root
  makeTreeNib({ id: "t5", type: "task", parentId: "missing-parent-xyz" }), // dangling parent
];

// Expected grouping-tier ranks, derived from the single source of truth
// (typeRank) rather than frozen literals — so a future TYPE_RANK change that
// desyncs the lens boundaries would fail these tests instead of passing silently.
const GROUPING_LENS_RANKS: Record<Exclude<ViewLevel, "none" | "flat">, number> = {
  milestones: typeRank("milestone"),
  epics: typeRank("epic"),
  features: typeRank("feature"),
};

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

  // A parent cycle used to erase every member: each one has a parent present in
  // the node map, so the "root = no parent, or parent missing" rule matched none
  // of them and the whole cycle vanished. One member is promoted to a root
  // instead — the same rule the Go builder (internal/ui/tree.go) applies, over
  // the same fixtures, so a divergence fails one of the two suites.
  describe("parent cycles degrade to a visible oddity", () => {
    /** Flattens a forest depth-first into "depth:id" entries. */
    function shape<T extends TreeNib>(nodes: TreeNode<T>[]): string[] {
      const out: string[] = [];
      const walk = (ns: TreeNode<T>[], depth: number) => {
        for (const n of ns) {
          out.push(`${depth}:${n.nib.id}`);
          walk(n.children, depth + 1);
        }
      };
      walk(nodes, 0);
      return out;
    }

    const cases: { name: string; nibs: TreeNib[]; want: string[] }[] = [
      {
        name: "self cycle renders once",
        nibs: [makeTreeNib({ id: "a", parentId: "a" })],
        want: ["0:a"],
      },
      {
        name: "two-cycle renders both members",
        nibs: [
          makeTreeNib({ id: "a", parentId: "b" }),
          makeTreeNib({ id: "b", parentId: "a" }),
        ],
        want: ["0:a", "1:b"],
      },
      {
        // The lowest id is promoted, not the first one seen: "m" is second in
        // the input and still becomes the root.
        name: "promotion picks the lowest id, not the input order",
        nibs: [
          makeTreeNib({ id: "z", parentId: "m" }),
          makeTreeNib({ id: "m", parentId: "z" }),
        ],
        want: ["0:m", "1:z"],
      },
      {
        name: "three-cycle renders all members as a chain",
        nibs: [
          makeTreeNib({ id: "a", parentId: "c" }),
          makeTreeNib({ id: "b", parentId: "a" }),
          makeTreeNib({ id: "c", parentId: "b" }),
        ],
        want: ["0:a", "1:b", "2:c"],
      },
      {
        name: "two disjoint cycles each get their own root",
        nibs: [
          makeTreeNib({ id: "a", parentId: "b" }),
          makeTreeNib({ id: "b", parentId: "a" }),
          makeTreeNib({ id: "x", parentId: "y" }),
          makeTreeNib({ id: "y", parentId: "x" }),
        ],
        want: ["0:a", "1:b", "0:x", "1:y"],
      },
      {
        // A nib whose parent chain reaches a cycle but that is not itself in it
        // keeps rendering under its real parent.
        name: "child hanging off a cycle stays under its parent",
        nibs: [
          makeTreeNib({ id: "a", parentId: "b" }),
          makeTreeNib({ id: "b", parentId: "a" }),
          makeTreeNib({ id: "d", parentId: "b" }),
          makeTreeNib({ id: "g", parentId: "d" }),
        ],
        want: ["0:a", "1:b", "2:d", "3:g"],
      },
      {
        name: "cycle alongside an ordinary tree",
        nibs: [
          makeTreeNib({ id: "a", parentId: "b" }),
          makeTreeNib({ id: "b", parentId: "a" }),
          makeTreeNib({ id: "m1" }),
          makeTreeNib({ id: "m2", parentId: "m1" }),
        ],
        want: ["0:a", "1:b", "0:m1", "1:m2"],
      },
      {
        // Must-not-regress: an ordinary tree plus a root-level orphan is
        // unaffected by cycle handling.
        name: "ordinary tree with no cycle is unchanged",
        nibs: [
          makeTreeNib({ id: "m1" }),
          makeTreeNib({ id: "m2", parentId: "m1" }),
          makeTreeNib({ id: "m3", parentId: "m2" }),
          makeTreeNib({ id: "z1" }),
        ],
        want: ["0:m1", "1:m2", "2:m3", "0:z1"],
      },
    ];

    for (const c of cases) {
      it(c.name, () => {
        expect(shape(buildTree(c.nibs))).toEqual(c.want);
      });
    }

    it("keeps a cycle visible through buildViewTree's grouping lenses", () => {
      // The lenses classify the forest buildTree produces, so an erased cycle
      // stays erased downstream too.
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "a", type: "epic", parentId: "b" }),
        makeTreeNib({ id: "b", type: "task", parentId: "a" }),
      ];
      const ids = collectIds(buildViewTree(nibs, "epics")).filter((id) => !isSyntheticRowId(id));
      expect(ids.sort()).toEqual(["a", "b"]);
    });

    // Presence is not the whole story: a lens decides a nib's section from its
    // ancestor chain, and a cycle HAS no outermost ancestor of its own. The rule
    // is that the chain is read only as far as the member `buildTree` promoted
    // to a root, which is where the rendered path starts — so the lens follows
    // the forest instead of second-guessing it.
    it("arranges a cycle in a grouping lens the way buildTree rooted it", () => {
      const twoCycle: TreeNib[] = [
        makeTreeNib({ id: "a", type: "epic", parentId: "b" }),
        makeTreeNib({ id: "b", type: "task", parentId: "a" }),
      ];
      // "a" is the promoted root AND an epic, so it heads its own section and
      // keeps "b" beneath it — no bucket is minted at all.
      const twoCycleTree = buildViewTree(twoCycle, "epics");
      expect(twoCycleTree.map((r) => r.nib.id)).toEqual(["a"]);
      expect(twoCycleTree[0].children.map((c) => c.nib.id)).toEqual(["b"]);

      // A cycle spanning the tier: the two milestones are still hidden and the
      // epic still surfaces as the header, exactly as an acyclic chain would.
      const spanning: TreeNib[] = [
        makeTreeNib({ id: "d1", type: "milestone", parentId: "d3" }),
        makeTreeNib({ id: "d2", type: "milestone", parentId: "d1" }),
        makeTreeNib({ id: "d3", type: "epic", parentId: "d2" }),
      ];
      expect(buildViewTree(spanning, "epics").map((r) => r.nib.id)).toEqual(["d3"]);
    });
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
  describe("none lens", () => {
    it("returns the full tree unchanged (nothing hidden, depths preserved)", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Milestone", type: "milestone" }),
        makeTreeNib({ id: "nibs-002", title: "Epic", type: "epic", parentId: "nibs-001" }),
        makeTreeNib({ id: "nibs-003", title: "Feature", type: "feature", parentId: "nibs-002" }),
        makeTreeNib({ id: "nibs-004", title: "Task", type: "task", parentId: "nibs-003" }),
        makeTreeNib({ id: "nibs-005", title: "Standalone task", type: "task" }),
      ];

      const result = buildViewTree(nibs, "none");

      // Two roots: the milestone chain and the standalone task
      expect(result).toHaveLength(2);
      expect(result[0].nib.id).toBe("nibs-001");
      expect(result[0].depth).toBe(0);

      const epic = result[0].children[0];
      expect(epic.nib.id).toBe("nibs-002");
      expect(epic.depth).toBe(1);
      const feature = epic.children[0];
      expect(feature.nib.id).toBe("nibs-003");
      expect(feature.depth).toBe(2);
      const task = feature.children[0];
      expect(task.nib.id).toBe("nibs-004");
      expect(task.depth).toBe(3);

      // Standalone task stays a root at depth 0 — nothing swept into a bucket
      expect(result[1].nib.id).toBe("nibs-005");
      expect(result[1].depth).toBe(0);
    });
  });

  describe("flat lens", () => {
    it("returns every nib as an ungrouped depth-0 root (no nesting, no buckets)", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Milestone", type: "milestone" }),
        makeTreeNib({ id: "nibs-002", title: "Epic", type: "epic", parentId: "nibs-001" }),
        makeTreeNib({ id: "nibs-003", title: "Feature", type: "feature", parentId: "nibs-002" }),
        makeTreeNib({ id: "nibs-004", title: "Task", type: "task", parentId: "nibs-003" }),
        makeTreeNib({ id: "nibs-005", title: "Standalone task", type: "task" }),
      ];

      const result = buildViewTree(nibs, "flat");

      // One node per nib, all at depth 0, none nested.
      expect(result).toHaveLength(5);
      for (const node of result) {
        expect(node.depth).toBe(0);
        expect(node.children).toEqual([]);
      }

      // Order preserved (the incoming manual `order` sequence), no bucket nodes.
      expect(result.map((n) => n.nib.id)).toEqual([
        "nibs-001",
        "nibs-002",
        "nibs-003",
        "nibs-004",
        "nibs-005",
      ]);
      expect(result.some((n) => isSyntheticRowId(n.nib.id))).toBe(false);
    });

    it("returns an empty forest for empty input", () => {
      expect(buildViewTree([], "flat")).toEqual([]);
    });
  });

  describe("milestones lens", () => {
    it("fills a milestone's section from its ASSIGNEES, and a structural nest schedules nothing", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Milestone A", type: "milestone" }),
        makeTreeNib({ id: "nibs-002", title: "Assigned epic", type: "epic", milestone: "nibs-001", milestoneOrder: "a" }),
        makeTreeNib({ id: "nibs-003", title: "Task under epic", type: "task", parentId: "nibs-002" }),
        // Nested UNDER the milestone with no assignment: decomposition data, not
        // scheduling, so it belongs to no queue at all.
        makeTreeNib({ id: "nibs-004", title: "Nested epic", type: "epic", parentId: "nibs-001" }),
        makeTreeNib({ id: "nibs-005", title: "Root bug", type: "bug" }),
      ];

      const result = buildViewTree(nibs, "milestones");

      // The milestone heads its own section; the assignee brings its subtree.
      expect(result[0].nib.id).toBe("nibs-001");
      expect(result[0].depth).toBe(0);
      expect(result[0].children.map(c => c.nib.id)).toEqual(["nibs-002"]);
      expect(result[0].children[0].depth).toBe(1);
      expect(result[0].children[0].children[0].nib.id).toBe("nibs-003");
      expect(result[0].children[0].children[0].depth).toBe(2);

      const backlog = result.find(r => isSyntheticRowId(r.nib.id))!;
      expect(backlog).toBeDefined();
      expect(backlog.nib.id).toBe("/__backlog__");
      expect(backlog.children.map(c => c.nib.id)).toEqual(["nibs-004", "nibs-005"]);

      expect(result.filter(r => r.nib.id === "nibs-001")).toHaveLength(1);
    });

    it("gives a nested milestone a section of its own, and its subtree the backlog", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Parent milestone", type: "milestone" }),
        makeTreeNib({ id: "nibs-002", title: "Child milestone", type: "milestone", parentId: "nibs-001" }),
        makeTreeNib({ id: "nibs-003", title: "Task under child", type: "task", parentId: "nibs-002" }),
      ];

      const result = buildViewTree(nibs, "milestones");

      // Every milestone heads a section — a milestone is a container of its own,
      // never a member of another, however hand-edited data nests it. The task
      // under the child milestone carries no assignment, so it is backlog.
      expect(result.map(r => r.nib.id)).toEqual(["nibs-001", "nibs-002", "/__backlog__"]);
      expect(result[0].children).toEqual([]);
      expect(result[1].children).toEqual([]);
      expect(result[2].children.map(c => c.nib.id)).toEqual(["nibs-003"]);
    });
  });

  describe("epics lens", () => {
    it("hides the milestone row, surfaces its epic as a header, buckets loose feature/bug", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Milestone", type: "milestone" }),
        makeTreeNib({ id: "nibs-002", title: "Epic", type: "epic", parentId: "nibs-001" }),
        makeTreeNib({ id: "nibs-003", title: "Feature under epic", type: "feature", parentId: "nibs-002" }),
        makeTreeNib({ id: "nibs-004", title: "Task under feature", type: "task", parentId: "nibs-003" }),
        makeTreeNib({ id: "nibs-005", title: "Loose feature", type: "feature" }),
        makeTreeNib({ id: "nibs-006", title: "Loose bug", type: "bug" }),
      ];

      const result = buildViewTree(nibs, "epics");

      // Milestone row absent
      expect(result.find(r => r.nib.id === "nibs-001")).toBeUndefined();

      // Epic is a top-level header with its full subtree
      expect(result[0].nib.id).toBe("nibs-002");
      expect(result[0].depth).toBe(0);
      expect(result[0].children[0].nib.id).toBe("nibs-003");
      expect(result[0].children[0].children[0].nib.id).toBe("nibs-004");

      // Loose feature/bug go under "No epic"
      const bucket = result.find(r => r.nib.id === "/__no_epic__")!;
      expect(bucket).toBeDefined();
      expect(bucket.children.map(c => c.nib.id)).toEqual(["nibs-005", "nibs-006"]);
    });

    it("puts loose tasks of a milestone (with no epic) under 'No epic'", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Milestone", type: "milestone" }),
        makeTreeNib({ id: "nibs-002", title: "Task 1", type: "task", parentId: "nibs-001" }),
        makeTreeNib({ id: "nibs-003", title: "Task 2", type: "task", parentId: "nibs-001" }),
      ];

      const result = buildViewTree(nibs, "epics");

      // No epic headers exist; milestone row hidden; tasks fall to the bucket
      expect(result).toHaveLength(1);
      const bucket = result[0];
      expect(bucket.nib.id).toBe("/__no_epic__");
      expect(bucket.children.map(c => c.nib.id)).toEqual(["nibs-002", "nibs-003"]);
    });
  });

  describe("features & bugs lens", () => {
    it("surfaces feature/bug headers; a task directly under an epic lands in 'No feature or bug'", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Milestone", type: "milestone" }),
        makeTreeNib({ id: "nibs-002", title: "Epic", type: "epic", parentId: "nibs-001" }),
        makeTreeNib({ id: "nibs-003", title: "Feature", type: "feature", parentId: "nibs-002" }),
        makeTreeNib({ id: "nibs-004", title: "Task under feature", type: "task", parentId: "nibs-003" }),
        makeTreeNib({ id: "nibs-005", title: "Task under epic", type: "task", parentId: "nibs-002" }),
      ];

      const result = buildViewTree(nibs, "features");

      // Milestone and epic rows are absent
      expect(result.find(r => r.nib.id === "nibs-001")).toBeUndefined();
      expect(result.find(r => r.nib.id === "nibs-002")).toBeUndefined();

      // Feature becomes a header with its task subtree
      expect(result[0].nib.id).toBe("nibs-003");
      expect(result[0].depth).toBe(0);
      expect(result[0].children[0].nib.id).toBe("nibs-004");

      // The task under the epic (no feature/bug ancestor) lands in the bucket
      const bucket = result.find(r => isSyntheticRowId(r.nib.id))!;
      expect(bucket).toBeDefined();
      expect(bucket.nib.id).toBe("/__no_feature_or_bug__");
      expect(bucket.children.map(c => c.nib.id)).toEqual(["nibs-005"]);
    });

    it("never drops research nibs and keeps research nested under a feature header", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Root research", type: "research" }),
        makeTreeNib({ id: "nibs-002", title: "Task under research", type: "task", parentId: "nibs-001" }),
        makeTreeNib({ id: "nibs-003", title: "Feature", type: "feature" }),
        makeTreeNib({ id: "nibs-004", title: "Research under feature", type: "research", parentId: "nibs-003" }),
      ];

      const result = buildViewTree(nibs, "features");

      // Feature is a header; the research under it stays nested (research rank 0, not a grouping type)
      const feature = result.find(r => r.nib.id === "nibs-003")!;
      expect(feature).toBeDefined();
      expect(feature.children.map(c => c.nib.id)).toContain("nibs-004");

      // Root research (rank 0, not feature/bug) becomes a bucket item, carrying its task subtree
      const bucket = result.find(r => r.nib.id === "/__no_feature_or_bug__")!;
      expect(bucket).toBeDefined();
      const rootResearch = bucket.children.find(c => c.nib.id === "nibs-001")!;
      expect(rootResearch).toBeDefined();
      expect(rootResearch.children.map(c => c.nib.id)).toEqual(["nibs-002"]);

      // Every research id appears somewhere in the output forest
      const allIds = collectIds(result);
      expect(allIds).toContain("nibs-001");
      expect(allIds).toContain("nibs-004");
    });
  });

  describe("completeness invariant (messy hierarchy)", () => {
    for (const lens of ["milestones", "epics", "features"] as const) {
      it(`${lens}: rank<=gRank items appear exactly once, rank>gRank items are hidden but descendants survive`, () => {
        const gRank = GROUPING_LENS_RANKS[lens];
        const result = buildViewTree(MESSY_FIXTURE, lens);

        // Real nib ids only (drop synthetic bucket nodes)
        const outputIds = collectIds(result).filter(id => !isSyntheticRowId(id));

        const counts = new Map<string, number>();
        for (const id of outputIds) counts.set(id, (counts.get(id) ?? 0) + 1);

        for (const nib of MESSY_FIXTURE) {
          if (typeRank(nib.type) <= gRank) {
            // (a) present exactly once — no drop, no duplication
            expect(counts.get(nib.id), `${nib.id} (${nib.type}) should appear exactly once`).toBe(1);
          } else {
            // (b) above-tier container: not its own row, but its descendants survive
            expect(counts.get(nib.id), `${nib.id} (${nib.type}) should be hidden`).toBeUndefined();
          }
        }

        // (b) descendants survive: children of hidden containers still appear.
        // e1's child f1 (feature) survives in epics/features lenses even though e1 is hidden there.
        if (gRank < typeRank("epic")) {
          expect(outputIds).toContain("f1");
        }
      });
    }
  });

  describe("bucket node", () => {
    it("titles the bucket with its direct-child count (not recursive descendants)", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Loose feature", type: "feature" }),
        makeTreeNib({ id: "nibs-002", title: "Loose bug", type: "bug" }),
        // A nested task under the loose feature: it must NOT inflate the bucket count.
        makeTreeNib({ id: "nibs-003", title: "Task under loose feature", type: "task", parentId: "nibs-001" }),
      ];

      const result = buildViewTree(nibs, "epics");

      const bucket = result.find(r => isSyntheticRowId(r.nib.id))!;
      // Count is direct children only — the nested task is (3) if recursive, (2) if direct.
      expect(bucket.nib.title).toBe("No epic (2)");
      expect(bucket.children).toHaveLength(2);
      // The nested task lives under the feature, not as a direct bucket child.
      const feature = bucket.children.find(c => c.nib.id === "nibs-001")!;
      expect(feature.children.map(c => c.nib.id)).toEqual(["nibs-003"]);
    });
  });

  // nibs-2lqm: in the epics/features lenses, promoted headers (and bucket items)
  // descend THROUGH a hidden higher-tier ancestor, so `classify` emits them in
  // DFS order grouped by that hidden ancestor. When an active sort's comparator
  // is supplied, they must instead be ordered GLOBALLY by the sort field. The
  // `none`/`flat`/milestones paths already order correctly and stay untouched.
  describe("global header + bucket ordering under an active sort (nibs-2lqm)", () => {
    const cmpFor = (nibs: TreeTableNib[], sort: TableSort) =>
      makeNibComparator(sort, new Map(nibs.map((n) => [n.id, n])));

    describe("epics lens: three epics under DIFFERENT hidden milestones", () => {
      // Input laid out so `classify`'s DFS yields the GROUPED order [Mango, Zebra,
      // Apple] (each epic follows its milestone's position). That order is neither
      // the asc nor the desc title order, so BOTH directions independently
      // discriminate the global re-sort from the grouped DFS emission:
      //   grouped DFS → [eM, eZ, eA];  title asc → [eA, eM, eZ];  desc → [eZ, eM, eA]
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "m1", title: "Mmm1", type: "milestone" }),
        makeTreeTableNib({ id: "eM", title: "Mango", type: "epic", parentId: "m1" }),
        makeTreeTableNib({ id: "m2", title: "Mmm2", type: "milestone" }),
        makeTreeTableNib({ id: "eZ", title: "Zebra", type: "epic", parentId: "m2" }),
        makeTreeTableNib({ id: "m3", title: "Mmm3", type: "milestone" }),
        makeTreeTableNib({ id: "eA", title: "Apple", type: "epic", parentId: "m3" }),
      ];

      it("no comparator → grouped DFS header order (control for the bug)", () => {
        const result = buildViewTree(nibs, "epics");
        expect(result.map((r) => r.nib.id)).toEqual(["eM", "eZ", "eA"]);
      });

      // The control above cannot tell a DFS emission from an INPUT-ORDER one:
      // the fixture lists each milestone immediately before its own epic, so
      // both orders read [eM, eZ, eA] and the assertion holds either way. The
      // unsorted header order is the DFS walk, and only a fixture whose epics
      // appear in the input in a DIFFERENT order from the walk says so.
      it("no comparator → headers follow the DFS walk, NOT the input order", () => {
        const scrambled: TreeTableNib[] = [
          makeTreeTableNib({ id: "eB", title: "Bravo", type: "epic", parentId: "m2" }),
          makeTreeTableNib({ id: "m1", title: "Mmm1", type: "milestone" }),
          makeTreeTableNib({ id: "eA", title: "Alpha", type: "epic", parentId: "m1" }),
          makeTreeTableNib({ id: "m2", title: "Mmm2", type: "milestone" }),
        ];
        // Walk: m1 (hidden) → eA, then m2 (hidden) → eB. Input order is [eB, eA].
        const result = buildViewTree(scrambled, "epics");
        expect(result.map((r) => r.nib.id)).toEqual(["eA", "eB"]);
      });

      it("title asc → promoted headers in GLOBAL order [Apple, Mango, Zebra]", () => {
        const cmp = cmpFor(nibs, { field: "title", direction: "asc" });
        const result = buildViewTree(nibs, "epics", cmp);
        expect(result.map((r) => r.nib.id)).toEqual(["eA", "eM", "eZ"]);
        expect(result.map((r) => r.nib.title)).toEqual(["Apple", "Mango", "Zebra"]);
        // Each header keeps its (empty here) subtree; only top-level order changed.
        expect(result.every((r) => r.depth === 0)).toBe(true);
      });

      it("title desc → global order reverses to [Zebra, Mango, Apple]", () => {
        const cmp = cmpFor(nibs, { field: "title", direction: "desc" });
        const result = buildViewTree(nibs, "epics", cmp);
        expect(result.map((r) => r.nib.id)).toEqual(["eZ", "eM", "eA"]);
      });
    });

    describe("features lens: three features under DIFFERENT hidden epics", () => {
      // Same discriminating layout as the epics block: the grouped DFS order
      // matches neither the asc nor the desc title order, so both directions bite.
      //   grouped DFS → [fM, fZ, fA];  title asc → [fA, fM, fZ];  desc → [fZ, fM, fA]
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "e1", title: "Eee1", type: "epic" }),
        makeTreeTableNib({ id: "fM", title: "Mango", type: "feature", parentId: "e1" }),
        makeTreeTableNib({ id: "e2", title: "Eee2", type: "epic" }),
        makeTreeTableNib({ id: "fZ", title: "Zebra", type: "feature", parentId: "e2" }),
        makeTreeTableNib({ id: "e3", title: "Eee3", type: "epic" }),
        makeTreeTableNib({ id: "fA", title: "Apple", type: "feature", parentId: "e3" }),
      ];

      it("no comparator → grouped DFS header order (control for the bug)", () => {
        const result = buildViewTree(nibs, "features");
        expect(result.map((r) => r.nib.id)).toEqual(["fM", "fZ", "fA"]);
      });

      // Same blind spot as the epics block, same remedy: this fixture's features
      // appear in the input in the reverse of the order the walk reaches them.
      it("no comparator → headers follow the DFS walk, NOT the input order", () => {
        const scrambled: TreeTableNib[] = [
          makeTreeTableNib({ id: "fB", title: "Bravo", type: "feature", parentId: "e2" }),
          makeTreeTableNib({ id: "e1", title: "Eee1", type: "epic" }),
          makeTreeTableNib({ id: "fA", title: "Alpha", type: "feature", parentId: "e1" }),
          makeTreeTableNib({ id: "e2", title: "Eee2", type: "epic" }),
        ];
        // Walk: e1 (hidden) → fA, then e2 (hidden) → fB. Input order is [fB, fA].
        const result = buildViewTree(scrambled, "features");
        expect(result.map((r) => r.nib.id)).toEqual(["fA", "fB"]);
      });

      it("title asc → promoted headers in GLOBAL order [Apple, Mango, Zebra]", () => {
        const cmp = cmpFor(nibs, { field: "title", direction: "asc" });
        const result = buildViewTree(nibs, "features", cmp);
        expect(result.map((r) => r.nib.id)).toEqual(["fA", "fM", "fZ"]);
      });

      it("title desc → global order reverses to [Zebra, Mango, Apple]", () => {
        const cmp = cmpFor(nibs, { field: "title", direction: "desc" });
        const result = buildViewTree(nibs, "features", cmp);
        expect(result.map((r) => r.nib.id)).toEqual(["fZ", "fM", "fA"]);
      });
    });

    describe("bucket items from DISTINCT above-tier parents", () => {
      // Two loose tasks under two different milestones fall into the "No epic"
      // bucket. `classify` DFS yields [Zebra, Apple] (grouped by hidden
      // milestone); a global title sort must reorder them to [Apple, Zebra].
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "m1", title: "Mmm1", type: "milestone" }),
        makeTreeTableNib({ id: "tZ", title: "Zebra", type: "task", parentId: "m1" }),
        makeTreeTableNib({ id: "m2", title: "Mmm2", type: "milestone" }),
        makeTreeTableNib({ id: "tA", title: "Apple", type: "task", parentId: "m2" }),
      ];

      it("no comparator → grouped DFS bucket-item order (control)", () => {
        const result = buildViewTree(nibs, "epics");
        const bucket = result.find((r) => isSyntheticRowId(r.nib.id))!;
        expect(bucket.children.map((c) => c.nib.id)).toEqual(["tZ", "tA"]);
      });

      // The control shares the epics/features blind spot — its tasks happen to
      // sit in the input in walk order. A bucket's loose items are emitted in
      // the order the walk reaches them, which this fixture separates from the
      // order they were handed in.
      it("no comparator → bucket items follow the DFS walk, NOT the input order", () => {
        const scrambled: TreeTableNib[] = [
          makeTreeTableNib({ id: "tB", title: "Bravo", type: "task", parentId: "m2" }),
          makeTreeTableNib({ id: "m1", title: "Mmm1", type: "milestone" }),
          makeTreeTableNib({ id: "tA", title: "Alpha", type: "task", parentId: "m1" }),
          makeTreeTableNib({ id: "m2", title: "Mmm2", type: "milestone" }),
        ];
        // Walk: m1 (hidden) → tA, then m2 (hidden) → tB. Input order is [tB, tA].
        const result = buildViewTree(scrambled, "epics");
        const bucket = result.find((r) => isSyntheticRowId(r.nib.id))!;
        expect(bucket.children.map((c) => c.nib.id)).toEqual(["tA", "tB"]);
      });

      it("title asc → bucket items in GLOBAL order [Apple, Zebra]", () => {
        const cmp = cmpFor(nibs, { field: "title", direction: "asc" });
        const result = buildViewTree(nibs, "epics", cmp);
        const bucket = result.find((r) => isSyntheticRowId(r.nib.id))!;
        expect(bucket.children.map((c) => c.nib.id)).toEqual(["tA", "tZ"]);
      });
    });

    it("milestones lens: an unsorted array is globally sorted when a comparator is supplied", () => {
      // Milestone headers are always display roots, so with no comparator they stay
      // in raw input order [m2, m1]. Supplying the comparator must re-sort the roots
      // to [m1, m2] — this fails (roots stay [m2, m1]) if the threading is removed.
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "m2", title: "Zebra", type: "milestone" }),
        makeTreeTableNib({ id: "m1", title: "Alpha", type: "milestone" }),
      ];
      const cmp = cmpFor(nibs, { field: "title", direction: "asc" });
      const result = buildViewTree(nibs, "milestones", cmp);
      expect(result.map((r) => r.nib.id)).toEqual(["m1", "m2"]);
    });

    it("epics lens: a header's subtree order is untouched by the header re-sort", () => {
      // Two epics under two hidden milestones so the TOP-LEVEL header re-sort
      // actually reorders roots — the "Zebra epic" carries two child tasks in a
      // fixed order that the header re-sort must NOT reach into.
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "m1", title: "Mmm1", type: "milestone" }),
        makeTreeTableNib({ id: "eZ", title: "Zebra epic", type: "epic", parentId: "m1" }),
        makeTreeTableNib({ id: "tB", title: "Zebra child", type: "task", parentId: "eZ" }),
        makeTreeTableNib({ id: "tA", title: "Apple child", type: "task", parentId: "eZ" }),
        makeTreeTableNib({ id: "m2", title: "Mmm2", type: "milestone" }),
        makeTreeTableNib({ id: "eA", title: "Apple epic", type: "epic", parentId: "m2" }),
      ];
      const cmp = cmpFor(nibs, { field: "title", direction: "asc" });
      const result = buildViewTree(nibs, "epics", cmp);
      // The top-level headers ARE globally re-sorted (Apple epic before Zebra epic)...
      expect(result.map((r) => r.nib.id)).toEqual(["eA", "eZ"]);
      const epic = result.find((r) => r.nib.id === "eZ")!;
      // ...but each header's subtree stays in incoming order — not re-sorted by title.
      expect(epic.children.map((c) => c.nib.id)).toEqual(["tB", "tA"]);
    });
  });
});

/**
 * The lens the Milestones view SHIPS: sections are milestones, membership is the
 * assignment axis, and the leftover is the Backlog.
 *
 * Separate from the `GroupingLens seam` block below, which exercises the
 * interface through a hand-written lens. These reach the shipped one through
 * `viewShapeFor("milestones")`, so they answer for what a user sees.
 */
describe("milestone membership lens", () => {
  const BACKLOG = "/__backlog__";

  const lookupOver = (nibs: TreeNib[]) => {
    const byId = new Map(nibs.map((n) => [n.id, n]));
    return (id: string) => byId.get(id);
  };

  /**
   * Every shape that could plausibly lose, duplicate or mis-file a row under a
   * membership model, in one fixture: an assigned epic with a structural
   * subtree, a directly queued task, a CLOSED milestone with assignees, a
   * hand-authored parent+assignment pair (`p1` is under an epic queued to `m1`
   * and names `m2` itself — the server refuses to write that pair), a dangling
   * assignment, an assignment naming a nib that is not a milestone, a child
   * whose parent the response does not carry, and a parent cycle.
   *
   * `q1` is listed AFTER `e1` but ordered before it in the queue, so the
   * section's own order is visible rather than incidental.
   */
  const MESSY_MEMBERSHIP: TreeNib[] = [
    makeTreeNib({ id: "m1", type: "milestone", title: "v1.0", status: "in-progress" }),
    makeTreeNib({ id: "e1", type: "epic", title: "Assigned epic", milestone: "m1", milestoneOrder: "b" }),
    makeTreeNib({ id: "f1", type: "feature", title: "Feature", parentId: "e1" }),
    makeTreeNib({ id: "t1", type: "task", title: "Task", parentId: "f1" }),
    makeTreeNib({ id: "q1", type: "task", title: "Queued task", milestone: "m1", milestoneOrder: "a" }),
    makeTreeNib({ id: "m2", type: "milestone", title: "v0.9", status: "completed" }),
    makeTreeNib({ id: "e2", type: "epic", title: "Shipped epic", milestone: "m2", milestoneOrder: "a" }),
    makeTreeNib({ id: "p1", type: "task", title: "Parent and assignment", parentId: "e1", milestone: "m2", milestoneOrder: "b" }),
    makeTreeNib({ id: "d1", type: "task", title: "Dangling assignment", milestone: "m9" }),
    makeTreeNib({ id: "n1", type: "task", title: "Assigned to an epic", milestone: "e1" }),
    makeTreeNib({ id: "o1", type: "task", title: "Parent not in response", parentId: "e-gone" }),
    makeTreeNib({ id: "c1", type: "task", title: "Cycle A", parentId: "c2" }),
    makeTreeNib({ id: "c2", type: "task", title: "Cycle B", parentId: "c1" }),
    makeTreeNib({ id: "l1", type: "task", title: "Loose" }),
  ];

  it("renders every nib of the messy fixture exactly once", () => {
    const ids = collectIds(buildViewTree(MESSY_MEMBERSHIP, "milestones"))
      .filter((id) => !isSyntheticRowId(id));
    const counts = new Map<string, number>();
    for (const id of ids) counts.set(id, (counts.get(id) ?? 0) + 1);
    for (const nib of MESSY_MEMBERSHIP) {
      expect(counts.get(nib.id), `${nib.id} should render exactly once`).toBe(1);
    }
    expect(ids).toHaveLength(MESSY_MEMBERSHIP.length);
  });

  /**
   * The invariant `childRegion` says the lens owns: every row a section declares
   * its queue over is in that queue by the server's own rule — which is the
   * DIRECT one (`scopeTable[ScopeMilestone].group` is `resolvedMilestoneID`),
   * not the derived membership that decided the section.
   *
   * The two agree for the rows that matter because a declaration reaches a
   * section node's DIRECT children and no further, and a DERIVED member's parent
   * lands in the same section — so `buildTree` nests it and it is never a direct
   * child. Read off the emitted node rather than restated, so it cannot drift
   * from what `flatten` hands the row.
   */
  function expectDeclaredRowsAreDirectlyAssigned(nibs: TreeNib[]): void {
    const lookup = lookupOver(nibs);
    let checked = 0;
    for (const section of buildViewTree(nibs, "milestones")) {
      const region = section.childRegion ?? null;
      if (region === null || region.axis !== "milestone") continue;
      for (const child of section.children) {
        checked++;
        expect(
          resolvedMilestoneId(child.nib, lookup),
          `${child.nib.id} takes ${region.milestoneId}'s queue declaration but is not in that queue`,
        ).toBe(region.milestoneId);
      }
    }
    expect(checked, "no declared row was checked — this guard would pass vacuously").toBeGreaterThan(0);
  }

  it("declares a milestone's queue only over rows the server agrees are in it", () => {
    expectDeclaredRowsAreDirectlyAssigned(MESSY_MEMBERSHIP);
  });

  it("keeps an assigned epic's structural children beneath it, inside the section", () => {
    const tree = buildViewTree(MESSY_MEMBERSHIP, "milestones");
    const m1 = tree.find((r) => r.nib.id === "m1")!;
    // Queue order, not input order: q1 carries "a" and is listed second.
    expect(m1.children.map((c) => c.nib.id)).toEqual(["q1", "e1"]);
    const e1 = m1.children[1];
    // Inherited membership: neither f1 nor t1 carries an assignment of its own,
    // and they are drawn under the epic rather than beside it. Filing them by
    // the DIRECT rule instead would drop both into the Backlog.
    expect(e1.children.map((c) => c.nib.id)).toEqual(["f1"]);
    expect(e1.children[0].children.map((c) => c.nib.id)).toEqual(["t1"]);
    expect([m1.depth, e1.depth, e1.children[0].depth]).toEqual([0, 1, 2]);
    expect(holdsChildrenByDisplay(m1)).toBe(true);
    expect(isSyntheticRowId(m1.nib.id)).toBe(false);
  });

  it("sends an unresolvable or non-milestone assignment to the Backlog rather than minting a section for it", () => {
    const tree = buildViewTree(MESSY_MEMBERSHIP, "milestones");
    // Only the two real milestones head sections, and nothing else is a section
    // row at all — keying on the RESOLVED id is what rules out a headless
    // section labeled with a raw `milestone:` string.
    expect(tree.map((r) => r.nib.id)).toEqual(["m1", "m2", BACKLOG]);
    const backlog = tree[2];
    expect(backlog.children.map((c) => c.nib.id)).toEqual(["d1", "n1", "o1", "c1", "l1"]);
    // The cycle keeps both members: buildTree promotes one and nests the other.
    expect(backlog.children.find((c) => c.nib.id === "c1")!.children.map((c) => c.nib.id)).toEqual(["c2"]);
  });

  it("puts a hand-authored parent+assignment pair in the queue its own assignment names", () => {
    // p1 is a structural child of e1, which is queued to m1, but names m2. The
    // nib's own assignment wins: `milestoneOf` answers before it ever walks up.
    const tree = buildViewTree(MESSY_MEMBERSHIP, "milestones");
    const m2 = tree.find((r) => r.nib.id === "m2")!;
    expect(m2.children.map((c) => c.nib.id)).toEqual(["e2", "p1"]);
  });

  describe("a closed milestone is a section like any other", () => {
    // The decision this feature owed. Status is not consulted: which milestones
    // exist is the response's call, and `(*membership.View).Backlog` settles it
    // the same way on the Go side ("work under a status-hidden milestone is
    // scheduled work, not backlog").
    it("heads its own section, holding its assignees", () => {
      const tree = buildViewTree(MESSY_MEMBERSHIP, "milestones");
      const m2 = tree.find((r) => r.nib.id === "m2")!;
      expect(m2.nib.status).toBe("completed");
      expect(isSyntheticRowId(m2.nib.id)).toBe(false);
      expect(m2.childRegion).toEqual({ axis: "milestone", milestoneId: "m2" });
      expect(collectIds([tree.find((r) => r.nib.id === BACKLOG)!])).not.toContain("e2");
    });

    it("keeps its position in the array's sequence rather than being pushed after the open ones", () => {
      // m2 is listed first here. Nothing reorders it behind the open milestone:
      // section order is the array's, which is the server's `order` sequence.
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "done", type: "milestone", title: "v0.9", status: "completed" }),
        makeTreeNib({ id: "open", type: "milestone", title: "v1.0", status: "in-progress" }),
      ];
      expect(buildViewTree(nibs, "milestones").map((r) => r.nib.id)).toEqual(["done", "open"]);
    });
  });

  describe("section order", () => {
    it("follows the HEADERS' sequence, which is neither id, title, nor discovery order", () => {
      // The client holds no `order` key — `TreeNib` does not carry one — so the
      // sequence it renders is the array's, and the array arrives from
      // `TREE_TABLE_QUERY`'s `sort: { field: ORDER, direction: ASC }`.
      //
      // The fixture discriminates on three counts at once. Ids and titles both
      // run the other way, so neither could produce this order. And `e1` is
      // listed FIRST while belonging to the LAST section: `SortByOrder` is a
      // flat sort over `order` across the whole result irrespective of parent
      // (internal/nib/sort.go), so an assignee routinely precedes the milestone
      // it is in — and a section order taken from whichever nib DISCOVERED each
      // section would draw a1 above z1.
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "e1", type: "epic", title: "Assigned ahead of both headers", milestone: "a1" }),
        makeTreeNib({ id: "z1", type: "milestone", title: "Zulu" }),
        makeTreeNib({ id: "a1", type: "milestone", title: "Alpha" }),
      ];
      const tree = buildViewTree(nibs, "milestones");
      expect(tree.map((r) => r.nib.id)).toEqual(["z1", "a1"]);
      expect(tree[1].children.map((c) => c.nib.id)).toEqual(["e1"]);
    });

    it("puts the Backlog last, whatever it was discovered before", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "loose", type: "task" }),
        makeTreeNib({ id: "m1", type: "milestone" }),
      ];
      expect(buildViewTree(nibs, "milestones").map((r) => r.nib.id)).toEqual(["m1", "/__backlog__"]);
    });

    it("hands an active column sort the section order instead", () => {
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "z1", type: "milestone", title: "Zulu" }),
        makeTreeTableNib({ id: "a1", type: "milestone", title: "Alpha" }),
      ];
      const cmp = makeNibComparator(
        { field: "title", direction: "asc" },
        new Map(nibs.map((n) => [n.id, n])),
      );
      expect(buildViewTree(nibs, "milestones", cmp).map((r) => r.nib.id)).toEqual(["a1", "z1"]);
    });
  });

  describe("queue order within a section", () => {
    const nibs: TreeNib[] = [
      makeTreeNib({ id: "m1", type: "milestone" }),
      makeTreeNib({ id: "x3", type: "task", title: "Aaa", milestone: "m1", milestoneOrder: "c" }),
      makeTreeNib({ id: "x1", type: "task", title: "Zzz", milestone: "m1", milestoneOrder: "a" }),
      makeTreeNib({ id: "x2", type: "task", title: "Mmm", milestone: "m1", milestoneOrder: "b" }),
    ];

    it("orders by milestoneOrder", () => {
      expect(buildViewTree(nibs, "milestones")[0].children.map((c) => c.nib.id)).toEqual(["x1", "x2", "x3"]);
    });

    it("appends an assignee with no key, rather than floating it to the top", () => {
      // An assignment can predate the queue it is in — `milestoneOrder` is empty
      // for a nib never placed in one — and the server's own listing appends
      // those (nib.CompareByKey: keyed before unkeyed, then title, then id).
      const unkeyed: TreeNib[] = [
        ...nibs,
        makeTreeNib({ id: "u2", type: "task", title: "Bbb", milestone: "m1" }),
        makeTreeNib({ id: "u1", type: "task", title: "Aaa", milestone: "m1" }),
      ];
      expect(buildViewTree(unkeyed, "milestones")[0].children.map((c) => c.nib.id))
        .toEqual(["x1", "x2", "x3", "u1", "u2"]);
    });

    it("lets an active column sort outrank it", () => {
      const rows = nibs.map((n) => ({ ...n, blockingIds: [], blockedByIds: [], etag: "" }));
      const cmp = makeNibComparator(
        { field: "title", direction: "asc" },
        new Map(rows.map((n) => [n.id, n])),
      );
      expect(buildViewTree(rows, "milestones", cmp)[0].children.map((c) => c.nib.id)).toEqual(["x3", "x2", "x1"]);
    });

    it("leaves the Backlog in the walk's order, having no queue to sort by", () => {
      const loose: TreeNib[] = [
        makeTreeNib({ id: "b2", type: "task", title: "Zzz" }),
        makeTreeNib({ id: "b1", type: "task", title: "Aaa", milestoneOrder: "a" }),
      ];
      expect(buildViewTree(loose, "milestones")[0].children.map((c) => c.nib.id)).toEqual(["b2", "b1"]);
    });
  });

  /**
   * What a NARROWED lookup does to the answer — the client's lookup spans only
   * the rows the page holds, and the server's `noMilestone` is answered over the
   * whole store, so the two can disagree. `milestoneOf` documents both
   * directions; these are what they look like on screen.
   *
   * The decision recorded here: a filtered view may legitimately draw scheduled
   * work in the Backlog. The lens is a pure function of the rows it is handed
   * and cannot load an absent ancestor; the alternative is for the QUERY to
   * re-add ancestor chains, which changes what a filter means — it would put
   * rows the user excluded back on the page — and belongs to the query layer.
   */
  describe("under a lookup narrowed by the response filter", () => {
    it("draws a row whose intermediate ancestor is missing in the Backlog", () => {
      // The dominant case: a type or status filter drops the epics carrying the
      // assignments while their tasks remain.
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "m1", type: "milestone" }),
        makeTreeNib({ id: "t1", type: "task", parentId: "e-filtered-out" }),
      ];
      const tree = buildViewTree(nibs, "milestones");
      expect(tree.find((r) => r.nib.id === BACKLOG)!.children.map((c) => c.nib.id)).toEqual(["t1"]);
      // The safe direction: a row in the Backlog claims no queue, and its own
      // ordering group is still the one the server resolved for it.
      expect(milestoneOf(nibs[1], lookupOver(nibs))).toBe("");
    });

    it("draws a missing milestone's assignees in the Backlog when no ancestor carries an assignment", () => {
      // The shape the write path produces: the server refuses to assign a nib
      // whose ancestor is already assigned, so a chain carries at most one
      // assignment and losing it leaves nothing for the walk to find.
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "e1", type: "epic", milestone: "m-filtered-out", milestoneOrder: "a" }),
        makeTreeNib({ id: "t1", type: "task", parentId: "e1" }),
      ];
      const tree = buildViewTree(nibs, "milestones");
      expect(tree.map((r) => r.nib.id)).toEqual([BACKLOG]);
      expect(tree[0].children.map((c) => c.nib.id)).toEqual(["e1"]);
    });

    it("draws a missing milestone's assignees in an ANCESTOR's queue when hand-authored data puts one there", () => {
      // The unsafe direction, and the reason the closed-milestone question was
      // NOT answered by dropping closed milestones from the response: the walk
      // continues past the step the narrowing emptied rather than stopping. Only
      // hand-authored data reaches it, but it reaches it silently.
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "m0", type: "milestone", title: "v0.9" }),
        makeTreeNib({ id: "e0", type: "epic", milestone: "m0", milestoneOrder: "a" }),
        makeTreeNib({ id: "e1", type: "epic", parentId: "e0", milestone: "m-filtered-out", milestoneOrder: "a" }),
        makeTreeNib({ id: "t1", type: "task", parentId: "e1" }),
      ];
      const tree = buildViewTree(nibs, "milestones");
      expect(tree.map((r) => r.nib.id)).toEqual(["m0"]);
      // e1 is drawn inside m0's section — a milestone its own assignment does
      // not name — nested under e0, which is the row m0's queue is declared over.
      expect(tree[0].children.map((c) => c.nib.id)).toEqual(["e0"]);
      expect(tree[0].children[0].children.map((c) => c.nib.id)).toEqual(["e1"]);
      expectDeclaredRowsAreDirectlyAssigned(nibs);
    });
  });

  /**
   * The one shape the invariant above cannot cover, named outright so the limit
   * is visible rather than merely absent.
   *
   * A parent cycle lying wholly inside one section has no root of its own, so
   * `buildTree` promotes one member — the lowest id — and severs its parent
   * edge. When that member is not the directly assigned one it becomes a DIRECT
   * child of the section node and takes its queue declaration, though the
   * server's MILESTONE scope resolves it to "" (memberless) and would refuse a
   * move. `RowData.region` already names this divergence class ("a cycle member
   * `promotedCycleRoots` severed").
   *
   * Closing it needs a TRANSITIVE rule, which is why it is open rather than
   * closed cheaply. Demoting the promoted member to the Backlog does not do it:
   * on a three-member cycle the severance orphans the next row, which takes the
   * declaration in its place. What does work is demoting every derived member
   * whose walk to its assignment steps off a node `promotedCycleRoots` severs —
   * `place` gets `byId`, and `typeLens.place` re-derives that same promotion
   * decision inline for its own chain, so the ingredients are there. It is not
   * taken because the declaration it would remove names a queue no server write
   * would have accepted anyway, on data only a hand edit produces, and `place`'s
   * per-nib signature gives the promotion set nowhere to live but a
   * re-derivation or a cache keyed on `byId`.
   */
  it("hands a promoted cycle member the section's queue although it is not in it", () => {
    const nibs: TreeNib[] = [
      makeTreeNib({ id: "m1", type: "milestone" }),
      makeTreeNib({ id: "k1", type: "task", parentId: "k2" }),
      makeTreeNib({ id: "k2", type: "task", parentId: "k1", milestone: "m1", milestoneOrder: "a" }),
    ];
    const lookup = lookupOver(nibs);
    const m1 = buildViewTree(nibs, "milestones")[0];

    expect(m1.children.map((c) => c.nib.id)).toEqual(["k1"]);
    expect(m1.children[0].children.map((c) => c.nib.id)).toEqual(["k2"]);
    // Both are members of m1 by the derived rule, which is why they are drawn
    // here at all...
    expect(milestoneOf(nibs[1], lookup)).toBe("m1");
    // ...but only k2 is in the queue, and k1 is the row that takes the
    // declaration. Flip the ids and the promotion lands on k2 instead.
    expect(resolvedMilestoneId(nibs[1], lookup)).toBe("");
    expect(resolvedMilestoneId(nibs[2], lookup)).toBe("m1");
  });

  it("names the section a row is in for containingSectionRowId", () => {
    const map = new Map(MESSY_MEMBERSHIP.map((n) => [n.id, n]));
    // A member names the milestone's own row, which is no ancestor of it — the
    // answer an ancestor-chain walk could not reach.
    expect(containingSectionRowId(map, "t1", "milestones")).toBe("m1");
    expect(containingSectionRowId(map, "p1", "milestones")).toBe("m2");
    expect(containingSectionRowId(map, "l1", "milestones")).toBe(BACKLOG);
    // A milestone heads its own section and is contained by nothing.
    expect(containingSectionRowId(map, "m1", "milestones")).toBeNull();
  });
});

/**
 * The seam itself, exercised through lenses this module does not ship.
 *
 * No view level selects a membership lens yet, so these are written here rather
 * than wired: the point is that grouping by an ASSIGNMENT — which does not run
 * along the parent chain the type lenses walk — needs nothing from
 * `buildShapedViewTree` but a different `GroupingLens`.
 */
describe("GroupingLens seam", () => {
  const BACKLOG = "/__backlog__";

  /**
   * Sections are milestone nibs; membership is the nearest assignment on the
   * parent chain (a child of an assigned epic is planned, not backlog); nothing
   * is hidden, because a membership view has no tier to be above.
   */
  const membershipLens: GroupingLens = {
    leftover: { key: BACKLOG, label: "Backlog" },
    nestHeadersStructurally: false,
    orderWithinSection: () => (a, b) => a.milestoneOrder.localeCompare(b.milestoneOrder),
    // The declaration a membership lens is FOR: a section's rows are in that
    // milestone's queue. The leftover declares NOTHING rather than the root
    // group — its members are in no queue, but they are not all at the top
    // level either (a member whose parent the filter left out has a real
    // resolved parent), and the per-row fallback gets both right.
    childRegion: (section) =>
      section === BACKLOG ? null : { axis: "milestone", milestoneId: section },
    place(nib, byId): Placement {
      if (nib.type === "milestone") return { kind: "header", section: nib.id };
      const visited = new Set<string>();
      let current: TreeNib | undefined = nib;
      while (current !== undefined && !visited.has(current.id)) {
        visited.add(current.id);
        if (current.milestone !== "") return { kind: "member", section: current.milestone };
        current = current.parentId !== null ? byId.get(current.parentId) : undefined;
      }
      return { kind: "member", section: BACKLOG };
    },
  };
  const membershipShape: ViewShape = { kind: "grouped", lens: membershipLens };

  it("renders an assigned epic under its milestone, structural children and all", () => {
    // E1 names NO parent — a milestone accepts no children of any type — so
    // nothing but the assignment can put it under M1.
    const nibs: TreeNib[] = [
      makeTreeNib({ id: "M1", type: "milestone", title: "v2.0" }),
      makeTreeNib({ id: "E1", type: "epic", parentId: null, milestone: "M1", milestoneOrder: "a0" }),
      makeTreeNib({ id: "F1", type: "feature", parentId: "E1" }),
      makeTreeNib({ id: "T1", type: "task", parentId: "F1" }),
      makeTreeNib({ id: "T9", type: "task" }),
    ];

    const tree = buildShapedViewTree(nibs, membershipShape);

    const m1 = tree.find((r) => r.nib.id === "M1")!;
    expect(m1).toBeDefined();
    expect(m1.children.map((c) => c.nib.id)).toEqual(["E1"]);
    // The epic keeps its own structural subtree beneath it, inherited membership
    // and all — F1 and T1 carry no assignment of their own.
    const e1 = m1.children[0];
    expect(e1.children.map((c) => c.nib.id)).toEqual(["F1"]);
    expect(e1.children[0].children.map((c) => c.nib.id)).toEqual(["T1"]);
    // Depths are relative to the new roots.
    expect([m1.depth, e1.depth, e1.children[0].depth]).toEqual([0, 1, 2]);
    // M1 holds rows no nib names as its parent — the arrangement question the
    // table asks to decide collapse and reorder targets.
    expect(holdsChildrenByDisplay(m1)).toBe(true);
    expect(isSyntheticRowId(m1.nib.id)).toBe(false);

    const backlog = tree.find((r) => r.nib.id === BACKLOG)!;
    expect(backlog.children.map((c) => c.nib.id)).toEqual(["T9"]);
  });

  it("puts the lens's childRegion declaration on the node that renders the section", () => {
    const nibs: TreeNib[] = [
      makeTreeNib({ id: "M1", type: "milestone" }),
      makeTreeNib({ id: "E1", type: "epic", milestone: "M1", milestoneOrder: "a0" }),
      makeTreeNib({ id: "T9", type: "task" }),
    ];

    const tree = buildShapedViewTree(nibs, membershipShape);

    // Both kinds of section container carry what the lens answered: the one a
    // real nib heads, and the fabricated leftover — which here declares nothing,
    // so its members keep the per-row fallback.
    expect(tree.find((r) => r.nib.id === "M1")!.childRegion).toEqual({
      axis: "milestone",
      milestoneId: "M1",
    });
    expect(tree.find((r) => r.nib.id === BACKLOG)!.childRegion).toBeNull();
  });

  it("carries a non-null declaration onto a fabricated section container too", () => {
    // The headless branch of `assembleSection`, which the test above cannot
    // reach: its leftover declares null, and its other section is headed by a
    // real nib. A section is fabricated AND declares something whenever the
    // header nib is missing from the response — a filter that excluded the
    // milestone, or an assignment left dangling — so the members still land in
    // a queue whose header has no row.
    const nibs: TreeNib[] = [
      makeTreeNib({ id: "E1", type: "epic", milestone: "M1", milestoneOrder: "a0" }),
      makeTreeNib({ id: "T9", type: "task" }),
    ];

    const tree = buildShapedViewTree(nibs, membershipShape);

    const section = tree.find((r) => r.nib.id === "/section:M1_")!;
    expect(section).toBeDefined();
    expect(isSyntheticRowId(section.nib.id)).toBe(true);
    expect(section.children.map((c) => c.nib.id)).toEqual(["E1"]);
    expect(section.childRegion).toEqual({ axis: "milestone", milestoneId: "M1" });
  });

  it("leaves a type lens's section nodes declaring nothing", () => {
    const shape = viewShapeFor("epics");
    expect(shape.kind).toBe("grouped");
    const tree = buildShapedViewTree(MESSY_FIXTURE, shape);

    // Grouping by type moves no row out of its parent's sibling set, so every
    // section declares null and each row keeps its own parent group.
    for (const node of tree) expect(node.childRegion).toBeNull();
  });

  it("orders a section's top-level members by the lens, and lets a column sort outrank it", () => {
    const nibs: TreeNib[] = [
      makeTreeNib({ id: "M1", type: "milestone" }),
      makeTreeNib({ id: "Q3", type: "task", title: "Aaa", milestone: "M1", milestoneOrder: "c" }),
      makeTreeNib({ id: "Q1", type: "task", title: "Zzz", milestone: "M1", milestoneOrder: "a" }),
      makeTreeNib({ id: "Q2", type: "task", title: "Mmm", milestone: "M1", milestoneOrder: "b" }),
    ];

    const queued = buildShapedViewTree(nibs, membershipShape);
    expect(queued[0].children.map((c) => c.nib.id)).toEqual(["Q1", "Q2", "Q3"]);

    // Sorting a column means the user asked for THAT order specifically.
    const byTitle = buildShapedViewTree(nibs, membershipShape, (a, b) => a.title.localeCompare(b.title));
    expect(byTitle[0].children.map((c) => c.nib.id)).toEqual(["Q3", "Q2", "Q1"]);
  });

  /**
   * The exactly-once audit, over every shape that could plausibly lose or
   * duplicate a row under a membership model: an assignment naming no nib at
   * all, an assignment naming a nib that is NOT a section (and renders
   * elsewhere), a nib nested under a milestone that cannot legally hold it, and
   * a parent cycle.
   */
  const messyMembership: TreeNib[] = [
    makeTreeNib({ id: "M1", type: "milestone" }),
    makeTreeNib({ id: "E1", type: "epic", milestone: "M1", milestoneOrder: "a" }),
    makeTreeNib({ id: "T1", type: "task", parentId: "E1" }),
    makeTreeNib({ id: "D1", type: "task", milestone: "M9" }),
    makeTreeNib({ id: "N1", type: "task", parentId: "M1" }),
    makeTreeNib({ id: "X1", type: "task", parentId: "X2" }),
    makeTreeNib({ id: "X2", type: "task", parentId: "X1" }),
    makeTreeNib({ id: "L1", type: "task" }),
    makeTreeNib({ id: "A1", type: "task", milestone: "T1" }),
  ];

  it("renders every nib of a messy membership fixture exactly once", () => {
    const ids = collectIds(buildShapedViewTree(messyMembership, membershipShape))
      .filter((id) => !isSyntheticRowId(id));
    const counts = new Map<string, number>();
    for (const id of ids) counts.set(id, (counts.get(id) ?? 0) + 1);
    for (const nib of messyMembership) {
      expect(counts.get(nib.id), `${nib.id} should render exactly once`).toBe(1);
    }
    expect(ids).toHaveLength(messyMembership.length);
  });

  it("gives a dangling assignment a section of its own rather than dropping its rows", () => {
    const tree = buildShapedViewTree(messyMembership, membershipShape);
    // D1 names M9, which is in no result set. The union-of-sections rule creates
    // the section anyway, so the row surfaces as an oddity instead of vanishing.
    const stray = tree.find((r) => collectIds([r]).includes("D1"))!;
    expect(stray.nib.id).not.toBe("M9");
    expect(stray.nib.title).toContain("M9");
    expect(isSyntheticRowId(stray.nib.id)).toBe(true);
  });

  it("keeps a section keyed on a real nib from rendering that nib's id twice", () => {
    // A1 is assigned to T1 — a task, which heads nothing and is itself rendered
    // under M1. A container carrying the key verbatim would put "T1" in the
    // table twice, breaking every consumer that addresses a row by id.
    const tree = buildShapedViewTree(messyMembership, membershipShape);
    const ids = collectIds(tree);
    expect(ids.filter((id) => id === "T1")).toHaveLength(1);
    const container = tree.find((r) => r.children.some((c) => c.nib.id === "A1"))!;
    expect(container.nib.id).not.toBe("T1");
    expect(isSyntheticRowId(container.nib.id)).toBe(true);
  });

  it("renders a garbage section key once, under a section of that name", () => {
    const garbage = "not a nib id at all";
    const strayLens: GroupingLens = {
      leftover: { key: "/__nowhere__", label: "Nowhere" },
      nestHeadersStructurally: false,
      childRegion: () => null,
      place: (nib): Placement =>
        nib.id === "stray"
          ? { kind: "member", section: garbage }
          : { kind: "member", section: "/__nowhere__" },
    };
    const nibs: TreeNib[] = [
      makeTreeNib({ id: "stray", type: "task" }),
      makeTreeNib({ id: "ordinary", type: "task" }),
    ];

    const tree = buildShapedViewTree(nibs, { kind: "grouped", lens: strayLens });

    const ids = collectIds(tree).filter((id) => !isSyntheticRowId(id));
    expect(ids.filter((id) => id === "stray")).toHaveLength(1);
    const section = tree.find((r) => r.children.some((c) => c.nib.id === "stray"))!;
    expect(section.nib.title).toContain(garbage);
    // Named after the key but not addressed by it, so an arbitrary key can never
    // collide with a nib id.
    expect(section.nib.id).not.toBe(garbage);
    expect(isSyntheticRowId(section.nib.id)).toBe(true);
  });

  // One ask per nib, no more and no fewer, in BOTH arrangements. "No fewer" is
  // the substantive half: under `nestHeadersStructurally` the walk stops at a
  // claimed section and never looks inside it, so `place` answering for the whole
  // input is not incidental — `containingSectionRowId` goes on to ask about nibs
  // the walk never reached. "No more" keeps the memo doing the one thing it is
  // for, confining a contract-breaking lens to a single decision per build.
  describe("the placement memo asks the lens exactly once per nib", () => {
    function countingLens(inner: GroupingLens, calls: string[]): GroupingLens {
      return { ...inner, place: (nib, byId) => (calls.push(nib.id), inner.place(nib, byId)) };
    }

    it("under a membership lens", () => {
      const calls: string[] = [];
      buildShapedViewTree(messyMembership, {
        kind: "grouped",
        lens: countingLens(membershipLens, calls),
      });
      expect(calls).toHaveLength(messyMembership.length);
      expect(new Set(calls)).toEqual(new Set(messyMembership.map((n) => n.id)));
    });

    it("under a type lens, whose walk stops at every header", () => {
      const calls: string[] = [];
      const shape = viewShapeFor("epics");
      // Guards the case against going vacuous if "epics" ever stops grouping.
      expect(shape.kind).toBe("grouped");
      if (shape.kind !== "grouped") return;
      buildShapedViewTree(MESSY_FIXTURE, {
        kind: "grouped",
        lens: countingLens(shape.lens, calls),
      });
      expect(calls).toHaveLength(MESSY_FIXTURE.length);
      expect(new Set(calls)).toEqual(new Set(MESSY_FIXTURE.map((n) => n.id)));
    });
  });
});

describe("containingSectionRowId", () => {
  function nibMapOf(nibs: TreeNib[]): Map<string, TreeNib> {
    return new Map(nibs.map(n => [n.id, n]));
  }

  it("returns null for the none lens (no sections)", () => {
    const map = nibMapOf([makeTreeNib({ id: "t1", type: "task" })]);
    expect(containingSectionRowId(map, "t1", "none")).toBeNull();
  });

  it("returns the lens bucket for a loose item with no grouping ancestor", () => {
    const map = nibMapOf([makeTreeNib({ id: "t1", type: "task" })]);
    expect(containingSectionRowId(map, "t1", "epics")).toBe("/__no_epic__");
  });

  // Asks which row CONTAINS the item, so an item under a header names that
  // header. The caller un-collapses it, which for a type lens the ancestor-chain
  // walk beside it would also have done — but a membership section header is no
  // ancestor of its members, and only this answer reaches it.
  it("returns the header when the item sits under a grouping header", () => {
    const map = nibMapOf([
      makeTreeNib({ id: "e1", type: "epic" }),
      makeTreeNib({ id: "f1", type: "feature", parentId: "e1" }),
      makeTreeNib({ id: "t1", type: "task", parentId: "f1" }),
    ]);
    expect(containingSectionRowId(map, "t1", "epics")).toBe("e1");
  });

  it("returns null when the item IS a grouping header itself", () => {
    const map = nibMapOf([makeTreeNib({ id: "e1", type: "epic" })]);
    expect(containingSectionRowId(map, "e1", "epics")).toBeNull();
  });

  it("buckets a loose task under a milestone (above-tier ancestor, no epic)", () => {
    const map = nibMapOf([
      makeTreeNib({ id: "m1", type: "milestone" }),
      makeTreeNib({ id: "t1", type: "task", parentId: "m1" }),
    ]);
    expect(containingSectionRowId(map, "t1", "epics")).toBe("/__no_epic__");
  });

  it("features lens: task under an epic (no feature/bug) lands in the bucket", () => {
    const map = nibMapOf([
      makeTreeNib({ id: "e1", type: "epic" }),
      makeTreeNib({ id: "t1", type: "task", parentId: "e1" }),
    ]);
    expect(containingSectionRowId(map, "t1", "features")).toBe("/__no_feature_or_bug__");
  });

  it("features lens: task under a feature names that feature", () => {
    const map = nibMapOf([
      makeTreeNib({ id: "f1", type: "feature" }),
      makeTreeNib({ id: "t1", type: "task", parentId: "f1" }),
    ]);
    expect(containingSectionRowId(map, "t1", "features")).toBe("f1");
  });

  // An item whose OWN rank is above the grouping tier is hidden outright by the
  // lens (not swept into a bucket), so it is inside no section — this must agree
  // and return null, else ensure-visible would spuriously un-collapse a bucket
  // when deep-linking to such a container.
  it("returns null for an above-tier container queried in a lower lens", () => {
    const map = nibMapOf([
      makeTreeNib({ id: "m1", type: "milestone" }),
      makeTreeNib({ id: "e1", type: "epic", parentId: "m1" }),
    ]);
    expect(containingSectionRowId(map, "m1", "epics")).toBeNull(); // milestone hidden in epics lens
    expect(containingSectionRowId(map, "m1", "features")).toBeNull(); // milestone hidden in features lens
    expect(containingSectionRowId(map, "e1", "features")).toBeNull(); // epic hidden in features lens
  });

  it("returns null for an id the map does not hold", () => {
    expect(containingSectionRowId(nibMapOf([]), "nibs-gone", "epics")).toBeNull();
  });

  // The whole point of routing this through `place`: the answer is read off the
  // same decision the emitted tree was built from, so the two cannot disagree
  // about where a row is. Checked against the real tree rather than asserted.
  //
  // Every nib in the fixture is asked, in every grouping lens, so a fixture only
  // has to be a shape — nothing has to predict which nibs land in a section.
  function expectEveryAnswerNamesAContainingRow(nibs: TreeNib[]): void {
    const map = nibMapOf(nibs);
    let answered = 0;
    for (const viewLevel of ["milestones", "epics", "features"] as const) {
      const tree = buildViewTree(nibs, viewLevel);
      const rowIds = new Set<string>();
      const walk = (nodes: TreeNode<TreeNib>[]): void => {
        for (const node of nodes) {
          rowIds.add(node.nib.id);
          walk(node.children);
        }
      };
      walk(tree);

      for (const nib of nibs) {
        // null is a legitimate answer — the nib heads a section, or the lens
        // hides it — and carries no claim about a row.
        const containerId = containingSectionRowId(map, nib.id, viewLevel);
        if (containerId === null) continue;
        answered++;
        expect(
          rowIds,
          `${nib.id} named ${containerId} in the ${viewLevel} view, which is not a row there`,
        ).toContain(containerId);
        expect(
          collectDescendantIds(tree, containerId),
          `${containerId} should hold ${nib.id} in the ${viewLevel} view`,
        ).toContain(nib.id);
      }
    }
    expect(answered, "no nib named a container — this check would pass vacuously").toBeGreaterThan(0);
  }

  it("names a row that really does contain the item in the emitted tree", () => {
    expectEveryAnswerNamesAContainingRow([
      makeTreeNib({ id: "m1", type: "milestone" }),
      makeTreeNib({ id: "e1", type: "epic", parentId: "m1" }),
      makeTreeNib({ id: "t1", type: "task", parentId: "e1" }),
      makeTreeNib({ id: "t2", type: "task" }),
    ]);
  });

  // The sweep above skips a null answer, since heading a section and being hidden
  // are both legitimate reasons for one. That makes it blind in one direction: an
  // item wrongly placed `hidden` answers null, gets skipped, and never contradicts
  // anything. This names the leftover direction outright so that regression has
  // somewhere to fail.
  it("names the leftover section for an item no header at this tier claims", () => {
    const nibs = [
      makeTreeNib({ id: "e1", type: "epic" }),
      makeTreeNib({ id: "loose", type: "task" }),
    ];
    const map = nibMapOf(nibs);

    const containerId = containingSectionRowId(map, "loose", "epics");

    expect(containerId).not.toBeNull();
    expect(collectDescendantIds(buildViewTree(nibs, "epics"), containerId!)).toContain("loose");
  });

  // A cycle has no root of its own, so `buildTree` promotes one member (lowest
  // id) and severs its parent edge — which decides the rendered root-to-nib path
  // for every nib in the cycle AND every nib merely hanging off one. `place` must
  // reach the same conclusion, or it names a section the emitted tree does not
  // contain. The sole caller today deletes the id from a collapse set, where an
  // absent id is a silent no-op, so nothing but these fixtures would notice.
  describe("under a parent cycle", () => {
    it("holds when the item merely leads into the cycle", () => {
      expectEveryAnswerNamesAContainingRow([
        makeTreeNib({ id: "a", type: "milestone", parentId: "b" }),
        makeTreeNib({ id: "b", type: "epic", parentId: "a" }),
        makeTreeNib({ id: "t", type: "task", parentId: "b" }),
      ]);
    });

    it("holds when the promoted member is not the last one the climb reaches", () => {
      // Three epics in a cycle: "x" is promoted and becomes the root, so the
      // path to "t" is x -> t and the section is x — not the last epic a climb
      // from "t" happens to walk through.
      expectEveryAnswerNamesAContainingRow([
        makeTreeNib({ id: "x", type: "epic", parentId: "y" }),
        makeTreeNib({ id: "y", type: "epic", parentId: "z" }),
        makeTreeNib({ id: "z", type: "epic", parentId: "x" }),
        makeTreeNib({ id: "t", type: "task", parentId: "x" }),
      ]);
    });

    it("holds for a cycle of above-tier containers", () => {
      expectEveryAnswerNamesAContainingRow([
        makeTreeNib({ id: "m1", type: "milestone", parentId: "m2" }),
        makeTreeNib({ id: "m2", type: "milestone", parentId: "m1" }),
        makeTreeNib({ id: "e1", type: "epic", parentId: "m1" }),
        makeTreeNib({ id: "t1", type: "task", parentId: "m2" }),
      ]);
    });

    it("holds for a self-parent", () => {
      expectEveryAnswerNamesAContainingRow([
        makeTreeNib({ id: "s1", type: "epic", parentId: "s1" }),
        makeTreeNib({ id: "s2", type: "task", parentId: "s1" }),
        makeTreeNib({ id: "s3", type: "task", parentId: "s3" }),
      ]);
    });
  });
});

describe("holdsChildrenByDisplay", () => {
  /**
   * Nodes are built by hand rather than through `buildViewTree`, and they have to
   * be: `buildTree` nests a child only under the parent its `parentId` names, so
   * the only node in any tree it emits whose children disagree is the synthetic
   * bucket. A real nib heading a section of members that are not its children —
   * the shape this predicate exists to recognize — is unreachable from a nib list,
   * which would make every assertion below pass against the bucket-only predicate
   * it replaces.
   */
  function nodeOf(nib: TreeNib, children: TreeNib[]): TreeNode<TreeNib> {
    return { nib, children: children.map((c) => ({ nib: c, children: [], depth: 1 })), depth: 0 };
  }

  it("is false for a container whose children all name it as their parent", () => {
    const epic = makeTreeNib({ id: "e1", type: "epic" });
    const node = nodeOf(epic, [
      makeTreeNib({ id: "t1", type: "task", parentId: "e1" }),
      makeTreeNib({ id: "t2", type: "task", parentId: "e1" }),
    ]);
    expect(holdsChildrenByDisplay(node)).toBe(false);
  });

  it("is false for a leaf, which holds nothing either way", () => {
    expect(holdsChildrenByDisplay(nodeOf(makeTreeNib({ id: "t1", type: "task" }), []))).toBe(false);
  });

  it("is true for a synthetic bucket, whose loose items name some other parent or none", () => {
    const node = nodeOf(makeTreeNib({ id: "/__no_epic__", type: "" }), [
      makeTreeNib({ id: "t1", type: "task", parentId: null }),
      makeTreeNib({ id: "t2", type: "task", parentId: "m1" }),
    ]);
    expect(holdsChildrenByDisplay(node)).toBe(true);
  });

  it("is true for a real nib heading a section of members that are not its children", () => {
    // A task cannot BE a milestone's child — VALID_CHILD_TYPES.milestone is []
    // (asserted in typeHierarchy.test.ts), so `nibs new "Ship it" -t task
    // --parent nibs-mile1` is refused by the server. Membership is therefore
    // always by arrangement here, never by parentage, and the disagreement below
    // is the only signal available.
    const node = nodeOf(makeTreeNib({ id: "nibs-mile1", type: "milestone" }), [
      makeTreeNib({ id: "t1", type: "task", parentId: null }),
    ]);
    expect(holdsChildrenByDisplay(node)).toBe(true);
    // ...and it is emphatically not synthetic. The two questions are independent.
    expect(isSyntheticRowId(node.nib.id)).toBe(false);
  });

  it("is true when only SOME children disagree", () => {
    const node = nodeOf(makeTreeNib({ id: "e1", type: "epic" }), [
      makeTreeNib({ id: "t1", type: "task", parentId: "e1" }),
      makeTreeNib({ id: "t2", type: "task", parentId: null }),
    ]);
    expect(holdsChildrenByDisplay(node)).toBe(true);
  });
});

describe("isSyntheticRowId", () => {
  it("is true for synthetic bucket ids and false for real nib ids", () => {
    expect(isSyntheticRowId("/__no_epic__")).toBe(true);
    expect(isSyntheticRowId("/__no_milestone__")).toBe(true);
    expect(isSyntheticRowId("/__no_feature_or_bug__")).toBe(true);
    expect(isSyntheticRowId("nibs-abc1")).toBe(false);
    expect(isSyntheticRowId("")).toBe(false);
    // Both halves are required, so an id missing either is an ordinary nib id —
    // which is what keeps one that merely RESEMBLES a bucket out.
    expect(isSyntheticRowId("__proj-abc1")).toBe(false); // no leading slash
    expect(isSyntheticRowId("/__no_epic__x")).toBe(false); // ends inside [0-9a-z]
    expect(isSyntheticRowId("no_epic")).toBe(false); // neither half
    // ...and it admits ANY id holding both halves, not only the shipped keys:
    // that is what lets `sectionRowId` derive a container id from a section key
    // it has never seen.
    expect(isSyntheticRowId("/section:tnib-a1b2_")).toBe(true);
    // The underscore-fenced strings are ordinary nib ids: a filename holds every
    // character in them, so `__no_epic__.md` parses back to exactly this id and
    // it must NOT be mistaken for a bucket.
    expect(isSyntheticRowId("__no_epic__")).toBe(false);
    expect(isSyntheticRowId("__no_milestone__")).toBe(false);
    expect(isSyntheticRowId("__no_feature_or_bug__")).toBe(false);
  });

  // The property every caller of this predicate leans on. It asks whether a row
  // has a nib behind it — NOT whether the row is a section header, which is a
  // separate question `holdsChildrenByDisplay` answers from the node. A real nib
  // heading a section of its own is still a nib: selection, detail-open, the
  // action-target resolver, drag and the id column all admit it, and a predicate
  // that swept section headers in would take every one of those away at once.
  it("is false for a real nib that heads a section", () => {
    const header = makeTreeNib({ id: "nibs-mile1", type: "milestone", title: "v2.0" });
    expect(isSyntheticRowId(header.id)).toBe(false);
  });

  // The disjointness `isSyntheticRowId` rests on needs TWO properties of every bucket
  // id, and neither is sufficient alone (see isSyntheticRowId): the leading "/"
  // keeps it out of the filename-derived id space, and a last character outside
  // [0-9a-z] keeps it out of `nib.NewID`'s prefix-plus-nanoid space. This rule
  // has already been written down wrongly once as prose, which has no compiler —
  // so it is asserted here instead, against the derived set the production code
  // actually uses.
  it("every bucket id leads with a slash and ends outside the nanoid charset", () => {
    expect(BUCKET_IDS.size, "no bucket ids to check — this guard would pass vacuously").toBeGreaterThan(0);
    for (const id of BUCKET_IDS) {
      expect(
        id.startsWith("/"),
        `bucket id ${JSON.stringify(id)} must lead with "/" — otherwise an id parsed from a filename could equal it`,
      ).toBe(true);
      expect(
        /[0-9a-z]$/.test(id),
        `bucket id ${JSON.stringify(id)} must not end in [0-9a-z] — otherwise nib.NewID could mint it from a caller prefix`,
      ).toBe(false);
    }
  });
});

// `assembleSection` returns `[...header.children, ...members]` under
// `nestHeadersStructurally`, and those header children are the header's own
// structural subtree — so a lens that nests structurally AND declares a region
// sweeps genuine parent-axis children into it, giving them a group whose
// membership the server would refuse. Nothing in the type system rules the
// combination out, so it is asserted here, over lenses DERIVED from
// `viewShapeFor` for the same reason BUCKET_IDS is: a hand-kept list enrolls a
// new lens only if someone remembers to.
describe("shipped grouping lenses", () => {
  it("never nest headers structurally AND declare a childRegion", () => {
    const lenses = VIEW_LEVELS.map(viewShapeFor)
      .filter((shape) => shape.kind === "grouped")
      .map((shape) => shape.lens);
    expect(
      lenses.length,
      "no grouped lenses to check — this guard would pass vacuously",
    ).toBeGreaterThan(0);
    for (const lens of lenses) {
      if (!lens.nestHeadersStructurally) continue;
      // Section keys are derived from nib data, so probe an arbitrary one as
      // well as the leftover: a lens may answer differently per section.
      for (const key of [lens.leftover.key, "nibs-probe1"]) {
        expect(lens.childRegion(key), `${lens.leftover.key} / ${key}`).toBeNull();
      }
    }
  });
});

describe("collectDescendantIds", () => {
  it("returns an empty set when the root id is not in the tree", () => {
    const nibs: TreeNib[] = [makeTreeNib({ id: "nibs-001", type: "milestone" })];
    const tree = buildViewTree(nibs, "milestones");
    expect(collectDescendantIds(tree, "nibs-missing")).toEqual(new Set());
  });

  it("returns an empty set for a leaf root (no descendants)", () => {
    const nibs: TreeNib[] = [
      makeTreeNib({ id: "nibs-001", type: "milestone" }),
      makeTreeNib({ id: "nibs-002", type: "epic", parentId: "nibs-001" }),
    ];
    const tree = buildViewTree(nibs, "milestones");
    // nibs-002 (epic) is a leaf in this tree.
    expect(collectDescendantIds(tree, "nibs-002")).toEqual(new Set());
  });

  it("collects all descendants of a root, excluding the root itself", () => {
    const nibs: TreeNib[] = [
      makeTreeNib({ id: "nibs-001", type: "milestone" }),
      makeTreeNib({ id: "nibs-002", type: "epic", milestone: "nibs-001", milestoneOrder: "a" }),
      makeTreeNib({ id: "nibs-003", type: "feature", parentId: "nibs-002" }),
      makeTreeNib({ id: "nibs-004", type: "task", parentId: "nibs-003" }),
      makeTreeNib({ id: "nibs-005", type: "task", parentId: "nibs-002" }),
    ];
    const tree = buildViewTree(nibs, "milestones");
    const result = collectDescendantIds(tree, "nibs-001");
    expect(result).toEqual(new Set(["nibs-002", "nibs-003", "nibs-004", "nibs-005"]));
    expect(result.has("nibs-001")).toBe(false);
  });

  it("collects descendants of a mid-tree node (not just the root)", () => {
    const nibs: TreeNib[] = [
      makeTreeNib({ id: "nibs-001", type: "milestone" }),
      makeTreeNib({ id: "nibs-002", type: "epic", parentId: "nibs-001" }),
      makeTreeNib({ id: "nibs-003", type: "feature", parentId: "nibs-002" }),
      makeTreeNib({ id: "nibs-004", type: "task", parentId: "nibs-003" }),
    ];
    const tree = buildViewTree(nibs, "milestones");
    expect(collectDescendantIds(tree, "nibs-002")).toEqual(new Set(["nibs-003", "nibs-004"]));
  });

  it("computes descendants against the DISPLAYED view tree, honouring the grouping lens", () => {
    // In the epics lens, the milestone is hidden (above the epic tier) and the
    // epic becomes a root header keeping its full subtree.
    const nibs: TreeNib[] = [
      makeTreeNib({ id: "m1", type: "milestone" }),
      makeTreeNib({ id: "e1", type: "epic", parentId: "m1" }),
      makeTreeNib({ id: "f1", type: "feature", parentId: "e1" }),
      makeTreeNib({ id: "t1", type: "task", parentId: "f1" }),
    ];
    const tree = buildViewTree(nibs, "epics");
    // Milestone is not a node in this view → not found.
    expect(collectDescendantIds(tree, "m1")).toEqual(new Set());
    // Epic header owns its whole subtree.
    expect(collectDescendantIds(tree, "e1")).toEqual(new Set(["f1", "t1"]));
  });

  it("collects loose items under the synthetic 'No X' bucket", () => {
    // Loose tasks with no epic ancestor fall into the /__no_epic__ bucket.
    const nibs: TreeNib[] = [
      makeTreeNib({ id: "t1", type: "task" }),
      makeTreeNib({ id: "t2", type: "task" }),
    ];
    const tree = buildViewTree(nibs, "epics");
    expect(collectDescendantIds(tree, "/__no_epic__")).toEqual(new Set(["t1", "t2"]));
  });

  it("does not overflow on a cyclic tree (visited guard)", () => {
    // Hand-build a pathological cyclic node graph (buildTree cannot normally
    // produce this, but the walk must not hang if one ever appears).
    const a: TreeNode<TreeNib> = { nib: makeTreeNib({ id: "a" }), children: [], depth: 0 };
    const b: TreeNode<TreeNib> = { nib: makeTreeNib({ id: "b" }), children: [], depth: 1 };
    a.children.push(b);
    b.children.push(a); // cycle: b -> a -> b -> ...
    const result = collectDescendantIds([a], "a");
    expect(result).toEqual(new Set(["a", "b"]));
  });
});
