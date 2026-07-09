/**
 * Minimal reactive binder around `NIB_CHANGED_SUBSCRIPTION` — the only
 * rune-bearing unit of the live nib-change model. It owns the urql subscription
 * lifecycle (open / re-subscribe / pause) and reference-dedup; classification,
 * self-echo suppression, dedup policy, and the payload→snapshot mapping all live
 * in the pure reducer (`nibChange.ts`).
 *
 * The whole model NOTIFIES only: it exposes reactive `deleted` / `external` /
 * `error`. Banners, highlights, and the pending/keep-mine/apply conflict
 * lifecycle belong to the view and the form model.
 */

import { untrack } from "svelte";
import type { Client } from "@urql/core";
import { subscriptionStore as urqlSubscriptionStore } from "@urql/svelte";
import { NIB_CHANGED_SUBSCRIPTION } from "./queries";
import type { NibSnapshot } from "./nibForm.svelte";
import {
  classifyNibEvent,
  initialNibChangeState,
  type NibChangeState,
  type RawNibEvent,
} from "./nibChange";

export interface LiveNibOptions {
  client: Client;
  /** Reactive: `undefined` => create mode => no subscription is opened. */
  nibId: () => string | undefined;
  /** Reactive: wire to the form etag; read LIVE at event time for self-echo. */
  selfEtag: () => string | undefined;
  /** Test seam; defaults to urql's `subscriptionStore`. */
  subscriptionStore?: typeof urqlSubscriptionStore;
}

export interface LiveNib {
  /** The viewed nib was deleted on the server (resets on nibId change). */
  readonly deleted: boolean;
  /** Latest non-self, de-duped external snapshot (null until one arrives). */
  readonly external: NibSnapshot | null;
  readonly error: unknown;
}

export function createLiveNib(opts: LiveNibOptions): LiveNib {
  const subStore = opts.subscriptionStore ?? urqlSubscriptionStore;

  // `$state.raw`: state is replaced wholesale (never mutated in place), so the
  // reducer's reference-stability doubles as reactivity-dedup — assigning the
  // same reference back is a `!==` no-op and fires nothing.
  let state = $state.raw<NibChangeState>(initialNibChangeState);
  let error = $state<unknown>(undefined);

  // Plain (non-reactive) dedup guard on the store's emitted `data` object —
  // the urql store re-emits the same reference on unrelated field changes.
  let lastSubData: unknown = null;

  $effect(() => {
    const id = opts.nibId();

    // Re-subscription boundary (id change): reset per-nib state. This runs only
    // when `id` (the sole tracked read) changes — event assignments happen in
    // the async callback below and never re-run this effect.
    lastSubData = null;
    state = initialNibChangeState;
    error = undefined;

    // Create mode / no id: do not open a subscription.
    if (!id) return;

    const store = subStore({
      client: opts.client,
      query: NIB_CHANGED_SUBSCRIPTION,
      variables: { id },
    });

    const unsubscribe = store.subscribe((result) => {
      if (result.error) {
        error = result.error;
        console.warn("Live nib subscription error:", result.error);
      }

      const data = result.data as { nibChanged?: RawNibEvent } | undefined;
      if (!data || data === lastSubData) return;
      lastSubData = data;

      const event = data.nibChanged;
      if (!event) return;

      // Read selfEtag LIVE (untracked) so a post-save etag change filters the
      // echo without re-subscribing this effect.
      const self = untrack(() => opts.selfEtag()) ?? null;
      state = classifyNibEvent(state, event, self);
    });

    return () => unsubscribe();
  });

  return {
    get deleted() {
      return state.deleted;
    },
    get external() {
      return state.external;
    },
    get error() {
      return error;
    },
  };
}
