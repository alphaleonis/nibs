import { describe, it, expect } from "vitest";
import { EMPTY_SPINE, LOADING_SPINE, makeViewSpine } from "./viewSpine";
import { createAreaVocabulary, EMPTY_AREAS } from "./areas";
import type { AreaNode } from "./areas";
import { VIEW_LEVELS } from "./types";
import type { TreeNib } from "./types";

function area(path: string, depth: number): AreaNode {
  const segments = path.split("/");
  return { path, name: segments[segments.length - 1], description: "", color: "", depth };
}

function makeTreeNib(overrides: Partial<TreeNib> = {}): TreeNib {
  return {
    id: "nibs-abc1",
    title: "Fix login bug",
    status: "in-progress",
    type: "task",
    priority: "high",
    estimate: "m",
    tags: [],
    createdAt: "2026-03-15T10:00:00Z",
    updatedAt: "2026-03-20T10:00:00Z",
    parentId: null,
    milestone: "",
    milestoneOrder: "",
    area: "",
    ...overrides,
  };
}

describe("bucketIds", () => {
  it("answers membership for the leftover key of every shipped lens", () => {
    const declared = VIEW_LEVELS.flatMap((level) => {
      const shape = EMPTY_SPINE.viewShapeFor(level);
      return shape.kind === "grouped" ? [shape.lens.leftover.key] : [];
    });
    expect(declared.length).toBeGreaterThan(0);
    for (const key of declared) expect(EMPTY_SPINE.bucketIds.has(key)).toBe(true);
    expect(EMPTY_SPINE.bucketIds.has("nibs-abc1")).toBe(false);
  });

  // `ReadonlySet` is erased, so a cast would hand back the live set — and
  // `Object.freeze` leaves a Set's contents writable. EMPTY_SPINE is a module
  // singleton every test file in a vitest worker shares, so one `add` there
  // would follow the worker into unrelated suites.
  it("cannot be mutated through a cast", () => {
    const bucketIds = EMPTY_SPINE.bucketIds;
    expect(() => (bucketIds as unknown as Set<string>).add("nibs-abc1")).toThrow(TypeError);
    expect(() => {
      (bucketIds as unknown as Record<string, unknown>).has = () => true;
    }).toThrow(TypeError);
    expect(Object.isFrozen(bucketIds)).toBe(true);
    expect(bucketIds.has("nibs-abc1")).toBe(false);
  });
});

describe("spine identity", () => {
  const webAreas = createAreaVocabulary([area("web", 0), area("web/dashboard", 1)]);
  const infraAreas = createAreaVocabulary([area("infra", 0)]);

  it("binds each spine to its own vocabulary", () => {
    const a = makeViewSpine(webAreas);
    const b = makeViewSpine(infraAreas);
    expect(a.areas.sections().map((n) => n.path)).toEqual(["web", "web/dashboard"]);
    expect(b.areas.sections().map((n) => n.path)).toEqual(["infra"]);
  });

  it("keeps one spine's sections out of reach of the other", () => {
    const a = makeViewSpine(webAreas);
    const b = makeViewSpine(infraAreas);
    expect(() => (a.areas.sections() as AreaNode[]).push(area("smuggled", 0))).toThrow(TypeError);
    expect(b.areas.sections().map((n) => n.path)).toEqual(["infra"]);
    expect(a.areas.sections().map((n) => n.path)).toEqual(["web", "web/dashboard"]);
  });

  it("distinguishes the pre-load spine from a project that declares no areas", () => {
    expect(LOADING_SPINE.areas.status).toBe("loading");
    expect(LOADING_SPINE.areas.validity("web")).toBe("unknown");
    expect(EMPTY_SPINE.areas.status).toBe("none");
    expect(EMPTY_SPINE.areas.validity("web")).toBe("undeclared");
  });

  // `EMPTY_SPINE` and `LOADING_SPINE` are module singletons every test file in a
  // vitest worker shares, so a swapped method or a swapped vocabulary would
  // follow the worker into unrelated suites. The shape this replaced was a
  // module namespace, whose bindings are non-writable; the freeze is what keeps
  // that property.
  it("cannot have a method or its vocabulary swapped through a cast", () => {
    for (const spine of [EMPTY_SPINE, LOADING_SPINE, makeViewSpine(webAreas)]) {
      expect(Object.isFrozen(spine)).toBe(true);
      expect(() => {
        (spine as unknown as Record<string, unknown>).buildTableData = () => undefined;
      }).toThrow(TypeError);
      expect(() => {
        (spine as unknown as Record<string, unknown>).areas = EMPTY_AREAS;
      }).toThrow(TypeError);
    }
    expect(EMPTY_SPINE.areas.status).toBe("none");
    expect(LOADING_SPINE.areas.status).toBe("loading");
  });

  it("gives the same view answers whatever vocabulary it is bound to, while no lens reads one", () => {
    const nibs = [
      makeTreeNib({ id: "m1", type: "milestone", title: "v2.0" }),
      makeTreeNib({ id: "t1", milestone: "m1", milestoneOrder: "a" }),
    ];
    const bound = makeViewSpine(webAreas);
    expect(bound.buildViewTree(nibs, "milestones").map((n) => n.nib.id)).toEqual(
      EMPTY_SPINE.buildViewTree(nibs, "milestones").map((n) => n.nib.id),
    );
  });
});

// Closures in an object literal, never `this`-dependent — which is what makes
// the one-line destructure at the top of the view tests legal.
describe("destructured methods", () => {
  it("work detached from the spine", () => {
    const { buildViewTree, viewShapeFor, containingSectionRowId, buildTableData, dragBlockFor, adjacencyReflectsOrdering } =
      makeViewSpine(EMPTY_AREAS);
    const nibs = [makeTreeNib({ id: "m1", type: "milestone" }), makeTreeNib({ id: "t1" })];
    const byId = new Map(nibs.map((n) => [n.id, n]));

    expect(viewShapeFor("flat")).toEqual({ kind: "flat" });
    expect(buildViewTree(nibs, "flat").map((n) => n.nib.id)).toEqual(["m1", "t1"]);
    expect(containingSectionRowId(byId, "t1", "milestones")).not.toBeNull();
    expect(containingSectionRowId(byId, "t1", "flat")).toBeNull();
    expect(buildTableData([], {}, "none", new Set()).rows).toEqual([]);
    expect(dragBlockFor({}, "none", null)).toBeNull();
    expect(adjacencyReflectsOrdering({}, "none", null)).toBe(true);
  });
});
