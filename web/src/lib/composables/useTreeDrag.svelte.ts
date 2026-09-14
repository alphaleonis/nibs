import type { ContainmentIndex } from "../containment";
import type { SelectionState } from "../selection.svelte";
import type { AcceptedDrop, DragState, DropZone } from "../drag.svelte";
import type { RowData } from "../tableData";
import { computeDropZone, collectDescendantIds } from "../dropZone";
import { planDrop, type DropIndicator, type DropPlan } from "../ordering/dropPlan";
import type { DragBlock } from "../dragBlock";

const DRAG_THRESHOLD = 5;
const AUTO_SCROLL_EDGE = 50;
const AUTO_SCROLL_SPEED = 8;

/** A plan's indicator in the vocabulary `TreeTableRow`'s drop classes are keyed on. */
function dropZoneOf(indicator: DropIndicator): DropZone {
  return indicator === "into" ? "reparent" : indicator;
}

/**
 * The accepted plan as the badge and indicator read it, or null for a refusal.
 * No default arm: handle every `DropPlan` kind here.
 */
function acceptedDropOf(plan: DropPlan): AcceptedDrop | null {
  if (!plan.ok) return null;
  switch (plan.kind) {
    case "position":
      return { kind: "position", label: plan.label, region: plan.region };
    case "assign":
      return { kind: "assign", label: plan.label };
  }
}

export function useTreeDrag(opts: {
  selection: SelectionState;
  drag: DragState;
  getRows: () => RowData[];
  getScrollContainer: () => HTMLElement | null;
  /** What the current view draws inside what, for the destination check
   *  `planDrop` cannot answer from the rows alone. */
  getContainment: () => ContainmentIndex;
  /** The gate currently suppressing drag-reorder, or null when drag is available. */
  getDragBlock?: () => DragBlock | null;
  /** The plan the gesture ended on, refusals included, so the caller can say why
   *  nothing moved. */
  ondrop?: (plan: DropPlan) => void;
  /** A drag was attempted on a blocked row — raise the explanation to the user. */
  onblockeddrag?: (block: DragBlock) => void;
}): {
  onRowPointerDown: (nibId: string, e: PointerEvent) => void;
  onDragKeyDown: (e: KeyboardEvent) => void;
} {
  const { selection, drag } = opts;

  // The rows change during a drag (refetches, expands), so this is a cache keyed
  // on the list's identity, not a snapshot. `getRows` returns a `$derived`, so an
  // unchanged list costs one comparison per pointermove.
  let cachedRows: RowData[] | null = null;
  let rowsById: Map<string, RowData> = new Map();
  // Titles for the ids a plan's prose names, including containers with no row
  // of their own (reached as some row's `parentNib`).
  let titlesById: Map<string, string> = new Map();
  // Fixed at startDrag: what the gesture picked up. Resolving the dragged rows
  // live would read one scrolled out of view as hidden by the filter.
  let dragDescendantIds: Set<string> = new Set();
  let draggedRowsById: Map<string, RowData> = new Map();
  // The plan behind the indicator on screen; the drop executes this value.
  let dropPlan: DropPlan | null = null;
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

  function createDragPreview(nibId: string) {
    const scrollContainer = opts.getScrollContainer();
    if (!scrollContainer) return;

    const tr = scrollContainer.querySelector(`tr[data-nib-id="${CSS.escape(nibId)}"]`) as HTMLElement | null;
    if (!tr) return;

    const table = tr.closest("table");
    if (!table) return;

    // Capture grab offset so the preview stays anchored to cursor position
    const rowRect = tr.getBoundingClientRect();
    dragOffsetX = dragStartX - rowRect.left;
    dragOffsetY = dragStartY - rowRect.top;

    const preview = document.createElement("div");
    preview.dataset.testid = "drag-preview";
    // Below the drag badge (`--z-modal`), which this full-width row would cover.
    preview.style.cssText =
      "position:fixed;pointer-events:none;opacity:0.6;border-radius:4px;overflow:hidden;" +
      "box-shadow:0 4px 12px rgba(0,0,0,0.15);z-index:var(--z-drag-ghost);";

    // Create table with matching layout so column widths are preserved
    const previewTable = document.createElement("table");
    previewTable.style.cssText =
      `table-layout:fixed;width:${table.offsetWidth}px;border-collapse:collapse;`;

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
    // Band classes mark a seam with the row above, which the ghost does not have.
    clone.classList.remove("dragged", "any-dragging", "region-band", "region-band-queue");
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

  /** The current rows; the lookups rebuild only when the list is replaced. Keyed
   *  by nib id, unique per the `RowData` row-list invariant. */
  function syncRows(): RowData[] {
    const rows = opts.getRows();
    if (rows !== cachedRows) {
      cachedRows = rows;
      rowsById = new Map(rows.map(row => [row.nib.id, row]));
      titlesById = new Map();
      for (const row of rows) {
        titlesById.set(row.nib.id, row.nib.title);
        if (row.parentNib !== null) titlesById.set(row.parentNib.id, row.parentNib.title);
      }
    }
    return rows;
  }

  /** Spells an id for `planDrop`'s prose; an id with no loaded nib keeps itself. */
  function nameOf(id: string): string | undefined {
    return titlesById.get(id);
  }

  function startDrag(nibId: string) {
    const ids = selection.selectedIds.has(nibId) && selection.selectedIds.size > 1
      ? [...selection.selectedIds]
      : [nibId];

    // Create preview before starting drag (so the row isn't dimmed yet in the clone)
    createDragPreview(nibId);
    updateDragPreview(dragStartX, dragStartY);

    const rows = syncRows();
    dragDescendantIds = collectDescendantIds(ids, rows);
    draggedRowsById = new Map(
      ids.flatMap(id => { const row = rowsById.get(id); return row ? [[id, row] as const] : []; }),
    );

    drag.startDrag(ids);
    document.body.style.cursor = "grabbing";
  }

  function onDragPointerMove(e: PointerEvent) {
    if (dragPending && !drag.isDragging) {
      const dx = e.clientX - dragStartX;
      const dy = e.clientY - dragStartY;
      if (Math.sqrt(dx * dx + dy * dy) < DRAG_THRESHOLD) return;
      dragPending = false;
      // Report a block only past the threshold, where a drag is distinct from a
      // click. Cleanup first detaches the listeners, so it reports once.
      const block = opts.getDragBlock?.() ?? null;
      if (block) {
        cleanupDrag();
        opts.onblockeddrag?.(block);
        return;
      }
      if (dragStartNibId) {
        startDrag(dragStartNibId);
      }
    }

    if (!drag.isDragging) return;

    syncRows();
    drag.cursorX = e.clientX;
    drag.cursorY = e.clientY;
    updateDragPreview(e.clientX, e.clientY);

    const el = document.elementFromPoint(e.clientX, e.clientY);
    const tr = el?.closest("tr[data-nib-id]") as HTMLElement | null;
    if (!tr) {
      clearDrop();
      stopAutoScroll();
      handleAutoScroll(e);
      return;
    }

    const targetRow = rowsById.get(tr.dataset.nibId!);
    if (!targetRow) {
      clearDrop();
      handleAutoScroll(e);
      return;
    }

    // `planDrop` owns the whole decision. Add no checks here, or the indicator
    // and the executed drop could disagree.
    const zone = computeDropZone(e.clientY, tr.getBoundingClientRect());
    const plan = planDrop({
      draggedIds: drag.draggedIds,
      rowsById,
      draggedRowsById,
      target: targetRow,
      zone,
      descendantIds: dragDescendantIds,
      containment: opts.getContainment(),
      nameOf,
    });
    dropPlan = plan;
    // A refusal passes the raw zone (only `.drop-invalid` applies) and no accepted
    // drop, which empties the badge.
    drag.setDropTarget(
      targetRow.nib.id,
      plan.ok ? dropZoneOf(plan.indicator) : zone,
      plan.ok,
      acceptedDropOf(plan),
    );
    handleAutoScroll(e);
  }

  function clearDrop() {
    dropPlan = null;
    drag.clearDropTarget();
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
    cachedRows = null;
    rowsById = new Map();
    titlesById = new Map();
    dragDescendantIds = new Set();
    draggedRowsById = new Map();
    dropPlan = null;
    dragPending = false;
    dragStartNibId = null;
    stopAutoScroll();
  }

  function onDragPointerUp(_e: PointerEvent) {
    if (dragPending || !drag.isDragging) {
      cleanupDrag();
      return;
    }

    // Refusals too, so the caller can explain them.
    if (dropPlan !== null) opts.ondrop?.(dropPlan);

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
    // A blocked row can never move, so keep its native defaults for select/open.
    if (!opts.getDragBlock?.()) {
      e.preventDefault();
    }

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
