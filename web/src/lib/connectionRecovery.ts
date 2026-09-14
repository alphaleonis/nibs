/**
 * Decides when to re-establish the live GraphQL subscription and re-read what
 * went stale while it was down. No Svelte, DOM or timers of its own;
 * `useConnectionRecovery.svelte.ts` binds the real listeners and ports.
 *
 * A page restored from the back/forward cache does not re-run its scripts: it
 * resumes with a client that may believe its closed socket is live, and nothing
 * else reconnects or refetches.
 */

/** Opaque timer token, stored but never inspected. */
export type DeferredHandle = unknown;

/** What brought the page back to the user. */
export type ResumeReason =
  /** Restored from the back/forward cache (`pageshow` with `persisted`). */
  | "pageshow-restored"
  /** The tab became visible again. */
  | "visible"
  /** The browser regained network connectivity. */
  | "online";

/**
 * `connecting` also covers a drop before the first successful connect, so a
 * cold load does not flash the disconnected chip.
 */
export type ConnectionStatus = "connecting" | "connected" | "disconnected";

export interface ConnectionRecoveryPorts {
  /** Force the socket to drop and re-establish. */
  reconnect(): void;
  scheduleDeferred(fn: () => void, ms: number): DeferredHandle;
  cancelDeferred(handle: DeferredHandle): void;
}

export interface ConnectionRecovery {
  readonly status: ConnectionStatus;
  onConnected(): void;
  onClosed(): void;
  onResume(reason: ResumeReason): void;
  /**
   * Register a listener fired when the socket comes back after a gap, when cached
   * query results may be stale. Returns an unsubscribe.
   */
  onRecovered(listener: () => void): () => void;
}

/**
 * How long one resume signal suppresses the next. Waking a laptop fires
 * `pageshow`, `visibilitychange` and `online` within milliseconds of each other.
 */
export const RESUME_COALESCE_MS = 1000;

export function createConnectionRecovery(ports: ConnectionRecoveryPorts): ConnectionRecovery {
  let status: ConnectionStatus = "connecting";
  let everConnected = false;
  let coalescing: DeferredHandle | null = null;
  const listeners = new Set<() => void>();

  return {
    get status() {
      return status;
    },

    onRecovered(listener: () => void) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },

    onConnected() {
      // Only a reconnect implies a gap; refetching on the first connect would
      // double every cold load.
      const recovered = everConnected && status !== "connected";
      status = "connected";
      everConnected = true;
      if (recovered) for (const l of [...listeners]) l();
    },

    onClosed() {
      status = everConnected ? "disconnected" : "connecting";
    },

    onResume(reason: ResumeReason) {
      // `visible` fires constantly, so it acts only on a socket known to be down.
      // The other reasons reconnect regardless: the recorded status predates the
      // freeze or the network loss.
      if (reason === "visible" && status === "connected") return;

      if (coalescing !== null) return;
      coalescing = ports.scheduleDeferred(() => {
        coalescing = null;
      }, RESUME_COALESCE_MS);
      ports.reconnect();
    },
  };
}
