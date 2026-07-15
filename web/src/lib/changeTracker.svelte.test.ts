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

  it("handleEvent('archived') fades too — the nib leaves the visible tree", () => {
    const tracker = new NibChangeTracker({ fadeDurationMs: 500 });

    tracker.handleEvent({ type: "archived", nibId: "nibs-arc1" });
    // Archiving moves the nib out of the main list, so its row leaves the same
    // way a deleted one does. Highlighting it would be wrong (it isn't staying).
    expect(tracker.isFading("nibs-arc1")).toBe(true);
    expect(tracker.isHighlighted("nibs-arc1")).toBe(false);

    vi.advanceTimersByTime(500);
    expect(tracker.isFading("nibs-arc1")).toBe(false);

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
