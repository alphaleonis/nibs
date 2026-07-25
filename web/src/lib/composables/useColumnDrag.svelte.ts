import { COLUMNS } from "../columns";
import type { ColumnKey, SortKey } from "../columns";

// Movement threshold before a header pointerdown becomes a reorder-drag instead
// of a sort-click — mirrors useTreeDrag's DRAG_THRESHOLD pattern. Below the
// threshold the gesture is a plain click (the nibs-6grg sort toggle); past it, a
// column reorder. The two never both fire: a completed drag suppresses the click.
const COLUMN_DRAG_THRESHOLD = 5;

// Down-right nudge of the floating ghost from the pointer so the clone sits just
// off the cursor (and its top-left corner never lands under the hit-test point).
// Consumed by TableHeader's inline positioning and the ghost-position tests.
export const GHOST_OFFSET_X = 12;
export const GHOST_OFFSET_Y = 8;

export type ColumnDropSide = "before" | "after";

/**
 * Cursor-following clone of the dragged header (parity with row drag's native
 * HTML5 drag-image). Captured once when a real drag starts; `x`/`y` track the
 * pointer live so TableHeader can float a fixed clone at the cursor.
 */
export interface ColumnDragGhost {
  /** The dragged column's label. */
  readonly label: string;
  /** The dragged column's sort field, for the direction arrow (null if the column is non-sortable). */
  readonly sortKey: SortKey | null;
  /** Captured width (px) of the dragged header, so the clone matches the column. */
  readonly width: number;
  /** Live pointer x (clientX), updated each pointermove. */
  readonly x: number;
  /** Live pointer y (clientY), updated each pointermove. */
  readonly y: number;
}

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
  /**
   * Cursor-following clone of the dragged header while a real (past-threshold)
   * drag is in flight; null when idle. Mirrors row drag's drag-image: the
   * original header stays dimmed in place (`.col-dragging`) AND this floats at
   * the pointer. Cleared whenever the gesture ends — drop, Escape, pointercancel,
   * OR the host component unmounting mid-drag (an `$effect` cleanup runs
   * `cleanup()` on teardown).
   */
  readonly ghost: ColumnDragGhost | null;
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

  // Ghost (cursor-following header clone) state. Width + label + sort field are
  // captured once at drag start; pointerX/Y track the pointer live. The ghost
  // getter assembles these into ColumnDragGhost (or null when idle).
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

  // Pointer identity + capture target for the active gesture. `activePointerId` is
  // the pointer recorded at pointerdown — pointermove/up/cancel/lostpointercapture
  // ignore any other pointer so a foreign/second pointer can't move, end, or commit
  // the drag. `captureEl` is the header that holds pointer capture, retained so
  // cleanup() can release it. Both null when idle.
  let activePointerId: number | null = null;
  let captureEl: HTMLElement | null = null;

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

  // Snapshot the dragged header's geometry + content for the floating clone,
  // read once when a real drag starts (past threshold). Width comes from the live
  // <th> rect so the clone matches the on-screen column; label/arrow come from the
  // column registry keyed by the dragged key.
  function captureGhost(key: ColumnKey) {
    const th = document.querySelector(`th[data-col-key="${key}"]`) as HTMLElement | null;
    ghostWidth = th ? th.getBoundingClientRect().width : 0;
    const def = COLUMNS[key];
    ghostLabel = def.label;
    // Mirror the header's own gate (TableHeader: `sortField = def.sortable ?
    // def.sortKey : null`) so the ghost never shows a sort arrow the real header
    // suppresses for a non-sortable column.
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
    // Only the pointer that started the gesture may advance it — ignore a
    // foreign/second pointer so it can't cross the threshold or retarget the drag.
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

    // Track the live pointer so TableHeader can float the ghost at the cursor.
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

    // The cursor mirrors drop validity: `grabbing` over a droppable header,
    // `no-drop` over the actions column / table body / outside (targetKey null).
    // Driven via a body attribute (not `body.style.cursor`) so global
    // `!important` rules can override the element-level `cursor: grab` on
    // `.col-header`/`.tree-row.draggable`, which would otherwise win over an
    // inherited body cursor exactly over the surfaces a reorder passes over.
    document.body.dataset.colDrag = targetKey != null ? "grabbing" : "no-drop";
  }

  function onPointerUp(e: PointerEvent) {
    // Ignore a foreign/second pointer's release — only the active pointer may end
    // and commit the drag.
    if (e.pointerId !== activePointerId) return;
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

  function onPointerCancel(e: PointerEvent) {
    // Ignore a foreign/second pointer's cancel — only the active pointer's cancel
    // aborts this drag.
    if (e.pointerId !== activePointerId) return;
    // A canceled pointer (e.g. touch interruption mid-drag) is an abort: if a
    // real drag was underway, suppress its trailing click as Escape does, then
    // fully reset gesture state, cursor, and window listeners via cleanup().
    if (dragging) {
      suppressClick();
    }
    cleanup();
  }

  function onLostPointerCapture(e: PointerEvent) {
    // Capture was lost for the active pointer (focus change, forced release, or
    // the pointer leaving the window with no matching pointerup). Tear down
    // cleanly so no frozen ghost, stuck `data-col-drag` cursor, or leaked window
    // listeners linger — and so no later stray release can commit a reorder.
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
    // Release pointer capture if this gesture still holds it. Guarded: the browser
    // may have already released it (the lostpointercapture path), in which case
    // releasePointerCapture throws for an unknown pointerId — nothing to do.
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
    // Only the primary button initiates a reorder-drag.
    if (e.button !== 0) return;
    // A fresh gesture: any leftover suppression from a prior drag is cleared, so a
    // plain click here still sorts.
    suppressNextClick = false;
    pending = true;
    startX = e.clientX;
    startY = e.clientY;
    pendingKey = key;
    activePointerId = e.pointerId;
    // Capture the pointer on the header (the element the pointerdown fired on) so
    // the browser keeps delivering this pointer's move/up/cancel/lostpointercapture
    // even when it leaves the window — closing the lost-pointerup gap that would
    // otherwise strand the drag (frozen ghost, stuck cursor, leaked listeners, and
    // a later stray release committing an unintended reorder). Captured pointer
    // events still bubble to the window listeners registered below.
    const el = e.currentTarget as HTMLElement | null;
    if (el) {
      try {
        el.setPointerCapture(e.pointerId);
        captureEl = el;
      } catch {
        // setPointerCapture can throw if the pointer is no longer active; fall back
        // to the window listeners alone (no capture to release later).
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

  // Teardown safety net: if the host unmounts mid-drag (App's `{#key position}`
  // dock-toggle remount, a view switch, a background refetch swapping the table),
  // the gesture's window listeners, the global drag cursor, and the ghost would
  // otherwise leak — `cleanup()` is only reachable from drop/Escape/pointercancel.
  // An `$effect` cleanup (not `onDestroy`) so the composable also works under
  // `$effect.root` in tests, matching `useTableData.svelte.ts`.
  $effect(() => () => cleanup());

  // Single ghost object per state change (stable identity, one allocation) rather
  // than a fresh literal on every read. Null unless a real drag is in flight.
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
