import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { SelectionState } from "../selection.svelte";
import { DragState } from "../drag.svelte";
import type { RowData } from "../tableData";
import { buildTableData } from "../tableData";
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
    ...opts,
  };
}

describe("useTreeDrag", () => {
  // Track the composable's cleanup so we can call it after each test
  let cleanup: (() => void) | undefined;

  beforeEach(() => {
    cleanup = undefined;
  });

  /** Helper to create drag composable with common defaults */
  function setup(overrides: {
    selection?: SelectionState;
    drag?: DragState;
    rows?: RowData[];
    ondrop?: (targetNibId: string, zone: import("../drag.svelte").DropZone) => void;
    scrollContainer?: HTMLElement | null;
    dragBlock?: import("../dragBlock").DragBlock | null;
    onblockeddrag?: (block: import("../dragBlock").DragBlock) => void;
  } = {}) {
    const selection = overrides.selection ?? new SelectionState();
    const drag = overrides.drag ?? new DragState();
    const ondrop = overrides.ondrop ?? vi.fn();
    const rows = overrides.rows ?? [];
    const onblockeddrag = overrides.onblockeddrag ?? vi.fn();

    const result = useTreeDrag({
      selection,
      drag,
      getRows: () => rows,
      getScrollContainer: () => overrides.scrollContainer ?? null,
      getDragBlock: () => overrides.dragBlock ?? null,
      ondrop,
      onblockeddrag,
    });

    // Provide a way for tests to trigger cleanup via pointerup
    cleanup = () => {
      const upEvent = new PointerEvent("pointerup", { bubbles: true });
      window.dispatchEvent(upEvent);
    };

    return { ...result, selection, drag, ondrop, onblockeddrag };
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

  it("pointer up calls ondrop when valid drop target exists", () => {
    const nib1 = makeNib({ id: "nibs-001", type: "task" });
    const nib2 = makeNib({ id: "nibs-002", type: "milestone" });
    const rows = [
      makeRow(nib2, { hasChildren: true }),
      makeRow(nib1),
    ];

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    // Start drag on nib1
    startDragOn("nibs-001", composable);
    expect(drag.isDragging).toBe(true);

    // Manually set a valid drop target (simulating what pointer move would do)
    drag.setDropTarget("nibs-002", "reparent", true);

    // Pointer up should trigger ondrop
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    expect(ondrop).toHaveBeenCalledWith("nibs-002", "reparent", null);
    expect(drag.isDragging).toBe(false);
  });

  it("cross-container before/after drop onto a loose bucket item stays valid (real onDragPointerMove path)", () => {
    // Regression guard for the #jeu5 producer invariant, driving the REAL
    // onDragPointerMove cross-parent type-validation branch — the resolution
    // tests below deliberately bypass it via setDropTarget(..., true), which is
    // why the original regression went uncaught. Rows come from the real
    // buildTableData pipeline so the producer fix's effect on a loose bucket
    // item's displayParentId is exercised end-to-end.
    //
    // Epics lens: E1 (epic header) → F1 (feature child, displayParentId "E1") is
    // dragged; T1 (loose task) lands in the "No epic" bucket. Dropping F1 BEFORE
    // the loose bucket item T1 is a cross-container reorder to root. Before the
    // producer fix, T1's displayParentId was the synthetic bucket id, so the
    // validation looked up the bucket pseudo-nib (type "") and
    // isValidCrossParentDrop(["feature"], "") rejected the drop (dropValid=false).
    // After the fix T1 resolves to null (root) → the drop is valid.
    const nibs: TreeTableNib[] = [
      makeNib({ id: "E1", type: "epic", parentId: null }),
      makeNib({ id: "F1", type: "feature", parentId: "E1" }),
      makeNib({ id: "T1", type: "task", parentId: null }),
    ];
    const { rows } = buildTableData(nibs, {}, "epics", new Set<string>());

    // The synthetic bucket row is present in the rows fed to the composable, so
    // nibMap includes the pseudo-nib (type "") — reproducing the exact bug
    // surface. The discriminating assertion below is drag.dropValid: with the
    // producer fix reverted, T1's displayParentId is the bucket id and the drop
    // is rejected there.
    expect(rows.some(r => r.nib.id === "/__no_epic__")).toBe(true);

    const drag = new DragState();
    const composable = setup({ drag, rows });

    // Drag F1 (feature under the visible E1 header → draggedParentId "E1").
    startDragOn("F1", composable);
    expect(drag.isDragging).toBe(true);

    // Build a <tr data-nib-id="T1"> and point elementFromPoint at it. Cursor in
    // the top 30% of the row → "before" zone (cross-container reorder, not
    // reparent).
    const tr = document.createElement("tr");
    tr.dataset.nibId = "T1";
    tr.getBoundingClientRect = () => ({
      top: 200, bottom: 240, left: 0, right: 800,
      height: 40, width: 800, x: 0, y: 200,
      toJSON: () => {},
    }) as DOMRect;
    document.body.appendChild(tr);
    const origElementFromPoint = document.elementFromPoint;
    document.elementFromPoint = () => tr;

    // clientY=205 → 5px into a 40px row → ratio 0.125 < 0.3 → "before".
    window.dispatchEvent(new PointerEvent("pointermove", {
      clientX: 200, clientY: 205, bubbles: true,
    }));

    expect(drag.dropTargetId).toBe("T1");
    expect(drag.dropZone).toBe("before");
    expect(drag.dropValid).toBe(true);

    document.elementFromPoint = origElementFromPoint;
    document.body.removeChild(tr);
    cleanup?.();
  });

  it("before/after reorder where dragged & target share a display container but have DIFFERENT real parents is INVALID", () => {
    // In a grouping lens two rows can share the same DISPLAY
    // container (both display at root → displayParentId null) while having
    // DIFFERENT real nib.parentId — e.g. a promoted feature header (real parent
    // a hidden epic) and a loose "No X" bucket task (real parent null). A plain
    // before/after reorder here fires a parent-less reorderNibCmd, but the
    // backend computes siblings from the dragged item's UNCHANGED real parent and
    // rejects ("not a sibling (different parent)"). "Reorder" only means something
    // within a single real parent, so the affordance must read INVALID and the
    // drop must never be offered. This drives the REAL onDragPointerMove validity
    // path (not setDropTarget), which is where the false-valid slips through.
    const dragged = makeNib({ id: "F1", type: "feature", parentId: "hidden-epic" });
    const target = makeNib({ id: "T1", type: "task", parentId: null });
    const rows = [
      // Promoted header: real parent a hidden epic, but display parent is root.
      makeRow(dragged, { displayParentId: null }),
      // Loose "No epic" bucket item: real parent null, display parent root too.
      makeRow(target, { displayParentId: null }),
    ];

    const drag = new DragState();
    const composable = setup({ drag, rows });

    startDragOn("F1", composable);
    expect(drag.isDragging).toBe(true);

    const tr = document.createElement("tr");
    tr.dataset.nibId = "T1";
    tr.getBoundingClientRect = () => ({
      top: 200, bottom: 240, left: 0, right: 800,
      height: 40, width: 800, x: 0, y: 200,
      toJSON: () => {},
    }) as DOMRect;
    document.body.appendChild(tr);
    const origElementFromPoint = document.elementFromPoint;
    document.elementFromPoint = () => tr;

    // clientY=205 → 5px into a 40px row → ratio 0.125 < 0.3 → "before" (reorder).
    window.dispatchEvent(new PointerEvent("pointermove", {
      clientX: 200, clientY: 205, bubbles: true,
    }));

    expect(drag.dropTargetId).toBe("T1");
    expect(drag.dropZone).toBe("before");
    // Equal display parent (null) but different real parent → INVALID.
    expect(drag.dropValid).toBe(false);

    document.elementFromPoint = origElementFromPoint;
    document.body.removeChild(tr);
    cleanup?.();
  });

  it("before/after reorder between genuine real siblings (same display + same real parent) stays VALID (guardrail)", () => {
    // Scope guardrail for the real-parent guard: when the dragged and target rows are
    // real siblings (same real nib.parentId) and share the same display
    // container, a before/after reorder must remain VALID — the new real-parent
    // guard only rejects the equal-display / DIFFERENT-real-parent case.
    const dragged = makeNib({ id: "C1", type: "task", parentId: "epic-A" });
    const target = makeNib({ id: "C2", type: "task", parentId: "epic-A" });
    const rows = [
      makeRow(dragged, { displayParentId: "epic-A" }),
      makeRow(target, { displayParentId: "epic-A" }),
    ];

    const drag = new DragState();
    const composable = setup({ drag, rows });

    startDragOn("C1", composable);
    expect(drag.isDragging).toBe(true);

    const tr = document.createElement("tr");
    tr.dataset.nibId = "C2";
    tr.getBoundingClientRect = () => ({
      top: 200, bottom: 240, left: 0, right: 800,
      height: 40, width: 800, x: 0, y: 200,
      toJSON: () => {},
    }) as DOMRect;
    document.body.appendChild(tr);
    const origElementFromPoint = document.elementFromPoint;
    document.elementFromPoint = () => tr;

    window.dispatchEvent(new PointerEvent("pointermove", {
      clientX: 200, clientY: 205, bubbles: true,
    }));

    expect(drag.dropTargetId).toBe("C2");
    expect(drag.dropZone).toBe("before");
    // Genuine same-real-parent reorder → still VALID.
    expect(drag.dropValid).toBe(true);

    document.elementFromPoint = origElementFromPoint;
    document.body.removeChild(tr);
    cleanup?.();
  });

  it("before/after reorder for a MULTI-SELECT drag spanning MIXED real parents is INVALID (fail-closed)", () => {
    // Completeness gap in the real-parent guard: for a multi-select drag whose
    // selected rows span DIFFERENT real parents, the shared-real-parent Set
    // collapses to size≠1 → draggedRealParentId === undefined. A guard that only
    // rejects when draggedRealParentId is DEFINED-and-different fails OPEN here —
    // the drop reads valid, then handleDrop fires a parent-less reorder the
    // backend rejects ("not a sibling"). With no single PROVABLE shared real
    // parent we cannot establish real-sibling-hood, so a before/after reorder
    // must be INVALID (fail closed). Drives the real onDragPointerMove path.
    const d1 = makeNib({ id: "D1", type: "feature", parentId: "epic-A" });
    const d2 = makeNib({ id: "D2", type: "task", parentId: "epic-B" });
    const target = makeNib({ id: "T1", type: "task", parentId: "epic-C" });
    const rows = [
      // Both dragged rows display at root (equal displayParentId null) but have
      // DIFFERENT real parents → draggedRealParentId becomes undefined.
      makeRow(d1, { displayParentId: null }),
      makeRow(d2, { displayParentId: null }),
      makeRow(target, { displayParentId: null }),
    ];

    // Multi-select: both D1 and D2 selected, drag started on D1.
    const selection = new SelectionState();
    selection.selectedIds = new Set(["D1", "D2"]);

    const drag = new DragState();
    const composable = setup({ selection, drag, rows });

    startDragOn("D1", composable);
    expect(drag.isDragging).toBe(true);
    expect(drag.draggedIds).toEqual(expect.arrayContaining(["D1", "D2"]));

    const tr = document.createElement("tr");
    tr.dataset.nibId = "T1";
    tr.getBoundingClientRect = () => ({
      top: 200, bottom: 240, left: 0, right: 800,
      height: 40, width: 800, x: 0, y: 200,
      toJSON: () => {},
    }) as DOMRect;
    document.body.appendChild(tr);
    const origElementFromPoint = document.elementFromPoint;
    document.elementFromPoint = () => tr;

    // clientY=205 → "before" zone (reorder).
    window.dispatchEvent(new PointerEvent("pointermove", {
      clientX: 200, clientY: 205, bubbles: true,
    }));

    expect(drag.dropTargetId).toBe("T1");
    expect(drag.dropZone).toBe("before");
    // No single provable shared real parent → INVALID.
    expect(drag.dropValid).toBe(false);

    document.elementFromPoint = origElementFromPoint;
    document.body.removeChild(tr);
    cleanup?.();
  });

  it("before/after drop forwards a promoted header's display parent (null), not its hidden real parent", () => {
    // What this verifies at the useTreeDrag layer: onDragPointerUp forwards the
    // TARGET row's displayParentId verbatim — it does NOT read nib.parentId. The
    // target is a promoted header (a feature whose real parent is an epic/
    // milestone hidden by the lens), which tableData emits with
    // displayParentId === null though nib.parentId is non-null. Forwarding null
    // (not the hidden real parent) is what keeps a reorder from silently
    // reparenting under the hidden container.
    // (Forwarding targetRow.nib.parentId instead would send "nibs-hidden-epic"
    // here and fail this test.)
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

    // Before/after (reorder) drop near the promoted header.
    drag.setDropTarget("nibs-header", "before", true);
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    // Display parent is root (null), NOT the hidden epic id.
    expect(ondrop).toHaveBeenCalledWith("nibs-header", "before", null);
  });

  it("before/after drop forwards the target row's non-null displayParentId (same-container reorder keeps the container, not null)", () => {
    // What this verifies at the useTreeDrag layer: when the target row has a
    // non-null display parent, onDragPointerUp forwards THAT value rather than
    // collapsing to null (which would make handleDrop see a cross-parent move and
    // re-root the item). Both rows share the same real display container here — a
    // plain same-container reorder — so the forwarded value is that container.
    //
    // This test does NOT discriminate a source/target swap (both rows carry the
    // same displayParentId, so reading the source would yield the same value):
    // that swap is owned by the sibling "DIFFERENT display container" test below,
    // which gives source and target distinct values. What this test does pin is
    // the "not null" half — forwarding targetRow.nib.parentId (null here) would
    // send null and fail this assertion. The symmetric property (two loose
    // siblings resolving to the SAME container) lives in tableData.flatten and is
    // tested in tableData.test.ts.
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

    // Reorder before E2; the drop must forward E2's non-null display parent.
    drag.setDropTarget("nibs-e2", "before", true);
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    expect(ondrop).toHaveBeenCalledWith("nibs-e2", "before", "nibs-shared-epic");
  });

  it("before/after drop next to an item under a DIFFERENT display container resolves the target's container", () => {
    // The dragged item and the target sit under DIFFERENT display containers
    // (here two distinct buckets/parents). Re-rooting the drop to null whenever
    // the target's real parent isn't a visible row is the trap; resolving
    // through displayParentId instead
    // keeps the target's own container, so handleDrop performs a correct
    // cross-parent move rather than dumping the item at root.
    const dragged = makeNib({ id: "nibs-drag", type: "feature", parentId: null });
    const target = makeNib({ id: "nibs-target", type: "feature", parentId: null });
    const rows = [
      makeRow(dragged, { displayParentId: "/__no_milestone__" }),
      makeRow(target, { displayParentId: "nibs-other-epic" }),
    ];

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    startDragOn("nibs-drag", composable);
    drag.setDropTarget("nibs-target", "before", true);
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    // Resolves to the target's display container (NOT null): a genuine
    // cross-parent move, not a silent re-root.
    expect(ondrop).toHaveBeenCalledWith("nibs-target", "before", "nibs-other-epic");
  });

  it("before/after drop where the target's display parent is a visible parent resolves that parent id", () => {
    // Guards against an always-null over-correction: the target sits under a
    // normally-expanded parent row present in the visible rows, so its display
    // parent id must be preserved for the cross-parent move to work.
    const dragged = makeNib({ id: "nibs-drag", type: "feature", parentId: null });
    const epic = makeNib({ id: "nibs-epic", type: "epic", parentId: null });
    const child = makeNib({ id: "nibs-child", type: "feature", parentId: "nibs-epic" });
    const rows = [
      makeRow(dragged, { displayParentId: null }),
      makeRow(epic, { hasChildren: true, displayParentId: null }),
      makeRow(child, { depth: 1, parentNib: epic, displayParentId: "nibs-epic" }),
    ];

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    startDragOn("nibs-drag", composable);
    drag.setDropTarget("nibs-child", "after", true);
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    // The epic parent IS visible → resolve its id as the display parent.
    expect(ondrop).toHaveBeenCalledWith("nibs-child", "after", "nibs-epic");
  });

  it("reparent-zone drop resolves a promoted header's display parent to null", () => {
    // Reparent zone: the target itself becomes the new parent (handleDrop uses
    // the target id and ignores targetParentId), so dropping onto a promoted
    // header whose display parent is root must still fire the reparent path.
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
    drag.setDropTarget("nibs-epic", "reparent", true);
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    // Reparent still fires; targetParentId is resolved to null (display parent is
    // root) but handleDrop ignores it for the reparent zone.
    expect(ondrop).toHaveBeenCalledWith("nibs-epic", "reparent", null);
  });

  it("Escape during drag cancels without calling ondrop", () => {
    const nib1 = makeNib({ id: "nibs-001" });
    const rows = [makeRow(nib1)];

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    // Start drag
    startDragOn("nibs-001", composable);
    expect(drag.isDragging).toBe(true);

    // Set a valid drop target
    drag.setDropTarget("nibs-002", "reparent", true);

    // Press Escape via onDragKeyDown
    const escEvent = new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true });
    composable.onDragKeyDown(escEvent);

    // Drag should be canceled, ondrop should NOT have been called
    expect(drag.isDragging).toBe(false);
    expect(ondrop).not.toHaveBeenCalled();
    expect(escEvent.defaultPrevented).toBe(true);
  });

  it("global Escape keydown cancels drag (no focus required)", () => {
    const nib1 = makeNib({ id: "nibs-001" });
    const rows = [makeRow(nib1)];

    const drag = new DragState();
    const ondrop = vi.fn();
    const composable = setup({ drag, rows, ondrop });

    startDragOn("nibs-001", composable);
    expect(drag.isDragging).toBe(true);

    drag.setDropTarget("nibs-002", "reparent", true);

    // Dispatch Escape on window (not through the composable's onDragKeyDown)
    const escEvent = new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true });
    window.dispatchEvent(escEvent);

    expect(drag.isDragging).toBe(false);
    expect(ondrop).not.toHaveBeenCalled();
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
});
