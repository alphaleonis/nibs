/**
 * Svelte adapter over the pure `tableDataSource` core. It owns the re-keyed
 * list query, the change subscription and the `NibChangeTracker`, and delegates
 * when to refetch to the core (`../tableDataSource.ts`).
 *
 * Stores are bridged into `$state` by manual `.subscribe`, since `.svelte.ts`
 * has no `$store` syntax. Teardown is an effect cleanup rather than `onDestroy`
 * so it also runs under `$effect.root` in tests.
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
  client: Client;
  /** Reactive getter for the server filter; the query re-keys when its content
   *  changes. */
  getServerFilter: () => ServerFilter;
  /** Debounce (ms) before a server-filter change re-keys the list query, so
   *  typing refetches once it settles. The first value applies immediately.
   *  0 (the default) is synchronous. */
  refetchDebounceMs?: number;
  /** Test seam; defaults to urql's `queryStore`. */
  queryStore?: typeof urqlQueryStore;
  /** Test seam; defaults to urql's `subscriptionStore`. */
  subscriptionStore?: typeof urqlSubscriptionStore;
}

/** Order-independent content key, so a rebuilt but equal filter does not re-key. */
function filterKey(filter: ServerFilter): string {
  const record = filter as Record<string, unknown>;
  const keys = Object.keys(record).sort();
  return JSON.stringify(keys.map((k) => [k, record[k]]));
}

/** Per-row highlight/fade state, read from the adapter's `NibChangeTracker`. */
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
  /** Re-read the list from the network, bypassing the cache. For recovering
   *  from a gap in the live subscription. */
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

  // Adapter-side: its fade timers mutate `$state` outside any core call. The
  // core reaches it only through the `applyChange` port.
  const changeTracker = new NibChangeTracker();

  // The debounced server filter, which is what re-keys the query.
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

  // A fresh query store per debounced filter change.
  const result = $derived(
    makeQuery({
      client: opts.client,
      query: TREE_TABLE_QUERY,
      variables: { filter: debouncedFilter },
    }),
  );

  // Re-subscribes when `result` re-keys; the cleanup unsubscribes the old store.
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

  // All nib changes (no id). Handlers run untracked so a synchronous first
  // emission adds no dependencies to this effect.
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
