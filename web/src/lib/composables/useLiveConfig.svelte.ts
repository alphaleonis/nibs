/**
 * The store's configuration as one reactive answer, assembled from the two
 * channels that carry it and made durable against a failed re-ask.
 *
 * Three things live here because they are one policy, not three:
 *
 *   - **Precedence.** A pushed config (the `configChanged` subscription, which
 *     fires when the store's areas.yml is reloaded) wins over the queried one.
 *   - **The latch.** Once a config has been answered, it is HELD. urql drops
 *     `data` on a failed network-only re-execution — measured, not assumed — so
 *     without this a re-ask that failed would take away a vocabulary the session
 *     already had, which is why the re-ask used to be gated on there being none.
 *     Holding the last good one is what makes an unconditional re-ask safe, and
 *     an unconditional re-ask is what picks up a vocabulary edited while the
 *     socket was down.
 *   - **The retry.** `CONFIG_QUERY` runs once and urql retries nothing, so a 502
 *     from a reverse proxy, a single failed fetch or a resolver error left the
 *     session with no vocabulary for its whole life (nibs-zwnm). The only
 *     re-ask was App's on WEBSOCKET recovery, and queries do not travel on the
 *     socket — a failure with a healthy socket signalled nothing at all.
 *
 * The retry is bounded rather than endless. A vocabulary that cannot be fetched
 * is a real state, and a page that re-asks forever is worse for the server than
 * one that stops and offers the reader a button.
 *
 * The cost of NOT healing is bigger than the Areas view it is most visible on:
 * with no vocabulary, `withSendableArea` withholds every `area:` filter value,
 * so the table quietly answers with the whole store while the filter box still
 * reads `area:web` (see lib/filter.ts). That is why this heals on its own rather
 * than waiting for a reader to find the retry button on one screen.
 */

/**
 * How long to wait before each automatic re-ask, and — by its length — how many
 * to make. Growing delays so a server that is down is not hammered, and a
 * finite list so the page stops.
 */
export const CONFIG_RETRY_DELAYS = [1_000, 2_000, 4_000, 8_000, 16_000] as const;

export interface LiveConfigPorts<T> {
  /** Reactive: what the config query has answered, or undefined. */
  queried: () => T | undefined;
  /** Reactive: what the server last pushed, or undefined. */
  pushed: () => T | undefined;
  /** Reactive: the config query's current error, or undefined. */
  error: () => unknown;
  /** Reactive: whether a config query is in flight. */
  fetching: () => boolean;
  /** Re-ask the config query over the network. */
  reask: () => void;
  /** Test seam over the clock; defaults to setTimeout/clearTimeout. */
  schedule?: (fn: () => void, ms: number) => unknown;
  cancel?: (handle: unknown) => void;
}

export interface LiveConfig<T> {
  /** The config in hand: pushed, else queried, else the last one either gave. */
  readonly config: T | undefined;
  /** Nothing has ever answered and the query failed — the dead end a reader
   *  can be offered a retry for. A held config makes this false however badly
   *  the latest re-ask went. */
  readonly unavailable: boolean;
  /** Re-ask now and restore the automatic budget. The manual escape, and what
   *  App calls on socket recovery so a reconnect is a fresh start. */
  retry: () => void;
}

export function useLiveConfig<T>(ports: LiveConfigPorts<T>): LiveConfig<T> {
  const schedule = ports.schedule ?? ((fn: () => void, ms: number) => setTimeout(fn, ms));
  const cancel =
    ports.cancel ?? ((handle: unknown) => clearTimeout(handle as ReturnType<typeof setTimeout>));

  let held = $state.raw<T | undefined>(undefined);
  let attempt = $state(0);

  const answered = $derived(ports.pushed() ?? ports.queried());
  const config = $derived(answered ?? held);

  // The latch itself, plus the budget reset: an answer is proof the server is
  // reachable, so the next failure starts its backoff from the beginning rather
  // than from wherever the last one gave up.
  $effect(() => {
    const current = answered;
    if (current === undefined) return;
    held = current;
    attempt = 0;
  });

  // One pending re-ask at a time. `fetching` is in the condition rather than
  // only the delay because the effect re-runs when `attempt` changes — without
  // it a failure would schedule the next attempt on top of the request it just
  // made, spending the whole budget before any of it could answer.
  $effect(() => {
    if (ports.error() === undefined || ports.fetching()) return;
    const delay = CONFIG_RETRY_DELAYS[attempt];
    if (delay === undefined) return;

    const handle = schedule(() => {
      attempt += 1;
      ports.reask();
    }, delay);
    return () => cancel(handle);
  });

  return {
    get config() {
      return config;
    },
    get unavailable() {
      return config === undefined && ports.error() !== undefined;
    },
    retry() {
      attempt = 0;
      ports.reask();
    },
  };
}
