import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { SelectionState } from "../selection.svelte";
import { buildContainmentIndex, type ContainmentIndex } from "../containment";
import { DragState } from "../drag.svelte";
import { batch, reorderNib, reparentAndReorder, setParent } from "../mutations/commands";
import type { DropPlan } from "../ordering/dropPlan";
import type { RowData } from "../tableData";
import { rowRegion } from "../tableData";
import { EMPTY_SPINE } from "../viewSpine";

const { buildTableData } = EMPTY_SPINE;
import type { TreeTableNib } from "../types";
import { useTreeDrag } from "./useTreeDrag.svelte";

// jsdom doesn't implement elementFromPoint — stub it
if (typeof document.elementFromPoint !== "function") {
  document.elementFromPoint = () => null;
}

function makeNib(overrides: Partial<TreeTableNib> = {}): TreeTableNib {
  return {
    id: "nibs-001",
    title: "Task 1",
    status: "todo",
    type: "task",
    priority: "normal",
    estimate: "",
    tags: [],
    createdAt: "",
    updatedAt: "",
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

function makeRow(nib: TreeTableNib, opts: Partial<RowData> = {}): RowData {
  return {
    nib,
    depth: 0,
    hasChildren: false,
    dimmed: false,
    parentNib: null,
    displayParentId: null,
    // Production's own rule, called rather than restated — a fabricated bucket
    // built through this helper must come out a member of nothing, as it does
    // in a real table.
    region: rowRegion(nib.id, nib.parentId),
    drawsSection: null,
    section: null,
    ...opts,
  };
}

describe("useTreeDrag", () => {
  // Track the composable's cleanup so we can call it after each test
  let cleanup: (() => void) | undefined;

  // The two pieces of global state a hover fakes. Their undo runs here rather
  // than only at the end of a test body: an assertion between the fake and its
  // undo throws on exactly the runs where a test is catching a regression, and a
  // leaked stub then decides the drop target of every test after it — the first
  // pointermove of the next `startDragOn` already reads `elementFromPoint`.
  const realElementFromPoint = document.elementFromPoint;

  beforeEach(() => {
    cleanup = undefined;
  });

  afterEach(() => {
    document.elementFromPoint = realElementFromPoint;
    document.body.querySelectorAll("tr[data-nib-id]").forEach(tr => tr.remove());
  });

  /** Helper to create drag composable with common defaults */
  function setup(overrides: {
    selection?: SelectionState;
    drag?: DragState;
    rows?: RowData[];
    ondrop?: (plan: DropPlan) => void;
    scrollContainer?: HTMLElement | null;
    dragBlock?: import("../dragBlock").DragBlock | null;
    onblockeddrag?: (block: import("../dragBlock").DragBlock) => void;
    /** Defaults to the index of an EMPTY view: these cases are about the
     *  gesture, and the destination check they would reach is `dropPlan`'s. */
    containment?: ContainmentIndex;
  } = {}) {
    const selection = overrides.selection ?? new SelectionState();
    const drag = overrides.drag ?? new DragState();
    const ondrop = overrides.ondrop ?? vi.fn();
    let rows = overrides.rows ?? [];
    const onblockeddrag = overrides.onblockeddrag ?? vi.fn();

    const result = useTreeDrag({
      selection,
      drag,
      getRows: () => rows,
      getScrollContainer: () => overrides.scrollContainer ?? null,
      getContainment: () => overrides.containment ?? buildContainmentIndex([]),
      getDragBlock: () => overrides.dragBlock ?? null,
      ondrop,
      onblockeddrag,
    });

    // Provide a way for tests to trigger cleanup via pointerup
    cleanup = () => {
      const upEvent = new PointerEvent("pointerup", { bubbles: true });
      window.dispatchEvent(upEvent);
    };

    /**
     * Hand the composable a NEW row list, which is the only shape a change
     * reaches it in: `rows` is a Svelte `$derived` over a freshly built array,
     * so a refetch or an expand replaces it rather than mutating it in place.
     */
    const setRows = (next: RowData[]) => {
      rows = next;
    };

    return { ...result, selection, drag, ondrop, onblockeddrag, setRows };
  }

  it("pointer down sets pending state, move past threshold starts drag", () => {
    const nib1 = makeNib({ id: "nibs-001" });
    const rows = [makeRow(nib1)];
    const { onRowPointerDown, drag } = setup({ rows });

    // Simulate pointer down
    const downEvent = new PointerEvent("pointerdown", {
      clientX: 100,
      clientY: 100,
      bubbles: true,
    });
    onRowPointerDown("nibs-001", downEvent);

    // Drag should NOT be started yet (below threshold)
    expect(drag.isDragging).toBe(false);

    // Move past 5px threshold
    const moveEvent = new PointerEvent("pointermove", {
      clientX: 106,
      clientY: 100,
      bubbles: true,
    });
    window.dispatchEvent(moveEvent);

    // Now drag should have started
    expect(drag.isDragging).toBe(true);
    expect(drag.draggedIds).toContain("nibs-001");

    // Clean up
    cleanup?.();
  });

  it("drop zone computation during drag: setDropTarget called on move over row, clearDropTarget off row", () => {
    const nib1 = makeNib({ id: "nibs-001", type: "milestone" });
    const nib2 = makeNib({ id: "nibs-002", type: "task", parentId: "nibs-001" });
    const rows = [
      makeRow(nib1, { hasChildren: true }),
      makeRow(nib2),
    ];

    const drag = new DragState();
    const setDropTargetSpy = vi.spyOn(drag, "setDropTarget");
    const clearDropTargetSpy = vi.spyOn(drag, "clearDropTarget");

    const { onRowPointerDown } = setup({ drag, rows });

    // Start drag on nib1 (milestone)
    const downEvent = new PointerEvent("pointerdown", {
      clientX: 100, clientY: 100, bubbles: true,
    });
    onRowPointerDown("nibs-001", downEvent);

    // Move past threshold
    window.dispatchEvent(new PointerEvent("pointermove", {
      clientX: 106, clientY: 100, bubbles: true,
    }));
    expect(drag.isDragging).toBe(true);

    // Create a fake <tr> element for nib2 and mock elementFromPoint to return it
    const tr = document.createElement("tr");
    tr.dataset.nibId = "nibs-002";
    // Mock getBoundingClientRect to return known rect
    tr.getBoundingClientRect = () => ({
      top: 200, bottom: 240, left: 0, right: 800,
      height: 40, width: 800, x: 0, y: 200,
      toJSON: () => {},
    });
    document.body.appendChild(tr);

    // Mock elementFromPoint to return the tr
    const origElementFromPoint = document.elementFromPoint;
    document.elementFromPoint = () => tr;

    // Move over the row — cursor Y in the middle of the row = "reparent" zone
    window.dispatchEvent(new PointerEvent("pointermove", {
      clientX: 200, clientY: 220, bubbles: true,
    }));

    // setDropTarget should have been called (zone depends on cursor position relative to row)
    expect(setDropTargetSpy).toHaveBeenCalled();

    // Now move off the row
    document.elementFromPoint = () => null;
    clearDropTargetSpy.mockClear();

    window.dispatchEvent(new PointerEvent("pointermove", {
      clientX: 200, clientY: 500, bubbles: true,
    }));

    expect(clearDropTargetSpy).toHaveBeenCalled();

    // Restore
    document.elementFromPoint = origElementFromPoint;
    document.body.removeChild(tr);
    cleanup?.();
  });

  /** Helper: start a drag on nibId, moving past the threshold */
  function startDragOn(
    nibId: string,
    composable: { onRowPointerDown: (id: string, e: PointerEvent) => void },
  ) {
    composable.onRowPointerDown(nibId, new PointerEvent("pointerdown", {
      clientX: 100, clientY: 100, bubbles: true,
    }));
    window.dispatchEvent(new PointerEvent("pointermove", {
      clientX: 106, clientY: 100, bubbles: true,
    }));
  }

  /**
   * Move the cursor `ratio` of the way down `targetNibId`'s row, through the path
   * production uses: `document.elementFromPoint` plus the row's rect are the only
   * things the composable reads to find a target, so calling `setDropTarget`
   * directly instead would skip the decision under test. `computeDropZone` reads
   * the ratio — below 0.3 is "before", above 0.7 "after", between them the
   * container zone. Returns the undo.
   */
  function hoverRow(targetNibId: string, ratio: number): () => void {
    const tr = document.createElement("tr");
    tr.dataset.nibId = targetNibId;
    tr.getBoundingClientRect = () => ({
      top: 200, bottom: 240, left: 0, right: 800,
      height: 40, width: 800, x: 0, y: 200,
      toJSON: () => {},
    }) as DOMRect;
    document.body.appendChild(tr);
    const origElementFromPoint = document.elementFromPoint;
    document.elementFromPoint = () => tr;

    window.dispatchEvent(new PointerEvent("pointermove", {
      clientX: 200, clientY: 200 + 40 * ratio, bubbles: true,
    }));

    return () => {
      document.elementFromPoint = origElementFromPoint;
      tr.remove();
    };
  }

  /** The single plan a finished drag handed to `ondrop`. */
  function droppedPlan(ondrop: ReturnType<typeof vi.fn>): DropPlan {
    expect(ondrop).toHaveBeenCalledTimes(1);
    return ondrop.mock.calls[0][0] as DropPlan;
  }

  it("pointer up hands ondrop the same plan the affordance was drawn from", () => {
    const nib1 = makeNib({ id: "nibs-001", type: "task" });
    const nib2 = makeNib({ id: "nibs-002", type: "epic" });
    const rows = [
      makeRow(nib2, { hasChildren: true }),
      makeRow(nib1),
    ];

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    startDragOn("nibs-001", composable);
    expect(drag.isDragging).toBe(true);

    const unhover = hoverRow("nibs-002", 0.5);
    expect(drag.dropValid).toBe(true);

    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    expect(droppedPlan(ondrop)).toMatchObject({
      ok: true,
      region: { axis: "parent", parentId: "nibs-002" },
      indicator: "into",
      command: batch([setParent("nibs-001", "nibs-002")]),
    });
    expect(drag.isDragging).toBe(false);

    unhover();
  });

  it("cross-region before/after drop onto a loose bucket item is planned as a reparent", () => {
    // The one test here whose rows come from the real `buildTableData` pipeline
    // rather than hand-built `makeRow`s, so a synthetic bucket row and its loose
    // members are exactly as the producer emits them.
    //
    // Epics lens: E1 (epic header) → F1 (feature child) is dragged; T1 (loose
    // task) lands in the "No epic" bucket. Dropping F1 BEFORE T1 crosses ordering
    // regions — F1 is ordered among E1's children, T1 at the top level — so the
    // plan is a reparent POSITIONED against T1, not a bare reorder.
    //
    // The #jeu5 producer invariant this fixture was built for (a loose bucket
    // item's `displayParentId` must be the bucket's own display parent, never the
    // synthetic bucket id) no longer decides this drop — the plan follows
    // ordering REGIONS, and these rows declare none, so theirs come from
    // `nib.parentId`. The invariant's own guard is `tableData.test.ts`'s "a loose
    // bucket item inherits the bucket's own display parent" case: reverting the
    // producer fix reddens that file with 5 failures and leaves this one green.
    const nibs: TreeTableNib[] = [
      makeNib({ id: "E1", type: "epic", parentId: null }),
      makeNib({ id: "F1", type: "feature", parentId: "E1" }),
      makeNib({ id: "T1", type: "task", parentId: null }),
    ];
    const { rows } = buildTableData(nibs, {}, "epics", new Set<string>());

    // The synthetic bucket row really is in the rows handed to the composable, so
    // the plan is computed against a list that contains a non-nib row.
    expect(rows.some(r => r.nib.id === "/__no_epic__")).toBe(true);

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    startDragOn("F1", composable);
    expect(drag.isDragging).toBe(true);

    // Top 30% of T1's row → the "before" zone (a positioned drop, not an entry).
    const unhover = hoverRow("T1", 0.125);

    expect(drag.dropTargetId).toBe("T1");
    expect(drag.dropZone).toBe("before");
    expect(drag.dropValid).toBe(true);

    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    expect(droppedPlan(ondrop)).toMatchObject({
      ok: true,
      region: { axis: "parent", parentId: null },
      indicator: "before",
      command: reparentAndReorder(["F1"], null, "T1", "before"),
    });

    unhover();
  });

  it("before/after next to a row with a DIFFERENT real parent is an accepted cross-parent reparent", () => {
    // In a grouping lens two rows can share the same DISPLAY container (both
    // display at root → displayParentId null) while having DIFFERENT real
    // nib.parentId — a promoted feature header (real parent a hidden epic) beside
    // a loose "No X" bucket task (real parent null).
    //
    // The gesture is offered now, where it was refused before. The refusal was
    // never about the gesture: a plain before/after here would fire a parent-less
    // reorderNib, which the backend rejects ("not a sibling (different parent)")
    // because it groups by the DRAGGED row's own unchanged real parent. Without a
    // way to name the destination group, refusing was the only safe answer.
    // `planDrop` names it — the destination is the target's real parent group
    // (root here), the dragged row is not in it, so the plan is a reparent
    // POSITIONED against the target rather than a bare reorder, which is a write
    // the server accepts. The affordance follows the same value, so it reads
    // valid.
    const dragged = makeNib({ id: "F1", type: "feature", parentId: "hidden-epic" });
    const target = makeNib({ id: "T1", type: "task", parentId: null });
    const rows = [
      // Promoted header: real parent a hidden epic, but display parent is root.
      makeRow(dragged, { displayParentId: null }),
      // Loose "No epic" bucket item: real parent null, display parent root too.
      makeRow(target, { displayParentId: null }),
    ];

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    startDragOn("F1", composable);
    expect(drag.isDragging).toBe(true);

    const unhover = hoverRow("T1", 0.125);

    expect(drag.dropTargetId).toBe("T1");
    expect(drag.dropZone).toBe("before");
    expect(drag.dropValid).toBe(true);

    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    // A reparent that carries the position, not a bare reorder: the hidden epic
    // is left behind and F1 lands at the top level, before T1.
    expect(droppedPlan(ondrop)).toMatchObject({
      ok: true,
      region: { axis: "parent", parentId: null },
      indicator: "before",
      command: reparentAndReorder(["F1"], null, "T1", "before"),
    });

    unhover();
  });

  it("before/after reorder between rows of one ordering region stays VALID (guardrail)", () => {
    // Scope guardrail: dragged and target sit in ONE ordering region
    // ({axis:"parent", parentId:"epic-A"}), so the plan is the bare `reorderNib`
    // and the affordance reads valid. The equal-display / DIFFERENT-real-parent
    // case is a separate population and is now an ACCEPTED cross-region reparent
    // — see the test above.
    const dragged = makeNib({ id: "C1", type: "task", parentId: "epic-A" });
    const target = makeNib({ id: "C2", type: "task", parentId: "epic-A" });
    const rows = [
      makeRow(dragged, { displayParentId: "epic-A" }),
      makeRow(target, { displayParentId: "epic-A" }),
    ];

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    startDragOn("C1", composable);
    expect(drag.isDragging).toBe(true);

    const unhover = hoverRow("C2", 0.125);

    expect(drag.dropTargetId).toBe("C2");
    expect(drag.dropZone).toBe("before");
    expect(drag.dropValid).toBe(true);

    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    expect(droppedPlan(ondrop)).toMatchObject({
      ok: true,
      region: { axis: "parent", parentId: "epic-A" },
      indicator: "before",
      command: reorderNib("C1", { beforeId: "C2" }),
    });

    unhover();
  });

  it("before/after for a MULTI-SELECT drag spanning MIXED ordering regions is refused", () => {
    // One move positions rows within a SINGLE ordering region, and these two do
    // not share one: D1 is ordered among epic-A's children, D2 among epic-B's.
    // `commonRegion` has no answer, so the plan refuses `mixed-source` and the
    // affordance follows it. Sharing a display container (both draw at root)
    // changes nothing — these rows declare no region, so theirs come from
    // `nib.parentId` rather than from where the view drew them. Drives the real
    // onDragPointerMove path.
    const d1 = makeNib({ id: "D1", type: "feature", parentId: "epic-A" });
    const d2 = makeNib({ id: "D2", type: "task", parentId: "epic-B" });
    const target = makeNib({ id: "T1", type: "task", parentId: "epic-C" });
    const rows = [
      makeRow(d1, { displayParentId: null }),
      makeRow(d2, { displayParentId: null }),
      makeRow(target, { displayParentId: null }),
    ];

    // Multi-select: both D1 and D2 selected, drag started on D1.
    const selection = new SelectionState();
    selection.selectedIds = new Set(["D1", "D2"]);

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ selection, drag, rows, ondrop });

    startDragOn("D1", composable);
    expect(drag.isDragging).toBe(true);
    expect(drag.draggedIds).toEqual(expect.arrayContaining(["D1", "D2"]));

    // Top 30% of the row → the "before" zone (a positioned drop, not an entry).
    const unhover = hoverRow("T1", 0.125);

    expect(drag.dropTargetId).toBe("T1");
    expect(drag.dropZone).toBe("before");
    expect(drag.dropValid).toBe(false);

    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    const plan = droppedPlan(ondrop);
    expect(plan.ok).toBe(false);
    if (plan.ok) throw new Error("unreachable");
    // Pins the reason this fixture actually exercises: without it the case would
    // still pass on any refusal at all.
    expect(plan.refusal.reason).toBe("mixed-source");

    unhover();
  });

  it("a destination this view carries no nib for is refused, and the refusal reaches ondrop", () => {
    // The target is a promoted header — a feature whose real parent is a
    // container the lens hid — so the group a before/after drop lands in is that
    // hidden container, and nothing here can say whether it may hold a feature:
    // it has no row, and the row does not carry it as `parentNib` either. The
    // plan refuses rather than guessing, and the refusal is what `ondrop`
    // receives, so the gesture can be explained instead of just not happening.
    const dragged = makeNib({ id: "nibs-drag", type: "feature", parentId: null });
    const header = makeNib({ id: "nibs-header", type: "feature", parentId: "nibs-hidden-epic" });
    const rows = [
      makeRow(dragged, { displayParentId: null }),
      // Promoted header: real parentId set, but its DISPLAY parent is root (null).
      makeRow(header, { displayParentId: null }),
    ];

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    startDragOn("nibs-drag", composable);
    expect(drag.isDragging).toBe(true);

    const unhover = hoverRow("nibs-header", 0.125);
    expect(drag.dropValid).toBe(false);

    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    const plan = droppedPlan(ondrop);
    expect(plan.ok).toBe(false);
    expect(plan).toMatchObject({
      refusal: { reason: "unknown-destination", region: { axis: "parent", parentId: "nibs-hidden-epic" } },
    });

    unhover();
  });

  it("a reorder within one region sends the bare reorderNib, whatever container the view draws the rows under", () => {
    // Both rows are top-level nibs the view happens to draw inside a container
    // (equal, non-null displayParentId). The drop follows the ordering REGION —
    // which comes from `nib.parentId` — so it is a plain same-group reorder, and
    // the command names no parent at all. A path still reading `displayParentId`
    // would have to invent a destination out of "nibs-shared-epic", which is not
    // where either row is ordered.
    const e1 = makeNib({ id: "nibs-e1", type: "feature", parentId: null });
    const e2 = makeNib({ id: "nibs-e2", type: "feature", parentId: null });
    const rows = [
      makeRow(e1, { displayParentId: "nibs-shared-epic" }),
      makeRow(e2, { displayParentId: "nibs-shared-epic" }),
    ];

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    startDragOn("nibs-e1", composable);
    expect(drag.isDragging).toBe(true);

    const unhover = hoverRow("nibs-e2", 0.125);
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    expect(droppedPlan(ondrop)).toMatchObject({
      ok: true,
      region: { axis: "parent", parentId: null },
      indicator: "before",
      command: reorderNib("nibs-e1", { beforeId: "nibs-e2" }),
    });

    unhover();
  });

  it("a cross-region drop names the target's REAL parent, not the container the view draws it under", () => {
    // The target is drawn inside a bucket (displayParentId "/__no_milestone__")
    // while really being a child of nibs-other-epic, which has no row here — only
    // the target's `parentNib` can answer the type question for it. The
    // destination is that real parent, so the drop reparents there; naming the
    // bucket instead would be a move to a container that is not a nib.
    const otherEpic = makeNib({ id: "nibs-other-epic", type: "epic", parentId: null });
    const dragged = makeNib({ id: "nibs-drag", type: "feature", parentId: null });
    const target = makeNib({ id: "nibs-target", type: "feature", parentId: "nibs-other-epic" });
    const rows = [
      makeRow(dragged, { displayParentId: "/__no_milestone__" }),
      makeRow(target, { parentNib: otherEpic, displayParentId: "/__no_milestone__" }),
    ];

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    startDragOn("nibs-drag", composable);
    const unhover = hoverRow("nibs-target", 0.125);
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    expect(droppedPlan(ondrop)).toMatchObject({
      ok: true,
      region: { axis: "parent", parentId: "nibs-other-epic" },
      indicator: "before",
      command: reparentAndReorder(["nibs-drag"], "nibs-other-epic", "nibs-target", "before"),
    });

    unhover();
  });

  it("the bottom edge of a container enters it, and the row draws the reparent zone for it", () => {
    // The composable's one piece of translation: a plan whose indicator is "into"
    // reaches the row as the "reparent" DropZone, which is the class
    // TreeTableRow keys the container highlight on. The zone the cursor read here
    // is "after" — `planDrop` owns the promotion, and the drag path no longer
    // does it before asking.
    const dragged = makeNib({ id: "nibs-drag", type: "task", parentId: null });
    const epic = makeNib({ id: "nibs-epic", type: "epic", parentId: null });
    const rows = [
      makeRow(dragged),
      makeRow(epic, { hasChildren: true }),
    ];

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    startDragOn("nibs-drag", composable);
    const unhover = hoverRow("nibs-epic", 0.875);

    expect(drag.dropZone).toBe("reparent");
    expect(drag.dropValid).toBe(true);

    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    expect(droppedPlan(ondrop)).toMatchObject({
      ok: true,
      region: { axis: "parent", parentId: "nibs-epic" },
      indicator: "into",
      command: batch([setParent("nibs-drag", "nibs-epic")]),
    });

    unhover();
  });

  it("entering a promoted header names the header, not the container it really sits under", () => {
    // The target is an epic the lens promoted to the display root while its real
    // parent (a milestone) has no row. Entering it must set the header itself as
    // the new parent — the hidden container is where the header lives, not where
    // its children go.
    const dragged = makeNib({ id: "nibs-drag", type: "feature", parentId: null });
    const epic = makeNib({ id: "nibs-epic", type: "epic", parentId: "nibs-hidden-ms" });
    const rows = [
      makeRow(dragged, { displayParentId: null }),
      makeRow(epic, { hasChildren: true, displayParentId: null }),
    ];

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    startDragOn("nibs-drag", composable);
    const unhover = hoverRow("nibs-epic", 0.5);
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    expect(droppedPlan(ondrop)).toMatchObject({
      ok: true,
      region: { axis: "parent", parentId: "nibs-epic" },
      indicator: "into",
      command: batch([setParent("nibs-drag", "nibs-epic")]),
    });

    unhover();
  });

  it("a refused drop reaches ondrop carrying the reason and a message to show", () => {
    // The behavior this lap adds: a drop the plan refuses used to be swallowed
    // before `ondrop`, so the gesture just did nothing. Now the refusal is
    // delivered, and it is the only thing the caller has to explain the drag
    // with. The middle of a leaf is a container entry into a row that holds no
    // children.
    const dragged = makeNib({ id: "nibs-drag", type: "task", parentId: null });
    const leaf = makeNib({ id: "nibs-leaf", type: "task", parentId: null });
    const rows = [makeRow(dragged), makeRow(leaf)];

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    startDragOn("nibs-drag", composable);
    const unhover = hoverRow("nibs-leaf", 0.5);

    // The affordance refuses it too — one value behind both.
    expect(drag.dropValid).toBe(false);

    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    const plan = droppedPlan(ondrop);
    expect(plan.ok).toBe(false);
    if (plan.ok) throw new Error("unreachable");
    expect(plan.refusal.reason).toBe("invalid-parent-type");
    expect(plan.refusal.message).toContain("task");

    unhover();
  });

  it("a drag released over no row reports nothing", () => {
    // The gate that replaced `dropValid`: `ondrop` fires on the plan the cursor
    // last stood on, and there is none when the drag never reached a row. A
    // refusal is a decision about a target; this is the absence of one.
    const rows = [makeRow(makeNib({ id: "nibs-001" }))];
    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    startDragOn("nibs-001", composable);
    expect(drag.isDragging).toBe(true);

    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    expect(ondrop).not.toHaveBeenCalled();
  });

  it("leaving a row before release reports nothing, rather than the plan that row had", () => {
    const dragged = makeNib({ id: "nibs-drag", type: "task", parentId: null });
    const epic = makeNib({ id: "nibs-epic", type: "epic", parentId: null });
    const rows = [makeRow(dragged), makeRow(epic, { hasChildren: true })];

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    startDragOn("nibs-drag", composable);
    const unhover = hoverRow("nibs-epic", 0.5);
    expect(drag.dropValid).toBe(true);

    // Off every row: elementFromPoint finds nothing.
    document.elementFromPoint = () => null;
    window.dispatchEvent(new PointerEvent("pointermove", {
      clientX: 200, clientY: 900, bubbles: true,
    }));
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    expect(ondrop).not.toHaveBeenCalled();

    unhover();
  });

  it("a row that arrives mid-drag can still be dropped on", () => {
    // The row list is LIVE for the whole gesture: a `nibChanged` refetch, an
    // ArrowRight expand, and `handleDrop`'s own `ensureVisible` each hand the
    // composable a new list while a drag is in flight. A list read once at
    // `startDrag` has no row for the arrival, so the cursor resolves to nothing,
    // the plan is cleared, and the release reports NOTHING — the silently
    // swallowed gesture this seam exists to remove, reached without a second
    // client.
    const dragged = makeNib({ id: "nibs-drag", type: "task", parentId: null });
    const rows = [makeRow(dragged)];

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    startDragOn("nibs-drag", composable);
    expect(drag.isDragging).toBe(true);

    const epic = makeNib({ id: "nibs-new", type: "epic", parentId: null });
    composable.setRows([...rows, makeRow(epic, { hasChildren: true })]);

    const unhover = hoverRow("nibs-new", 0.5);
    expect(drag.dropTargetId).toBe("nibs-new");
    expect(drag.dropValid).toBe(true);

    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    expect(droppedPlan(ondrop)).toMatchObject({
      ok: true,
      region: { axis: "parent", parentId: "nibs-new" },
      indicator: "into",
      command: batch([setParent("nibs-drag", "nibs-new")]),
    });

    unhover();
  });

  // Both Escape tests stand on a drag that WOULD have dropped: the cursor is over
  // a row whose plan is accepted, so a cancel that stopped working would be
  // caught. Setting the drop target directly instead leaves no plan behind, and
  // "ondrop was not called" would then hold for the wrong reason.
  it("Escape during drag cancels without calling ondrop", () => {
    const nib1 = makeNib({ id: "nibs-001", type: "task" });
    const epic = makeNib({ id: "nibs-002", type: "epic" });
    const rows = [makeRow(nib1), makeRow(epic, { hasChildren: true })];

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    startDragOn("nibs-001", composable);
    expect(drag.isDragging).toBe(true);

    const unhover = hoverRow("nibs-002", 0.5);
    expect(drag.dropValid).toBe(true);

    // Press Escape via onDragKeyDown
    const escEvent = new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true });
    composable.onDragKeyDown(escEvent);

    // Drag should be canceled, ondrop should NOT have been called
    expect(drag.isDragging).toBe(false);
    expect(ondrop).not.toHaveBeenCalled();
    expect(escEvent.defaultPrevented).toBe(true);

    unhover();
  });

  it("global Escape keydown cancels drag (no focus required)", () => {
    const nib1 = makeNib({ id: "nibs-001", type: "task" });
    const epic = makeNib({ id: "nibs-002", type: "epic" });
    const rows = [makeRow(nib1), makeRow(epic, { hasChildren: true })];

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    startDragOn("nibs-001", composable);
    expect(drag.isDragging).toBe(true);

    const unhover = hoverRow("nibs-002", 0.5);
    expect(drag.dropValid).toBe(true);

    // Dispatch Escape on window (not through the composable's onDragKeyDown)
    const escEvent = new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true });
    window.dispatchEvent(escEvent);

    expect(drag.isDragging).toBe(false);
    expect(ondrop).not.toHaveBeenCalled();

    unhover();
  });

  describe("drag preview", () => {
    let scrollContainer: HTMLElement;

    /** Build a minimal table with a <tr data-nib-id="..."> inside a scroll container */
    function buildTableDOM(nibId: string): HTMLElement {
      scrollContainer = document.createElement("div");
      const table = document.createElement("table");
      table.style.width = "600px";
      table.style.tableLayout = "fixed";

      const thead = document.createElement("thead");
      const headerRow = document.createElement("tr");
      const th = document.createElement("th");
      Object.defineProperty(th, "offsetWidth", { value: 600, configurable: true });
      headerRow.appendChild(th);
      thead.appendChild(headerRow);
      table.appendChild(thead);

      const tbody = document.createElement("tbody");
      const tr = document.createElement("tr");
      tr.dataset.nibId = nibId;
      tr.getBoundingClientRect = () => ({
        top: 50, bottom: 80, left: 10, right: 610,
        height: 30, width: 600, x: 10, y: 50,
        toJSON: () => {},
      });
      const td = document.createElement("td");
      td.textContent = "Test row";
      tr.appendChild(td);
      tbody.appendChild(tr);
      table.appendChild(tbody);

      scrollContainer.appendChild(table);
      document.body.appendChild(scrollContainer);
      return scrollContainer;
    }

    afterEach(() => {
      scrollContainer?.remove();
      // Clean up any stray previews
      document.querySelectorAll("[data-testid='drag-preview']").forEach(el => el.remove());
    });

    it("creates a ghost preview on drag start and removes it on cleanup", () => {
      const nib1 = makeNib({ id: "nibs-001" });
      const rows = [makeRow(nib1)];
      const container = buildTableDOM("nibs-001");

      const { onRowPointerDown, drag } = setup({ rows, scrollContainer: container });

      onRowPointerDown("nibs-001", new PointerEvent("pointerdown", {
        clientX: 100, clientY: 60, bubbles: true,
      }));

      // Move past threshold to start drag
      window.dispatchEvent(new PointerEvent("pointermove", {
        clientX: 106, clientY: 60, bubbles: true,
      }));
      expect(drag.isDragging).toBe(true);

      // Preview should exist in the DOM
      const preview = document.querySelector("[data-testid='drag-preview']");
      expect(preview).not.toBeNull();
      expect(preview!.querySelector("table")).not.toBeNull();

      // Clean up via pointerup
      cleanup?.();

      // Preview should be removed
      const previewAfter = document.querySelector("[data-testid='drag-preview']");
      expect(previewAfter).toBeNull();
    });

    it("positions preview based on grab offset", () => {
      const nib1 = makeNib({ id: "nibs-001" });
      const rows = [makeRow(nib1)];
      const container = buildTableDOM("nibs-001");

      const { onRowPointerDown, drag } = setup({ rows, scrollContainer: container });

      // Grab at clientX=100 — row left is 10, so offsetX = 90
      onRowPointerDown("nibs-001", new PointerEvent("pointerdown", {
        clientX: 100, clientY: 60, bubbles: true,
      }));

      window.dispatchEvent(new PointerEvent("pointermove", {
        clientX: 200, clientY: 120, bubbles: true,
      }));
      expect(drag.isDragging).toBe(true);

      const preview = document.querySelector("[data-testid='drag-preview']") as HTMLElement;
      expect(preview).not.toBeNull();
      // offsetX = 100 - 10 = 90, offsetY = 60 - 50 = 10
      // position = cursor - offset → left = 200 - 90 = 110, top = 120 - 10 = 110
      expect(preview.style.left).toBe("110px");
      expect(preview.style.top).toBe("110px");

      cleanup?.();
    });

    it("drops the region band from the ghost, which has no row above it", () => {
      const nib1 = makeNib({ id: "nibs-001" });
      const rows = [makeRow(nib1)];
      const container = buildTableDOM("nibs-001");
      // The band marks a seam between this row and the one above it in the
      // table. Cloned onto a free-floating ghost it becomes a rule at the top of
      // a single row, in the queue's own color — the badge's color, on a ghost
      // that carries no axis claim at all.
      const source = container.querySelector("tr[data-nib-id]") as HTMLElement;
      source.classList.add("region-band", "region-band-queue");

      const { onRowPointerDown, drag } = setup({ rows, scrollContainer: container });
      onRowPointerDown("nibs-001", new PointerEvent("pointerdown", {
        clientX: 100, clientY: 60, bubbles: true,
      }));
      window.dispatchEvent(new PointerEvent("pointermove", {
        clientX: 106, clientY: 60, bubbles: true,
      }));
      expect(drag.isDragging).toBe(true);

      const clone = document
        .querySelector("[data-testid='drag-preview']")!
        .querySelector("tr[data-nib-id]") as HTMLElement;
      // The clone really is of the banded row, so the absences below are not an
      // assertion about some other element.
      expect(clone.dataset.nibId).toBe("nibs-001");
      expect(clone.classList.contains("region-band")).toBe(false);
      expect(clone.classList.contains("region-band-queue")).toBe(false);
      // Still the banded row in the table itself — the ghost is a copy.
      expect(source.classList.contains("region-band")).toBe(true);

      cleanup?.();
    });
  });

  // A gate (Flat view / search / active sort) suppresses drag deliberately, but
  // silently — the row simply refuses to move. The composable owns the drag
  // threshold, so it is what can tell a real drag ATTEMPT from a plain click and
  // report only the former; a per-click toast would fire on every row selection.
  describe("blocked drag attempts", () => {
    const SORT_BLOCK = {
      reason: "sort",
      message: "Reordering is off while sorted by Title",
      actionLabel: "Clear sort",
    } as const;

    it("reports the block once the pointer passes the drag threshold", () => {
      const rows = [makeRow(makeNib({ id: "nibs-001" }))];
      const { onRowPointerDown, onblockeddrag } = setup({ rows, dragBlock: SORT_BLOCK });

      onRowPointerDown("nibs-001", new PointerEvent("pointerdown", {
        clientX: 100, clientY: 100, bubbles: true,
      }));
      window.dispatchEvent(new PointerEvent("pointermove", {
        clientX: 106, clientY: 100, bubbles: true,
      }));

      expect(onblockeddrag).toHaveBeenCalledWith(SORT_BLOCK);
      cleanup?.();
    });

    it("does not start a drag when blocked", () => {
      const rows = [makeRow(makeNib({ id: "nibs-001" }))];
      const { onRowPointerDown, drag } = setup({ rows, dragBlock: SORT_BLOCK });

      onRowPointerDown("nibs-001", new PointerEvent("pointerdown", {
        clientX: 100, clientY: 100, bubbles: true,
      }));
      window.dispatchEvent(new PointerEvent("pointermove", {
        clientX: 106, clientY: 100, bubbles: true,
      }));

      expect(drag.isDragging).toBe(false);
      expect(document.querySelector("[data-testid='drag-preview']")).toBeNull();
      cleanup?.();
    });

    it("stays silent on a plain click that never crosses the threshold", () => {
      const rows = [makeRow(makeNib({ id: "nibs-001" }))];
      const { onRowPointerDown, onblockeddrag } = setup({ rows, dragBlock: SORT_BLOCK });

      onRowPointerDown("nibs-001", new PointerEvent("pointerdown", {
        clientX: 100, clientY: 100, bubbles: true,
      }));
      window.dispatchEvent(new PointerEvent("pointermove", {
        clientX: 102, clientY: 101, bubbles: true,
      }));
      window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

      expect(onblockeddrag).not.toHaveBeenCalled();
    });

    it("reports once per gesture, not once per pointermove", () => {
      const rows = [makeRow(makeNib({ id: "nibs-001" }))];
      const { onRowPointerDown, onblockeddrag } = setup({ rows, dragBlock: SORT_BLOCK });

      onRowPointerDown("nibs-001", new PointerEvent("pointerdown", {
        clientX: 100, clientY: 100, bubbles: true,
      }));
      for (const x of [106, 120, 140, 180]) {
        window.dispatchEvent(new PointerEvent("pointermove", {
          clientX: x, clientY: 100, bubbles: true,
        }));
      }

      expect(onblockeddrag).toHaveBeenCalledTimes(1);
      cleanup?.();
    });

    it("stays silent and drags normally when nothing blocks", () => {
      const rows = [makeRow(makeNib({ id: "nibs-001" }))];
      const { onRowPointerDown, drag, onblockeddrag } = setup({ rows, dragBlock: null });

      onRowPointerDown("nibs-001", new PointerEvent("pointerdown", {
        clientX: 100, clientY: 100, bubbles: true,
      }));
      window.dispatchEvent(new PointerEvent("pointermove", {
        clientX: 106, clientY: 100, bubbles: true,
      }));

      expect(onblockeddrag).not.toHaveBeenCalled();
      expect(drag.isDragging).toBe(true);
      cleanup?.();
    });
  });

  // What the affordance reads while the drag is in flight. The plan already
  // decides the drop; these are the two facts it hands the drag state so the
  // badge and the row indicator describe THAT plan rather than re-reading the
  // gesture.
  describe("the accepted plan reaches the drag state", () => {
    const EPIC = makeNib({ id: "E1", type: "epic", title: "User Authentication" });

    /** E1 with two children — a sibling reorder inside a titled container. */
    function siblingRows(): RowData[] {
      return [
        makeRow(EPIC, { hasChildren: true }),
        makeRow(makeNib({ id: "T1", parentId: "E1", title: "One" }), { depth: 1, parentNib: EPIC }),
        makeRow(makeNib({ id: "T2", parentId: "E1", title: "Two" }), { depth: 1, parentNib: EPIC }),
      ];
    }

    it("carries the plan's sentence and region, with the container spelled as its title", () => {
      const composable = setup({ rows: siblingRows() });

      startDragOn("T2", composable);
      const unhover = hoverRow("T1", 0.125);

      expect(composable.drag.dropValid).toBe(true);
      // The id would read "the children of E1"; the namer is what makes the
      // badge worth showing.
      expect(composable.drag.dropLabel).toBe("Reorder in the children of User Authentication");
      expect(composable.drag.dropAccepted).toEqual({
        kind: "position",
        label: "Reorder in the children of User Authentication",
        region: { axis: "parent", parentId: "E1" },
      });

      unhover();
      cleanup?.();
    });

    it("names a container the lens gave no row to, off a row's parentNib", () => {
      // The promoted-header population: P's real parent H is not rendered, so
      // the only place its title exists is P's own `parentNib` — the same route
      // `destContainerType` takes to answer H's type.
      const hidden = makeNib({ id: "H", type: "epic", title: "Hidden epic" });
      const rows = [
        makeRow(makeNib({ id: "P", type: "feature", parentId: "H", title: "Promoted" }), { parentNib: hidden }),
        makeRow(makeNib({ id: "L", type: "task", title: "Loose" })),
      ];
      const composable = setup({ rows });

      startDragOn("L", composable);
      const unhover = hoverRow("P", 0.125);

      expect(composable.drag.dropValid).toBe(true);
      expect(composable.drag.dropLabel).toBe("Move into the children of Hidden epic");

      unhover();
      cleanup?.();
    });

    it("carries neither for a refused drop, though the target is still marked", () => {
      const composable = setup({ rows: siblingRows() });

      startDragOn("T2", composable);
      // Released back on the row it grabbed: a plan, refused (`drop-on-self`).
      const unhover = hoverRow("T2", 0.125);

      expect(composable.drag.dropTargetId).toBe("T2");
      expect(composable.drag.dropValid).toBe(false);
      expect(composable.drag.dropLabel).toBeNull();
      expect(composable.drag.dropAccepted).toBeNull();

      unhover();
      cleanup?.();
    });

    it("clears both when the cursor leaves every row", () => {
      const composable = setup({ rows: siblingRows() });

      startDragOn("T2", composable);
      const unhover = hoverRow("T1", 0.125);
      expect(composable.drag.dropLabel).not.toBeNull();
      unhover();

      document.elementFromPoint = () => null;
      window.dispatchEvent(new PointerEvent("pointermove", { clientX: 200, clientY: 500, bubbles: true }));

      expect(composable.drag.dropLabel).toBeNull();
      expect(composable.drag.dropAccepted).toBeNull();
      cleanup?.();
    });
  });
});
