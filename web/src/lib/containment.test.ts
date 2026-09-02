import { describe, expect, it } from "vitest";
import { buildContainmentIndex } from "./containment";
import { buildShapedTableData } from "./tableData";
import { EMPTY_SPINE } from "./viewSpine";
import type { TreeNib, TreeNode, TreeTableNib, ViewLevel } from "./types";

const { buildViewTree, viewShapeFor } = EMPTY_SPINE;

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
    area: "",
    ...overrides,
  };
}

function makeTableNib(overrides: Partial<TreeTableNib> = {}): TreeTableNib {
  return {
    ...makeTreeNib(overrides),
    blockingIds: [],
    blockedByIds: [],
    etag: "etag-test",
    ...overrides,
  };
}

/** The index of a whole view. */
function indexOf(nibs: TreeNib[], level: ViewLevel) {
  return buildContainmentIndex(buildViewTree(nibs, level));
}

describe("containerOf and chainOf", () => {
  it("answer null and empty for a root", () => {
    const index = indexOf([makeTreeNib({ id: "t1", type: "task" })], "none");
    expect(index.containerOf("t1")).toBeNull();
    expect(index.chainOf("t1")).toEqual([]);
  });

  it("name the section a grouped view drew a loose row into", () => {
    const index = indexOf([makeTreeNib({ id: "t1", type: "task" })], "epics");
    expect(index.containerOf("t1")).toBe("/__no_epic__");
    expect(index.chainOf("t1")).toEqual(["/__no_epic__"]);
  });

  it("name a membership section, which is no ancestor of its members", () => {
    const nibs = [
      makeTreeNib({ id: "m1", type: "milestone" }),
      makeTreeNib({ id: "t1", type: "task", milestone: "m1" }),
      makeTreeNib({ id: "l1", type: "task" }),
    ];
    const index = indexOf(nibs, "milestones");

    expect(index.containerOf("t1")).toBe("m1");
    expect(index.containerOf("l1")).toBe("/__backlog__");
    // A milestone heads its own section and is contained by nothing.
    expect(index.containerOf("m1")).toBeNull();
  });

  // The whole reason this is a CHAIN and not one id: a type lens keeps a
  // header's subtree, so a row can sit several containers deep, and the
  // innermost is not the section.
  it("walk out through every enclosing container, innermost first", () => {
    const nibs = [
      makeTreeNib({ id: "e1", type: "epic" }),
      makeTreeNib({ id: "f1", type: "feature", parentId: "e1" }),
      makeTreeNib({ id: "t1", type: "task", parentId: "f1" }),
    ];
    const index = indexOf(nibs, "epics");

    expect(index.containerOf("t1")).toBe("f1");
    expect(index.chainOf("t1")).toEqual(["f1", "e1"]);
  });
});

describe("has", () => {
  it("separates a root from an id this view never drew", () => {
    const nibs = [
      makeTreeNib({ id: "m1", type: "milestone" }),
      makeTreeNib({ id: "e1", type: "epic", parentId: "m1" }),
    ];
    const index = indexOf(nibs, "epics");

    // Both answer null from `containerOf`, and they are not the same answer: the
    // epic is a root, the milestone is ranked above this lens's tier and has no
    // node at all.
    expect(index.containerOf("e1")).toBeNull();
    expect(index.containerOf("m1")).toBeNull();
    expect(index.has("e1")).toBe(true);
    expect(index.has("m1")).toBe(false);
  });

  it("is false for an id no nib in the response carries, which then has no chain", () => {
    const index = indexOf([], "epics");
    expect(index.has("nibs-gone")).toBe(false);
    expect(index.chainOf("nibs-gone")).toEqual([]);
  });
});

describe("descendantsOf", () => {
  it("is empty for an id the tree has no node for", () => {
    expect(indexOf([makeTreeNib({ id: "m1", type: "milestone" })], "milestones").descendantsOf("nope"))
      .toEqual(new Set());
  });

  it("is empty for a leaf", () => {
    const nibs = [
      makeTreeNib({ id: "m1", type: "milestone" }),
      makeTreeNib({ id: "e1", type: "epic", parentId: "m1" }),
    ];
    expect(indexOf(nibs, "milestones").descendantsOf("e1")).toEqual(new Set());
  });

  it("collects the whole subtree, excluding the id itself", () => {
    const nibs = [
      makeTreeNib({ id: "m1", type: "milestone" }),
      makeTreeNib({ id: "e1", type: "epic", milestone: "m1", milestoneOrder: "a" }),
      makeTreeNib({ id: "f1", type: "feature", parentId: "e1" }),
      makeTreeNib({ id: "t1", type: "task", parentId: "f1" }),
      makeTreeNib({ id: "t2", type: "task", parentId: "e1" }),
    ];
    const index = indexOf(nibs, "milestones");

    expect(index.descendantsOf("m1")).toEqual(new Set(["e1", "f1", "t1", "t2"]));
    expect(index.descendantsOf("m1").has("m1")).toBe(false);
    expect(index.descendantsOf("e1")).toEqual(new Set(["f1", "t1", "t2"]));
  });

  it("follows the DISPLAYED tree, so a hidden container has no subtree here", () => {
    const nibs = [
      makeTreeNib({ id: "m1", type: "milestone" }),
      makeTreeNib({ id: "e1", type: "epic", parentId: "m1" }),
      makeTreeNib({ id: "f1", type: "feature", parentId: "e1" }),
    ];
    const index = indexOf(nibs, "epics");

    // The milestone is ranked above the epic tier: descended into, never drawn.
    expect(index.descendantsOf("m1")).toEqual(new Set());
    expect(index.descendantsOf("e1")).toEqual(new Set(["f1"]));
  });

  it("hands back the same set on a second ask", () => {
    const index = indexOf([makeTreeNib({ id: "t1", type: "task" })], "epics");
    expect(index.descendantsOf("/__no_epic__")).toBe(index.descendantsOf("/__no_epic__"));
  });
});

/**
 * A node graph `buildTree` cannot produce, so nothing upstream rules it out for
 * this module: the walks have to terminate on their own. The first occurrence of
 * an id is the one indexed, which is what makes the container relation a forest;
 * the downward walk gets no such property from that and carries a visited set.
 */
describe("a cyclic node graph", () => {
  const a: TreeNode<TreeNib> = { nib: makeTreeNib({ id: "a" }), children: [], depth: 0 };
  const b: TreeNode<TreeNib> = { nib: makeTreeNib({ id: "b" }), children: [], depth: 1 };
  a.children.push(b);
  b.children.push(a);
  const index = buildContainmentIndex([a]);

  it("climbs out of it", () => {
    expect(index.chainOf("b")).toEqual(["a"]);
    expect(index.chainOf("a")).toEqual([]);
    expect(index.contains("a", "b")).toBe(true);
    expect(index.contains("b", "a")).toBe(false);
  });

  it("walks down it without collecting the root back", () => {
    expect(index.descendantsOf("a")).toEqual(new Set(["b"]));
  });
});

/**
 * The index is read off the view TREE, so it answers for rows the table never
 * drew — which is the whole reason it can serve reveal, whose subject has no row
 * by definition. No method of it asks what was drawn.
 */
describe("contains", () => {
  const NIBS: TreeTableNib[] = [
    makeTableNib({ id: "m1", type: "milestone", title: "v1" }),
    makeTableNib({ id: "e1", type: "epic", title: "Epic", milestone: "m1" }),
    makeTableNib({ id: "t1", type: "task", title: "Task", parentId: "e1", priority: "low" }),
  ];

  function tableOf(collapsed: string[], filter = {}) {
    return buildShapedTableData(NIBS, filter, viewShapeFor("milestones"), new Set(collapsed), null);
  }

  it("holds while everything is drawn", () => {
    const { rows, containment } = tableOf([]);
    expect(rows.map((r) => r.nib.id)).toEqual(["m1", "e1", "t1"]);
    for (const id of ["e1", "t1"]) {
      expect(containment.contains("m1", id), id).toBe(true);
    }
  });

  it("holds unchanged where a subtree is COLLAPSED", () => {
    const { rows, containment } = tableOf(["m1"]);

    expect(rows.map((r) => r.nib.id)).toEqual(["m1"]);
    // The chain survives the collapse — nothing about the index moved.
    expect(containment.chainOf("t1")).toEqual(["e1", "m1"]);
    expect(containment.has("e1")).toBe(true);
    expect(containment.contains("m1", "e1")).toBe(true);
  });

  it("holds unchanged where a row is FILTERED OUT", () => {
    // `t1` is the only low-priority nib, so this filter drops it and keeps the
    // section around it. Nothing is collapsed, so the filter is the only reason
    // it has no row.
    const { rows, containment } = tableOf([], { priority: ["high"] });

    expect(rows.map((r) => r.nib.id)).toEqual(["m1", "e1"]);
    expect(containment.chainOf("t1")).toEqual(["e1", "m1"]);
    expect(containment.has("t1")).toBe(true);
    expect(containment.contains("m1", "t1")).toBe(true);
  });

  it("answers false for a container that is not an ancestor", () => {
    const { containment } = tableOf([]);
    expect(containment.contains("e1", "m1")).toBe(false);
    expect(containment.contains("t1", "t1")).toBe(false);
  });
});

/**
 * The two facts every consumer leans on, asserted over every shipped view rather
 * than argued: reveal focuses a container it expects to be on screen, and
 * ArrowLeft focuses one without a presence lookup of its own.
 */
describe("what the table drew and what the index holds", () => {
  const NIBS: TreeTableNib[] = [
    makeTableNib({ id: "m1", type: "milestone", title: "v1" }),
    makeTableNib({ id: "e1", type: "epic", title: "Epic", parentId: "m1" }),
    makeTableNib({ id: "f1", type: "feature", title: "Feature", parentId: "e1" }),
    makeTableNib({ id: "t1", type: "task", title: "Task", parentId: "f1" }),
    makeTableNib({ id: "q1", type: "task", title: "Queued", milestone: "m1" }),
    makeTableNib({ id: "l1", type: "task", title: "Loose" }),
  ];

  it.each(["none", "flat", "milestones", "epics", "features"] as const)(
    "%s: a drawn row's container is drawn too, and every drawn row is in the index",
    (level) => {
      const { rows, containment, viewMemberIds } = buildShapedTableData(
        NIBS,
        {},
        viewShapeFor(level),
        new Set(),
        null,
      );
      const drawn = new Set(rows.map((r) => r.nib.id));
      expect(drawn.size).toBeGreaterThan(0);

      for (const id of drawn) {
        expect(containment.has(id), id).toBe(true);
        const container = containment.containerOf(id);
        if (container !== null) expect(drawn, `${id} is drawn inside ${container}`).toContain(container);
      }
      // `viewMemberIds` answers the same membership question off the same tree,
      // so the two must not be able to disagree in either direction.
      for (const id of viewMemberIds) expect(containment.has(id), id).toBe(true);
      for (const id of drawn) expect(viewMemberIds.has(id), id).toBe(true);
    },
  );
});
