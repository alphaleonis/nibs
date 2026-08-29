import { describe, it, expect } from "vitest";
import { buildTree, buildViewTree, isBucketId, bucketIdForItem, collectDescendantIds, BUCKET_IDS } from "./tree";
import { makeNibComparator } from "./tableSort";
import { typeRank } from "./typeHierarchy";
import type { TreeNib, TreeTableNib, TreeNode, ViewLevel, TableSort } from "./types";

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
      const ids = collectIds(buildViewTree(nibs, "epics")).filter((id) => !isBucketId(id));
      expect(ids.sort()).toEqual(["a", "b"]);
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
      expect(result.some((n) => isBucketId(n.nib.id))).toBe(false);
    });

    it("returns an empty forest for empty input", () => {
      expect(buildViewTree([], "flat")).toEqual([]);
    });
  });

  describe("milestones lens", () => {
    it("promotes milestones to headers with full subtrees; loose feature/bug go under 'No milestone'", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Milestone A", type: "milestone" }),
        makeTreeNib({ id: "nibs-002", title: "Epic under A", type: "epic", parentId: "nibs-001" }),
        makeTreeNib({ id: "nibs-003", title: "Task under epic", type: "task", parentId: "nibs-002" }),
        makeTreeNib({ id: "nibs-004", title: "Root feature", type: "feature" }),
        makeTreeNib({ id: "nibs-005", title: "Root bug", type: "bug" }),
      ];

      const result = buildViewTree(nibs, "milestones");

      // Milestone header keeps its full subtree
      expect(result[0].nib.id).toBe("nibs-001");
      expect(result[0].depth).toBe(0);
      expect(result[0].children[0].nib.id).toBe("nibs-002");
      expect(result[0].children[0].depth).toBe(1);
      expect(result[0].children[0].children[0].nib.id).toBe("nibs-003");
      expect(result[0].children[0].children[0].depth).toBe(2);

      // Single "No milestone" bucket holds the loose feature and bug
      const bucket = result.find(r => isBucketId(r.nib.id))!;
      expect(bucket).toBeDefined();
      expect(bucket.nib.id).toBe("/__no_milestone__");
      const bucketChildIds = bucket.children.map(c => c.nib.id);
      expect(bucketChildIds).toEqual(["nibs-004", "nibs-005"]);

      // No milestone-rank item was hidden (Milestone A is present as a header)
      expect(result.filter(r => r.nib.id === "nibs-001")).toHaveLength(1);
    });

    it("keeps a nested milestone inside its parent milestone (no duplication)", () => {
      const nibs: TreeNib[] = [
        makeTreeNib({ id: "nibs-001", title: "Parent milestone", type: "milestone" }),
        makeTreeNib({ id: "nibs-002", title: "Child milestone", type: "milestone", parentId: "nibs-001" }),
        makeTreeNib({ id: "nibs-003", title: "Task under child", type: "task", parentId: "nibs-002" }),
      ];

      const result = buildViewTree(nibs, "milestones");

      // Only the outer milestone is a top-level header; child stays nested (not re-promoted)
      expect(result).toHaveLength(1);
      expect(result[0].nib.id).toBe("nibs-001");
      expect(result[0].children).toHaveLength(1);
      expect(result[0].children[0].nib.id).toBe("nibs-002");
      expect(result[0].children[0].children[0].nib.id).toBe("nibs-003");
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
      const bucket = result.find(r => isBucketId(r.nib.id))!;
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
        const outputIds = collectIds(result).filter(id => !isBucketId(id));

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

      const bucket = result.find(r => isBucketId(r.nib.id))!;
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
        const bucket = result.find((r) => isBucketId(r.nib.id))!;
        expect(bucket.children.map((c) => c.nib.id)).toEqual(["tZ", "tA"]);
      });

      it("title asc → bucket items in GLOBAL order [Apple, Zebra]", () => {
        const cmp = cmpFor(nibs, { field: "title", direction: "asc" });
        const result = buildViewTree(nibs, "epics", cmp);
        const bucket = result.find((r) => isBucketId(r.nib.id))!;
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

describe("bucketIdForItem", () => {
  function nibMapOf(nibs: TreeNib[]): Map<string, TreeNib> {
    return new Map(nibs.map(n => [n.id, n]));
  }

  it("returns null for the none lens (no buckets)", () => {
    const map = nibMapOf([makeTreeNib({ id: "t1", type: "task" })]);
    expect(bucketIdForItem(map, "t1", "none")).toBeNull();
  });

  it("returns the lens bucket for a loose item with no grouping ancestor", () => {
    const map = nibMapOf([makeTreeNib({ id: "t1", type: "task" })]);
    expect(bucketIdForItem(map, "t1", "epics")).toBe("/__no_epic__");
  });

  it("returns null when the item sits under a grouping header", () => {
    const map = nibMapOf([
      makeTreeNib({ id: "e1", type: "epic" }),
      makeTreeNib({ id: "f1", type: "feature", parentId: "e1" }),
      makeTreeNib({ id: "t1", type: "task", parentId: "f1" }),
    ]);
    expect(bucketIdForItem(map, "t1", "epics")).toBeNull();
  });

  it("returns null when the item IS a grouping header itself", () => {
    const map = nibMapOf([makeTreeNib({ id: "e1", type: "epic" })]);
    expect(bucketIdForItem(map, "e1", "epics")).toBeNull();
  });

  it("buckets a loose task under a milestone (above-tier ancestor, no epic)", () => {
    const map = nibMapOf([
      makeTreeNib({ id: "m1", type: "milestone" }),
      makeTreeNib({ id: "t1", type: "task", parentId: "m1" }),
    ]);
    expect(bucketIdForItem(map, "t1", "epics")).toBe("/__no_epic__");
  });

  it("features lens: task under an epic (no feature/bug) lands in the bucket", () => {
    const map = nibMapOf([
      makeTreeNib({ id: "e1", type: "epic" }),
      makeTreeNib({ id: "t1", type: "task", parentId: "e1" }),
    ]);
    expect(bucketIdForItem(map, "t1", "features")).toBe("/__no_feature_or_bug__");
  });

  it("features lens: task under a feature is under a header (null)", () => {
    const map = nibMapOf([
      makeTreeNib({ id: "f1", type: "feature" }),
      makeTreeNib({ id: "t1", type: "task", parentId: "f1" }),
    ]);
    expect(bucketIdForItem(map, "t1", "features")).toBeNull();
  });

  // An item whose OWN rank is above the grouping tier is hidden outright by
  // buildViewTree (not swept into a bucket), so it has no enclosing bucket —
  // bucketIdForItem must agree and return null, else ensure-visible would
  // spuriously un-collapse a bucket when deep-linking to such a container.
  it("returns null for an above-tier container queried in a lower lens", () => {
    const map = nibMapOf([
      makeTreeNib({ id: "m1", type: "milestone" }),
      makeTreeNib({ id: "e1", type: "epic", parentId: "m1" }),
    ]);
    expect(bucketIdForItem(map, "m1", "epics")).toBeNull(); // milestone hidden in epics lens
    expect(bucketIdForItem(map, "m1", "features")).toBeNull(); // milestone hidden in features lens
    expect(bucketIdForItem(map, "e1", "features")).toBeNull(); // epic hidden in features lens
  });
});

describe("isBucketId", () => {
  it("is true for synthetic bucket ids and false for real nib ids", () => {
    expect(isBucketId("/__no_epic__")).toBe(true);
    expect(isBucketId("/__no_milestone__")).toBe(true);
    expect(isBucketId("/__no_feature_or_bug__")).toBe(true);
    expect(isBucketId("nibs-abc1")).toBe(false);
    expect(isBucketId("")).toBe(false);
    // Membership is exact, not prefix- or substring-based: an id that merely
    // resembles a bucket id is still an ordinary nib id.
    expect(isBucketId("__proj-abc1")).toBe(false);
    expect(isBucketId("/__no_epic__x")).toBe(false);
    expect(isBucketId("no_epic")).toBe(false);
    // The underscore-fenced strings are ordinary nib ids: a filename holds every
    // character in them, so `__no_epic__.md` parses back to exactly this id and
    // it must NOT be mistaken for a bucket.
    expect(isBucketId("__no_epic__")).toBe(false);
    expect(isBucketId("__no_milestone__")).toBe(false);
    expect(isBucketId("__no_feature_or_bug__")).toBe(false);
  });

  // The disjointness `isBucketId` rests on needs TWO properties of every bucket
  // id, and neither is sufficient alone (see GROUPING_LENSES): the leading "/"
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
      makeTreeNib({ id: "nibs-002", type: "epic", parentId: "nibs-001" }),
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
