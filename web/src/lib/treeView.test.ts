import { describe, it, expect } from "vitest";
import { TreeViewState } from "./treeView.svelte";
import { DEFAULT_VIEW_LEVEL } from "./types";

describe("TreeViewState", () => {
  it("starts with an empty collapsed set", () => {
    const state = new TreeViewState(DEFAULT_VIEW_LEVEL);
    expect(state.collapsedIds.size).toBe(0);
  });

  it("toggle() adds an id when it is absent", () => {
    const state = new TreeViewState(DEFAULT_VIEW_LEVEL);
    state.toggle("a");
    expect(state.collapsedIds.has("a")).toBe(true);
    expect(state.collapsedIds.size).toBe(1);
  });

  it("toggle() removes an id when it is present", () => {
    const state = new TreeViewState(DEFAULT_VIEW_LEVEL);
    state.toggle("a");
    state.toggle("a");
    expect(state.collapsedIds.has("a")).toBe(false);
    expect(state.collapsedIds.size).toBe(0);
  });

  it("toggle() replaces the Set instance (reassign-invariant guard)", () => {
    const state = new TreeViewState(DEFAULT_VIEW_LEVEL);
    const before = state.collapsedIds;
    state.toggle("x");
    expect(state.collapsedIds).not.toBe(before);
  });

  it("expandAll() clears the collapsed set", () => {
    const state = new TreeViewState(DEFAULT_VIEW_LEVEL);
    state.toggle("a");
    state.toggle("b");
    state.expandAll();
    expect(state.collapsedIds.size).toBe(0);
  });

  it("expandAll() replaces the Set instance", () => {
    const state = new TreeViewState(DEFAULT_VIEW_LEVEL);
    state.toggle("a");
    const before = state.collapsedIds;
    state.expandAll();
    expect(state.collapsedIds).not.toBe(before);
  });

  it("collapseAll() sets exactly the given ids", () => {
    const state = new TreeViewState(DEFAULT_VIEW_LEVEL);
    state.collapseAll(["a", "b", "c"]);
    expect(state.collapsedIds.size).toBe(3);
    expect(state.collapsedIds.has("a")).toBe(true);
    expect(state.collapsedIds.has("b")).toBe(true);
    expect(state.collapsedIds.has("c")).toBe(true);
  });

  it("collapseAll() replaces any previous collapsed ids", () => {
    const state = new TreeViewState(DEFAULT_VIEW_LEVEL);
    state.collapseAll(["a", "b"]);
    state.collapseAll(["c"]);
    expect(state.collapsedIds.size).toBe(1);
    expect(state.collapsedIds.has("a")).toBe(false);
    expect(state.collapsedIds.has("c")).toBe(true);
  });

  it("setCollapsed() replaces the collapsed set wholesale", () => {
    const state = new TreeViewState(DEFAULT_VIEW_LEVEL);
    state.collapseAll(["a", "b"]);
    const before = state.collapsedIds;
    state.setCollapsed(["c", "d"]);
    expect(state.collapsedIds).not.toBe(before);
    expect(state.collapsedIds.size).toBe(2);
    expect(state.collapsedIds.has("a")).toBe(false);
    expect(state.collapsedIds.has("c")).toBe(true);
    expect(state.collapsedIds.has("d")).toBe(true);
  });

  it("setCollapsed() accepts any iterable of ids", () => {
    const state = new TreeViewState(DEFAULT_VIEW_LEVEL);
    state.setCollapsed(new Set(["a", "b"]));
    expect(state.collapsedIds.size).toBe(2);
    expect(state.collapsedIds.has("a")).toBe(true);
    expect(state.collapsedIds.has("b")).toBe(true);
  });

  it("isCollapsed() reflects membership", () => {
    const state = new TreeViewState(DEFAULT_VIEW_LEVEL);
    expect(state.isCollapsed("a")).toBe(false);
    state.toggle("a");
    expect(state.isCollapsed("a")).toBe(true);
    state.toggle("a");
    expect(state.isCollapsed("a")).toBe(false);
  });

  it("collapseAll([]) / setCollapsed([]) clear the set (empty-iterable boundary)", () => {
    const state = new TreeViewState(DEFAULT_VIEW_LEVEL);
    state.collapseAll(["a", "b"]);
    state.collapseAll([]);
    expect(state.collapsedIds.size).toBe(0);
    state.collapseAll(["c"]);
    state.setCollapsed([]);
    expect(state.collapsedIds.size).toBe(0);
  });

  it("scrollTop defaults to 0 and persists when assigned", () => {
    const state = new TreeViewState(DEFAULT_VIEW_LEVEL);
    expect(state.scrollTop).toBe(0);
    state.scrollTop = 300;
    expect(state.scrollTop).toBe(300);
  });

  it("collapsedIds is typed read-only but returns the live Set (no runtime freeze)", () => {
    // Documents the actual runtime boundary: the getter's ReadonlySet type is
    // erased at runtime, so a cast can mutate the backing Set in place. This is a
    // known gap — the reassign invariant is enforced by TS + the methods below,
    // not by a frozen collection. Writes must go through the methods, never here.
    const state = new TreeViewState(DEFAULT_VIEW_LEVEL);
    state.toggle("a");
    (state.collapsedIds as unknown as Set<string>).add("z");
    expect(state.collapsedIds.has("z")).toBe(true);
  });

  describe("view transitions", () => {
    it("starts with nothing pending", () => {
      expect(new TreeViewState(DEFAULT_VIEW_LEVEL).pendingTransition).toBeNull();
    });

    it("beginTransition() records the pair a reconciler needs", () => {
      const state = new TreeViewState("none");
      state.beginTransition("none", "epics");
      expect(state.pendingTransition).toEqual({ from: "none", to: "epics" });
    });

    it("clearTransition() consumes the slot", () => {
      const state = new TreeViewState("none");
      state.beginTransition("none", "epics");
      state.clearTransition();
      expect(state.pendingTransition).toBeNull();
    });

    it("activeLevel seeds from the constructor, which every caller must supply", () => {
      // No default: the seed is the key the first parked offset is filed under,
      // so a caller that has a level and omits it must not compile.
      expect(new TreeViewState("flat").activeLevel).toBe("flat");
      expect(new TreeViewState("milestones").activeLevel).toBe("milestones");
    });

    it("activeLevel still names the OUTGOING view while a transition is pending", () => {
      // `prefs.viewLevel` flips synchronously when the toolbar writes it, so it
      // cannot answer "which view is on screen" for anything running between the
      // write and the reconcile.
      const state = new TreeViewState("none");
      state.beginTransition("none", "epics");
      expect(state.activeLevel).toBe("none");
    });

    it("clearTransition() advances activeLevel to the destination", () => {
      const state = new TreeViewState("none");
      state.beginTransition("none", "epics");
      state.clearTransition();
      expect(state.activeLevel).toBe("epics");
    });

    it("clearTransition() with nothing pending leaves activeLevel alone", () => {
      const state = new TreeViewState("milestones");
      state.clearTransition();
      expect(state.activeLevel).toBe("milestones");
    });

    it("beginTransition() replaces an unconsumed slot, so activeLevel follows the last switch", () => {
      // Two switches inside one flush: only the second destination is on screen.
      const state = new TreeViewState("none");
      state.beginTransition("none", "epics");
      state.beginTransition("epics", "flat");
      expect(state.pendingTransition).toEqual({ from: "epics", to: "flat" });
      state.clearTransition();
      expect(state.activeLevel).toBe("flat");
    });
  });

  describe("per-view scroll memory", () => {
    it("starts at epoch 0", () => {
      expect(new TreeViewState(DEFAULT_VIEW_LEVEL).scrollEpoch).toBe(0);
    });

    it("switchScroll() adopts 0 for a destination that has never been scrolled", () => {
      const state = new TreeViewState("none");
      state.scrollTop = 420;
      state.switchScroll("none", "epics");
      expect(state.scrollTop).toBe(0);
      expect(state.scrollEpoch).toBe(1);
    });

    it("switchScroll() gives the outgoing view's offset back on the way home", () => {
      const state = new TreeViewState("none");
      state.scrollTop = 500;
      state.switchScroll("none", "epics");
      expect(state.scrollTop).toBe(0);

      state.switchScroll("epics", "none");
      expect(state.scrollTop).toBe(500);
    });

    it("parks the offset under the view it was measured in, not the destination", () => {
      // The failure this pins: filing 500 under `to` would hand it straight back
      // on arrival, so the destination would inherit an offset from a view whose
      // geometry it does not share.
      const state = new TreeViewState("none");
      state.scrollTop = 500;
      state.switchScroll("none", "epics");
      expect(state.scrollTop).toBe(0);

      // ...and the destination keeps its OWN offset once it has one.
      state.scrollTop = 120;
      state.switchScroll("epics", "none");
      expect(state.scrollTop).toBe(500);
      state.switchScroll("none", "epics");
      expect(state.scrollTop).toBe(120);
    });

    it("keeps a third view's slot independent of the other two", () => {
      const state = new TreeViewState("none");
      state.scrollTop = 100;
      state.switchScroll("none", "epics");
      state.scrollTop = 200;
      state.switchScroll("epics", "milestones");
      state.scrollTop = 300;

      state.switchScroll("milestones", "none");
      expect(state.scrollTop).toBe(100);
      state.switchScroll("none", "epics");
      expect(state.scrollTop).toBe(200);
      state.switchScroll("epics", "milestones");
      expect(state.scrollTop).toBe(300);
    });

    it("is inert on a self-transition — no epoch bump, no offset churn", () => {
      // Reachable even though the planner refuses `from === to`: two switches
      // inside one flush collapse into one slot, and the applier keys the origin
      // on activeLevel, which the planner never sees.
      const state = new TreeViewState("none");
      state.scrollTop = 400;
      state.switchScroll("none", "epics");
      state.scrollTop = 900;
      const epochBefore = state.scrollEpoch;

      state.switchScroll("epics", "epics");

      expect(state.scrollTop).toBe(900);
      expect(state.scrollEpoch).toBe(epochBefore);
      // Nor did it disturb the other view's parked offset.
      state.switchScroll("epics", "none");
      expect(state.scrollTop).toBe(400);

      // The other boundary: a self-transition on a level that DOES hold a parked
      // offset must leave that entry alone as well.
      state.switchScroll("none", "none");
      expect(state.scrollTop).toBe(400);
      expect(state.scrollEpoch).toBe(epochBefore + 1);
      state.switchScroll("none", "epics");
      expect(state.scrollTop).toBe(900);
    });

    it("advances the epoch on every switch, so each one retires scroll ownership", () => {
      const state = new TreeViewState("none");
      state.switchScroll("none", "epics");
      state.switchScroll("epics", "none");
      expect(state.scrollEpoch).toBe(2);
    });
  });
});
