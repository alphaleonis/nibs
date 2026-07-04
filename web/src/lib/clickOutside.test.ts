import { describe, it, expect, vi, afterEach } from "vitest";
import { clickOutside } from "./clickOutside";

// Track nodes to clean up so listeners/elements don't leak across tests.
const cleanups: Array<() => void> = [];
afterEach(() => {
  while (cleanups.length) cleanups.pop()!();
});

function pointerdown(el: Element | Document) {
  // Use a real PointerEvent (jsdom supports it) rather than a bare MouseEvent so
  // the dispatched event carries pointer-specific fields, matching what the
  // browser and @testing-library's fireEvent.pointerDown produce.
  el.dispatchEvent(new PointerEvent("pointerdown", { bubbles: true }));
}

function mountNode() {
  const node = document.createElement("div");
  const inside = document.createElement("button");
  node.appendChild(inside);
  document.body.appendChild(node);
  cleanups.push(() => node.remove());
  return { node, inside };
}

describe("clickOutside", () => {
  it("calls onOutside on a pointerdown outside the node", () => {
    const onOutside = vi.fn();
    const { node } = mountNode();
    const outside = document.createElement("button");
    document.body.appendChild(outside);
    cleanups.push(() => outside.remove());

    const handle = clickOutside(node, { enabled: true, onOutside });
    cleanups.push(() => handle?.destroy?.());

    pointerdown(outside);
    expect(onOutside).toHaveBeenCalledTimes(1);
  });

  it("does not call onOutside for a pointerdown inside the node", () => {
    const onOutside = vi.fn();
    const { node, inside } = mountNode();

    const handle = clickOutside(node, { enabled: true, onOutside });
    cleanups.push(() => handle?.destroy?.());

    pointerdown(inside);
    pointerdown(node);
    expect(onOutside).not.toHaveBeenCalled();
  });

  it("does not call onOutside when the pointerdown lands on the ignored element", () => {
    const onOutside = vi.fn();
    const { node } = mountNode();
    const ignore = document.createElement("button");
    const ignoreChild = document.createElement("span");
    ignore.appendChild(ignoreChild);
    document.body.appendChild(ignore);
    cleanups.push(() => ignore.remove());

    const handle = clickOutside(node, { enabled: true, onOutside, ignore });
    cleanups.push(() => handle?.destroy?.());

    pointerdown(ignore);
    pointerdown(ignoreChild);
    expect(onOutside).not.toHaveBeenCalled();
  });

  it("does nothing when enabled is false", () => {
    const onOutside = vi.fn();
    const { node } = mountNode();
    const outside = document.createElement("button");
    document.body.appendChild(outside);
    cleanups.push(() => outside.remove());

    const handle = clickOutside(node, { enabled: false, onOutside });
    cleanups.push(() => handle?.destroy?.());

    pointerdown(outside);
    expect(onOutside).not.toHaveBeenCalled();
  });

  it("reflects enabled updates via update()", () => {
    const onOutside = vi.fn();
    const { node } = mountNode();
    const outside = document.createElement("button");
    document.body.appendChild(outside);
    cleanups.push(() => outside.remove());

    const handle = clickOutside(node, { enabled: false, onOutside });
    cleanups.push(() => handle?.destroy?.());

    pointerdown(outside);
    expect(onOutside).not.toHaveBeenCalled();

    handle?.update?.({ enabled: true, onOutside });
    pointerdown(outside);
    expect(onOutside).toHaveBeenCalledTimes(1);
  });

  it("removes the listener on destroy", () => {
    const onOutside = vi.fn();
    const { node } = mountNode();
    const outside = document.createElement("button");
    document.body.appendChild(outside);
    cleanups.push(() => outside.remove());

    const handle = clickOutside(node, { enabled: true, onOutside });
    handle?.destroy?.();

    pointerdown(outside);
    expect(onOutside).not.toHaveBeenCalled();
  });
});
