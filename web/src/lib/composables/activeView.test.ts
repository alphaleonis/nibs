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
const pickingNoResume: ViewState = {
  kind: "pickingType",
  parentId: "p1",
  parentType: "epic",
  validTypes: ["feature", "task", "bug"],
  presentation: "docked",
  resume: null,
};
const pickingResume: ViewState = {
  kind: "pickingType",
  parentId: "p1",
  parentType: "epic",
  validTypes: ["feature", "task", "bug"],
  presentation: "expanded",
  resume: { nibId: "n1", presentation: "docked" },
};

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

  it("opening a nib from pickingType resolves the picker", () => {
    expect(reduce(pickingResume, { type: "OPEN", nibId: "n2" })).toEqual({
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

  it("EXPAND on pickingType keeps parent/resume", () => {
    const r = reduce(pickingNoResume, { type: "EXPAND" });
    expect(r).toEqual({ ...pickingNoResume, presentation: "expanded" });
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
});

// --- START_CREATE_CHILD ------------------------------------------------------

describe("reduce · START_CREATE_CHILD", () => {
  it("with a single valid type goes straight to creating", () => {
    expect(
      reduce(viewingD, {
        type: "START_CREATE_CHILD",
        parentId: "p1",
        parentType: "milestone",
        validTypes: ["epic"],
      }),
    ).toEqual({
      kind: "creating",
      defaults: { type: "epic", parent: "p1" },
      presentation: "docked",
    });
  });

  it("with several valid types opens the type picker, carrying a resume target from viewing", () => {
    expect(
      reduce(viewingE, {
        type: "START_CREATE_CHILD",
        parentId: "p1",
        parentType: "epic",
        validTypes: ["feature", "task", "bug"],
      }),
    ).toEqual({
      kind: "pickingType",
      parentId: "p1",
      parentType: "epic",
      validTypes: ["feature", "task", "bug"],
      presentation: "expanded",
      resume: { nibId: "n1", presentation: "expanded" },
    });
  });

  it("from closed the picker has no resume target", () => {
    expect(
      reduce(closed, {
        type: "START_CREATE_CHILD",
        parentId: "p1",
        parentType: "epic",
        validTypes: ["feature", "task", "bug"],
      }),
    ).toEqual({
      kind: "pickingType",
      parentId: "p1",
      parentType: "epic",
      validTypes: ["feature", "task", "bug"],
      presentation: "docked",
      resume: null,
    });
  });

  it("with no valid child types is a no-op (leaf parent)", () => {
    expect(
      reduce(viewingD, {
        type: "START_CREATE_CHILD",
        parentId: "p1",
        parentType: "task",
        validTypes: [],
      }),
    ).toEqual(viewingD);
  });
});

// --- CHOOSE_TYPE / CANCEL_TYPE ----------------------------------------------

describe("reduce · CHOOSE_TYPE / CANCEL_TYPE", () => {
  it("CHOOSE_TYPE resolves pickingType into creating (parent carried)", () => {
    expect(reduce(pickingResume, { type: "CHOOSE_TYPE", nibType: "feature" })).toEqual({
      kind: "creating",
      defaults: { type: "feature", parent: "p1" },
      presentation: "expanded",
    });
  });

  it("CANCEL_TYPE with a resume target returns to viewing that nib", () => {
    expect(reduce(pickingResume, { type: "CANCEL_TYPE" })).toEqual({
      kind: "viewing",
      nibId: "n1",
      presentation: "docked",
    });
  });

  it("CANCEL_TYPE without a resume target returns to closed", () => {
    expect(reduce(pickingNoResume, { type: "CANCEL_TYPE" })).toEqual(closed);
  });

  it("CHOOSE_TYPE outside pickingType is a no-op", () => {
    expect(reduce(viewingD, { type: "CHOOSE_TYPE", nibType: "task" })).toEqual(viewingD);
  });

  it("CANCEL_TYPE outside pickingType is a no-op", () => {
    expect(reduce(creatingD, { type: "CANCEL_TYPE" })).toEqual(creatingD);
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
    for (const s of [viewingD, viewingE, goneD, creatingD, pickingResume]) {
      expect(reduce(s, { type: "CLOSE" })).toEqual(closed);
    }
  });
});

// --- Totality: illegal pairs are no-ops -------------------------------------

describe("reduce · illegal (state, action) pairs are no-ops", () => {
  const cases: Array<[string, ViewState, Action]> = [
    ["closed + EXPAND", closed, { type: "EXPAND" }],
    ["closed + COLLAPSE", closed, { type: "COLLAPSE" }],
    ["closed + CHOOSE_TYPE", closed, { type: "CHOOSE_TYPE", nibType: "task" }],
    ["closed + CANCEL_TYPE", closed, { type: "CANCEL_TYPE" }],
    ["closed + SAVED", closed, { type: "SAVED", nibId: "x" }],
    ["closed + DELETED", closed, { type: "DELETED" }],
    ["viewing + CHOOSE_TYPE", viewingD, { type: "CHOOSE_TYPE", nibType: "task" }],
    ["viewing + CANCEL_TYPE", viewingD, { type: "CANCEL_TYPE" }],
    ["viewing + SAVED(same)", viewingD, { type: "SAVED", nibId: "n1" }],
    ["gone + DELETED", goneD, { type: "DELETED" }],
    ["gone + CHOOSE_TYPE", goneD, { type: "CHOOSE_TYPE", nibType: "task" }],
    ["gone + SAVED", goneD, { type: "SAVED", nibId: "x" }],
    ["creating + CHOOSE_TYPE", creatingD, { type: "CHOOSE_TYPE", nibType: "task" }],
    ["creating + CANCEL_TYPE", creatingD, { type: "CANCEL_TYPE" }],
    ["creating + DELETED", creatingD, { type: "DELETED" }],
    ["pickingType + DELETED", pickingResume, { type: "DELETED" }],
    ["pickingType + SAVED", pickingResume, { type: "SAVED", nibId: "x" }],
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
    ["pickingType + OPEN", pickingResume, { type: "OPEN", nibId: "n2" }, false],
    // START_CREATE / START_CREATE_CHILD
    ["viewing + START_CREATE", viewingD, { type: "START_CREATE", defaults: { type: "task" } }, true],
    ["closed + START_CREATE", closed, { type: "START_CREATE", defaults: { type: "task" } }, false],
    ["creating + START_CREATE", creatingD, { type: "START_CREATE", defaults: { type: "bug" } }, true],
    [
      "viewing + START_CREATE_CHILD",
      viewingD,
      { type: "START_CREATE_CHILD", parentId: "p1", parentType: "epic", validTypes: ["feature", "task"] },
      true,
    ],
    [
      "pickingType + START_CREATE_CHILD",
      pickingResume,
      { type: "START_CREATE_CHILD", parentId: "p1", parentType: "epic", validTypes: ["feature", "task"] },
      false,
    ],
    [
      // Leaf parent → reduce is a no-op → nothing is abandoned → no discard prompt.
      "viewing + START_CREATE_CHILD leaf parent (no valid types)",
      viewingD,
      { type: "START_CREATE_CHILD", parentId: "p1", parentType: "task", validTypes: [] },
      false,
    ],
    // CLOSE
    ["viewing + CLOSE", viewingD, { type: "CLOSE" }, true],
    ["gone + CLOSE", goneD, { type: "CLOSE" }, true],
    ["creating + CLOSE", creatingD, { type: "CLOSE" }, true],
    ["closed + CLOSE", closed, { type: "CLOSE" }, false],
    ["pickingType + CLOSE", pickingResume, { type: "CLOSE" }, false],
    // Buffer-preserving actions never abandon
    ["viewing + EXPAND", viewingD, { type: "EXPAND" }, false],
    ["viewing + COLLAPSE", viewingE, { type: "COLLAPSE" }, false],
    ["viewing + SAVED", viewingD, { type: "SAVED", nibId: "n1" }, false],
    ["viewing + DELETED", viewingD, { type: "DELETED" }, false],
    ["creating + SAVED", creatingD, { type: "SAVED", nibId: "x" }, false],
    ["pickingType + CHOOSE_TYPE", pickingResume, { type: "CHOOSE_TYPE", nibType: "task" }, false],
    ["pickingType + CANCEL_TYPE", pickingResume, { type: "CANCEL_TYPE" }, false],
  ];

  it.each(cases)("%s -> %s", (_label, state, action, expected) => {
    expect(abandonsBuffer(state, action)).toBe(expected);
  });
});
