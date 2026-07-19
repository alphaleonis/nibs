import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { NibChangeTracker } from "./changeTracker.svelte";

describe("NibChangeTracker", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("handleEvent('updated') highlights with auto-clear", () => {
    const tracker = new NibChangeTracker({ highlightDurationMs: 1000 });

    tracker.handleEvent({ type: "updated", nibId: "nibs-abc1" });
    expect(tracker.isHighlighted("nibs-abc1")).toBe(true);

    // Should auto-clear after the configured duration
    vi.advanceTimersByTime(1000);
    expect(tracker.isHighlighted("nibs-abc1")).toBe(false);

    tracker.destroy();
  });

  it("handleEvent('created') highlights same as updated", () => {
    const tracker = new NibChangeTracker({ highlightDurationMs: 1000 });

    tracker.handleEvent({ type: "created", nibId: "nibs-new1" });
    expect(tracker.isHighlighted("nibs-new1")).toBe(true);

    vi.advanceTimersByTime(1000);
    expect(tracker.isHighlighted("nibs-new1")).toBe(false);

    tracker.destroy();
  });

  it("handleEvent('deleted') fades with auto-clear", () => {
    const tracker = new NibChangeTracker({ fadeDurationMs: 500 });

    tracker.handleEvent({ type: "deleted", nibId: "nibs-del1" });
    expect(tracker.isFading("nibs-del1")).toBe(true);
    expect(tracker.isHighlighted("nibs-del1")).toBe(false);

    vi.advanceTimersByTime(500);
    expect(tracker.isFading("nibs-del1")).toBe(false);

    tracker.destroy();
  });

  it("handleEvent('archived') neither fades nor highlights — the row stays put", () => {
    const tracker = new NibChangeTracker({ fadeDurationMs: 500 });

    tracker.handleEvent({ type: "archived", nibId: "nibs-arc1" });
    // An archived nib keeps being returned by the list query, so its row does
    // not leave. Fading it would drop the row to invisible and then pop it back;
    // highlighting it would announce a change the user made deliberately.
    expect(tracker.isFading("nibs-arc1")).toBe(false);
    expect(tracker.isHighlighted("nibs-arc1")).toBe(false);

    vi.advanceTimersByTime(500);
    expect(tracker.isFading("nibs-arc1")).toBe(false);

    tracker.destroy();
  });

  it("handleEvent('unarchived') highlights same as updated — the row re-entered/changed", () => {
    const tracker = new NibChangeTracker({ highlightDurationMs: 1000 });

    // An unarchive moves the nib back to its main path and changes its row, so it
    // reads as an update (highlight), never a removal (fade) — and it must not
    // fall through to the DEV "unhandled event type" warning.
    tracker.handleEvent({ type: "unarchived", nibId: "nibs-unarc1" });
    expect(tracker.isHighlighted("nibs-unarc1")).toBe(true);
    expect(tracker.isFading("nibs-unarc1")).toBe(false);

    vi.advanceTimersByTime(1000);
    expect(tracker.isHighlighted("nibs-unarc1")).toBe(false);

    tracker.destroy();
  });

  it("rapid duplicate events reset the timer", () => {
    const tracker = new NibChangeTracker({ highlightDurationMs: 1000 });

    tracker.handleEvent({ type: "updated", nibId: "nibs-abc1" });
    expect(tracker.isHighlighted("nibs-abc1")).toBe(true);

    // Advance 800ms (not yet expired)
    vi.advanceTimersByTime(800);
    expect(tracker.isHighlighted("nibs-abc1")).toBe(true);

    // Fire another update for the same nib — should reset the timer
    tracker.handleEvent({ type: "updated", nibId: "nibs-abc1" });
    expect(tracker.isHighlighted("nibs-abc1")).toBe(true);

    // Advance 800ms from second event — original timer would have cleared at 200ms, but reset means still highlighted
    vi.advanceTimersByTime(800);
    expect(tracker.isHighlighted("nibs-abc1")).toBe(true);

    // Advance remaining 200ms — now the 1000ms from second event has elapsed
    vi.advanceTimersByTime(200);
    expect(tracker.isHighlighted("nibs-abc1")).toBe(false);

    tracker.destroy();
  });

  it("destroy() clears all timers and state", () => {
    const tracker = new NibChangeTracker({ highlightDurationMs: 1000, fadeDurationMs: 500 });

    tracker.handleEvent({ type: "updated", nibId: "nibs-abc1" });
    tracker.handleEvent({ type: "deleted", nibId: "nibs-del1" });
    expect(tracker.isHighlighted("nibs-abc1")).toBe(true);
    expect(tracker.isFading("nibs-del1")).toBe(true);

    tracker.destroy();

    // All state cleared immediately
    expect(tracker.isHighlighted("nibs-abc1")).toBe(false);
    expect(tracker.isFading("nibs-del1")).toBe(false);

    // Timers should not fire after destroy (no errors, no state changes)
    vi.advanceTimersByTime(2000);
    expect(tracker.isHighlighted("nibs-abc1")).toBe(false);
    expect(tracker.isFading("nibs-del1")).toBe(false);
  });
});
