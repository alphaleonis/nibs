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

  it("treats any element in an ignore array as inside (portal case)", () => {
    const onOutside = vi.fn();
    const { node } = mountNode();
    // Two body-level siblings of `node`, as a portaled panel and its trigger
    // would be: neither is a DOM descendant of `node`.
    const portal = document.createElement("div");
    const portalChild = document.createElement("span");
    portal.appendChild(portalChild);
    const trigger = document.createElement("button");
    document.body.append(portal, trigger);
    cleanups.push(() => portal.remove());
    cleanups.push(() => trigger.remove());

    const handle = clickOutside(node, {
      enabled: true,
      onOutside,
      ignore: [portal, trigger],
    });
    cleanups.push(() => handle?.destroy?.());

    // Pointerdown on either ignored element (or its subtree) does not fire.
    pointerdown(portal);
    pointerdown(portalChild);
    pointerdown(trigger);
    expect(onOutside).not.toHaveBeenCalled();

    // A pointerdown truly outside (not node, not any ignore) still fires.
    const outside = document.createElement("button");
    document.body.appendChild(outside);
    cleanups.push(() => outside.remove());
    pointerdown(outside);
    expect(onOutside).toHaveBeenCalledTimes(1);
  });

  it("treats a target matched by an ignore predicate as inside", () => {
    const onOutside = vi.fn();
    const { node } = mountNode();
    const inPortal = document.createElement("div");
    inPortal.setAttribute("data-test-portal", "");
    const inPortalChild = document.createElement("span");
    inPortal.appendChild(inPortalChild);
    const outside = document.createElement("button");
    document.body.append(inPortal, outside);
    cleanups.push(() => inPortal.remove());
    cleanups.push(() => outside.remove());

    const handle = clickOutside(node, {
      enabled: true,
      onOutside,
      ignore: (target) =>
        target instanceof Element &&
        target.closest("[data-test-portal]") !== null,
    });
    cleanups.push(() => handle?.destroy?.());

    // Predicate returns true for the portal subtree → no fire.
    pointerdown(inPortal);
    pointerdown(inPortalChild);
    expect(onOutside).not.toHaveBeenCalled();

    // Predicate returns false for an unrelated element → fire.
    pointerdown(outside);
    expect(onOutside).toHaveBeenCalledTimes(1);
  });

  it("treats an empty ignore array as no extra inside targets", () => {
    const onOutside = vi.fn();
    const { node } = mountNode();
    const outside = document.createElement("button");
    document.body.appendChild(outside);
    cleanups.push(() => outside.remove());

    const handle = clickOutside(node, { enabled: true, onOutside, ignore: [] });
    cleanups.push(() => handle?.destroy?.());

    pointerdown(outside);
    expect(onOutside).toHaveBeenCalledTimes(1);
  });

  it("tolerates null entries in an ignore array without throwing", () => {
    const onOutside = vi.fn();
    const { node } = mountNode();
    const ignore = document.createElement("button");
    document.body.appendChild(ignore);
    cleanups.push(() => ignore.remove());

    const handle = clickOutside(node, {
      enabled: true,
      onOutside,
      // A consumer may hold refs (bind:this) that haven't mounted yet.
      ignore: [null as unknown as HTMLElement, ignore],
    });
    cleanups.push(() => handle?.destroy?.());

    pointerdown(ignore);
    expect(onOutside).not.toHaveBeenCalled();
  });

  it("does not let a throwing ignore predicate break dismissal", () => {
    const onOutside = vi.fn();
    const { node } = mountNode();
    const outside = document.createElement("button");
    document.body.appendChild(outside);
    cleanups.push(() => outside.remove());

    const handle = clickOutside(node, {
      enabled: true,
      onOutside,
      ignore: () => {
        throw new Error("boom");
      },
    });
    cleanups.push(() => handle?.destroy?.());

    // The throw is swallowed (errored check → "not inside"), so an outside
    // pointerdown still dismisses instead of the error escaping the handler.
    pointerdown(outside);
    expect(onOutside).toHaveBeenCalledTimes(1);
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
