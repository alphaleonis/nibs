/**
 * Thin Svelte adapter over the pure `tableDataSource` core (ports & adapters).
 * It owns the framework-coupled concerns — the re-keyed `TREE_TABLE_QUERY` store,
 * the `NIB_CHANGED_SUBSCRIPTION` store, and the `NibChangeTracker` whose
 * highlight/fade `$state` mutates on async timers — and delegates the fragile
 * *when-to-refetch* decision (dedup / defer / single-timer / throw-isolation) to
 * the core through injected ports. See `../tableDataSource.ts`.
 *
 * `.svelte.ts` modules cannot use `$`-store auto-subscription (that is a `.svelte`
 * component feature), so the query/subscription store values are bridged into
 * `$state` via manual `.subscribe`, mirroring `../liveNib.svelte.ts`. Teardown is
 * an effect-cleanup rather than `onDestroy` so the composable works both inside a
 * component and under `$effect.root` in tests.
 */

import { untrack } from "svelte";
import type { Client } from "@urql/core";
import {
  queryStore as urqlQueryStore,
  subscriptionStore as urqlSubscriptionStore,
} from "@urql/svelte";
import { TREE_TABLE_QUERY, NIB_CHANGED_SUBSCRIPTION } from "../queries";
import type { PreparedFilter } from "../filter";
import type { TreeTableNib } from "../types";
import { NibChangeTracker } from "../changeTracker.svelte";
import { createTableDataSource, type NibChangeEvent } from "../tableDataSource";

/** The server-side slice of a filter, as produced by `prepareFilter`. */
type ServerFilter = PreparedFilter["serverFilter"];

export interface UseTableDataOptions {
  /** urql client. Component passes `getContextClient()`; tests inject a fake. */
  client: Client;
  /** Reactive getter for the server filter (`prepareFilter(...).serverFilter`);
   *  the query re-keys whenever its result changes. */
  getServerFilter: () => ServerFilter;
  /** Debounce (ms) before a server-filter change re-keys the list query. Typing
   *  in the filter box changes `search` on every keystroke; debouncing here makes
   *  the GraphQL list refetch wait for typing to settle instead of firing per
   *  character. The first value applies immediately; later changes apply through
   *  a single re-key once they settle. 0 (the default) is fully synchronous. */
  refetchDebounceMs?: number;
  /** Test seam; defaults to urql's `queryStore`. */
  queryStore?: typeof urqlQueryStore;
  /** Test seam; defaults to urql's `subscriptionStore`. */
  subscriptionStore?: typeof urqlSubscriptionStore;
}

/** Order-independent content key for a server filter, so a rebuilt filter object
 *  with identical fields (e.g. from a client-only facet toggle) is recognized as
 *  unchanged and never re-keys the query. */
function filterKey(filter: ServerFilter): string {
  const record = filter as Record<string, unknown>;
  const keys = Object.keys(record).sort();
  return JSON.stringify(keys.map((k) => [k, record[k]]));
}

/** Read-only, per-row live-change state (highlight/fade), delegated to the
 *  adapter's `NibChangeTracker` so its reactive `$state` stays live to render. */
export interface ChangedState {
  isHighlighted(id: string): boolean;
  isFading(id: string): boolean;
  readonly fadeDurationMs: number;
}

export interface TableDataView {
  readonly allNibs: TreeTableNib[];
  readonly fetching: boolean;
  readonly error: unknown;
  readonly changed: ChangedState;
  /**
   * Re-read the list from the network, bypassing the cache. For recovering from
   * a gap in the live subscription, whose missed events left the cached result
   * silently behind (nibs-1seo).
   */
  refetch(): void;
}

interface QueryValue {
  data?: { nibs?: TreeTableNib[] } | null;
  error?: unknown;
  fetching: boolean;
}

export function useTableData(opts: UseTableDataOptions): TableDataView {
  const makeQuery = opts.queryStore ?? urqlQueryStore;
  const makeSub = opts.subscriptionStore ?? urqlSubscriptionStore;

  // Highlight/fade lives adapter-side: its sets are `$state` whose fade timers
  // expire asynchronously, outside any core method, so they must stay reactive
  // to the render. The core touches it only via the `applyChange` port.
  const changeTracker = new NibChangeTracker();

  // Debounced view of the server filter that actually re-keys the query. Typing
  // in the filter box updates the live filter (and the box's dropdowns/highlight)
  // on every keystroke, but the list query — which re-keys on every server-filter
  // change — waits `refetchDebounceMs` for changes to settle, so a large project
  // refetches once typing pauses rather than per character. The first value is
  // applied immediately; content-equal churn (a new object with the same server
  // fields) never re-keys. A debounce of 0 stays fully synchronous.
  const refetchDebounceMs = opts.refetchDebounceMs ?? 0;
  const liveFilter = $derived(opts.getServerFilter());
  let debouncedFilter = $state(untrack(() => opts.getServerFilter()));
  let appliedKey = untrack(() => filterKey(debouncedFilter));
  let debounceTimer: ReturnType<typeof setTimeout> | null = null;
  $effect(() => {
    const next = liveFilter;
    const nextKey = filterKey(next);
    if (nextKey === appliedKey) return; // no meaningful change to the server query
    if (refetchDebounceMs <= 0) {
      appliedKey = nextKey;
      debouncedFilter = next;
      return;
    }
    if (debounceTimer) clearTimeout(debounceTimer);
    debounceTimer = setTimeout(() => {
      debounceTimer = null;
      appliedKey = nextKey;
      debouncedFilter = next;
    }, refetchDebounceMs);
  });

  // Re-keyed reactively: a fresh query store is created whenever the (debounced)
  // server filter changes. Read lazily by the value bridge below and by requestRefetch.
  const result = $derived(
    makeQuery({
      client: opts.client,
      query: TREE_TABLE_QUERY,
      variables: { filter: debouncedFilter },
    }),
  );

  // Bridge the current query store's value into `$state`. Re-subscribes when
  // `result` re-keys (the effect reads `result`, so a re-key re-runs it and the
  // cleanup unsubscribes the previous store).
  let queryValue = $state<QueryValue>({ fetching: true, data: null, error: undefined });
  $effect(() => {
    const store = result;
    const unsub = store.subscribe((v) => {
      queryValue = { data: v.data ?? null, error: v.error, fetching: v.fetching };
    });
    return unsub;
  });

  const source = createTableDataSource({
    requestRefetch: () => result.reexecute({ requestPolicy: "network-only" }),
    scheduleDeferred: (fn, ms) => setTimeout(fn, ms),
    cancelDeferred: (handle) => clearTimeout(handle as ReturnType<typeof setTimeout>),
    applyChange: (event) => changeTracker.handleEvent(event),
    fadeDurationMs: () => changeTracker.fadeDurationMs,
  });

  // Stable subscription store (id omitted → all nib changes). A single manual
  // subscription pumps error + data into the core. Side effects run untracked so
  // a synchronous initial emission cannot make this effect depend on `result` or
  // the tracker's `$state` (which would churn subscribe/unsubscribe or loop).
  const subscription = makeSub({
    client: opts.client,
    query: NIB_CHANGED_SUBSCRIPTION,
  });
  $effect(() => {
    const unsub = subscription.subscribe((v) => {
      if (v.error) {
        untrack(() => source.onSubscriptionError(v.error));
      }
      const event = v.data?.nibChanged as NibChangeEvent | null | undefined;
      if (event) {
        untrack(() => source.onChangeEvent(event));
      }
    });
    return unsub;
  });

  // Teardown: clear the core's pending delete timer and the tracker's fade/
  // highlight timers. An effect-cleanup (not `onDestroy`) so this composable is
  // usable under `$effect.root` in tests, matching `liveNib.svelte.ts`.
  $effect(() => () => {
    if (debounceTimer) clearTimeout(debounceTimer);
    source.destroy();
    changeTracker.destroy();
  });

  const changed: ChangedState = {
    isHighlighted: (id) => changeTracker.isHighlighted(id),
    isFading: (id) => changeTracker.isFading(id),
    get fadeDurationMs() {
      return changeTracker.fadeDurationMs;
    },
  };

  return {
    get allNibs() {
      return queryValue.data?.nibs ?? [];
    },
    get fetching() {
      return queryValue.fetching;
    },
    get error() {
      return queryValue.error;
    },
    get changed() {
      return changed;
    },
    refetch() {
      result.reexecute({ requestPolicy: "network-only" });
    },
  };
}
