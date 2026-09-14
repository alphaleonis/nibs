/**
 * The store's configuration as one reactive value. A pushed config (the
 * `configChanged` subscription, fired when areas.yml is reloaded) wins over the
 * queried one. The last answer is held, because urql drops `data` when a
 * network-only re-execution fails. A failed query is re-asked on a bounded
 * backoff.
 *
 * With no vocabulary, `withSendableArea` (lib/filter.ts) withholds every
 * `area:` value and the table answers for the whole store, so this heals on its
 * own rather than waiting for the retry button.
 */

/** Delay before each automatic re-ask; the list's length is the budget. */
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
  /** True when nothing has ever answered and the query failed. */
  readonly unavailable: boolean;
  /** Re-ask now and restore the automatic budget. */
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

  // Latch each answer; an answer also resets the backoff budget.
  $effect(() => {
    const current = answered;
    if (current === undefined) return;
    held = current;
    attempt = 0;
  });

  // Wait while a request is in flight: this re-runs when `attempt` changes, and
  // would otherwise spend the whole budget before any request answered.
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
