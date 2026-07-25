import { describe, it, expect, vi } from "vitest";
import { createTableDataSource, type NibChangeEvent, type SourcePorts } from "./tableDataSource";

// --- Hand-rolled fake clock -------------------------------------------------
// No framework, no jsdom, no fake timers global: the core takes its scheduler
// through ports, so a tiny deterministic clock (schedule / cancel / advance) is
// all we need to prove the deferred-delete timing.

class FakeClock {
  #seq = 1;
  #now = 0;
  #timers = new Map<number, { fire: () => void; at: number }>();

  schedule = (fire: () => void, ms: number): number => {
    const id = this.#seq++;
    this.#timers.set(id, { fire, at: this.#now + ms });
    return id;
  };

  cancel = (handle: unknown): void => {
    this.#timers.delete(handle as number);
  };

  /** Advance time, firing every timer whose deadline has passed, in schedule order. */
  advance(ms: number): void {
    this.#now += ms;
    for (const [id, timer] of [...this.#timers.entries()]) {
      if (timer.at <= this.#now) {
        this.#timers.delete(id);
        timer.fire();
      }
    }
  }

  /** How many timers are currently live (scheduled and not yet fired/cancelled). */
  get pending(): number {
    return this.#timers.size;
  }
}

const FADE_MS = 500;

function setup(overrides: Partial<SourcePorts> = {}) {
  const clock = new FakeClock();
  const requestRefetch = vi.fn();
  const applyChange = vi.fn();
  const reportError = vi.fn();
  const ports: SourcePorts = {
    requestRefetch,
    scheduleDeferred: clock.schedule,
    cancelDeferred: clock.cancel,
    applyChange,
    fadeDurationMs: () => FADE_MS,
    reportError,
    ...overrides,
  };
  const source = createTableDataSource(ports);
  return { clock, requestRefetch, applyChange, reportError, source };
}

function updated(nibId: string, etag?: string): NibChangeEvent {
  return { type: "updated", nibId, nib: etag === undefined ? null : { etag } };
}

function deleted(nibId: string): NibChangeEvent {
  return { type: "deleted", nibId, nib: null };
}

describe("createTableDataSource", () => {
  it("refetches immediately for a non-delete event, with no deferred timer", () => {
    const { source, clock, requestRefetch, applyChange } = setup();

    source.onChangeEvent(updated("nibs-a", "e1"));

    expect(requestRefetch).toHaveBeenCalledTimes(1);
    expect(applyChange).toHaveBeenCalledTimes(1);
    expect(applyChange).toHaveBeenCalledWith(updated("nibs-a", "e1"));
    expect(clock.pending).toBe(0); // immediate, nothing scheduled
  });

  it("defers a delete's refetch until fadeDurationMs elapses", () => {
    const { source, clock, requestRefetch, applyChange } = setup();

    source.onChangeEvent(deleted("nibs-a"));

    // applyChange (the fade) fires immediately; the refetch is scheduled, not run.
    expect(applyChange).toHaveBeenCalledTimes(1);
    expect(requestRefetch).not.toHaveBeenCalled();
    expect(clock.pending).toBe(1);

    // Not yet — the fade window has not fully elapsed.
    clock.advance(FADE_MS - 1);
    expect(requestRefetch).not.toHaveBeenCalled();

    // Deadline reached: the deferred refetch fires exactly once.
    clock.advance(1);
    expect(requestRefetch).toHaveBeenCalledTimes(1);
    expect(clock.pending).toBe(0);
  });

  it("keeps exactly one pending delete timer across successive deletes (new cancels prior)", () => {
    const { source, clock, requestRefetch } = setup();

    source.onChangeEvent(deleted("nibs-a"));
    expect(clock.pending).toBe(1);

    // A second, distinct delete replaces the pending timer rather than adding one.
    source.onChangeEvent(deleted("nibs-b"));
    expect(clock.pending).toBe(1);

    // Only the surviving (second) timer fires — the first was cancelled, so the
    // refetch runs once, not twice.
    clock.advance(FADE_MS);
    expect(requestRefetch).toHaveBeenCalledTimes(1);
    expect(clock.pending).toBe(0);
  });

  it("staggered distinct deletes: the earlier timer never fires at its own deadline (last-timer-wins)", () => {
    // Pins the deliberate single-timer behavior for a multi-client burst where the
    // deletes arrive at DIFFERENT times: delete A schedules a refetch at t=FADE_MS;
    // delete B at t=100 cancels A's timer and schedules its own at t=100+FADE_MS.
    // A's original deadline must pass with NO refetch (proving the cancel, not just a
    // pending-count of 1) and exactly one refetch fires at B's deadline.
    const { source, clock, requestRefetch } = setup();

    source.onChangeEvent(deleted("nibs-a")); // A's timer -> t=FADE_MS
    clock.advance(100); // t=100, A still pending
    source.onChangeEvent(deleted("nibs-b")); // cancels A, B's timer -> t=100+FADE_MS
    expect(clock.pending).toBe(1);

    clock.advance(FADE_MS - 100); // t=FADE_MS: A's ORIGINAL deadline
    expect(requestRefetch).not.toHaveBeenCalled(); // A's timer was cancelled, not fired

    clock.advance(100); // t=100+FADE_MS: B's deadline
    expect(requestRefetch).toHaveBeenCalledTimes(1);
    expect(clock.pending).toBe(0);
  });

  it("dedups a duplicate type:nibId:etag but refetches for a new etag", () => {
    const { source, requestRefetch, applyChange } = setup();

    source.onChangeEvent(updated("nibs-x", "e1"));
    expect(requestRefetch).toHaveBeenCalledTimes(1);
    expect(applyChange).toHaveBeenCalledTimes(1);

    // Identical key collapses: no second refetch, no second applyChange.
    source.onChangeEvent(updated("nibs-x", "e1"));
    expect(requestRefetch).toHaveBeenCalledTimes(1);
    expect(applyChange).toHaveBeenCalledTimes(1);

    // A genuinely distinct edit (new etag) to the same nib refetches again.
    source.onChangeEvent(updated("nibs-x", "e2"));
    expect(requestRefetch).toHaveBeenCalledTimes(2);
    expect(applyChange).toHaveBeenCalledTimes(2);
  });

  it("dedups null-etag events by type:nibId (etag falls back to \"\")", () => {
    const { source, applyChange, clock } = setup();

    source.onChangeEvent(deleted("nibs-a"));
    source.onChangeEvent(deleted("nibs-a")); // same type:nibId, null nib

    // Collapsed to one applied event and one scheduled timer.
    expect(applyChange).toHaveBeenCalledTimes(1);
    expect(clock.pending).toBe(1);
  });

  it("applies each fresh event exactly once", () => {
    const { source, applyChange } = setup();

    source.onChangeEvent(updated("nibs-a", "e1"));
    source.onChangeEvent(updated("nibs-a", "e1")); // dup
    source.onChangeEvent(updated("nibs-b", "e1")); // fresh (different nib)
    source.onChangeEvent(updated("nibs-b", "e1")); // dup

    expect(applyChange).toHaveBeenCalledTimes(2);
  });

  it("isolates a throwing requestRefetch: it never escapes onChangeEvent and hits reportError", () => {
    const boom = new TypeError("reexecute exploded");
    const requestRefetch = vi.fn(() => {
      throw boom;
    });
    const { source, reportError } = setup({ requestRefetch });

    // The throw is swallowed by safeRefetch, not propagated to the caller.
    expect(() => source.onChangeEvent(updated("nibs-a", "e1"))).not.toThrow();
    expect(requestRefetch).toHaveBeenCalledTimes(1);
    expect(reportError).toHaveBeenCalledWith(
      "Failed to refetch nibs after a change event:",
      boom,
    );

    // The bridge stays alive: a later distinct event is still processed.
    source.onChangeEvent(updated("nibs-b", "e1"));
    expect(requestRefetch).toHaveBeenCalledTimes(2);
    expect(reportError).toHaveBeenCalledTimes(2);
  });

  it("isolates a throwing requestRefetch in the deferred (delete) branch too", () => {
    const boom = new Error("deferred boom");
    const requestRefetch = vi.fn(() => {
      throw boom;
    });
    const { source, clock, reportError } = setup({ requestRefetch });

    source.onChangeEvent(deleted("nibs-a"));
    // Firing the deferred timer must not throw out of advance().
    expect(() => clock.advance(FADE_MS)).not.toThrow();
    expect(reportError).toHaveBeenCalledWith(
      "Failed to refetch nibs after a change event:",
      boom,
    );
  });

  it("destroy() clears the pending delete timer so no ghost refetch fires after teardown", () => {
    const { source, clock, requestRefetch } = setup();

    source.onChangeEvent(deleted("nibs-a"));
    expect(clock.pending).toBe(1);

    source.destroy();
    expect(clock.pending).toBe(0);

    clock.advance(FADE_MS);
    expect(requestRefetch).not.toHaveBeenCalled();
  });

  it("destroy() is idempotent", () => {
    const { source, clock } = setup();
    source.onChangeEvent(deleted("nibs-a"));
    source.destroy();
    expect(() => source.destroy()).not.toThrow();
    expect(clock.pending).toBe(0);
  });

  it("onSubscriptionError reports and never throws", () => {
    const { source, reportError } = setup();
    const err = new Error("stream down");

    expect(() => source.onSubscriptionError(err)).not.toThrow();
    expect(reportError).toHaveBeenCalledWith("Nib subscription error:", err);
  });

  it("defaults reportError to console.error when no port is supplied", () => {
    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});
    try {
      const boom = new Error("no reporter");
      const source = createTableDataSource({
        requestRefetch: () => {
          throw boom;
        },
        scheduleDeferred: () => 0,
        cancelDeferred: () => {},
        applyChange: () => {},
        fadeDurationMs: () => FADE_MS,
        // reportError intentionally omitted
      });

      source.onChangeEvent(updated("nibs-a", "e1"));
      expect(errorSpy).toHaveBeenCalledWith(
        "Failed to refetch nibs after a change event:",
        boom,
      );
    } finally {
      errorSpy.mockRestore();
    }
  });
});
