import { describe, it, expect, vi, afterEach } from "vitest";
import type { TreeTableNib, TreeNode, NibFilter } from "./types";
import type { Region } from "./ordering/region";

/**
 * A section header that is BOTH a header and a real nib — a milestone heading
 * its own membership section — cannot be produced from a nib list: `buildTree`
 * only ever nests a child under the parent its `parentId` names, so the only
 * node in any tree it emits whose children disagree is the synthetic "No X"
 * bucket. `buildTableData` builds its own view tree and takes no tree argument,
 * so the shape is injected at the module boundary instead. Everything the
 * assertions below actually exercise — the display-container fold and the
 * flatten pass — is the real code.
 */
const hooks = vi.hoisted(() => ({ viewTree: null as unknown }));

vi.mock("./tree", async (importOriginal) => {
  const actual = await importOriginal<typeof import("./tree")>();
  return {
    ...actual,
    buildShapedViewTree: (...args: Parameters<typeof actual.buildShapedViewTree>) =>
      hooks.viewTree ?? actual.buildShapedViewTree(...args),
  };
});

const { buildTableData } = await import("./tableData");

function makeTreeTableNib(overrides: Partial<TreeTableNib> = {}): TreeTableNib {
  return {
    id: "nibs-abc1",
    title: "Fix login bug",
    status: "in-progress",
    type: "bug",
    priority: "high",
    estimate: "m",
    tags: [],
    createdAt: "2026-03-15T10:00:00Z",
    updatedAt: "2026-03-20T10:00:00Z",
    parentId: null,
    milestone: "",
    milestoneOrder: "",
    blockingIds: [],
    blockedByIds: [],
    etag: "etag-test",
    ...overrides,
  };
}

const emptyFilter: NibFilter = {};
const noCollapsed = new Set<string>();

afterEach(() => {
  hooks.viewTree = null;
});

describe("a membership section header (a real nib holding rows that are not its children)", () => {
  // The member's parentId is null, not "M1": a task can never be a milestone's
  // child. `nibs new "Ship it" -t task --parent M1` is refused by the server
  // because VALID_CHILD_TYPES.milestone is [] (asserted in typeHierarchy.test.ts),
  // which is what makes "held by display" and "held by parentage" answerable
  // from the tree alone.
  const header = makeTreeTableNib({ id: "M1", title: "v2.0", type: "milestone" });
  const member = makeTreeTableNib({ id: "T1", title: "Ship it", type: "task", tags: ["queued"] });
  const outsider = makeTreeTableNib({ id: "T2", title: "Elsewhere", type: "task" });
  const allNibs = [header, member, outsider];

  function membershipTree(): TreeNode<TreeTableNib>[] {
    return [
      { nib: header, children: [{ nib: member, children: [], depth: 1 }], depth: 0 },
      { nib: outsider, children: [], depth: 0 },
    ];
  }

  it("its members inherit the header's display parent, never the header's own id", () => {
    hooks.viewTree = membershipTree();

    const { rows } = buildTableData(allNibs, emptyFilter, "milestones", noCollapsed);

    // displayParentId is what drag-reorder hands the backend as a parent id, so
    // naming the header here would issue a reparent the server rejects outright
    // — a milestone accepts no children of any type. The header is itself a
    // display root, so its members inherit that root position (null).
    expect(rows.find((r) => r.nib.id === "T1")!.displayParentId).toBeNull();
    expect(rows.find((r) => r.nib.id === "M1")!.displayParentId).toBeNull();
  });

  it("is collapsible, though no nib names it as a parent", () => {
    hooks.viewTree = membershipTree();

    const { parentIds } = buildTableData(allNibs, emptyFilter, "milestones", noCollapsed);

    // parentIds is seeded from real `parentId` links, which a membership section
    // has none of; without the display-container fold the header would render a
    // section it offers no way to close.
    expect(parentIds.has("M1")).toBe(true);
  });

  it("hides its members when collapsed", () => {
    hooks.viewTree = membershipTree();

    const { rows } = buildTableData(allNibs, emptyFilter, "milestones", new Set(["M1"]));

    const ids = rows.map((r) => r.nib.id);
    expect(ids).toContain("M1");
    expect(ids).not.toContain("T1");
  });

  it("survives a filter that matches only inside it", () => {
    hooks.viewTree = membershipTree();
    const filter: NibFilter = { tags: ["queued"] };

    const { rows } = buildTableData(allNibs, filter, "milestones", noCollapsed);

    const ids = rows.map((r) => r.nib.id);
    // The header is not itself a match and is not any member's ancestor, so
    // nothing puts it in visibleIds but the fold. Dropping it drops its whole
    // section with it — the matching member included.
    expect(ids).toContain("T1");
    expect(ids).toContain("M1");
    expect(ids).not.toContain("T2");
    // Present but dimmed: the header is a real nib, so a filter it does not
    // match dims it like any other row rather than passing it through.
    expect(rows.find((r) => r.nib.id === "M1")!.dimmed).toBe(true);
    expect(rows.find((r) => r.nib.id === "T1")!.dimmed).toBe(false);
  });
});

/**
 * The same injection, for the other half of the section header: what it declares
 * its rows are ORDERED in. No shipped lens declares anything yet, so the
 * declaration is put on the injected node directly.
 */
describe("a container that declares a childRegion", () => {
  const header = makeTreeTableNib({ id: "M1", title: "v2.0", type: "milestone" });
  const queued = makeTreeTableNib({ id: "E1", title: "Queued epic", type: "epic" });
  const sub = makeTreeTableNib({ id: "T1", title: "Subtask", type: "task", parentId: "E1" });
  const allNibs = [header, queued, sub];
  const queue: Region = { axis: "milestone", milestoneId: "M1" };

  function queueTree(): TreeNode<TreeTableNib>[] {
    return [
      {
        nib: header,
        depth: 0,
        childRegion: queue,
        children: [{ nib: queued, depth: 1, children: [{ nib: sub, depth: 2, children: [] }] }],
      },
    ];
  }

  it("puts its own rows in the declared region, and only its own", () => {
    hooks.viewTree = queueTree();

    const { rows } = buildTableData(allNibs, emptyFilter, "milestones", noCollapsed);
    const row = (id: string) => rows.find((r) => r.nib.id === id)!;

    expect(row("E1").region).toEqual(queue);
    // The declaration covers the container's rows, not everything beneath them:
    // a queued epic's subtask orders under the epic, not in the queue the epic
    // sits in.
    expect(row("E1").childRegion).toBeNull();
    expect(row("T1").region).toEqual({ axis: "parent", parentId: "E1" });
    // The container is a real nib, so it is a member of its own parent group
    // like any other row; only what it declares for its children differs.
    expect(row("M1").region).toEqual({ axis: "parent", parentId: null });
    expect(row("M1").childRegion).toEqual(queue);
  });

  it("keeps a descendant with its own assignment out of the queue too", () => {
    // The one case the one-level rule could plausibly have overlooked. It is not
    // reachable through the API — `updateNib` refuses to assign a nib whose
    // ancestor is already assigned, and `setParent` refuses the move that would
    // create the same pair — but a hand-authored file can hold it, and then the
    // answer must still be the position the row is DRAWN in: T1 sits under E1,
    // so its row orders among E1's children. Its queue position exists on the
    // server and is simply not what this view is showing.
    const assigned = { ...sub, milestone: "M1" };
    hooks.viewTree = [
      {
        nib: header,
        depth: 0,
        childRegion: queue,
        children: [{ nib: queued, depth: 1, children: [{ nib: assigned, depth: 2, children: [] }] }],
      },
    ] satisfies TreeNode<TreeTableNib>[];

    const { rows } = buildTableData([header, queued, assigned], emptyFilter, "milestones", noCollapsed);

    expect(rows.find((r) => r.nib.id === "T1")!.region).toEqual({ axis: "parent", parentId: "E1" });
  });

  it("changes no row's displayParentId", () => {
    hooks.viewTree = queueTree();

    const { rows } = buildTableData(allNibs, emptyFilter, "milestones", noCollapsed);
    const row = (id: string) => rows.find((r) => r.nib.id === id)!;

    // The two fields answer different questions and are threaded separately: E1
    // is drawn at the header's own display root while ordering in the queue, and
    // T1 is drawn under E1 while ordering among E1's children.
    expect(row("M1").displayParentId).toBeNull();
    expect(row("E1").displayParentId).toBeNull();
    expect(row("T1").displayParentId).toBe("E1");
  });
});
