import type { ColumnKey } from "../columns";

// Movement threshold before a header pointerdown becomes a reorder-drag instead
// of a sort-click — mirrors useTreeDrag's DRAG_THRESHOLD pattern. Below the
// threshold the gesture is a plain click (the nibs-6grg sort toggle); past it, a
// column reorder. The two never both fire: a completed drag suppresses the click.
const COLUMN_DRAG_THRESHOLD = 5;

export type ColumnDropSide = "before" | "after";

// Pure move: produce the next FULL column order after relocating `dragged` to sit
// immediately before/after `target`. The order carries every ColumnKey (visible
// or not); only visible headers are drop targets, so hidden columns keep their
// relative positions. A no-op or a missing key returns a fresh copy unchanged.
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
  /** Which side of the target the drop indicator sits on. */
  readonly targetSide: ColumnDropSide | null;
  /** True once the movement threshold is crossed (a real reorder-drag). */
  readonly isDragging: boolean;
  /** Whole-header pointerdown that MAY become a reorder-drag. */
  onHeaderPointerDown: (key: ColumnKey, e: PointerEvent) => void;
  /**
   * Click-vs-drag disambiguation for the nibs-6grg sort control (the header
   * <th>): its onclick calls this FIRST. Returns (and clears) true exactly once
   * after a completed reorder-drag, telling the sort handler to skip — so a past-
   * threshold gesture reorders without also toggling the sort. A below-threshold
   * gesture never sets it, so a plain click still sorts.
   */
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

  // Pending (pre-threshold) gesture state.
  let pending = false;
  let startX = 0;
  let startY = 0;
  let pendingKey: ColumnKey | null = null;

  // Set true when a real drag completes so the ensuing click on the header sort
  // control is swallowed instead of toggling the sort. Consumed by
  // consumeClickSuppression (the mouse sort handler) and reset at the start of the
  // next gesture.
  let suppressNextClick = false;

  // Raise the suppression flag AND bound its lifetime to the current event-loop
  // task. The synthetic `click` that follows a past-threshold pointerup is
  // dispatched SYNCHRONOUSLY in the same task, so a SAME-header click still
  // consumes the flag via consumeClickSuppression() before this timeout fires.
  // A CROSS-header drop lands its click on a common ancestor (<tr>/<thead>) with
  // no sort handler, so nothing consumes the flag — the setTimeout(0) clears it so
  // a stale flag can't suppress a LATER, unrelated mouse sort-click on a header.
  // Keyboard sort activation (Enter/Space on the <th>) never touches this flag: it
  // runs handleHeaderSortKeydown → onSort directly and dispatches no click, so the
  // flag's only consumer is the mouse handleHeaderSortClick path.
  function suppressClick() {
    suppressNextClick = true;
    setTimeout(() => {
      suppressNextClick = false;
    }, 0);
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
    if (pending && !dragging) {
      const dx = e.clientX - startX;
      const dy = e.clientY - startY;
      if (Math.sqrt(dx * dx + dy * dy) < COLUMN_DRAG_THRESHOLD) return;
      pending = false;
      dragging = true;
      draggedKey = pendingKey;
      document.body.style.cursor = "grabbing";
    }

    if (!dragging) return;

    const hit = headerAt(e.clientX, e.clientY);
    if (!hit || hit.key === draggedKey) {
      targetKey = null;
      targetSide = null;
      return;
    }
    // Before/after decided by which half of the target header the cursor is over.
    const mid = hit.rect.left + hit.rect.width / 2;
    targetKey = hit.key;
    targetSide = e.clientX < mid ? "before" : "after";
  }

  function onPointerUp() {
    if (dragging) {
      if (draggedKey && targetKey && targetSide) {
        const current = opts.getOrder();
        const next = moveColumn(current, draggedKey, targetKey, targetSide);
        if (changed(current, next)) {
          opts.onReorder(next);
        }
      }
      // Any past-threshold gesture is a reorder-drag, not a sort-click — swallow
      // the click it produces so the header's sort-click doesn't also toggle,
      // even when the drag ended without a distinct target (no reorder).
      suppressClick();
    }
    cleanup();
  }

  function onKeyDown(e: KeyboardEvent) {
    if (e.key === "Escape" && dragging) {
      e.preventDefault();
      e.stopPropagation();
      // Canceled — no reorder. But a real (past-threshold) drag was in progress,
      // so a synthetic `click` still follows the eventual pointer release over
      // the origin header; swallow it (auto-cleared next task) so a canceled
      // gesture doesn't toggle sort. A sub-threshold pending gesture never reaches
      // here (guarded by `dragging`), so a plain click still sorts.
      suppressClick();
      cleanup();
    }
  }

  function onPointerCancel() {
    // A canceled pointer (e.g. touch interruption mid-drag) is an abort: if a
    // real drag was underway, suppress its trailing click as Escape does, then
    // fully reset gesture state, cursor, and window listeners via cleanup().
    if (dragging) {
      suppressClick();
    }
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
    document.body.style.cursor = "";
    pending = false;
    pendingKey = null;
    dragging = false;
    draggedKey = null;
    targetKey = null;
    targetSide = null;
  }

  function onHeaderPointerDown(key: ColumnKey, e: PointerEvent) {
    // Only the primary button initiates a reorder-drag.
    if (e.button !== 0) return;
    // A fresh gesture: any leftover suppression from a prior drag is cleared, so a
    // plain click here still sorts.
    suppressNextClick = false;
    pending = true;
    startX = e.clientX;
    startY = e.clientY;
    pendingKey = key;
    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", onPointerUp);
    window.addEventListener("pointercancel", onPointerCancel);
    window.addEventListener("keydown", onKeyDown);
  }

  function consumeClickSuppression(): boolean {
    if (!suppressNextClick) return false;
    suppressNextClick = false;
    return true;
  }

  return {
    get draggedKey() { return draggedKey; },
    get targetKey() { return targetKey; },
    get targetSide() { return targetSide; },
    get isDragging() { return dragging; },
    onHeaderPointerDown,
    consumeClickSuppression,
  };
}
