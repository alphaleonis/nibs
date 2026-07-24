import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { useColumnDrag, moveColumn } from "./useColumnDrag.svelte";
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
  const order: ColumnKey[] = ["id", "title", "state"];

  it("moves a column BEFORE the target", () => {
    expect(moveColumn(order, "state", "id", "before")).toEqual(["state", "id", "title"]);
  });

  it("moves a column AFTER the target", () => {
    expect(moveColumn(order, "id", "state", "after")).toEqual(["title", "state", "id"]);
  });

  it("moves a column BEFORE a middle target", () => {
    expect(moveColumn(order, "id", "state", "before")).toEqual(["title", "id", "state"]);
  });

  it("is a no-op (fresh copy) when dragged === target", () => {
    const out = moveColumn(order, "title", "title", "before");
    expect(out).toEqual(order);
    expect(out).not.toBe(order);
  });

  it("returns the order unchanged when a key is missing", () => {
    expect(moveColumn(["id", "title"], "state", "id", "before")).toEqual(["id", "title"]);
    expect(moveColumn(["id", "title"], "id", "state", "after")).toEqual(["id", "title"]);
  });
});

describe("useColumnDrag", () => {
  // Fake timers give control over suppressClick()'s setTimeout(0) auto-clear:
  // the flag that swallows a completed drag's synthetic click is cleared on the
  // next event-loop task, and tests advance timers to observe that lifetime.
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    document.body.style.cursor = "";
    document.elementFromPoint = () => null;
    // Flush any lingering gesture listeners from a test that didn't pointer-up.
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
    vi.useRealTimers();
  });

  function setup(order: ColumnKey[] = ["id", "title", "state"]) {
    const onReorder = vi.fn();
    const drag = useColumnDrag({ getOrder: () => order, onReorder });
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
    const { drag, onReorder } = setup(["id", "title", "state"]);
    const th = makeTh("state", { left: 0, right: 100, width: 100 });
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
    expect(drag.targetKey).toBe("state");
    expect(drag.targetSide).toBe("after");

    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    expect(onReorder).toHaveBeenCalledTimes(1);
    expect(onReorder).toHaveBeenCalledWith(["title", "state", "id"]);
    // Drag state resets after the gesture.
    expect(drag.isDragging).toBe(false);
    expect(drag.draggedKey).toBeNull();
  });

  it("computes the BEFORE side when the cursor is over the left half of the target", () => {
    const { drag, onReorder } = setup(["id", "title", "state"]);
    const th = makeTh("id", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => th;

    // Drag "state" toward "id"; cursor x=20 (left half) → "before".
    startDrag(drag, "state", { x: 20, y: 10 });
    expect(drag.targetKey).toBe("id");
    expect(drag.targetSide).toBe("before");

    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
    expect(onReorder).toHaveBeenCalledWith(["state", "id", "title"]);
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
    const { drag } = setup(["id", "title", "state"]);
    const th = makeTh("state", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => th;

    startDrag(drag, "id", { x: 80, y: 10 });
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    // The click generated by the drag is swallowed once...
    expect(drag.consumeClickSuppression()).toBe(true);
    // ...and only once — a later genuine sort-click is not suppressed.
    expect(drag.consumeClickSuppression()).toBe(false);
  });

  it("Escape aborting a real drag reorders nothing but suppresses the trailing click", () => {
    const { drag, onReorder } = setup(["id", "title", "state"]);
    const th = makeTh("state", { left: 0, right: 100, width: 100 });
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
    const { drag, onReorder } = setup(["id", "title", "state"]);
    const th = makeTh("state", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => th;

    // A real cross-header reorder: drag "id" past threshold and drop over "state".
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

  it("pointercancel mid-drag aborts, restores cursor, and suppresses the trailing click", () => {
    const { drag, onReorder } = setup(["id", "title", "state"]);
    const th = makeTh("state", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => th;

    startDrag(drag, "id", { x: 80, y: 10 });
    expect(drag.isDragging).toBe(true);
    expect(document.body.style.cursor).toBe("grabbing");

    window.dispatchEvent(new PointerEvent("pointercancel", { bubbles: true }));

    // Gesture state is fully reset and the grab cursor is restored (no stuck grab).
    expect(drag.isDragging).toBe(false);
    expect(drag.draggedKey).toBeNull();
    expect(drag.targetKey).toBeNull();
    expect(document.body.style.cursor).toBe("");
    expect(onReorder).not.toHaveBeenCalled();
    // A real drag was underway, so its trailing click is swallowed, then cleared.
    expect(drag.consumeClickSuppression()).toBe(true);
    vi.runAllTimers();
    expect(drag.consumeClickSuppression()).toBe(false);
  });

  it("dragging a header onto itself (no distinct target) does not reorder", () => {
    const { drag, onReorder } = setup(["id", "title", "state"]);
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

  it("ignores a non-primary (right) button pointerdown", () => {
    const { drag, onReorder } = setup();
    const th = makeTh("state", { left: 0, right: 100, width: 100 });
    document.elementFromPoint = () => th;

    drag.onHeaderPointerDown("id", new PointerEvent("pointerdown", { clientX: 100, clientY: 10, button: 2, bubbles: true }));
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 180, clientY: 10, bubbles: true }));
    window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    expect(drag.isDragging).toBe(false);
    expect(onReorder).not.toHaveBeenCalled();
  });
});
