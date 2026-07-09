import type { SelectionState } from "../selection.svelte";
import type { DragState, DropZone } from "../drag.svelte";
import type { RowData } from "../tableData";
import type { TreeTableNib } from "../types";
import { computeDropZone, isValidDropTarget, isValidCrossParentDrop, collectDescendantIds } from "../dropZone";
import { canHaveChildren } from "../typeHierarchy";

const DRAG_THRESHOLD = 5;
const AUTO_SCROLL_EDGE = 50;
const AUTO_SCROLL_SPEED = 8;

export function useTreeDrag(opts: {
  selection: SelectionState;
  drag: DragState;
  getRows: () => RowData[];
  getScrollContainer: () => HTMLElement | null;
  ondrop?: (targetNibId: string, zone: DropZone, targetParentId: string | null) => void;
}): {
  onRowPointerDown: (nibId: string, e: PointerEvent) => void;
  onDragKeyDown: (e: KeyboardEvent) => void;
} {
  const { selection, drag } = opts;

  // Cached state for current drag
  let dragDescendantIds: Set<string> = new Set();
  let draggedTypes: string[] = [];
  let draggedParentId: string | null | undefined = undefined;
  let autoScrollRAF: number | null = null;

  // Pending drag state (before threshold)
  let dragPending = false;
  let dragStartX = 0;
  let dragStartY = 0;
  let dragStartNibId: string | null = null;

  // Drag preview (ghost row following cursor)
  let dragPreviewEl: HTMLElement | null = null;
  let dragOffsetX = 0;
  let dragOffsetY = 0;

  function nibMapFromRows(): Map<string, TreeTableNib> {
    const map = new Map<string, TreeTableNib>();
    for (const row of opts.getRows()) {
      map.set(row.nib.id, row.nib);
    }
    return map;
  }

  function createDragPreview(nibId: string) {
    const scrollContainer = opts.getScrollContainer();
    if (!scrollContainer) return;

    const tr = scrollContainer.querySelector(`tr[data-nib-id="${nibId}"]`) as HTMLElement | null;
    if (!tr) return;

    const table = tr.closest("table");
    if (!table) return;

    // Capture grab offset so the preview stays anchored to cursor position
    const rowRect = tr.getBoundingClientRect();
    dragOffsetX = dragStartX - rowRect.left;
    dragOffsetY = dragStartY - rowRect.top;

    // Create fixed-position container
    const preview = document.createElement("div");
    preview.dataset.testid = "drag-preview";
    preview.style.cssText =
      "position:fixed;pointer-events:none;opacity:0.6;border-radius:4px;overflow:hidden;" +
      "box-shadow:0 4px 12px rgba(0,0,0,0.15);z-index:9999;";

    // Create table with matching layout so column widths are preserved
    const previewTable = document.createElement("table");
    previewTable.style.cssText =
      `table-layout:fixed;width:${table.offsetWidth}px;border-collapse:collapse;`;

    // Copy actual column widths from the header
    const colgroup = document.createElement("colgroup");
    for (const th of table.querySelectorAll("thead th")) {
      const col = document.createElement("col");
      col.style.width = `${(th as HTMLElement).offsetWidth}px`;
      colgroup.appendChild(col);
    }
    previewTable.appendChild(colgroup);

    // Clone the row (before Svelte applies the .dragged class)
    const tbody = document.createElement("tbody");
    const clone = tr.cloneNode(true) as HTMLElement;
    clone.classList.remove("dragged", "any-dragging");
    clone.style.opacity = "";
    clone.style.backgroundColor = "var(--background)";
    tbody.appendChild(clone);
    previewTable.appendChild(tbody);

    preview.appendChild(previewTable);
    document.body.appendChild(preview);
    dragPreviewEl = preview;
  }

  function updateDragPreview(x: number, y: number) {
    if (!dragPreviewEl) return;
    dragPreviewEl.style.left = `${x - dragOffsetX}px`;
    dragPreviewEl.style.top = `${y - dragOffsetY}px`;
  }

  function removeDragPreview() {
    if (dragPreviewEl) {
      dragPreviewEl.remove();
      dragPreviewEl = null;
    }
  }

  function startDrag(nibId: string) {
    const ids = selection.selectedIds.has(nibId) && selection.selectedIds.size > 1
      ? [...selection.selectedIds]
      : [nibId];

    // Create preview before starting drag (so the row isn't dimmed yet in the clone)
    createDragPreview(nibId);
    updateDragPreview(dragStartX, dragStartY);

    const rows = opts.getRows();
    dragDescendantIds = collectDescendantIds(ids, rows);

    const nibMap = nibMapFromRows();
    draggedTypes = ids.map(id => nibMap.get(id)?.type ?? "").filter(Boolean);

    // Resolve the source's parent through the same display-parent authority as
    // the target (RowData.displayParentId), not the raw nib.parentId. This keeps
    // the shared-parent check in handleDrop lens-agnostic and symmetric.
    const parents = new Set(ids.map(id => rows.find(r => r.nib.id === id)?.displayParentId));
    draggedParentId = parents.size === 1 ? [...parents][0] : undefined;

    drag.startDrag(ids, draggedParentId);
    document.body.style.cursor = "grabbing";
  }

  function onDragPointerMove(e: PointerEvent) {
    if (dragPending && !drag.isDragging) {
      const dx = e.clientX - dragStartX;
      const dy = e.clientY - dragStartY;
      if (Math.sqrt(dx * dx + dy * dy) < DRAG_THRESHOLD) return;
      dragPending = false;
      if (dragStartNibId) {
        startDrag(dragStartNibId);
      }
    }

    if (!drag.isDragging) return;

    drag.cursorX = e.clientX;
    drag.cursorY = e.clientY;
    updateDragPreview(e.clientX, e.clientY);

    const el = document.elementFromPoint(e.clientX, e.clientY);
    const tr = el?.closest("tr[data-nib-id]") as HTMLElement | null;
    if (!tr) {
      drag.clearDropTarget();
      stopAutoScroll();
      handleAutoScroll(e);
      return;
    }

    const targetNibId = tr.dataset.nibId!;
    const rows = opts.getRows();
    const targetRow = rows.find(r => r.nib.id === targetNibId);
    if (!targetRow) {
      drag.clearDropTarget();
      handleAutoScroll(e);
      return;
    }

    const rect = tr.getBoundingClientRect();
    let zone = computeDropZone(e.clientY, rect);

    // Dropping "after" a non-leaf nib → reparent (become first child).
    // This matches standard tree-view UX: dropping below a parent node
    // makes the item a child rather than a sibling.
    if (zone === "after" && canHaveChildren(targetRow.nib.type)) {
      zone = "reparent";
    }

    let valid = isValidDropTarget(
      draggedTypes,
      targetRow.nib,
      zone,
      drag.draggedIds,
      dragDescendantIds,
    );

    // For before/after on a different parent, validate the type hierarchy.
    // Compare and look up the parent through displayParentId so move-validation
    // and the drop agree. displayParentId is guaranteed by the producer
    // (tableData's flatten) to be a real nib id or null — never a synthetic
    // bucket id (see the RowData.displayParentId invariant) — so no isBucketId
    // guard is needed here.
    if (valid && (zone === "before" || zone === "after")) {
      if (draggedParentId !== undefined && targetRow.displayParentId !== draggedParentId) {
        const nibMap = nibMapFromRows();
        const dp = targetRow.displayParentId;
        const targetParent = dp ? nibMap.get(dp) ?? null : null;
        valid = isValidCrossParentDrop(draggedTypes, targetParent?.type ?? null);
      }
    }

    drag.setDropTarget(targetNibId, zone, valid);
    handleAutoScroll(e);
  }

  function onGlobalKeyDown(e: KeyboardEvent) {
    if (e.key === "Escape" && drag.isDragging) {
      e.preventDefault();
      e.stopPropagation();
      cleanupDrag();
    }
  }

  function cleanupDrag() {
    window.removeEventListener("pointermove", onDragPointerMove);
    window.removeEventListener("pointerup", onDragPointerUp);
    window.removeEventListener("keydown", onGlobalKeyDown);
    document.body.style.cursor = "";
    removeDragPreview();
    drag.endDrag();
    dragDescendantIds = new Set();
    draggedTypes = [];
    draggedParentId = undefined;
    dragPending = false;
    dragStartNibId = null;
    stopAutoScroll();
  }

  function onDragPointerUp(_e: PointerEvent) {
    if (dragPending) {
      cleanupDrag();
      return;
    }

    if (!drag.isDragging) {
      cleanupDrag();
      return;
    }

    if (drag.dropTargetId && drag.dropZone && drag.dropValid && opts.ondrop) {
      const rows = opts.getRows();
      const targetRow = rows.find(r => r.nib.id === drag.dropTargetId);
      // Resolve the target's DISPLAY parent (the container it sits under in the
      // current view tree), which tableData derives from the node's display
      // position rather than its raw nib.parentId. Both the drag source
      // (draggedParentId) and the target are resolved through displayParentId, so
      // a reorder never adopts a hidden container as the new parent: a promoted
      // header resolves to root, siblings under the same hidden container keep
      // that shared container, and an item under a *different* hidden container
      // resolves to its own display parent.
      const targetParentId = targetRow?.displayParentId ?? null;
      opts.ondrop(drag.dropTargetId, drag.dropZone, targetParentId);
    }

    cleanupDrag();
  }

  function onDragKeyDown(e: KeyboardEvent) {
    if (!drag.isDragging) return;
    if (e.key === "Escape") {
      e.preventDefault();
      e.stopPropagation();
      cleanupDrag();
    }
  }

  function handleAutoScroll(e: PointerEvent) {
    const scrollContainer = opts.getScrollContainer();
    if (!scrollContainer || !drag.isDragging) {
      stopAutoScroll();
      return;
    }

    const rect = scrollContainer.getBoundingClientRect();
    const distFromTop = e.clientY - rect.top;
    const distFromBottom = rect.bottom - e.clientY;

    if (distFromTop < AUTO_SCROLL_EDGE) {
      startAutoScroll(-AUTO_SCROLL_SPEED);
    } else if (distFromBottom < AUTO_SCROLL_EDGE) {
      startAutoScroll(AUTO_SCROLL_SPEED);
    } else {
      stopAutoScroll();
    }
  }

  function startAutoScroll(delta: number) {
    stopAutoScroll();
    function tick() {
      const scrollContainer = opts.getScrollContainer();
      if (!scrollContainer || !drag.isDragging) {
        autoScrollRAF = null;
        return;
      }
      scrollContainer.scrollTop += delta;
      autoScrollRAF = requestAnimationFrame(tick);
    }
    autoScrollRAF = requestAnimationFrame(tick);
  }

  function stopAutoScroll() {
    if (autoScrollRAF !== null) {
      cancelAnimationFrame(autoScrollRAF);
      autoScrollRAF = null;
    }
  }

  function onRowPointerDown(nibId: string, e: PointerEvent) {
    e.preventDefault();

    dragPending = true;
    dragStartX = e.clientX;
    dragStartY = e.clientY;
    dragStartNibId = nibId;

    window.addEventListener("pointermove", onDragPointerMove);
    window.addEventListener("pointerup", onDragPointerUp);
    window.addEventListener("keydown", onGlobalKeyDown);
  }

  return {
    onRowPointerDown,
    onDragKeyDown,
  };
}
