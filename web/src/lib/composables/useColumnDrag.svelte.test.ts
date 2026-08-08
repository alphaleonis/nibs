import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { flushSync } from "svelte";
import { useColumnDrag, moveColumn } from "./useColumnDrag.svelte";
import type { ColumnDrag } from "./useColumnDrag.svelte";
import type { ColumnKey } from "../columns";

// jsdom doesn't implement elementFromPoint — stub it (tests override per-case).
if (typeof document.elementFromPoint !== "function") {
  document.elementFromPoint = () => null;
}

function makeTh(key: ColumnKey, rect: Partial<DOMRect> = {}): HTMLElement {
  const th = document.createElement("th");
  th.dataset.colKey = key;
  th.getBoundingClientRect = () =>
    ({
      top: 0, bottom: 40, left: 0, right: 100, width: 100, height: 40, x: 0, y: 0,
      toJSON: () => {},
      ...rect,
    }) as DOMRect;
  return th;
}

describe("moveColumn", () => {
  const order: ColumnKey[] = ["id", "title", "status"];

  it("moves a column BEFORE the target", () => {
    expect(moveColumn(order, "status", "id", "before")).toEqual(["status", "id", "title"]);
  });

  it("moves a column AFTER the target", () => {
    expect(moveColumn(order, "id", "status", "after")).toEqual(["title", "status", "id"]);
  });

  it("moves a column BEFORE a middle target", () => {
    expect(moveColumn(order, "id", "status", "before")).toEqual(["title", "id", "status"]);
  });

  it("is a no-op (fresh copy) when dragged === target", () => {
    const out = moveColumn(order, "title", "title", "before");
    expect(out).toEqual(order);
    expect(out).not.toBe(order);
  });

  it("returns the order unchanged when a key is missing", () => {
    expect(moveColumn(["id", "title"], "status", "id", "before")).toEqual(["id", "title"]);
    expect(moveColumn(["id", "title"], "id", "status", "after")).toEqual(["id", "title"]);
  });
});

describe("useColumnDrag", () => {
  // Fake timers give control over suppressClick()'s setTimeout(0) auto-clear:
  // the flag that swallows a completed drag's synthetic click is cleared on the
  // next event-loop task, and tests advance timers to observe that lifetime.
  beforeEach(() => {
    vi.useFakeTimers();
  });

  // Effect roots created by setup(). The composable registers its teardown via an
  // `$effect`, so it must be instantiated inside a root; disposing the roots here
  // fires that teardown so no gesture listeners leak across tests.
  const roots: Array<() => void> = [];

  afterEach(() => {
    while (roots.length) roots.pop()!();
    document.body.style.cursor = "";
    delete document.body.dataset.colDrag;
    document.elementFromPoint = () => null;
    // Remove any <th> appended so captureGhost could read a real rect.
    document.querySelectorAll("th[data-col-key]").forEach((el) => el.remove());
    // Flush any lingering gesture listeners from a test that didn't pointer-up.
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
    vi.useRealTimers();
  });

  function setup(order: ColumnKey[] = ["id", "title", "status"]) {
    const onReorder = vi.fn();
    let drag!: ColumnDrag;
    const dispose = $effect.root(() => {
      drag = useColumnDrag({ getOrder: () => order, onReorder });
    });
    // Flush so the teardown $effect registers its cleanup (fires on dispose()).
    flushSync();
    roots.push(dispose);
    return { drag, onReorder };
  }

  /** Dispatch pointerdown (via the composable) then a window pointermove. */
  function startDrag(
    drag: { onHeaderPointerDown: (k: ColumnKey, e: PointerEvent) => void },
    key: ColumnKey,
    move: { x: number; y: number },
  ) {
    drag.onHeaderPointerDown(key, new PointerEvent("pointerdown", { clientX: 100, clientY: 10, button: 0, bubbles: true }));
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: move.x, clientY: move.y, bubbles: true }));
  }

  it("crossing the threshold over another header reorders and writes the new order on pointer up", () => {
    const { drag, onReorder } = setup(["id", "title", "status"]);
    const th = makeTh("status", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => th;

    // Below threshold: still just pending, not dragging.
    drag.onHeaderPointerDown("id", new PointerEvent("pointerdown", { clientX: 100, clientY: 10, button: 0, bubbles: true }));
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 102, clientY: 10, bubbles: true }));
    expect(drag.isDragging).toBe(false);

    // Past threshold, cursor at x=80 (right half of the 0..100 target) → "after".
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 180, clientY: 10, bubbles: true }));
    // headerAt uses the mocked th; force the target resolution with a move over it.
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 80, clientY: 10, bubbles: true }));
    expect(drag.isDragging).toBe(true);
    expect(drag.draggedKey).toBe("id");
    expect(drag.targetKey).toBe("status");
    expect(drag.targetSide).toBe("after");

    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    expect(onReorder).toHaveBeenCalledTimes(1);
    expect(onReorder).toHaveBeenCalledWith(["title", "status", "id"]);
    // Drag state resets after the gesture.
    expect(drag.isDragging).toBe(false);
    expect(drag.draggedKey).toBeNull();
  });

  it("computes the BEFORE side when the cursor is over the left half of the target", () => {
    const { drag, onReorder } = setup(["id", "title", "status"]);
    const th = makeTh("id", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => th;

    // Drag "status" toward "id"; cursor x=20 (left half) → "before".
    startDrag(drag, "status", { x: 20, y: 10 });
    expect(drag.targetKey).toBe("id");
    expect(drag.targetSide).toBe("before");

    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
    expect(onReorder).toHaveBeenCalledWith(["status", "id", "title"]);
  });

  it("a below-threshold gesture does NOT reorder and does NOT suppress the sort click", () => {
    const { drag, onReorder } = setup();
    drag.onHeaderPointerDown("id", new PointerEvent("pointerdown", { clientX: 100, clientY: 10, button: 0, bubbles: true }));
    // Tiny move (2px) stays under the 5px threshold.
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 102, clientY: 10, bubbles: true }));
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    expect(drag.isDragging).toBe(false);
    expect(onReorder).not.toHaveBeenCalled();
    // The click that follows a plain tap must still reach the sort handler.
    expect(drag.consumeClickSuppression()).toBe(false);
  });

  it("a completed reorder-drag suppresses the next click exactly once (sort-click coordination)", () => {
    const { drag } = setup(["id", "title", "status"]);
    const th = makeTh("status", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => th;

    startDrag(drag, "id", { x: 80, y: 10 });
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    // The click generated by the drag is swallowed once...
    expect(drag.consumeClickSuppression()).toBe(true);
    // ...and only once — a later genuine sort-click is not suppressed.
    expect(drag.consumeClickSuppression()).toBe(false);
  });

  it("Escape aborting a real drag reorders nothing but suppresses the trailing click", () => {
    const { drag, onReorder } = setup(["id", "title", "status"]);
    const th = makeTh("status", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => th;

    startDrag(drag, "id", { x: 80, y: 10 });
    expect(drag.isDragging).toBe(true);

    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));

    expect(drag.isDragging).toBe(false);
    // Releasing after cancel does not reorder.
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
    expect(onReorder).not.toHaveBeenCalled();

    // A canceled past-threshold gesture still produces a trailing synthetic click
    // over the origin header's sort button; it must be swallowed so the abort
    // doesn't toggle sort.
    expect(drag.consumeClickSuppression()).toBe(true);
    // ...and it doesn't linger: after the auto-clear task a later sort proceeds.
    vi.runAllTimers();
    expect(drag.consumeClickSuppression()).toBe(false);
  });

  it("does not swallow a LATER keyboard sort after a cross-header reorder", () => {
    const { drag, onReorder } = setup(["id", "title", "status"]);
    const th = makeTh("status", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => th;

    // A real cross-header reorder: drag "id" past threshold and drop over "status".
    startDrag(drag, "id", { x: 80, y: 10 });
    expect(drag.isDragging).toBe(true);
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
    expect(onReorder).toHaveBeenCalledTimes(1);

    // The browser fires the post-drag synthetic click on the common ANCESTOR
    // (<tr>/<thead>), which has no sort handler — so consumeClickSuppression() is
    // never called for it and the flag would linger without the setTimeout(0)
    // auto-clear. Advancing timers fires that auto-clear.
    vi.runAllTimers();

    // A LATER keyboard-activated sort (a click with no preceding pointerdown
    // reset) must therefore proceed rather than be silently swallowed.
    expect(drag.consumeClickSuppression()).toBe(false);
  });

  it("pointercancel mid-drag aborts, clears the drag cursor, and suppresses the trailing click", () => {
    const { drag, onReorder } = setup(["id", "title", "status"]);
    const th = makeTh("status", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => th;

    startDrag(drag, "id", { x: 80, y: 10 });
    expect(drag.isDragging).toBe(true);
    expect(document.body.dataset.colDrag).toBe("grabbing");

    window.dispatchEvent(new PointerEvent("pointercancel", { bubbles: true }));

    // Gesture state is fully reset and the drag cursor is cleared (no stuck cursor).
    expect(drag.isDragging).toBe(false);
    expect(drag.draggedKey).toBeNull();
    expect(drag.targetKey).toBeNull();
    expect(document.body.dataset.colDrag).toBeUndefined();
    expect(onReorder).not.toHaveBeenCalled();
    // A real drag was underway, so its trailing click is swallowed, then cleared.
    expect(drag.consumeClickSuppression()).toBe(true);
    vi.runAllTimers();
    expect(drag.consumeClickSuppression()).toBe(false);
  });

  it("dragging a header onto itself (no distinct target) does not reorder", () => {
    const { drag, onReorder } = setup(["id", "title", "status"]);
    const th = makeTh("id", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => th;

    startDrag(drag, "id", { x: 50, y: 10 });
    expect(drag.isDragging).toBe(true);
    expect(drag.targetKey).toBeNull();

    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
    expect(onReorder).not.toHaveBeenCalled();
    // The past-threshold gesture is still a drag, so the ensuing click is swallowed
    // rather than toggling the sort.
    expect(drag.consumeClickSuppression()).toBe(true);
  });

  it("drives the cursor from drop validity: grabbing over a header, no-drop over a non-header, reset on drop", () => {
    const { drag } = setup(["id", "title", "status"]);
    const th = makeTh("status", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => th;

    // Past threshold, over the mocked header → droppable → grabbing.
    startDrag(drag, "id", { x: 80, y: 10 });
    expect(drag.targetKey).toBe("status");
    expect(document.body.dataset.colDrag).toBe("grabbing");

    // Now over a non-header (elementFromPoint miss → target null) → no-drop.
    document.elementFromPoint = () => null;
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 80, clientY: 10, bubbles: true }));
    expect(drag.targetKey).toBeNull();
    expect(document.body.dataset.colDrag).toBe("no-drop");

    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
    expect(document.body.dataset.colDrag).toBeUndefined();
  });

  it("a below-threshold press does NOT override the cursor", () => {
    const { drag } = setup();
    delete document.body.dataset.colDrag;
    drag.onHeaderPointerDown("id", new PointerEvent("pointerdown", { clientX: 100, clientY: 10, button: 0, bubbles: true }));
    // 2px move stays under the 5px threshold — no drag, no cursor override.
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 102, clientY: 10, bubbles: true }));
    expect(drag.isDragging).toBe(false);
    expect(document.body.dataset.colDrag).toBeUndefined();
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
  });

  it("exposes ghost state (label + width + live pointer) during a real drag, cleared on drop", () => {
    const { drag } = setup(["id", "title", "status"]);
    // captureGhost reads the dragged column's live <th> width from the document.
    const idTh = makeTh("id", { width: 123 });
    document.body.appendChild(idTh);
    const stateTh = makeTh("status", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => stateTh;

    expect(drag.ghost).toBeNull();

    startDrag(drag, "id", { x: 80, y: 10 });
    expect(drag.ghost).not.toBeNull();
    expect(drag.ghost?.label).toBe("ID");
    expect(drag.ghost?.sortKey).toBe("id");
    expect(drag.ghost?.width).toBe(123);
    expect(drag.ghost?.x).toBe(80);
    expect(drag.ghost?.y).toBe(10);

    // Live tracking: a subsequent pointermove updates the ghost x/y.
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 55, clientY: 22, bubbles: true }));
    expect(drag.ghost?.x).toBe(55);
    expect(drag.ghost?.y).toBe(22);

    // Dropped → ghost gone.
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
    expect(drag.ghost).toBeNull();
  });

  it("clears the ghost on Escape (cancel)", () => {
    const { drag } = setup(["id", "title", "status"]);
    document.body.appendChild(makeTh("id", { width: 100 }));
    const stateTh = makeTh("status", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => stateTh;

    startDrag(drag, "id", { x: 80, y: 10 });
    expect(drag.ghost).not.toBeNull();

    window.dispatchEvent(new KeyboardEvent("keydown", { key: "Escape", bubbles: true, cancelable: true }));
    expect(drag.ghost).toBeNull();

    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
  });

  it("clears the ghost on pointercancel (abort)", () => {
    const { drag } = setup(["id", "title", "status"]);
    document.body.appendChild(makeTh("id", { width: 100 }));
    const stateTh = makeTh("status", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => stateTh;

    startDrag(drag, "id", { x: 80, y: 10 });
    expect(drag.ghost).not.toBeNull();

    window.dispatchEvent(new PointerEvent("pointercancel", { bubbles: true }));
    expect(drag.ghost).toBeNull();
  });

  it("tears down mid-drag when the host unmounts: clears the drag cursor, ghost, and window listeners", () => {
    const onReorder = vi.fn();
    const order: ColumnKey[] = ["id", "title", "status"];
    let drag!: ColumnDrag;
    // Own root (not via setup) so this test controls the unmount and afterEach
    // doesn't double-dispose.
    const dispose = $effect.root(() => {
      drag = useColumnDrag({ getOrder: () => order, onReorder });
    });
    flushSync();

    document.body.appendChild(makeTh("id", { width: 100 }));
    const stateTh = makeTh("status", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => stateTh;

    startDrag(drag, "id", { x: 80, y: 10 });
    expect(drag.isDragging).toBe(true);
    expect(drag.ghost).not.toBeNull();
    expect(document.body.dataset.colDrag).toBe("grabbing");

    const removeSpy = vi.spyOn(window, "removeEventListener");
    // Host unmounts mid-drag → the composable's $effect cleanup runs cleanup().
    dispose();

    // Ghost and global cursor cleared even though no drop/Escape/pointercancel
    // fired. The ghost is asserted through `isDragging`/`draggedKey` rather than
    // by reading `drag.ghost`: `ghost` is a `$derived`, and reading a derived
    // whose owning effect has been destroyed returns its last cached value
    // instead of recomputing (svelte's `execute_derived` short-circuits on a
    // DESTROYED parent and warns `derived_inert`). Since `ghost` is null exactly
    // when `!dragging || draggedKey == null`, these two assertions are equivalent
    // to it — and unlike the direct read, they observe the post-teardown state.
    expect(drag.isDragging).toBe(false);
    expect(drag.draggedKey).toBeNull();
    expect(document.body.dataset.colDrag).toBeUndefined();
    // All four gesture listeners removed — no stale window listeners survive to
    // replay a stale reorder on a later unrelated release.
    const removed = removeSpy.mock.calls.map((c) => c[0]);
    expect(removed).toEqual(
      expect.arrayContaining(["pointermove", "pointerup", "pointercancel", "keydown"]),
    );
    removeSpy.mockRestore();
  });

  it("tears down on lostpointercapture when no matching pointerup arrives: no stuck ghost/cursor/listeners and no phantom reorder", () => {
    const { drag, onReorder } = setup(["id", "title", "status"]);
    document.body.appendChild(makeTh("id", { width: 100 }));
    const stateTh = makeTh("status", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => stateTh;

    // Real drag armed over a distinct target (a reorder is pending on release).
    startDrag(drag, "id", { x: 80, y: 10 });
    expect(drag.isDragging).toBe(true);
    expect(drag.ghost).not.toBeNull();
    expect(drag.targetKey).toBe("status");
    expect(document.body.dataset.colDrag).toBe("grabbing");

    const removeSpy = vi.spyOn(window, "removeEventListener");
    // The pointer leaves the window / focus is lost: no matching pointerup ever
    // fires, but the browser releases capture. The gesture must tear down cleanly.
    window.dispatchEvent(new PointerEvent("lostpointercapture", { bubbles: true }));

    // No stuck state: ghost, cursor, and drag flags are all reset.
    expect(drag.isDragging).toBe(false);
    expect(drag.ghost).toBeNull();
    expect(drag.draggedKey).toBeNull();
    expect(drag.targetKey).toBeNull();
    expect(document.body.dataset.colDrag).toBeUndefined();

    // Every gesture listener — including the new lostpointercapture one — is gone,
    // so nothing survives to replay a stale reorder on a later release.
    const removed = removeSpy.mock.calls.map((c) => c[0]);
    expect(removed).toEqual(
      expect.arrayContaining([
        "pointermove", "pointerup", "pointercancel", "keydown", "lostpointercapture",
      ]),
    );
    removeSpy.mockRestore();

    // A subsequent stray release must NOT commit a phantom reorder: the pointerup
    // listener is gone, so onReorder is never called.
    window.dispatchEvent(new PointerEvent("pointerup", { clientX: 80, clientY: 10, bubbles: true }));
    expect(onReorder).not.toHaveBeenCalled();
  });

  it("ignores a foreign pointerId: a non-active pointer cannot move, end, or commit the drag", () => {
    const { drag, onReorder } = setup(["id", "title", "status"]);
    const stateTh = makeTh("status", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => stateTh;

    // Active drag started by the default pointer (pointerId 0), armed over "status".
    startDrag(drag, "id", { x: 80, y: 10 });
    expect(drag.isDragging).toBe(true);
    expect(drag.targetKey).toBe("status");
    expect(drag.targetSide).toBe("after");

    // A FOREIGN pointer (pointerId 999) moves over the origin header — must be
    // ignored, leaving the target untouched (a processed move here would null it,
    // since the hit key equals the dragged key).
    const idTh = makeTh("id", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => idTh;
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 20, clientY: 10, pointerId: 999, bubbles: true }));
    expect(drag.targetKey).toBe("status");
    expect(drag.targetSide).toBe("after");

    // A FOREIGN pointerup must NOT end or commit the drag.
    window.dispatchEvent(new PointerEvent("pointerup", { pointerId: 999, bubbles: true }));
    expect(drag.isDragging).toBe(true);
    expect(onReorder).not.toHaveBeenCalled();

    // The ACTIVE pointer's release ends the drag and commits the armed reorder.
    window.dispatchEvent(new PointerEvent("pointerup", { pointerId: 0, bubbles: true }));
    expect(drag.isDragging).toBe(false);
    expect(onReorder).toHaveBeenCalledTimes(1);
    expect(onReorder).toHaveBeenCalledWith(["title", "status", "id"]);
  });

  it("ignores a non-primary (right) button pointerdown", () => {
    const { drag, onReorder } = setup();
    const th = makeTh("status", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => th;

    drag.onHeaderPointerDown("id", new PointerEvent("pointerdown", { clientX: 100, clientY: 10, button: 2, bubbles: true }));
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 180, clientY: 10, bubbles: true }));
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    expect(drag.isDragging).toBe(false);
    expect(onReorder).not.toHaveBeenCalled();
  });
});
