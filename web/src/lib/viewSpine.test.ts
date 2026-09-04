import { describe, it, expect } from "vitest";
import { EMPTY_SPINE, LOADING_SPINE, UNAVAILABLE_SPINE, makeViewSpine } from "./viewSpine";
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

  // The failed-query spine shares its EMPTINESS with both of the others and its
  // "unknown" with the pre-load one, so `status` is the only thing telling a
  // consumer it is looking at a failure rather than at a wait or at a healthy
  // project with nothing declared.
  it("distinguishes the failed-config spine from both of the other empty ones", () => {
    expect(UNAVAILABLE_SPINE.areas.status).toBe("unavailable");
    expect(UNAVAILABLE_SPINE.areas.validity("web")).toBe("unknown");
    expect(UNAVAILABLE_SPINE.areas.sections()).toEqual([]);
    expect(UNAVAILABLE_SPINE).not.toBe(LOADING_SPINE);
    expect(UNAVAILABLE_SPINE).not.toBe(EMPTY_SPINE);
  });

  // `EMPTY_SPINE` and `LOADING_SPINE` are module singletons every test file in a
  // vitest worker shares, so a swapped method or a swapped vocabulary would
  // follow the worker into unrelated suites. The shape this replaced was a
  // module namespace, whose bindings are non-writable; the freeze is what keeps
  // that property.
  it("cannot have a method or its vocabulary swapped through a cast", () => {
    for (const spine of [EMPTY_SPINE, LOADING_SPINE, UNAVAILABLE_SPINE, makeViewSpine(webAreas)]) {
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
    expect(UNAVAILABLE_SPINE.areas.status).toBe("unavailable");
  });

  it("gives the same answer at a level whose lens does not read the vocabulary", () => {
    const nibs = [
      makeTreeNib({ id: "m1", type: "milestone", title: "v2.0" }),
      makeTreeNib({ id: "t1", milestone: "m1", milestoneOrder: "a" }),
    ];
    const bound = makeViewSpine(webAreas);
    expect(bound.buildViewTree(nibs, "milestones").map((n) => n.nib.id)).toEqual(
      EMPTY_SPINE.buildViewTree(nibs, "milestones").map((n) => n.nib.id),
    );
  });

  /**
   * The other half, and the reason a spine binds a vocabulary at all: the Areas
   * level's sections ARE the vocabulary, so two spines answer differently for
   * the same nibs. Which is also what makes a shape built against the wrong
   * vocabulary a real defect rather than a theoretical one — and why
   * `viewShapeFor` is private to viewSpine.ts, reachable only through a spine.
   */
  it("gives DIFFERENT answers at the areas level, one per vocabulary", () => {
    const nibs = [makeTreeNib({ id: "t1", area: "web" })];
    const web = makeViewSpine(webAreas).buildViewTree(nibs, "areas");
    const infra = makeViewSpine(infraAreas).buildViewTree(nibs, "areas");

    // `web/dashboard` is declared INSIDE `web`, so it is that section's row
    // rather than a root, and it leads the rows `web` holds.
    expect(web.map((n) => n.nib.id)).toEqual(["/section:web_"]);
    expect(web[0].children.map((n) => n.nib.id)).toEqual(["/section:web/dashboard_", "t1"]);
    // The same nib, under a vocabulary that declares no `web`: its assignment
    // resolves to nothing, so it falls to the leftover.
    expect(infra.map((n) => n.nib.id)).toEqual(["/section:infra_", "/__no_area__"]);
    expect(infra[1].children.map((n) => n.nib.id)).toEqual(["t1"]);
  });
});

// Closures in an object literal, never `this`-dependent — which is what makes
// the one-line destructure at the top of the view tests legal.
describe("destructured methods", () => {
  it("work detached from the spine", () => {
    const { buildViewTree, viewShapeFor, buildTableData, dragBlockFor, adjacencyReflectsOrdering } =
      makeViewSpine(EMPTY_AREAS);
    const nibs = [makeTreeNib({ id: "m1", type: "milestone" }), makeTreeNib({ id: "t1" })];

    expect(viewShapeFor("flat")).toEqual({ kind: "flat" });
    expect(buildViewTree(nibs, "flat").map((n) => n.nib.id)).toEqual(["m1", "t1"]);
    const empty = buildTableData([], {}, "none", new Set());
    expect(empty.rows).toEqual([]);
    expect(empty.containment.has("t1")).toBe(false);
    expect(dragBlockFor({}, "none", null)).toBeNull();
    expect(adjacencyReflectsOrdering({}, "none", null)).toBe(true);
  });
});
