import { describe, it, expect } from "vitest";
import { planViewTransition } from "./viewTransition";
import type { ViewTransitionSnapshot } from "./viewTransition";
import type { ViewLevel } from "./types";

function snapshot(overrides: Partial<ViewTransitionSnapshot> = {}): ViewTransitionSnapshot {
  return {
    focusedNibId: null,
    selectedNibId: null,
    memberIds: new Set<string>(),
    ...overrides,
  };
}

describe("planViewTransition", () => {
  it("plans nothing when the lens is re-picked", () => {
    // Re-picking the current level is not a transition: nothing left the view, so
    // pruning the selection and resetting the scroll would both destroy state the
    // user never asked to lose.
    const plan = planViewTransition(
      { from: "epics", to: "epics" },
      snapshot({ focusedNibId: "m1", selectedNibId: "m1", memberIds: new Set(["m1"]) }),
    );

    expect(plan).toEqual({ retainIds: null, anchorId: null, resetScroll: false });
  });

  it("retains exactly the new view's members", () => {
    const memberIds = new Set(["e1", "t1", "/__no_epic__"]);
    const plan = planViewTransition({ from: "none", to: "epics" }, snapshot({ memberIds }));

    expect(plan.retainIds).toBe(memberIds);
  });

  it("resets the scroll on every real transition", () => {
    // The outgoing view's pixel offset describes geometry the incoming view does
    // not have, so it never carries over.
    const plan = planViewTransition({ from: "none", to: "flat" }, snapshot());

    expect(plan.resetScroll).toBe(true);
  });

  it("anchors on the focused row when the new view still has one for it", () => {
    const plan = planViewTransition(
      { from: "none", to: "epics" },
      snapshot({ focusedNibId: "t1", memberIds: new Set(["e1", "t1"]) }),
    );

    expect(plan.anchorId).toBe("t1");
  });

  it("drops the anchor when the focused row has no row in the new view", () => {
    // The headline case: a milestone focused in Tree has no row at all under the
    // Epics lens, which hides above-tier containers while descending into them.
    const plan = planViewTransition(
      { from: "none", to: "epics" },
      snapshot({ focusedNibId: "m1", memberIds: new Set(["e1", "t1"]) }),
    );

    expect(plan.anchorId).toBeNull();
    expect(plan.retainIds?.has("m1")).toBe(false);
  });

  it("falls back to the panel's nib when focus did not survive but the panel's did", () => {
    const plan = planViewTransition(
      { from: "none", to: "epics" },
      snapshot({ focusedNibId: "m1", selectedNibId: "t1", memberIds: new Set(["e1", "t1"]) }),
    );

    expect(plan.anchorId).toBe("t1");
  });

  it("prefers the focused row over the panel's nib when both survive", () => {
    // Focus is where the keyboard is, so it is the landmark to keep on screen.
    const plan = planViewTransition(
      { from: "none", to: "epics" },
      snapshot({ focusedNibId: "t2", selectedNibId: "t1", memberIds: new Set(["t1", "t2"]) }),
    );

    expect(plan.anchorId).toBe("t2");
  });

  it("drops the anchor when neither survives", () => {
    const plan = planViewTransition(
      { from: "none", to: "epics" },
      snapshot({ focusedNibId: "m1", selectedNibId: "m2", memberIds: new Set(["e1"]) }),
    );

    expect(plan.anchorId).toBeNull();
  });

  it("never names selectedNibId as something to write", () => {
    // The detail panel is nib-keyed state, not row-keyed, and is also the `?nib=`
    // URL — a view switch must not close it. The plan carries no sink for it.
    const plan = planViewTransition(
      { from: "none", to: "epics" },
      snapshot({ selectedNibId: "m1", memberIds: new Set(["e1"]) }),
    );

    expect(Object.keys(plan).sort()).toEqual(["anchorId", "resetScroll", "retainIds"]);
  });

  it("plans a transition between every ordered pair of distinct levels", () => {
    const levels: ViewLevel[] = ["none", "flat", "milestones", "epics", "features"];
    for (const from of levels) {
      for (const to of levels) {
        const plan = planViewTransition({ from, to }, snapshot({ memberIds: new Set(["a"]) }));
        if (from === to) {
          expect(plan.retainIds, `${from} -> ${to}`).toBeNull();
        } else {
          expect(plan.retainIds, `${from} -> ${to}`).not.toBeNull();
          expect(plan.resetScroll, `${from} -> ${to}`).toBe(true);
        }
      }
    }
  });
});
