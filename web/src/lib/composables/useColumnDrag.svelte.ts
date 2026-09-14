import { COLUMNS } from "../columns";
import type { ColumnKey, SortKey } from "../columns";

// Pointer travel before a header press becomes a reorder drag instead of a sort
// click. A completed drag suppresses the click.
const COLUMN_DRAG_THRESHOLD = 5;

// Ghost offset from the pointer.
export const GHOST_OFFSET_X = 12;
export const GHOST_OFFSET_Y = 8;

export type ColumnDropSide = "before" | "after";

/** Cursor-following clone of the dragged header; `x`/`y` track the pointer. */
export interface ColumnDragGhost {
  readonly label: string;
  /** null for a non-sortable column. */
  readonly sortKey: SortKey | null;
  /** The dragged header's width (px), captured at drag start. */
  readonly width: number;
  readonly x: number;
  readonly y: number;
}

// The full column order with `dragged` moved beside `target`. Hidden columns keep
// their relative positions. A no-op or unknown key returns an unchanged copy.
export function moveColumn(
  order: readonly ColumnKey[],
  dragged: ColumnKey,
  target: ColumnKey,
  side: ColumnDropSide,
): ColumnKey[] {
  if (dragged === target) return [...order];
  if (!order.includes(dragged) || !order.includes(target)) return [...order];
  const without = order.filter((k) => k !== dragged);
  const targetIdx = without.indexOf(target);
  const insertIdx = side === "before" ? targetIdx : targetIdx + 1;
  return [...without.slice(0, insertIdx), dragged, ...without.slice(insertIdx)];
}

export interface ColumnDrag {
  /** The header currently being dragged (null when idle). */
  readonly draggedKey: ColumnKey | null;
  /** The header under the cursor that the dragged column will drop next to. */
  readonly targetKey: ColumnKey | null;
  readonly targetSide: ColumnDropSide | null;
  /** True once past the movement threshold. */
  readonly isDragging: boolean;
  /** The ghost while a past-threshold drag is in flight, else null. */
  readonly ghost: ColumnDragGhost | null;
  /** Whole-header pointerdown that MAY become a reorder-drag. */
  onHeaderPointerDown: (key: ColumnKey, e: PointerEvent) => void;
  /** Call first in the header's click handler. True once after a completed
   *  drag: skip the sort. */
  consumeClickSuppression: () => boolean;
}

export function useColumnDrag(opts: {
  // The current resolved column order (full key list) to reorder.
  getOrder: () => ColumnKey[];
  // Persist the new order for the current view.
  onReorder: (next: ColumnKey[]) => void;
}): ColumnDrag {
  let draggedKey: ColumnKey | null = $state(null);
  let targetKey: ColumnKey | null = $state(null);
  let targetSide: ColumnDropSide | null = $state(null);
  let dragging = $state(false);

  // Captured at drag start, except pointerX/Y, which track the pointer.
  let ghostWidth = $state(0);
  let ghostLabel = $state("");
  let ghostSortKey: SortKey | null = $state(null);
  let pointerX = $state(0);
  let pointerY = $state(0);

  // Pending (pre-threshold) gesture state.
  let pending = false;
  let startX = 0;
  let startY = 0;
  let pendingKey: ColumnKey | null = null;

  // Only the pointer that started the gesture may move, end or commit it.
  // `captureEl` holds that pointer's capture so cleanup() can release it.
  let activePointerId: number | null = null;
  let captureEl: HTMLElement | null = null;

  let suppressNextClick = false;

  // The click trailing a same-header drop dispatches synchronously, so it
  // consumes the flag first. A cross-header drop's click lands on an ancestor
  // with no sort handler; the timeout clears the flag so it cannot swallow a
  // later click.
  function suppressClick() {
    suppressNextClick = true;
    setTimeout(() => {
      suppressNextClick = false;
    }, 0);
  }

  function captureGhost(key: ColumnKey) {
    const th = document.querySelector(`th[data-col-key="${key}"]`) as HTMLElement | null;
    ghostWidth = th ? th.getBoundingClientRect().width : 0;
    const def = COLUMNS[key];
    ghostLabel = def.label;
    // TableHeader's own gate, so a non-sortable column shows no arrow.
    ghostSortKey = def.sortable ? def.sortKey : null;
  }

  function headerAt(x: number, y: number): { key: ColumnKey; rect: DOMRect } | null {
    const el = document.elementFromPoint(x, y);
    const th = el?.closest("th[data-col-key]") as HTMLElement | null;
    if (!th) return null;
    const key = th.dataset.colKey as ColumnKey | undefined;
    if (!key) return null;
    return { key, rect: th.getBoundingClientRect() };
  }

  function onPointerMove(e: PointerEvent) {
    if (e.pointerId !== activePointerId) return;
    if (pending && !dragging) {
      const dx = e.clientX - startX;
      const dy = e.clientY - startY;
      if (Math.sqrt(dx * dx + dy * dy) < COLUMN_DRAG_THRESHOLD) return;
      pending = false;
      dragging = true;
      draggedKey = pendingKey;
      if (pendingKey) captureGhost(pendingKey);
    }

    if (!dragging) return;

    pointerX = e.clientX;
    pointerY = e.clientY;

    const hit = headerAt(e.clientX, e.clientY);
    if (!hit || hit.key === draggedKey) {
      targetKey = null;
      targetSide = null;
    } else {
      // Before/after decided by which half of the target header the cursor is over.
      const mid = hit.rect.left + hit.rect.width / 2;
      targetKey = hit.key;
      targetSide = e.clientX < mid ? "before" : "after";
    }

    // A body attribute rather than `body.style.cursor`, so app.css's `!important`
    // rules can beat the element-level `cursor: grab` on headers and rows.
    document.body.dataset.colDrag = targetKey != null ? "grabbing" : "no-drop";
  }

  function onPointerUp(e: PointerEvent) {
    if (e.pointerId !== activePointerId) return;
    if (dragging) {
      if (draggedKey && targetKey && targetSide) {
        const current = opts.getOrder();
        const next = moveColumn(current, draggedKey, targetKey, targetSide);
        if (changed(current, next)) {
          opts.onReorder(next);
        }
      }
      // Swallow the trailing click even when nothing was reordered.
      suppressClick();
    }
    cleanup();
  }

  function onKeyDown(e: KeyboardEvent) {
    if (e.key === "Escape" && dragging) {
      e.preventDefault();
      e.stopPropagation();
      // The eventual release still produces a click on the header; swallow it.
      suppressClick();
      cleanup();
    }
  }

  function onPointerCancel(e: PointerEvent) {
    if (e.pointerId !== activePointerId) return;
    if (dragging) {
      suppressClick();
    }
    cleanup();
  }

  function onLostPointerCapture(e: PointerEvent) {
    // Capture lost without a pointerup: tear down so no later release commits a
    // reorder.
    if (e.pointerId !== activePointerId) return;
    cleanup();
  }

  function changed(a: ColumnKey[], b: ColumnKey[]): boolean {
    if (a.length !== b.length) return true;
    return a.some((k, i) => k !== b[i]);
  }

  function cleanup() {
    window.removeEventListener("pointermove", onPointerMove);
    window.removeEventListener("pointerup", onPointerUp);
    window.removeEventListener("pointercancel", onPointerCancel);
    window.removeEventListener("keydown", onKeyDown);
    window.removeEventListener("lostpointercapture", onLostPointerCapture);
    if (captureEl && activePointerId !== null) {
      try {
        captureEl.releasePointerCapture(activePointerId);
      } catch {
        // Already released / invalid pointer id.
      }
    }
    captureEl = null;
    activePointerId = null;
    delete document.body.dataset.colDrag;
    pending = false;
    pendingKey = null;
    dragging = false;
    draggedKey = null;
    targetKey = null;
    targetSide = null;
    ghostWidth = 0;
    ghostLabel = "";
    ghostSortKey = null;
    pointerX = 0;
    pointerY = 0;
  }

  function onHeaderPointerDown(key: ColumnKey, e: PointerEvent) {
    if (e.button !== 0) return;
    // Clear leftover suppression so a plain click here still sorts.
    suppressNextClick = false;
    pending = true;
    startX = e.clientX;
    startY = e.clientY;
    pendingKey = key;
    activePointerId = e.pointerId;
    // Capture so this pointer's events keep arriving when it leaves the window.
    // Captured events still bubble to the window listeners below.
    const el = e.currentTarget as HTMLElement | null;
    if (el) {
      try {
        el.setPointerCapture(e.pointerId);
        captureEl = el;
      } catch {
        // Pointer no longer active; the window listeners alone remain.
        captureEl = null;
      }
    }
    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", onPointerUp);
    window.addEventListener("pointercancel", onPointerCancel);
    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("lostpointercapture", onLostPointerCapture);
  }

  function consumeClickSuppression(): boolean {
    if (!suppressNextClick) return false;
    suppressNextClick = false;
    return true;
  }

  // Unmounting mid-drag (e.g. App's `{#key position}` remount) would otherwise
  // leak the window listeners, cursor and ghost. An `$effect` cleanup rather than
  // `onDestroy`, so it also runs under `$effect.root` in tests.
  $effect(() => () => cleanup());

  const ghost = $derived<ColumnDragGhost | null>(
    !dragging || draggedKey == null
      ? null
      : { label: ghostLabel, sortKey: ghostSortKey, width: ghostWidth, x: pointerX, y: pointerY },
  );

  return {
    get draggedKey() { return draggedKey; },
    get targetKey() { return targetKey; },
    get targetSide() { return targetSide; },
    get isDragging() { return dragging; },
    get ghost() { return ghost; },
    onHeaderPointerDown,
    consumeClickSuppression,
  };
}
