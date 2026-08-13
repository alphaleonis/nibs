/**
 * Decides WHEN to re-establish the live GraphQL subscription and re-read what
 * went stale while it was down. Pure — no Svelte, no DOM, no timers of its own —
 * so the policy is testable without a browser; `useConnectionRecovery.svelte.ts`
 * binds the real listeners and ports.
 *
 * Why this exists (nibs-1seo): the browser closes the WebSocket when a page
 * enters the back/forward cache, and a bfcache-restored page does NOT re-run its
 * scripts — it resumes with every bit of JS state intact, including a client
 * that may still believe its socket is live. Nothing then re-establishes it and
 * nothing refetches, so the UI serves pre-freeze cached data indefinitely.
 */

/** Opaque timer token, stored but never inspected (matches SourcePorts). */
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
 * `connecting` covers start-up AND any drop before the first successful
 * connect — a cold load that has not finished yet is not a lost connection, and
 * reporting one would flash the disconnected chip on every page load.
 */
export type ConnectionStatus = "connecting" | "connected" | "disconnected";

export interface ConnectionRecoveryPorts {
  /** Force the socket to drop and re-establish (`wsClient.terminate()`). */
  reconnect(): void;
  scheduleDeferred(fn: () => void, ms: number): DeferredHandle;
  cancelDeferred(handle: DeferredHandle): void;
}

export interface ConnectionRecovery {
  readonly status: ConnectionStatus;
  /** The socket reported it is up. */
  onConnected(): void;
  /** The socket reported it went away. */
  onClosed(): void;
  /** The page came back to the user. */
  onResume(reason: ResumeReason): void;
  /**
   * Register a listener fired when the socket comes back after a gap, i.e. when
   * cached query results became suspect. Returns an unsubscribe.
   *
   * A registry rather than a single port because more than one region has to
   * re-read: the detail panel and the nib list hold separate queries, and both
   * miss events while the socket is down.
   */
  onRecovered(listener: () => void): () => void;
}

/**
 * How long one resume signal suppresses the next. Waking a laptop fires
 * `pageshow`, `visibilitychange` and `online` within milliseconds of each other,
 * and each tearing the socket down would thrash a connection that is already
 * being re-established.
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
      // Only a RE-connect implies a gap worth re-reading; the first connect
      // races the queries already in flight, and refetching there doubles every
      // cold load.
      const recovered = everConnected && status !== "connected";
      status = "connected";
      everConnected = true;
      if (recovered) for (const l of [...listeners]) l();
    },

    onClosed() {
      status = everConnected ? "disconnected" : "connecting";
    },

    onResume(reason: ResumeReason) {
      // `visible` is a trustworthy signal that fires constantly, so it only acts
      // on a socket already known to be down. The other two arrive precisely
      // when the recorded status cannot be trusted — a frozen page's belief
      // predates the freeze, and regaining the network says the previous state
      // was wrong — so they reconnect regardless of what `status` claims.
      if (reason === "visible" && status === "connected") return;

      if (coalescing !== null) return;
      coalescing = ports.scheduleDeferred(() => {
        coalescing = null;
      }, RESUME_COALESCE_MS);
      ports.reconnect();
    },
  };
}
