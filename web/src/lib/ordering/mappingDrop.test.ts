import { describe, it, expect } from "vitest";
import { mappingTargets, milestoneCounts, planMappingDrop, NO_MILESTONE_TARGET } from "./mappingDrop";
import type { MembershipLookup, MembershipNib } from "../membership";
import { sequence, setMilestoneBatch, updateNib } from "../mutations/commands";
import type { DropPlan } from "./dropPlan";

type Nib = MembershipNib & { readonly status: string; readonly title: string };

function nib(id: string, overrides: Partial<Nib> = {}): Nib {
  return { id, type: "task", milestone: "", parentId: null, status: "todo", title: id.toUpperCase(), ...overrides };
}

/**
 *   M1, M2 milestones
 *   E1 epic in M1        C1 task under E1 (inherits M1)
 *     H1 feature under E1   K1 task under H1 (inherit M1)
 *   T1, T2 unassigned tasks
 *   D1 task in M2
 *   E2 epic in M2        F2 feature under E2   G2 task under F2 (inherit M2)
 *   E3 unassigned epic   F3 feature under E3   G3 task under F3   C3 task under E3
 *   E4 unassigned epic   D4 task under E4, directly in M2
 *   E5 unassigned epic   F5 feature under E5   G5 task under F5, directly in M2
 *   E6 unassigned epic   D6 task under E6, directly in M2
 *     F6 feature under E6   U6 task under F6, directly in M1
 */
const NIBS: Nib[] = [
  nib("m1", { type: "milestone", title: "v1.0" }),
  nib("m2", { type: "milestone", title: "v2.0" }),
  nib("e1", { type: "epic", milestone: "m1", title: "Epic one" }),
  nib("c1", { parentId: "e1", title: "Child one" }),
  nib("t1", { title: "Task one" }),
  nib("t2", { title: "Task two" }),
  nib("d1", { milestone: "m2", title: "Direct two" }),
  nib("h1", { type: "feature", parentId: "e1", title: "Feature one" }),
  nib("k1", { parentId: "h1", title: "Grandchild one" }),
  nib("e2", { type: "epic", milestone: "m2", title: "Epic two" }),
  nib("f2", { type: "feature", parentId: "e2", title: "Feature two" }),
  nib("g2", { parentId: "f2", title: "Grandchild two" }),
  nib("e3", { type: "epic", title: "Epic three" }),
  nib("f3", { type: "feature", parentId: "e3", title: "Feature three" }),
  nib("g3", { parentId: "f3", title: "Grandchild three" }),
  nib("c3", { parentId: "e3", title: "Child three" }),
  nib("e4", { type: "epic", title: "Epic four" }),
  nib("d4", { parentId: "e4", milestone: "m2", title: "Direct four" }),
  nib("e5", { type: "epic", title: "Epic five" }),
  nib("f5", { type: "feature", parentId: "e5", title: "Feature five" }),
  nib("g5", { parentId: "f5", milestone: "m2", title: "Grandchild five" }),
  nib("e6", { type: "epic", title: "Epic six" }),
  nib("d6", { parentId: "e6", milestone: "m2", title: "Direct six" }),
  nib("f6", { type: "feature", parentId: "e6", title: "Feature six" }),
  nib("u6", { parentId: "f6", milestone: "m1", title: "Undragged six" }),
];
const BY_ID = new Map(NIBS.map((n) => [n.id, n]));
const lookup: MembershipLookup = (id) => BY_ID.get(id);
const childrenOf = (id: string) => NIBS.filter((n) => n.parentId === id);
const nameOf = (id: string) => BY_ID.get(id)?.title;
const statusOf = (id: string) => BY_ID.get(id)?.status;

function plan(ids: string[], target: string): DropPlan {
  return planMappingDrop({ draggedIds: ids, draggedNib: (id) => BY_ID.get(id), target, lookup, childrenOf, statusOf, nameOf });
}

/** Plans over the fixture plus `extra`, with the store marked stale when asked. */
function planWith(extra: Nib[], ids: string[], target: string, stale = false): DropPlan {
  const all = [...NIBS, ...extra];
  const byId = new Map(all.map((n) => [n.id, n]));
  return planMappingDrop({
    draggedIds: ids,
    draggedNib: (id) => byId.get(id),
    target,
    lookup: (id) => byId.get(id),
    childrenOf: (id) => all.filter((n) => n.parentId === id),
    statusOf: (id) => byId.get(id)?.status,
    stale,
    nameOf: (id) => byId.get(id)?.title,
  });
}

function reasonOf(p: DropPlan): string | null {
  return p.ok ? null : p.refusal.reason;
}

describe("planMappingDrop", () => {
  it("assigns every dragged nib to the target milestone", () => {
    const p = plan(["t1", "t2"], "m1");
    expect(p).toMatchObject({ ok: true, kind: "assign", assignment: { field: "milestone", value: "m1" }, indicator: "into", label: "Assign to v1.0" });
    if (!p.ok) return;
    expect(p.command).toEqual(setMilestoneBatch(["t1", "t2"], "m1"));
  });

  it("moves a nib directly assigned elsewhere", () => {
    const p = plan(["d1"], "m1");
    expect(p.ok && p.command).toEqual(setMilestoneBatch(["d1"], "m1"));
  });

  it("clears a direct assignment on a drop onto No milestone", () => {
    const p = plan(["d1"], NO_MILESTONE_TARGET);
    expect(p).toMatchObject({ ok: true, assignment: { field: "milestone", value: "" }, label: "Remove from milestone" });
    if (!p.ok) return;
    expect(p.command).toEqual(setMilestoneBatch(["d1"], ""));
  });

  it("leaves nibs already in the target out of the batch", () => {
    // c1 is in m1 through e1, so it is already there.
    const p = plan(["t1", "e1", "c1"], "m1");
    expect(p.ok && p.command).toEqual(setMilestoneBatch(["t1"], "m1"));
  });

  it("refuses when every dragged nib is already in the target", () => {
    expect(reasonOf(plan(["e1", "c1"], "m1"))).toBe("already-in-section");
    expect(reasonOf(plan(["t1", "t2"], NO_MILESTONE_TARGET))).toBe("already-in-section");
  });

  it("refuses a dragged milestone", () => {
    const p = plan(["t1", "m2"], "m1");
    expect(reasonOf(p)).toBe("unassignable-type");
    expect(!p.ok && p.refusal.message).toContain("v2.0");
  });

  it("refuses to clear a membership the nib inherits, naming the ancestor that holds it", () => {
    const p = plan(["c1"], NO_MILESTONE_TARGET);
    expect(reasonOf(p)).toBe("inherited-membership");
    expect(!p.ok && p.refusal.message).toBe("Child one is in v1.0 through Epic one, so clear the milestone of Epic one instead.");
  });

  it("refuses a selected nib the view does not show", () => {
    const p = planMappingDrop({ draggedIds: ["t1", "t2"], draggedNib: (id) => (id === "t1" ? BY_ID.get(id) : undefined), target: "m1", lookup, childrenOf, statusOf, nameOf });
    expect(reasonOf(p)).toBe("hidden-member");
  });

  describe("a dragged nib whose ancestor is dragged with it", () => {
    function commandOf(ids: string[], target: string) {
      const p = plan(ids, target);
      expect(p.ok, p.ok ? "" : p.refusal.message).toBe(true);
      return p.ok ? p.command : null;
    }

    it("joins a milestone through its ancestor's write", () => {
      expect(commandOf(["e3", "c3"], "m1")).toEqual(setMilestoneBatch(["e3"], "m1"));
    });

    it("joins through an ancestor two levels up", () => {
      expect(commandOf(["e3", "g3"], "m1")).toEqual(setMilestoneBatch(["e3"], "m1"));
    });

    it("joins through the nearer of two dragged ancestors", () => {
      expect(commandOf(["e3", "f3", "g3"], "m1")).toEqual(setMilestoneBatch(["e3"], "m1"));
    });

    it("is already in the target when that ancestor is", () => {
      expect(reasonOf(plan(["e1", "c1"], "m1"))).toBe("already-in-section");
    });

    it("leaves the unrelated nibs of a mixed set their own writes", () => {
      expect(commandOf(["c3", "t1", "e3", "d1"], "m1")).toEqual(setMilestoneBatch(["t1", "e3", "d1"], "m1"));
    });

    it("has its own assignment cleared before the ancestor is assigned", () => {
      expect(commandOf(["e4", "d4"], "m1")).toEqual(
        sequence([updateNib("d4", { milestone: null }), updateNib("e4", { milestone: "m1" })]),
      );
    });

    it("leaves No milestone through its ancestor's cleared assignment", () => {
      expect(commandOf(["e2", "f2"], NO_MILESTONE_TARGET)).toEqual(setMilestoneBatch(["e2"], ""));
      expect(commandOf(["g2", "e2"], NO_MILESTONE_TARGET)).toEqual(setMilestoneBatch(["e2"], ""));
    });

    it("is cleared and re-enters through the ancestor when it is already directly in the target", () => {
      expect(commandOf(["e4", "d4"], "m2")).toEqual(
        sequence([updateNib("d4", { milestone: null }), updateNib("e4", { milestone: "m2" })]),
      );
    });

    it("is cleared two levels below its dragged ancestor", () => {
      expect(commandOf(["e5", "g5"], "m2")).toEqual(
        sequence([updateNib("g5", { milestone: null }), updateNib("e5", { milestone: "m2" })]),
      );
    });

    it("is already in the target when the dragged ancestor is too", () => {
      expect(reasonOf(plan(["e2", "g2"], "m2"))).toBe("already-in-section");
    });

    it("is cleared beside an unrelated nib's assign, clears first", () => {
      expect(commandOf(["t1", "e4", "d4"], "m2")).toEqual(
        sequence([updateNib("d4", { milestone: null }), updateNib("t1", { milestone: "m2" }), updateNib("e4", { milestone: "m2" })]),
      );
    });

    it("keeps its own write onto No milestone when the ancestor holds nothing", () => {
      expect(commandOf(["e4", "d4"], NO_MILESTONE_TARGET)).toEqual(setMilestoneBatch(["d4"], ""));
    });

    it("is refused onto No milestone when the ancestor holding the assignment is not dragged", () => {
      const p = plan(["h1", "k1"], NO_MILESTONE_TARGET);
      expect(reasonOf(p)).toBe("inherited-membership");
      expect(!p.ok && p.refusal.message).toContain("through Epic one");
    });
  });

  describe("a drop that clears an assignment before assigning", () => {
    it("is refused before any write when an undragged descendant holds an assignment", () => {
      const p = plan(["e6", "d6"], "m1");
      expect(reasonOf(p)).toBe("exclusivity-conflict");
      expect(!p.ok && p.refusal.message).toBe(
        "Epic six cannot join v1.0, because Undragged six is in v1.0 and a nib and its ancestor are never both assigned.",
      );
    });

    it("is refused before any write when an undragged ancestor holds an assignment", () => {
      const p = plan(["e4", "d4", "k1"], "m2");
      expect(reasonOf(p)).toBe("exclusivity-conflict");
      expect(!p.ok && p.refusal.message).toContain("Grandchild one cannot join v2.0, because Epic one is in v1.0");
    });

    it("is not refused for the assignment it clears itself", () => {
      expect(plan(["e4", "d4"], "m1").ok).toBe(true);
    });
  });

  describe("a parent cycle", () => {
    const cycle = [nib("y1", { parentId: "y2", title: "Loop one" }), nib("y2", { parentId: "y1", title: "Loop two" })];

    it("is refused when two of its nibs are dragged together", () => {
      const p = planWith(cycle, ["y1", "y2"], "m1");
      expect(reasonOf(p)).toBe("parent-cycle");
      expect(!p.ok && p.refusal.message).toContain("Loop one is its own ancestor");
    });

    it("is not refused for one nib dragged alone", () => {
      expect(planWith(cycle, ["y1"], "m1")).toMatchObject({ ok: true, command: setMilestoneBatch(["y1"], "m1") });
    });
  });

  describe("the target of a drop that clears first", () => {
    const released = nib("m3", { type: "milestone", status: "completed", title: "v0.9" });
    const deferred = nib("m4", { type: "milestone", status: "deferred", title: "v0.8" });
    const doneEpic = nib("x1", { type: "epic", status: "completed", title: "Done epic" });
    const doneChild = nib("x2", { parentId: "x1", milestone: "m2", status: "completed", title: "Done child" });

    it("refuses open work when it is closed as a releasing status", () => {
      const p = planWith([released], ["e4", "d4"], "m3");
      expect(reasonOf(p)).toBe("entry-refused");
      expect(!p.ok && p.refusal.message).toBe("Epic four cannot join v0.9, which is completed and takes no more open work.");
    });

    it("accepts closed work when closed as a releasing status", () => {
      expect(planWith([released, doneEpic, doneChild], ["x1", "x2"], "m3").ok).toBe(true);
    });

    it("accepts open work when closed as a status that keeps its queue", () => {
      expect(planWith([deferred], ["e4", "d4"], "m4").ok).toBe(true);
    });

    it("is refused when the store does not hold it", () => {
      expect(reasonOf(plan(["e4", "d4"], "gone"))).toBe("unknown-destination");
    });

    it("is refused when it is not a milestone", () => {
      const p = plan(["e4", "d4"], "e3");
      expect(reasonOf(p)).toBe("entry-refused");
      expect(!p.ok && p.refusal.message).toBe("Epic three is not a milestone.");
    });
  });

  describe("the target of a drop that only assigns", () => {
    it("is left to the server when the store does not hold it", () => {
      expect(plan(["t1"], "gone")).toMatchObject({ ok: true, command: setMilestoneBatch(["t1"], "gone") });
    });

    it("is left to the server when it is closed as a releasing status", () => {
      const released = nib("m3", { type: "milestone", status: "completed", title: "v0.9" });
      expect(planWith([released], ["t1"], "m3")).toMatchObject({ ok: true, command: setMilestoneBatch(["t1"], "m3") });
    });
  });

  describe("a stale store", () => {
    it("refuses a drop that clears before it assigns", () => {
      expect(reasonOf(planWith([], ["e4", "d4"], "m1", true))).toBe("panel-unavailable");
    });

    it("still plans a drop that only assigns", () => {
      expect(planWith([], ["t1"], "m1", true)).toMatchObject({ ok: true, command: setMilestoneBatch(["t1"], "m1") });
    });

    it("still plans a drop onto No milestone", () => {
      expect(planWith([], ["d1"], NO_MILESTONE_TARGET, true)).toMatchObject({ ok: true, command: setMilestoneBatch(["d1"], "") });
    });
  });

  it("leaves a conflict to the server when nothing is cleared first", () => {
    const p = plan(["k1"], "m2");
    expect(p.ok && p.command).toEqual(setMilestoneBatch(["k1"], "m2"));
  });

  it("refuses an empty drag", () => {
    expect(reasonOf(plan([], "m1"))).toBe("no-source");
  });
});

describe("mappingTargets", () => {
  it("lists No milestone first, then open milestones in the given order", () => {
    const targets = mappingTargets([
      { id: "a", title: "A", status: "in-progress" },
      { id: "b", title: "B", status: "completed" },
      { id: "c", title: "C", status: "deferred" },
      { id: "d", title: "D", status: "scrapped" },
      { id: "e", title: "E", status: "todo" },
    ]);
    expect(targets.map((t) => t.value)).toEqual([NO_MILESTONE_TARGET, "a", "e"]);
    expect(targets[0].milestone).toBeNull();
  });
});

describe("milestoneCounts", () => {
  it("counts each milestone's whole plan, deferred as closed", () => {
    const counts = milestoneCounts([
      nib("m1", { type: "milestone", status: "in-progress" }),
      nib("m2", { type: "milestone" }),
      nib("e1", { type: "epic", milestone: "m1" }),
      nib("c1", { parentId: "e1", status: "completed" }),
      nib("c2", { parentId: "e1", status: "deferred" }),
      nib("c3", { parentId: "e1", status: "in-progress" }),
      nib("d1", { milestone: "m2", status: "scrapped" }),
      nib("t1"),
    ]);
    expect(counts.get("m1")).toEqual({ open: 2, closed: 2 });
    expect(counts.get("m2")).toEqual({ open: 0, closed: 1 });
    expect(counts.size).toBe(2);
  });
});
