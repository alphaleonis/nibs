import { describe, it, expect, vi } from "vitest";
import { createConnectionRecovery, RESUME_COALESCE_MS } from "./connectionRecovery";

/** Ports with a controllable clock, mirroring tableDataSource's test style. */
function setup() {
  const reconnect = vi.fn();
  const refetch = vi.fn();
  let pending: { fn: () => void; ms: number } | null = null;
  const core = createConnectionRecovery({
    reconnect,
    scheduleDeferred: (fn, ms) => {
      pending = { fn, ms };
      return pending;
    },
    cancelDeferred: () => {
      pending = null;
    },
  });
  core.onRecovered(refetch);
  return {
    core,
    reconnect,
    refetch,
    /** Fire the scheduled coalescing timer. */
    tick: () => {
      const p = pending;
      pending = null;
      p?.fn();
    },
    pendingMs: () => pending?.ms,
  };
}

describe("createConnectionRecovery", () => {
  describe("status", () => {
    it("starts as connecting, before the socket has ever been up", () => {
      expect(setup().core.status).toBe("connecting");
    });

    it("is connected once the socket reports up", () => {
      const { core } = setup();
      core.onConnected();
      expect(core.status).toBe("connected");
    });

    // A drop before the first successful connect is still start-up, not a lost
    // connection — reporting "disconnected" there would flash the chip on every
    // cold load.
    it("stays connecting when the socket drops before it ever connected", () => {
      const { core } = setup();
      core.onClosed();
      expect(core.status).toBe("connecting");
    });

    it("is disconnected when the socket drops after having been up", () => {
      const { core } = setup();
      core.onConnected();
      core.onClosed();
      expect(core.status).toBe("disconnected");
    });
  });

  describe("catching up after a gap", () => {
    it("refetches when the socket comes back after a drop", () => {
      const { core, refetch } = setup();
      core.onConnected();
      core.onClosed();
      refetch.mockClear();
      core.onConnected();
      expect(refetch).toHaveBeenCalledTimes(1);
    });

    // The queries are already in flight at start-up; refetching here would just
    // double every load.
    it("does not refetch on the first connect", () => {
      const { core, refetch } = setup();
      core.onConnected();
      expect(refetch).not.toHaveBeenCalled();
    });

    // The detail panel and the nib list hold separate queries and both miss
    // events while the socket is down, so every registered region must be told.
    it("notifies every registered listener", () => {
      const { core } = setup();
      const a = vi.fn();
      const b = vi.fn();
      core.onRecovered(a);
      core.onRecovered(b);
      core.onConnected();
      core.onClosed();
      core.onConnected();
      expect(a).toHaveBeenCalledTimes(1);
      expect(b).toHaveBeenCalledTimes(1);
    });

    it("stops notifying an unsubscribed listener", () => {
      const { core } = setup();
      const gone = vi.fn();
      const unsubscribe = core.onRecovered(gone);
      unsubscribe();
      core.onConnected();
      core.onClosed();
      core.onConnected();
      expect(gone).not.toHaveBeenCalled();
    });
  });

  describe("resuming", () => {
    // A bfcache-restored page resumes with its JS state frozen in time: the
    // client can still believe the socket is up when the browser closed it on
    // freeze. That belief is worthless, so reconnect regardless of status.
    it("reconnects after a bfcache restore even while it believes it is connected", () => {
      const { core, reconnect } = setup();
      core.onConnected();
      core.onResume("pageshow-restored");
      expect(reconnect).toHaveBeenCalledTimes(1);
    });

    it("reconnects when the network comes back", () => {
      const { core, reconnect } = setup();
      core.onConnected();
      core.onResume("online");
      expect(reconnect).toHaveBeenCalledTimes(1);
    });

    // Tab focus is a trustworthy signal and fires constantly; tearing down a
    // healthy socket on every alt-tab would be pure churn.
    it("does not reconnect on tab focus while the socket is up", () => {
      const { core, reconnect } = setup();
      core.onConnected();
      core.onResume("visible");
      expect(reconnect).not.toHaveBeenCalled();
    });

    it("does reconnect on tab focus while the socket is down", () => {
      const { core, reconnect } = setup();
      core.onConnected();
      core.onClosed();
      core.onResume("visible");
      expect(reconnect).toHaveBeenCalledTimes(1);
    });
  });

  describe("coalescing", () => {
    // Waking a laptop fires pageshow, visibilitychange and online within
    // milliseconds of each other; each one tearing down the socket would thrash.
    it("collapses a burst of resume signals into a single reconnect", () => {
      const { core, reconnect } = setup();
      core.onConnected();
      core.onResume("pageshow-restored");
      core.onResume("online");
      core.onResume("visible");
      expect(reconnect).toHaveBeenCalledTimes(1);
    });

    it("allows another reconnect once the coalescing window closes", () => {
      const { core, reconnect, tick, pendingMs } = setup();
      core.onConnected();
      core.onResume("online");
      expect(pendingMs()).toBe(RESUME_COALESCE_MS);
      tick();
      core.onResume("online");
      expect(reconnect).toHaveBeenCalledTimes(2);
    });
  });
});
