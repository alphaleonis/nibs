import { describe, it, expect } from "vitest";

// What the shared setup owes every other suite: portaled content is queryable,
// WITHOUT painting over the one state bits-ui uses that styling to report.
describe("the floating-wrapper harness", () => {
  // bits-ui asks `getClientRects().length === 0` to decide whether a floating
  // layer's anchor is gone (`isReferenceHidden`). jsdom answers empty for every
  // element, which made that question unanswerable here — every wrapper read as
  // anchorless. Answering for a connected element leaves `isConnected` as the
  // only thing the question turns on.
  it("reports a rect for a connected element and none for a detached one", () => {
    const el = document.createElement("div");
    expect(el.getClientRects()).toHaveLength(0);

    document.body.appendChild(el);
    expect(el.getClientRects().length).toBeGreaterThan(0);

    el.remove();
    expect(el.getClientRects()).toHaveLength(0);
  });

  // A wrapper bits-ui hid is reporting that its anchor is gone. Forcing it
  // visible would make that state unobservable in the one harness that could
  // catch it, so any guard written against it would pass vacuously.
  it("leaves a hidden floating wrapper hidden", async () => {
    const wrapper = document.createElement("div");
    wrapper.setAttribute("data-bits-floating-content-wrapper", "");
    wrapper.style.position = "absolute";
    wrapper.style.visibility = "hidden";
    document.body.appendChild(wrapper);

    // MutationObserver callbacks are delivered as microtasks.
    await Promise.resolve();

    expect(wrapper.style.visibility).toBe("hidden");
    wrapper.remove();
  });
});
