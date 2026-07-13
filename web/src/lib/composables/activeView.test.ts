import { describe, it, expect } from "vitest";
import {
  reduce,
  abandonsBuffer,
  type ViewState,
  type Action,
} from "./activeView";

// --- Fixture states ---------------------------------------------------------

const closed: ViewState = { kind: "closed" };
const viewingD: ViewState = { kind: "viewing", nibId: "n1", presentation: "docked" };
const viewingE: ViewState = { kind: "viewing", nibId: "n1", presentation: "expanded" };
const goneD: ViewState = { kind: "gone", nibId: "n1", presentation: "docked" };
const goneE: ViewState = { kind: "gone", nibId: "n1", presentation: "expanded" };
const creatingD: ViewState = { kind: "creating", defaults: { type: "task" }, presentation: "docked" };
const creatingE: ViewState = { kind: "creating", defaults: { type: "task" }, presentation: "expanded" };

// --- OPEN --------------------------------------------------------------------

describe("reduce · OPEN", () => {
  it("opens a nib from closed as docked (no presentation to fabricate)", () => {
    expect(reduce(closed, { type: "OPEN", nibId: "n2" })).toEqual({
      kind: "viewing",
      nibId: "n2",
      presentation: "docked",
    });
  });

  it("preserves presentation when opening another nib while expanded", () => {
    expect(reduce(viewingE, { type: "OPEN", nibId: "n2" })).toEqual({
      kind: "viewing",
      nibId: "n2",
      presentation: "expanded",
    });
  });

  it("opening a nib from creating discards the create buffer (view swap)", () => {
    expect(reduce(creatingE, { type: "OPEN", nibId: "n2" })).toEqual({
      kind: "viewing",
      nibId: "n2",
      presentation: "expanded",
    });
  });

  it("re-opens a gone nib as viewing", () => {
    expect(reduce(goneD, { type: "OPEN", nibId: "n1" })).toEqual(viewingD);
  });
});

// --- EXPAND / COLLAPSE -------------------------------------------------------

describe("reduce · EXPAND/COLLAPSE (presentation is a payload field)", () => {
  it("EXPAND only flips presentation, keeping the same nibId/tag", () => {
    expect(reduce(viewingD, { type: "EXPAND" })).toEqual(viewingE);
  });

  it("COLLAPSE only flips presentation back", () => {
    expect(reduce(viewingE, { type: "COLLAPSE" })).toEqual(viewingD);
  });

  it("EXPAND on creating keeps the same defaults buffer identity", () => {
    expect(reduce(creatingD, { type: "EXPAND" })).toEqual(creatingE);
  });

  it("EXPAND on gone keeps the nibId", () => {
    expect(reduce(goneD, { type: "EXPAND" })).toEqual(goneE);
  });

  it("EXPAND while closed is a no-op (nothing to present)", () => {
    expect(reduce(closed, { type: "EXPAND" })).toEqual(closed);
    expect(reduce(closed, { type: "COLLAPSE" })).toEqual(closed);
  });
});

// --- START_CREATE ------------------------------------------------------------

describe("reduce · START_CREATE", () => {
  it("enters creating with the given defaults, docked from closed", () => {
    expect(
      reduce(closed, { type: "START_CREATE", defaults: { type: "bug" } }),
    ).toEqual({ kind: "creating", defaults: { type: "bug" }, presentation: "docked" });
  });

  it("preserves presentation when starting a create while expanded", () => {
    expect(
      reduce(viewingE, { type: "START_CREATE", defaults: { type: "feature", parent: "p9" } }),
    ).toEqual({
      kind: "creating",
      defaults: { type: "feature", parent: "p9" },
      presentation: "expanded",
    });
  });

  it("a child create (parent carried in defaults) preserves presentation", () => {
    // Add-child now funnels through START_CREATE with a parent default (the type
    // picker lives outside this kernel). Docked from closed, expanded preserved.
    expect(
      reduce(closed, { type: "START_CREATE", defaults: { type: "epic", parent: "p1" } }),
    ).toEqual({ kind: "creating", defaults: { type: "epic", parent: "p1" }, presentation: "docked" });
  });
});

// --- SAVED (create -> edit hand-off) ----------------------------------------

describe("reduce · SAVED", () => {
  it("maps creating -> viewing(newId) atomically, preserving presentation", () => {
    expect(reduce(creatingE, { type: "SAVED", nibId: "nibs-new1" })).toEqual({
      kind: "viewing",
      nibId: "nibs-new1",
      presentation: "expanded",
    });
  });

  it("is a no-op when not creating (edit saves keep viewing)", () => {
    expect(reduce(viewingD, { type: "SAVED", nibId: "n1" })).toEqual(viewingD);
    expect(reduce(closed, { type: "SAVED", nibId: "n1" })).toEqual(closed);
  });
});

// --- DELETED -----------------------------------------------------------------

describe("reduce · DELETED", () => {
  it("maps viewing -> gone, keeping nibId and presentation", () => {
    expect(reduce(viewingE, { type: "DELETED" })).toEqual({
      kind: "gone",
      nibId: "n1",
      presentation: "expanded",
    });
  });

  it("is a no-op outside viewing", () => {
    expect(reduce(goneD, { type: "DELETED" })).toEqual(goneD);
    expect(reduce(creatingD, { type: "DELETED" })).toEqual(creatingD);
    expect(reduce(closed, { type: "DELETED" })).toEqual(closed);
  });
});

// --- CLOSE -------------------------------------------------------------------

describe("reduce · CLOSE", () => {
  it("returns to closed from any open state", () => {
    for (const s of [viewingD, viewingE, goneD, creatingD]) {
      expect(reduce(s, { type: "CLOSE" })).toEqual(closed);
    }
  });
});

// --- Totality: illegal pairs are no-ops -------------------------------------

describe("reduce · illegal (state, action) pairs are no-ops", () => {
  const cases: Array<[string, ViewState, Action]> = [
    ["closed + EXPAND", closed, { type: "EXPAND" }],
    ["closed + COLLAPSE", closed, { type: "COLLAPSE" }],
    ["closed + SAVED", closed, { type: "SAVED", nibId: "x" }],
    ["closed + DELETED", closed, { type: "DELETED" }],
    ["viewing + SAVED(same)", viewingD, { type: "SAVED", nibId: "n1" }],
    ["gone + DELETED", goneD, { type: "DELETED" }],
    ["gone + SAVED", goneD, { type: "SAVED", nibId: "x" }],
    ["creating + DELETED", creatingD, { type: "DELETED" }],
  ];

  it.each(cases)("%s leaves state unchanged", (_label, state, action) => {
    expect(reduce(state, action)).toEqual(state);
  });
});

// --- abandonsBuffer ----------------------------------------------------------

describe("abandonsBuffer", () => {
  const cases: Array<[string, ViewState, Action, boolean]> = [
    // OPEN
    ["viewing + OPEN other", viewingD, { type: "OPEN", nibId: "n2" }, true],
    ["viewing + OPEN same", viewingD, { type: "OPEN", nibId: "n1" }, false],
    ["gone + OPEN other", goneD, { type: "OPEN", nibId: "n2" }, true],
    ["gone + OPEN same", goneD, { type: "OPEN", nibId: "n1" }, false],
    ["creating + OPEN", creatingD, { type: "OPEN", nibId: "n2" }, true],
    ["closed + OPEN", closed, { type: "OPEN", nibId: "n2" }, false],
    // START_CREATE (add-child, top-level create, and picked-type create all use it)
    ["viewing + START_CREATE", viewingD, { type: "START_CREATE", defaults: { type: "task" } }, true],
    ["closed + START_CREATE", closed, { type: "START_CREATE", defaults: { type: "task" } }, false],
    ["creating + START_CREATE", creatingD, { type: "START_CREATE", defaults: { type: "bug" } }, true],
    // CLOSE
    ["viewing + CLOSE", viewingD, { type: "CLOSE" }, true],
    ["gone + CLOSE", goneD, { type: "CLOSE" }, true],
    ["creating + CLOSE", creatingD, { type: "CLOSE" }, true],
    ["closed + CLOSE", closed, { type: "CLOSE" }, false],
    // Buffer-preserving actions never abandon
    ["viewing + EXPAND", viewingD, { type: "EXPAND" }, false],
    ["viewing + COLLAPSE", viewingE, { type: "COLLAPSE" }, false],
    ["viewing + SAVED", viewingD, { type: "SAVED", nibId: "n1" }, false],
    ["viewing + DELETED", viewingD, { type: "DELETED" }, false],
    ["creating + SAVED", creatingD, { type: "SAVED", nibId: "x" }, false],
  ];

  it.each(cases)("%s -> %s", (_label, state, action, expected) => {
    expect(abandonsBuffer(state, action)).toBe(expected);
  });
});
