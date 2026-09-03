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
 * The accepted plan as the affordance reads it, or null for a refusal.
 *
 * An exhaustive switch on the plan's kind, so an arm added to `DropPlan` cannot
 * reach the badge and the drop indicator as one of the arms already there.
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
  /**
   * The drop the gesture ended on — the same plan the indicator was drawn from,
   * REFUSALS INCLUDED. A refused drop is reported rather than swallowed, so the
   * caller can say why nothing moved.
   */
  ondrop?: (plan: DropPlan) => void;
  /** A drag was attempted on a blocked row — raise the explanation to the user. */
  onblockeddrag?: (block: DragBlock) => void;
}): {
  onRowPointerDown: (nibId: string, e: PointerEvent) => void;
  onDragKeyDown: (e: KeyboardEvent) => void;
} {
  const { selection, drag } = opts;

  // The row list stays LIVE for the whole gesture: the table refetches on
  // `nibChanged` events, and an ArrowRight expand rebuilds it with nothing gating
  // on a drag being in flight. A row missing from the lookup resolves to NO
  // target, which clears the plan and ends the release in silence — so this is a
  // cache keyed on the list's IDENTITY, not a snapshot. `getRows` reaches here as
  // a Svelte `$derived`, which hands back the same array until the table is
  // rebuilt, so an unchanged list costs one comparison per pointermove.
  let cachedRows: RowData[] | null = null;
  let rowsById: Map<string, RowData> = new Map();
  // Titles for the ids a plan's prose names. Rebuilt beside `rowsById` and for
  // the same reason: `planDrop` runs on every pointermove, and a scan of the
  // rows per call is a cost this gesture does not pay. It also carries
  // containers the lens gave NO row to, which reach here as some row's
  // `parentNib` — where a promoted header's destination lives, and the route
  // `destContainerType` already takes for that container's type.
  let titlesById: Map<string, string> = new Map();
  // Both fixed at startDrag, because both describe what the gesture PICKED UP,
  // which no later render changes: the subtree it carries, and the rows
  // themselves. Resolving the dragged rows against the live list instead would
  // make one that scrolls out of view mid-gesture read as a selection the filter
  // hides, and answer `hidden-member` for the rest of the drag.
  let dragDescendantIds: Set<string> = new Set();
  let draggedRowsById: Map<string, RowData> = new Map();
  // The plan behind the indicator currently on screen, or null when the cursor is
  // over no row. The drop executes THIS value rather than deciding a second time.
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
    // `--z-drag-ghost` is the layer both cursor-following ghosts share (the
    // column-drag one in TableHeader is the other). Below `--z-modal`, which is
    // where the drag badge sits — the badge names the destination this ghost is
    // being dropped on, and a full-width row would otherwise cover it.
    preview.style.cssText =
      "position:fixed;pointer-events:none;opacity:0.6;border-radius:4px;overflow:hidden;" +
      "box-shadow:0 4px 12px rgba(0,0,0,0.15);z-index:var(--z-drag-ghost);";

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
    // The band classes go too: they mark a seam between the row and the one
    // ABOVE it in the table, and the ghost has no row above it. Left on, a
    // grabbed boundary row trails a stray rule at its top edge — cyan on the
    // queue axis, which is the badge's own color and would read as an axis
    // signal the ghost is not carrying.
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

  /**
   * The current row list, with `rowsById` rebuilt only when the list itself was
   * replaced. Keyed by nib id, which the `RowData` row-list invariant states a
   * real nib holds at most once across the rendered rows.
   */
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
      // Crossing the threshold is what separates a drag ATTEMPT from a click, so
      // it is the only moment a blocked row should explain itself — reporting on
      // pointerdown would fire on every row selection. Cleanup first: it detaches
      // the pointer listeners, so the report happens once per gesture.
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

    // `planDrop` owns the whole decision — the container-entry promotion the
    // "after" zone can take, every refusal, and the command each accepted drop
    // writes. Nothing may be added here: a check at this seam would be a second
    // reading of the drag, separate from the one the drop goes on to execute.
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
    // A refusal forwards the raw zone: `.drop-invalid` is the only class an
    // invalid target takes, so the zone is unread there. It carries no accepted
    // drop either, which is what empties the badge — the affordance for a target
    // nothing can happen on. On release App.svelte's handleDrop raises the
    // refusal's message, except for `drop-on-self`, which is the cancel gesture
    // and stays silent.
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

    // Reported whether or not the plan is an accepted one: a gesture that reached
    // a row and cannot happen is exactly the case that needs an explanation, and
    // gating on validity here is what made it silent.
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
    // A blocked row keeps its native pointer defaults: this gesture can only ever
    // explain the block, never move a row, so it must not alter how the row
    // selects or opens.
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
