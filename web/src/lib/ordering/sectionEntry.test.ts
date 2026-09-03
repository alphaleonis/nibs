import { describe, expect, it } from "vitest";
import { planDrop, type DropPlan } from "./dropPlan";
import { GOVERNS_NOTHING, type SectionMeaning } from "./sectionMeaning";
import { batch, reorderNib, updateNib } from "../mutations/commands";
import { collectDescendantIds } from "../dropZone";
import type { DropZone } from "../drag.svelte";
import { buildShapedTableData, type RowData } from "../tableData";
import type { GroupingLens, Placement, ViewShape } from "../tree";
import type { NibFilter, TreeTableNib } from "../types";
import type { AssignableField } from "./sectionMeaning";

/**
 * One fixture driven through the real pipeline — an assigning lens, then
 * `buildShapedTableData`, then `planDrop` over the rows it produced — because
 * the defect this file exists to pin lives in the SEAM between them: rows in two
 * different sections came out of the table carrying one ordering group, and the
 * planner, holding only rows, could not tell them apart.
 *
 * The lens is local. No shipped view level assigns (an areas view is Phase 9),
 * so the assign arm of `SectionEntry` has no production caller yet; what is
 * under test is the mechanism, not that lens.
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

const NO_AREA = "/__no_area__";

/**
 * A lens shaped like the areas view Phase 9 will ship: sections declared up
 * front, NO ordering axis of its own (there is no `AREA` order scope, and
 * `Region`'s arms are bound two-way to the ones there are), and entering a
 * section is an assignment of the `area` field.
 */
const areaLens: GroupingLens = {
  leftover: { key: NO_AREA, label: "No area" },
  declares: {
    kind: "forest",
    roots: [
      { key: "infra", label: "infra", description: "", color: "", children: [] },
      {
        key: "web",
        label: "web",
        description: "",
        color: "",
        children: [{ key: "web/api", label: "web/api", description: "", color: "", children: [] }],
      },
    ],
  },
  nestHeadersStructurally: false,
  orderWithinSection: () => null,
  meaning: (section): SectionMeaning =>
    section === NO_AREA
      ? GOVERNS_NOTHING
      : { memberRegion: null, onEnter: { kind: "assign", field: "area", value: section, noun: "area" } },
  place: (item): Placement => ({ kind: "member", section: item.area === "" ? NO_AREA : item.area }),
};

const areaShape: ViewShape = { kind: "grouped", lens: areaLens };

const INFRA = "/section:infra_";
const WEB = "/section:web_";

/**
 *   infra          xf (feature)  ─ xt (task, child of xf)
 *                  xa (task)
 *   web            yf (feature)  ─ yt (task, child of yf)
 *                  ya (task)
 *                  ya2 (task)
 *   web/api        (declared, empty)
 *   No area        ms (milestone)
 *                  na (task, unassigned)
 *                  nf (feature, unassigned — the one container outside any
 *                      assigning section, so a drop INTO it leaves one)
 *
 * Every top-level member's real parent is null, so they all carry one region —
 * `{axis:"parent", parentId:null}` — across three different sections. That is
 * the shape the erasure needs, and it is not contrived: an area lens declares no
 * member region, so every one of its top-level rows falls back to its own parent
 * group.
 *
 * The rows a position is aimed at are LEAF-typed on purpose: the bottom edge of
 * a container reads as "enter it" (`planDrop` promotes it), and that promotion
 * would answer the "after" cases with a container-entry refusal before the
 * section rule was reached.
 */
const FIXTURE: TreeTableNib[] = [
  nib({ id: "xf", title: "Infra feature", type: "feature", area: "infra" }),
  nib({ id: "xt", title: "Infra task", type: "task", area: "infra", parentId: "xf" }),
  nib({ id: "xa", title: "Infra loose task", type: "task", area: "infra" }),
  nib({ id: "yf", title: "Web feature", type: "feature", area: "web" }),
  nib({ id: "yt", title: "Web task", type: "task", area: "web", parentId: "yf" }),
  nib({ id: "ya", title: "Web loose task", type: "task", area: "web" }),
  nib({ id: "ya2", title: "Web loose task two", type: "task", area: "web" }),
  nib({ id: "ms", title: "A milestone", type: "milestone" }),
  nib({ id: "na", title: "Unassigned task", type: "task" }),
  nib({ id: "nf", title: "Unassigned feature", type: "feature" }),
];

const noFilter: NibFilter = {};
const TABLE = buildShapedTableData(FIXTURE, noFilter, areaShape, new Set());
const ROWS: RowData[] = TABLE.rows;
const ROWS_BY_ID = new Map(ROWS.map((r) => [r.nib.id, r]));

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
    descendantIds: collectDescendantIds(draggedIds, ROWS),
    containment: TABLE.containment,
  });
}

describe("the rows an assigning lens produces", () => {
  it("puts every top-level member of every section in ONE ordering group", () => {
    // The premise the refusal rests on, asserted rather than assumed: without
    // it, `sameRegion` would already separate the sections and there would be
    // nothing to refuse.
    for (const id of ["xf", "xa", "yf", "ya", "ya2", "ms", "na"]) {
      expect(row(id).region, id).toEqual({ axis: "parent", parentId: null });
    }
  });

  it("names the section each row is a MEMBER of, nested rows included", () => {
    expect(row("xf").section?.key).toBe("infra");
    expect(row("yf").section?.key).toBe("web");
    expect(row("ms").section?.key).toBe(NO_AREA);
    // Unlike the member region, which stops at the section's own rows, the
    // section identity reaches the whole subtree: a member's children were
    // placed by their own `area` too, so they are in that section as much as the
    // member is, and answering `null` here would say they are in none.
    expect(row("xt").section?.key).toBe("infra");
    expect(row("yt").section?.key).toBe("web");
  });

  it("says what entering each section does, on the section's own row", () => {
    expect(row(WEB).drawsSection).toEqual({
      key: "web",
      display: { label: "web", description: "", color: "" },
      count: 4,
      onEnter: { kind: "assign", field: "area", value: "web", noun: "area" },
    });
    expect(row(NO_AREA).drawsSection?.onEnter).toEqual({ kind: "byRow" });
  });
});

describe("a position drawn across two assigning sections", () => {
  /**
   * THE bug. With the `crosses-section` branch disabled, this exact call answers
   *
   *     {"ok":true,"kind":"position","region":{"axis":"parent","parentId":null},
   *      "indicator":"before","label":"Reorder in the top level",
   *      "command":{"kind":"reorder-nib","id":"xa","beforeId":"ya"}}
   *
   * — an affordance promising a reorder, a write landing in the global root
   * order, and a row snapping back because its `area` never changed. Run, not
   * reconstructed: the branch was disabled and these three tests failed, this
   * one printing the plan above.
   */
  it("is refused rather than written as a sibling reorder", () => {
    for (const zone of ["before", "after"] as const) {
      const plan = planFor(["xa"], "ya", zone);
      if (plan.ok) throw new Error(`expected a refusal for ${zone}, got ${plan.label}`);
      expect(plan.refusal.reason, zone).toBe("crosses-section");
      expect(plan.refusal.message, zone).toContain("the web area");
    }
  });

  it("offers the assignment as its remedy — the same write dropping onto the section performs", () => {
    const plan = planFor(["xa"], "ya", "after");
    if (plan.ok) throw new Error("expected a refusal");
    expect(plan.refusal.actionLabel).toBe("Move to the web area");
    expect(plan.refusal.actionCommand).toEqual(batch([updateNib("xa", { area: "web" })]));
  });

  it("answers a type that takes no assignment with the impossibility, not the axis", () => {
    // `nibtypes.RefusedAxes` refuses BOTH axes for a milestone, so an assignment
    // is not a remedy being withheld here — it is a thing that can never happen,
    // and saying "joining one is an assignment" would name it as the fix. The
    // two bands that can both answer therefore answer the same.
    const plan = planFor(["ms"], "ya", "before");
    if (plan.ok) throw new Error("expected a refusal");
    expect(plan.refusal.reason).toBe("unassignable-type");
    expect(plan.refusal.actionLabel).toBeUndefined();
    expect(plan.refusal.actionCommand).toBeUndefined();

    const onSectionRow = planFor(["ms"], WEB, "reparent");
    if (onSectionRow.ok) throw new Error("expected a refusal");
    expect(onSectionRow.refusal.reason).toBe(plan.refusal.reason);
    expect(onSectionRow.refusal.message).toBe(plan.refusal.message);
  });

  it("leaves a position INSIDE one section an ordinary sibling reorder", () => {
    // The affordance the refusal must not take away.
    const plan = planFor(["ya2"], "ya", "before");
    if (!plan.ok) throw new Error(plan.refusal.message);
    if (plan.kind !== "position") throw new Error(`expected a position plan, got ${plan.kind}`);
    expect(plan.region).toEqual({ axis: "parent", parentId: null });
    expect(plan.label).toBe("Reorder in the top level");
    expect(plan.command).toEqual(reorderNib("ya2", { beforeId: "ya" }));
  });

  it("refuses the same line drawn OUT of an assigning section, into the leftover", () => {
    // The direction a check keyed on the DESTINATION's section misses: the
    // leftover assigns nothing, so it has no `assign` entry to trip on — while
    // the erasure is identical, `ya` carrying `area: web` through a reorder in
    // the root group it shares with `na`.
    for (const zone of ["before", "after"] as const) {
      const plan = planFor(["ya"], "na", zone);
      if (plan.ok) throw new Error(`expected a refusal for ${zone}, got ${plan.label}`);
      expect(plan.refusal.reason, zone).toBe("crosses-section");
      expect(plan.refusal.message, zone).toBe(
        "ya is in the web area, and leaving one is an assignment rather than a move.",
      );
      // No remedy on this side: the destination's section declares no write for
      // entering it, so there is none to offer.
      expect(plan.refusal.actionLabel, zone).toBeUndefined();
      expect(plan.refusal.actionCommand, zone).toBeUndefined();
    }
  });

  it("leaves a promotion to the top level of a row's OWN section accepted", () => {
    // `yt` is nested under `yf` and both are in `web`, so this line crosses
    // nothing. Refusing it — which a check reading only the section that draws a
    // row's ANCESTOR does — takes away a working gesture and says something
    // false about `yt` while doing it.
    const plan = planFor(["yt"], "ya", "before");
    if (!plan.ok) throw new Error(plan.refusal.message);
    if (plan.kind !== "position") throw new Error(`expected a position plan, got ${plan.kind}`);
    expect(plan.region).toEqual({ axis: "parent", parentId: null });
  });

  it("refuses a reparent between two members' subtrees, because the write lands nowhere near the line", () => {
    const plan = planFor(["xt"], "yt", "before");
    if (plan.ok) throw new Error(`expected a refusal, got ${plan.label}`);
    expect(plan.refusal.reason).toBe("crosses-section");

    // Why refusing is right, executed rather than argued: performing the
    // reparent this line asked for leaves `xt` a TOP-LEVEL member of `infra`.
    // Membership here is `area`, and each section's nesting is rebuilt from the
    // nibs that landed in it, so `yf` is not in `xt`'s list to nest under.
    const reparented = FIXTURE.map((n) => (n.id === "xt" ? { ...n, parentId: "yf" } : n));
    const after = buildShapedTableData(reparented, noFilter, areaShape, new Set()).rows;
    const moved = after.find((r) => r.nib.id === "xt");
    expect(moved?.section?.key).toBe("infra");
    expect(moved?.depth).toBe(1);
  });
});

describe("a drag whose rows are in SEVERAL sections", () => {
  const spread = (first: string, second: string) =>
    `The 2 dragged nibs are spread across the ${first} area and the ${second} area, and leaving one is an assignment rather than a move.`;

  it("is refused rather than written as a reorder in the group they happen to share", () => {
    // `xa` is in infra and `ya` in web, so no ONE section is the section they
    // are in — and a single shared answer has to spell that the same way it
    // spells "in no section at all". Under the folded answer this drag reached
    // the ordinary reorder below and wrote `reorder-nib` in the global root
    // order, redrawing both rows in their own sections and changing their
    // position relative to their own section-mates on the way.
    //
    // The sentence follows selection order because the sections do, so both
    // orders are asserted rather than one being taken for the other.
    for (const zone of ["before", "after"] as const) {
      for (const [ids, message] of [
        [["xa", "ya"], spread("infra", "web")],
        [["ya", "xa"], spread("web", "infra")],
      ] as const) {
        const where = `${ids.join("+")} ${zone}`;
        const plan = planFor([...ids], "na", zone);
        if (plan.ok) throw new Error(`expected a refusal for ${where}, got ${plan.label}`);
        expect(plan.refusal.reason, where).toBe("crosses-section");
        expect(plan.refusal.message, where).toBe(message);
      }
    }
  });

  it("names only the sections it is actually leaving", () => {
    // `na` is in the leftover, which assigns nothing, so its side has nothing to
    // leave: one section is left, by one row, and the sentence takes the
    // single-section shape naming that row rather than the whole selection.
    const plan = planFor(["na", "ya"], "ms", "before");
    if (plan.ok) throw new Error(`expected a refusal, got ${plan.label}`);
    expect(plan.refusal.reason).toBe("crosses-section");
    expect(plan.refusal.message).toBe(
      "ya is in the web area, and leaving one is an assignment rather than a move.",
    );
  });

  it("offers a remedy for only the rows it would move, and says so", () => {
    // The joining direction of the same spanning drag. `ya` already holds
    // `area: web`, so the assignment is a change for `xa` alone: a remedy built
    // from the whole dragged set re-writes a value `ya` carries — the etag bump,
    // change pulse and history entry the accepted plan drops — and its sentence
    // is false about half the subject it names.
    const plan = planFor(["xa", "ya"], "ya2", "before");
    if (plan.ok) throw new Error(`expected a refusal, got ${plan.label}`);
    expect(plan.refusal.reason).toBe("crosses-section");
    expect(plan.refusal.message).toBe(
      "xa is not in the web area, and joining one is an assignment rather than a move.",
    );
    expect(plan.refusal.actionCommand).toEqual(batch([updateNib("xa", { area: "web" })]));

    // And it is the SAME write, under the same label, that dropping the same
    // selection onto the section performs — which is what sharing one label
    // between the two claims.
    const onSection = planFor(["xa", "ya"], WEB, "reparent");
    if (!onSection.ok) throw new Error(onSection.refusal.message);
    expect(onSection.command).toEqual(plan.refusal.actionCommand);
    expect(onSection.label).toBe(plan.refusal.actionLabel);
  });

  it("leaves a spanning drag alone where no section on either side assigns", () => {
    // The refusal is about assignment, not about disagreement: two rows in two
    // sections that decide nothing still take an ordinary reorder, because the
    // write says nothing false about where they end up.
    const byRowShape: ViewShape = { kind: "grouped", lens: { ...areaLens, meaning: () => GOVERNS_NOTHING } };
    const table = buildShapedTableData(FIXTURE, noFilter, byRowShape, new Set());
    const rows = table.rows;
    const byId = new Map(rows.map((r) => [r.nib.id, r]));
    const plan = planDrop({
      draggedIds: ["xa", "ya"],
      rowsById: byId,
      draggedRowsById: byId,
      target: byId.get("na")!,
      zone: "before",
      descendantIds: collectDescendantIds(["xa", "ya"], rows),
      containment: table.containment,
    });
    if (!plan.ok) throw new Error(plan.refusal.message);
    expect(plan.kind).toBe("position");
  });
});

describe("the band the cursor happened to be in", () => {
  it("does not change the answer for a cross-section reparent", () => {
    // The three bands of one row differ in what they WRITE — an order key from
    // the edges, a parent link from the middle — and not in which section the
    // row ends up drawn in, which is what decides whether either write says
    // something true. Gating the boundary on the band refused this gesture from
    // `yf`'s top edge and performed it from its middle.
    for (const zone of ["before", "after", "reparent"] as const) {
      const plan = planFor(["xa"], "yf", zone);
      if (plan.ok) throw new Error(`expected a refusal for ${zone}, got ${plan.label}`);
      expect(plan.refusal.reason, zone).toBe("crosses-section");
    }
  });

  it("because the middle band's write lands nowhere near the container it named", () => {
    // Executed, not argued: performing the reparent the middle band accepted
    // leaves `xa` a TOP-LEVEL member of infra with no display parent at all, so
    // the "Move under yf" it offered was false about every part of the result.
    const reparented = FIXTURE.map((n) => (n.id === "xa" ? { ...n, parentId: "yf" } : n));
    const after = buildShapedTableData(reparented, noFilter, areaShape, new Set()).rows;
    const moved = after.find((r) => r.nib.id === "xa");
    expect(moved?.section?.key).toBe("infra");
    expect(moved?.depth).toBe(1);
    expect(moved?.displayParentId).toBe(null);
  });

  it("and the OUT direction is the same gesture", () => {
    // `nf` is a container in the leftover, which assigns nothing — so its middle
    // band used to accept `ya` leaving web by a parent link, drawing "Move under
    // nf" over a row that stays a top-level member of web.
    const plan = planFor(["ya"], "nf", "reparent");
    if (plan.ok) throw new Error(`expected a refusal, got ${plan.label}`);
    expect(plan.refusal.reason).toBe("crosses-section");
    expect(plan.refusal.message).toBe(
      "ya is in the web area, and leaving one is an assignment rather than a move.",
    );

    const reparented = FIXTURE.map((n) => (n.id === "ya" ? { ...n, parentId: "nf" } : n));
    const after = buildShapedTableData(reparented, noFilter, areaShape, new Set()).rows;
    const moved = after.find((r) => r.nib.id === "ya");
    expect(moved?.section?.key).toBe("web");
    expect(moved?.displayParentId).toBe(null);
  });
});

describe("a drop ONTO an assigning section", () => {
  it("assigns, as a batch of updates and nothing else", () => {
    const plan = planFor(["xf", "xa"], WEB, "reparent");
    if (!plan.ok) throw new Error(plan.refusal.message);
    if (plan.kind !== "assign") throw new Error(`expected an assignment, got ${plan.kind}`);
    expect(plan.assignment).toEqual({ field: "area", value: "web" });
    expect(plan.indicator).toBe("into");
    expect(plan.label).toBe("Move to the web area");
    // A `batch`, not a `sequence`: no row's write waits on another's. The
    // milestone remedy interleaves only because a queue reorder is refused
    // while its subject is in no queue, and an assignment carries no position to
    // follow it.
    expect(plan.command).toEqual(batch([updateNib("xf", { area: "web" }), updateNib("xa", { area: "web" })]));
    expect(plan.command.kind).toBe("batch");
  });

  it("leaves a drop into a MEMBER to the member's own type", () => {
    // The section around a row does not speak for the row. `yf` is a feature
    // drawn inside the assigning `web` section, and aiming at its middle enters
    // the FEATURE — the distinction between the section a row draws and the one
    // it is drawn in, at the only place where conflating them would show.
    //
    // The dragged row is a `web` one so the answer turns on that distinction
    // alone: a subject from another section is refused for crossing the
    // boundary, and would tell us nothing about which of the two sections the
    // middle band read.
    const plan = planFor(["ya"], "yf", "reparent");
    if (!plan.ok) throw new Error(plan.refusal.message);
    if (plan.kind !== "position") throw new Error(`expected a position plan, got ${plan.kind}`);
    expect(plan.region).toEqual({ axis: "parent", parentId: "yf" });
  });

  it("refuses a type that takes no assignment", () => {
    const plan = planFor(["ms"], WEB, "reparent");
    if (plan.ok) throw new Error(`expected a refusal, got ${plan.label}`);
    expect(plan.refusal.reason).toBe("unassignable-type");
    expect(plan.refusal.message).toContain("the web area");
  });

  it("writes nothing when the rows are already in that section", () => {
    const plan = planFor(["ya", "ya2"], WEB, "reparent");
    if (plan.ok) throw new Error(`expected a refusal, got ${plan.label}`);
    expect(plan.refusal.reason).toBe("already-in-section");
  });

  it("writes nothing for a NESTED row already in that section either", () => {
    // `yt` sits under `yf`, and its `area` is already `web`, so the batch would
    // be one `updateNib` setting the value the nib holds — an etag bump and a
    // change pulse for a row nothing changed.
    const plan = planFor(["yt"], WEB, "reparent");
    if (plan.ok) throw new Error(`expected a refusal, got ${plan.label}`);
    expect(plan.refusal.reason).toBe("already-in-section");
  });

  it("refuses a position write into a section that says entering it is meaningless", () => {
    // The `refuse` arm used to fall through to the position write — the erasure
    // this guard exists to stop. A section that has just said entering it means
    // nothing cannot also take a row by reorder.
    const refusingShape: ViewShape = {
      kind: "grouped",
      lens: {
        ...areaLens,
        meaning: (section): SectionMeaning =>
          section === "web"
            ? { memberRegion: null, onEnter: { kind: "refuse", message: "The web area is read-only here." } }
            : areaLens.meaning(section),
      },
    };
    const table = buildShapedTableData(FIXTURE, noFilter, refusingShape, new Set());
    const rows = table.rows;
    const byId = new Map(rows.map((r) => [r.nib.id, r]));
    const plan = planDrop({
      draggedIds: ["xa"],
      rowsById: byId,
      draggedRowsById: byId,
      target: byId.get("ya")!,
      zone: "before",
      descendantIds: collectDescendantIds(["xa"], rows),
      containment: table.containment,
    });

    if (plan.ok) throw new Error(`expected a refusal, got ${plan.label}`);
    expect(plan.refusal.reason).toBe("entry-refused");
    expect(plan.refusal.message).toBe("The web area is read-only here.");
  });

  it("writes only for the rows a MIXED drag actually moves", () => {
    // `xa` is in infra and moves; `ya` already holds `web`. The all-or-nothing
    // refusal above cannot fire here, so without trimming, `ya` takes a write
    // setting the value it already has.
    const plan = planFor(["xa", "ya"], WEB, "reparent");
    if (!plan.ok) throw new Error(`expected a plan, got ${plan.refusal.reason}`);
    if (plan.kind !== "assign") throw new Error(`expected an assign plan, got ${plan.kind}`);
    if (plan.command.kind !== "batch") throw new Error(`expected a batch, got ${plan.command.kind}`);
    const written = plan.command.commands.map((c) => {
      if (c.kind !== "update-nib") throw new Error(`expected update-nib, got ${c.kind}`);
      return c.id;
    });
    expect(written).toEqual(["xa"]);
  });

  it("assigns a selection spanning two ordering groups", () => {
    // The shared-ordering-group guard is about positioning rows against each
    // OTHER, and an assignment positions nothing: it is one independent
    // `updateNib` per row, with no anchor and no scope, so rows in different
    // groups can take one as readily as siblings can. `ya` is top-level and `yt`
    // is a child of `yf`.
    expect(row("ya").region).not.toEqual(row("yt").region);
    const plan = planFor(["ya", "yt"], INFRA, "reparent");
    if (!plan.ok) throw new Error(`expected a plan, got ${plan.refusal.reason}`);
    if (plan.kind !== "assign") throw new Error(`expected an assign plan, got ${plan.kind}`);
    expect(plan.command).toEqual(batch([updateNib("ya", { area: "infra" }), updateNib("yt", { area: "infra" })]));
  });

  it("still refuses a fabricated container in the dragged set", () => {
    // The one guard that stays AHEAD of the assignment branch: a section row is
    // not a nib, so there is no subject for `updateNib` to name — and keeping it
    // first is what leaves the branch below with nothing synthetic to write for.
    const plan = planFor([WEB], INFRA, "reparent");
    if (plan.ok) throw new Error(`expected a refusal, got ${plan.label}`);
    expect(plan.refusal.reason).toBe("unorderable-source");
  });

  it("takes only the middle band — a section's edges name a position it cannot give", () => {
    for (const zone of ["before", "after"] as const) {
      const plan = planFor(["xf"], WEB, zone);
      if (plan.ok) throw new Error(`expected a refusal for ${zone}, got ${plan.label}`);
      expect(plan.refusal.reason, zone).toBe("unorderable-target");
    }
  });

  it("leaves a section that governs nothing to the row under the cursor", () => {
    // The leftover answers `byRow`, so entering it is refused exactly as it was
    // before any of this: a fabricated container names no nib to anchor on.
    const plan = planFor(["xf"], NO_AREA, "reparent");
    if (plan.ok) throw new Error(`expected a refusal, got ${plan.label}`);
    expect(plan.refusal.reason).toBe("unorderable-target");
  });

  it("does not reach a section drawn inside another one", () => {
    // `web/api` is declared inside `web`, and both are rows. Aiming at the inner
    // one assigns to the inner one — the key the row draws, not the one it sits
    // in.
    const plan = planFor(["xf"], "/section:web/api_", "reparent");
    if (!plan.ok) throw new Error(plan.refusal.message);
    if (plan.kind !== "assign") throw new Error(`expected an assignment, got ${plan.kind}`);
    expect(plan.assignment).toEqual({ field: "area", value: "web/api" });
  });
});

describe("a section that refuses entry", () => {
  const shape: ViewShape = {
    kind: "grouped",
    lens: {
      ...areaLens,
      meaning: (section) =>
        section === NO_AREA
          ? GOVERNS_NOTHING
          : { memberRegion: null, onEnter: { kind: "refuse", message: `${section} is read-only here.` } },
    },
  };
  const table = buildShapedTableData(FIXTURE, noFilter, shape, new Set());
  const rows = table.rows;
  const byId = new Map(rows.map((r) => [r.nib.id, r]));

  function planOntoWeb(draggedIds: string[]): DropPlan {
    return planDrop({
      draggedIds,
      rowsById: byId,
      draggedRowsById: byId,
      target: byId.get(WEB)!,
      zone: "reparent",
      descendantIds: collectDescendantIds(draggedIds, rows),
      containment: table.containment,
    });
  }

  it("answers with the sentence the section carries", () => {
    const plan = planOntoWeb(["xf"]);
    if (plan.ok) throw new Error(`expected a refusal, got ${plan.label}`);
    expect(plan.refusal.reason).toBe("entry-refused");
    expect(plan.refusal.message).toBe("web is read-only here.");
  });

  it("answers the same for a selection spanning two ordering groups", () => {
    // A section refusing entry refuses it for everyone, so this answer does not
    // wait on the rows agreeing about anything — reporting it as an
    // ordering-group problem would name a constraint the gesture never had.
    const plan = planOntoWeb(["ya", "yt"]);
    if (plan.ok) throw new Error(`expected a refusal, got ${plan.label}`);
    expect(plan.refusal.reason).toBe("entry-refused");
    expect(plan.refusal.message).toBe("web is read-only here.");
  });
});

// `INFRA` is exported by the fixture comment above as the row id of the infra
// section; it is asserted here so a change to `sectionRowId`'s escaping shows up
// as a failure rather than as a silently unreachable test above.
describe("the fixture's own section row ids", () => {
  it("are the ids the builder mints", () => {
    expect(ROWS_BY_ID.has(INFRA)).toBe(true);
    expect(ROWS_BY_ID.has(WEB)).toBe(true);
  });
});

describe("AssignableField", () => {
  it("admits the update input's scalar fields and refuses the rest", () => {
    const scalar: AssignableField[] = ["area", "milestone", "status", "title"];
    // @ts-expect-error `tags` is an array, so it is not a value a section can set
    const list: AssignableField = "tags";
    // @ts-expect-error `bodyMod` is an object, and no more assignable
    const structured: AssignableField = "bodyMod";
    // @ts-expect-error string-valued, and still not a field: `ifMatch` is
    // command-level in the mutation layer, not part of the input a write sets
    const command: AssignableField = "ifMatch";
    expect([scalar.length, list, structured, command]).toEqual([4, "tags", "bodyMod", "ifMatch"]);
  });
});
