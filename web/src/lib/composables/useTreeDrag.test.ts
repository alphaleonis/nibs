import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { SelectionState } from "../selection.svelte";
import { DragState } from "../drag.svelte";
import type { RowData } from "../tableData";
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
    updatedAt: "",
    parentId: null,
    blockingIds: [],
    blockedByIds: [],
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
  } = {}) {
    const selection = overrides.selection ?? new SelectionState();
    const drag = overrides.drag ?? new DragState();
    const ondrop = overrides.ondrop ?? vi.fn();
    const rows = overrides.rows ?? [];

    const result = useTreeDrag({
      selection,
      drag,
      getRows: () => rows,
      getScrollContainer: () => overrides.scrollContainer ?? null,
      ondrop,
    });

    // Provide a way for tests to trigger cleanup via pointerup
    cleanup = () => {
      const upEvent = new PointerEvent("pointerup", { bubbles: true });
      window.dispatchEvent(upEvent);
    };

    return { ...result, selection, drag, ondrop };
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

    // Drag should be cancelled, ondrop should NOT have been called
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
});
