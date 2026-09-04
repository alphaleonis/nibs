import { describe, expect, it } from "vitest";
import { makeViewSpine, EMPTY_SPINE, LOADING_SPINE, UNAVAILABLE_SPINE } from "./viewSpine";
import { createAreaVocabulary } from "./areas";
import type { AreaNode, AreaVocabulary } from "./areas";
import { isSyntheticRowId } from "./tree";
import type { GroupingLens, ViewShape } from "./tree";
import { buildShapedTableData } from "./tableData";
import type { RowData } from "./tableData";
import { planDrop } from "./ordering/dropPlan";
import type { DropPlan } from "./ordering/dropPlan";
import { collectDescendantIds } from "./dropZone";
import type { DropZone } from "./drag.svelte";
import { batch, updateNib } from "./mutations/commands";
import type { NibFilter, TreeNode, TreeTableNib } from "./types";

/**
 * The Areas view's lens, reached the only way anything can reach it — through a
 * spine bound to a vocabulary. `areaLens` and `viewShapeFor` are private to
 * viewSpine.ts, so a shape built against the wrong vocabulary is not a call a
 * test can make either.
 */

function nib(overrides: Partial<TreeTableNib> = {}): TreeTableNib {
  return {
    id: "x1",
    title: "Untitled",
    status: "todo",
    type: "task",
    priority: "normal",
    estimate: "m",
    tags: [],
    createdAt: "2026-03-15T10:00:00Z",
    updatedAt: "2026-03-20T10:00:00Z",
    parentId: null,
    milestone: "",
    milestoneOrder: "",
    area: "",
    blockingIds: [],
    blockedByIds: [],
    etag: "etag-test",
    ...overrides,
  };
}

function area(path: string, depth: number, rest: Partial<AreaNode> = {}): AreaNode {
  const segments = path.split("/");
  return {
    path,
    name: segments[segments.length - 1],
    description: "",
    color: "",
    depth,
    ...rest,
  };
}

/** The sample project's shape: two roots with a child each, two barren roots. */
const VOCABULARY: AreaVocabulary = createAreaVocabulary([
  area("auth", 0, { description: "Sign-in and sessions", color: "#3366ff" }),
  area("api", 0),
  area("api/webhooks", 1),
  area("web", 0),
  area("web/dashboard", 1, { color: "slateblue" }),
  area("infra", 0),
]);

const SPINE = makeViewSpine(VOCABULARY);

function lensOf(shape: ViewShape): GroupingLens {
  if (shape.kind !== "grouped") throw new Error(`expected a grouped shape, got ${shape.kind}`);
  return shape.lens;
}

const AREA_SHAPE = SPINE.viewShapeFor("areas");
const LENS = lensOf(AREA_SHAPE);

const NO_AREA = "/__no_area__";
const AUTH = "/section:auth_";
const API = "/section:api_";
const WEBHOOKS = "/section:api/webhooks_";
const WEB = "/section:web_";
const DASHBOARD = "/section:web/dashboard_";
const INFRA = "/section:infra_";

/**
 *   auth        (declared, empty)
 *   api         ah (task)
 *   api/webhooks  wh (task)
 *   web         wf (feature) ─ wt (task, child of wf)
 *               wa (task)
 *               wa2 (task)
 *   web/dashboard (declared, empty)
 *   infra       (declared, empty)
 *   No area     rt (task, area: "legacy" — retired, resolves to nothing)
 *               un (task, no area at all)
 *               ms (milestone)
 */
const FIXTURE: TreeTableNib[] = [
  nib({ id: "ah", title: "Api task", area: "api" }),
  nib({ id: "wh", title: "Webhook task", area: "api/webhooks" }),
  nib({ id: "wf", title: "Web feature", type: "feature", area: "web" }),
  nib({ id: "wt", title: "Web task", area: "web", parentId: "wf" }),
  nib({ id: "wa", title: "Web loose task", area: "web" }),
  nib({ id: "wa2", title: "Web loose task two", area: "web" }),
  nib({ id: "rt", title: "Retired area task", area: "legacy" }),
  nib({ id: "un", title: "Unfiled task" }),
  nib({ id: "ms", title: "A milestone", type: "milestone" }),
];

const noFilter: NibFilter = {};

function tableOf(nibs: TreeTableNib[] = FIXTURE, filter: NibFilter = noFilter) {
  const data = buildShapedTableData(nibs, filter, AREA_SHAPE, new Set());
  return {
    ...data,
    ids: data.rows.map((r) => r.nib.id),
    row: (id: string) => data.rows.find((r) => r.nib.id === id),
  };
}

describe("the areas lens declares the vocabulary", () => {
  it("builds the forest from the flat list's depth runs, in declaration order", () => {
    if (LENS.declares.kind !== "forest") throw new Error("the areas lens must declare a forest");
    const roots = LENS.declares.roots;

    expect(roots.map((r) => r.key)).toEqual(["auth", "api", "web", "infra"]);
    expect(roots.map((r) => r.children.map((c) => c.key))).toEqual([
      [],
      ["api/webhooks"],
      ["web/dashboard"],
      [],
    ]);
  });

  it("labels a node with its own segment and carries its description and color", () => {
    if (LENS.declares.kind !== "forest") throw new Error("the areas lens must declare a forest");
    const [auth, api, web] = LENS.declares.roots;

    expect(auth).toEqual({
      key: "auth",
      label: "auth",
      description: "Sign-in and sessions",
      color: "#3366ff",
      children: [],
    });
    // A nested node shows its own segment, not the path that addresses it — it
    // is drawn inside the parent that supplies the rest.
    expect(api.children[0].label).toBe("webhooks");
    expect(web.children[0]).toEqual({
      key: "web/dashboard",
      label: "dashboard",
      description: "",
      color: "slateblue",
      children: [],
    });
  });

  /**
   * The forest is the DEPTH RUNS — the contract `subtreeOf` reads — and not a
   * re-split of `path` on "/". The two agree on every vocabulary the server can
   * send, so the only thing that tells them apart is a list where depth and path
   * disagree: `alpha` at depth 0 and `beta` at depth 1 nest here and would be
   * two roots under a path split.
   */
  it("reads nesting off depth rather than off the path separator", () => {
    const odd = makeViewSpine(createAreaVocabulary([area("alpha", 0), area("beta", 1)]));
    const declares = lensOf(odd.viewShapeFor("areas")).declares;
    if (declares.kind !== "forest") throw new Error("expected a forest");

    expect(declares.roots.map((r) => r.key)).toEqual(["alpha"]);
    expect(declares.roots[0].children.map((c) => c.key)).toEqual(["beta"]);
  });

  it("keeps a node whose depth names no open ancestor, as a root", () => {
    // A list this walk cannot nest — depth 2 with nothing at depth 1 — still
    // renders every area in it rather than dropping the ones it cannot place.
    const gap = makeViewSpine(createAreaVocabulary([area("root", 0), area("orphan", 2)]));
    const declares = lensOf(gap.viewShapeFor("areas")).declares;
    if (declares.kind !== "forest") throw new Error("expected a forest");

    expect(declares.roots.map((r) => r.key)).toEqual(["root", "orphan"]);
  });

  it("declares no forest at all for a vocabulary with nothing in it", () => {
    for (const spine of [EMPTY_SPINE, LOADING_SPINE, UNAVAILABLE_SPINE]) {
      const declares = lensOf(spine.viewShapeFor("areas")).declares;
      if (declares.kind !== "forest") throw new Error("expected a forest");
      expect(declares.roots).toEqual([]);
    }
  });
});

describe("where the areas lens puts a nib", () => {
  it("groups by ASSIGNMENT, so a section's members are rebuilt from what landed in it", () => {
    expect(LENS.nestHeadersStructurally).toBe(false);
  });

  it("puts a declared assignment in that area's section, nested rows included", () => {
    const { row } = tableOf();

    expect(row("ah")!.section?.key).toBe("api");
    expect(row("wh")!.section?.key).toBe("api/webhooks");
    expect(row("wf")!.section?.key).toBe("web");
    // The child was placed by its own `area`, so it is in the section as much as
    // its parent is — and `buildTree` still nests it under the parent.
    expect(row("wt")!.section?.key).toBe("web");
    expect(row("wt")!.displayParentId).toBe("wf");
  });

  /**
   * RESOLVED, not trusted. A stored `area:` arrives verbatim and can name an
   * area the vocabulary no longer declares; the lens is the only place that can
   * hold that up, and it must, or the section it minted would answer `assign`
   * with a value the server refuses.
   */
  it("sweeps a retired assignment into the leftover, minting no section for it", () => {
    const { ids, row, containment } = tableOf();

    expect(row("rt")!.section?.key).toBe(NO_AREA);
    expect(containment.containerOf("rt")).toBe(NO_AREA);
    expect(ids).not.toContain("/section:legacy_");
  });

  it("sweeps a nib carrying no area into the same leftover", () => {
    const { row, containment } = tableOf();

    expect(row("un")!.section?.key).toBe(NO_AREA);
    expect(containment.containerOf("un")).toBe(NO_AREA);
  });

  it("names the leftover section in the synthetic id space", () => {
    expect(LENS.leftover.key).toBe(NO_AREA);
    expect(LENS.leftover.label).toBe("No area");
    expect(isSyntheticRowId(LENS.leftover.key)).toBe(true);
  });

  it("has no order of its own for any section", () => {
    for (const key of [NO_AREA, "web", "web/dashboard", "nothing-declares-this"]) {
      expect(LENS.orderWithinSection(key), key).toBeNull();
    }
  });
});

describe("the rows the areas view draws", () => {
  it("draws every declared area in declaration order, empty ones included", () => {
    const { ids } = tableOf();

    // A declared sub-section leads its parent's own rows, the same way the
    // declared roots lead the top level.
    expect(ids).toEqual([
      AUTH,
      API,
      WEBHOOKS,
      "wh",
      "ah",
      WEB,
      DASHBOARD,
      "wf",
      "wt",
      "wa",
      "wa2",
      INFRA,
      NO_AREA,
      "rt",
      "un",
      "ms",
    ]);
  });

  it("keeps a declared area with nothing in it, and its count says so", () => {
    const { row } = tableOf();

    expect(row(AUTH)!.drawsSection).toEqual({
      key: "auth",
      display: { label: "auth", description: "Sign-in and sessions", color: "#3366ff" },
      count: 0,
      onEnter: { kind: "assign", field: "area", value: "auth", noun: "area" },
    });
    expect(row(AUTH)!.hasChildren).toBe(false);
  });

  it("keeps a declared area every nib in it was filtered out of", () => {
    // Not vacuous: unfiltered, `api` holds two rows — `ah` and the one its
    // declared sub-section draws, which the rollup counts too.
    expect(tableOf().row(API)!.drawsSection?.count).toBe(2);
    expect(tableOf().ids).toContain("ah");

    const { ids } = tableOf(FIXTURE, { priority: ["critical"] });

    expect(ids).not.toContain("ah");
    expect(ids).toContain(API);
    // The leftover is DISCOVERED, so a filter that empties it prunes it — the
    // difference between the two persistences, on one render.
    expect(ids).not.toContain(NO_AREA);
  });
});

describe("what entering an area section means", () => {
  /**
   * An area is a FIELD, not an order. `Region`'s arms are the ordering groups the
   * server has, and there is no area one — so declaring a member region here
   * would claim a group the server cannot resolve, and `memberRegion: null` lets
   * every row fall back to its own parent group instead.
   */
  it("orders nothing and assigns the area field", () => {
    expect(LENS.meaning("web")).toEqual({
      memberRegion: null,
      onEnter: { kind: "assign", field: "area", value: "web", noun: "area" },
    });
    expect(LENS.meaning("web/dashboard").onEnter).toEqual({
      kind: "assign",
      field: "area",
      value: "web/dashboard",
      noun: "area",
    });
  });

  it("makes the leftover govern nothing, the way the Backlog does", () => {
    expect(LENS.meaning(NO_AREA)).toEqual({ memberRegion: null, onEnter: { kind: "byRow" } });
  });

  it("declares no member region on any section a render actually builds", () => {
    const sections: TreeNode<TreeTableNib>[] = [];
    const walk = (nodes: readonly TreeNode<TreeTableNib>[]): void => {
      for (const node of nodes) {
        if (node.section !== undefined) sections.push(node);
        walk(node.children);
      }
    };
    walk(SPINE.buildViewTree(FIXTURE, "areas"));

    // Every declared area, the empty ones included, plus the leftover.
    expect(sections.map((n) => n.section!.key)).toEqual([
      "auth",
      "api",
      "api/webhooks",
      "web",
      "web/dashboard",
      "infra",
      NO_AREA,
    ]);
    for (const node of sections) {
      expect(node.section!.meaning.memberRegion, node.section!.key).toBeNull();
    }
  });
});

/**
 * The finding this phase carried: a section declaring no ordering region lets a
 * line drawn between two of them read as an ordinary sibling reorder, because
 * every top-level member of every section falls back to the SAME root group. The
 * write that follows lands in the global `order` sequence and the row snaps
 * back, its `area` never having changed.
 *
 * Driven through the real pipeline — the shipped lens, then
 * `buildShapedTableData`, then `planDrop` over the rows it produced — because
 * the defect lives in the seam between them.
 */
describe("a drag across two area sections", () => {
  const TABLE = buildShapedTableData(FIXTURE, noFilter, AREA_SHAPE, new Set());
  const ROWS_BY_ID = new Map(TABLE.rows.map((r) => [r.nib.id, r]));

  function row(id: string): RowData {
    const found = ROWS_BY_ID.get(id);
    if (found === undefined) throw new Error(`no fixture row ${id}`);
    return found;
  }

  function planFor(draggedIds: string[], targetId: string, zone: DropZone): DropPlan {
    return planDrop({
      draggedIds,
      rowsById: ROWS_BY_ID,
      draggedRowsById: ROWS_BY_ID,
      target: row(targetId),
      zone,
      descendantIds: collectDescendantIds(draggedIds, TABLE.rows),
      containment: TABLE.containment,
    });
  }

  it("puts the top-level members of every section in ONE ordering group", () => {
    // The premise the refusal rests on, asserted rather than assumed: separate
    // groups would already keep these rows apart and there would be nothing to
    // refuse.
    for (const id of ["ah", "wf", "wa", "un", "rt"]) {
      expect(row(id).region, id).toEqual({ axis: "parent", parentId: null });
    }
  });

  it("is refused rather than written as a reorder in the root order", () => {
    for (const zone of ["before", "after"] as const) {
      const plan = planFor(["ah"], "wa", zone);
      if (plan.ok) throw new Error(`expected a refusal for ${zone}, got ${plan.label}`);
      expect(plan.refusal.reason, zone).toBe("crosses-section");
      expect(plan.refusal.message, zone).toBe(
        "ah is not in the web area, and joining one is an assignment rather than a move.",
      );
    }
  });

  it("offers the assignment as the remedy — the write dropping onto the section performs", () => {
    const plan = planFor(["ah"], "wa", "after");
    if (plan.ok) throw new Error("expected a refusal");
    expect(plan.refusal.actionLabel).toBe("Move to the web area");
    expect(plan.refusal.actionCommand).toEqual(batch([updateNib("ah", { area: "web" })]));
  });

  it("refuses the same line drawn OUT of an area, into the leftover", () => {
    const plan = planFor(["wa"], "un", "before");
    if (plan.ok) throw new Error(`expected a refusal, got ${plan.label}`);
    expect(plan.refusal.reason).toBe("crosses-section");
    expect(plan.refusal.message).toBe(
      "wa is in the web area, and leaving one is an assignment rather than a move.",
    );
  });

  it("leaves a reorder INSIDE one area an ordinary sibling reorder", () => {
    // The affordance the refusal must not take away.
    const plan = planFor(["wa2"], "wa", "before");
    if (!plan.ok) throw new Error(plan.refusal.message);
    expect(plan.kind).toBe("position");
    if (plan.kind !== "position") throw new Error("unreachable");
    expect(plan.region).toEqual({ axis: "parent", parentId: null });
  });

  it("assigns the area when the drop lands ON the section row", () => {
    const plan = planFor(["ah"], WEB, "reparent");
    if (!plan.ok) throw new Error(plan.refusal.message);
    if (plan.kind !== "assign") throw new Error(`expected an assign plan, got ${plan.kind}`);
    expect(plan.assignment).toEqual({ field: "area", value: "web" });
    expect(plan.command).toEqual(batch([updateNib("ah", { area: "web" })]));
  });
});
