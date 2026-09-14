/**
 * Reactive binder around `NIB_CHANGED_SUBSCRIPTION`. It owns the urql
 * subscription lifecycle; classification, self-echo suppression and the
 * payload→snapshot mapping belong in the pure reducer (`nibChange.ts`).
 *
 * It only notifies. Banners, highlights and conflict handling belong to the
 * view and the form model.
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
  type NibGoneReason,
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
  /** Why the viewed nib left its location, or null (resets on nibId change).
   *  An `"archived"` nib is still savable; a `"deleted"` one is not. */
  readonly gone: NibGoneReason | null;
  /** Latest non-self, de-duped external snapshot (null until one arrives). */
  readonly external: NibSnapshot | null;
  readonly error: unknown;
}

export function createLiveNib(opts: LiveNibOptions): LiveNib {
  const subStore = opts.subscriptionStore ?? urqlSubscriptionStore;

  // `$state.raw`: replace, never mutate. The reducer returns the same reference
  // when nothing changed, and reassigning it fires nothing.
  let state = $state.raw<NibChangeState>(initialNibChangeState);
  let error = $state<unknown>(undefined);

  // Non-reactive: the store can re-emit the same `data` object.
  let lastSubData: unknown = null;

  $effect(() => {
    // `id` is the only tracked read; assignments in the callback below do not
    // re-run this effect.
    const id = opts.nibId();

    lastSubData = null;
    state = initialNibChangeState;
    error = undefined;

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

      // Untracked, so a post-save etag change does not re-subscribe.
      const self = untrack(() => opts.selfEtag()) ?? null;
      state = classifyNibEvent(state, event, self);
    });

    return () => unsubscribe();
  });

  return {
    get gone() {
      return state.gone;
    },
    get external() {
      return state.external;
    },
    get error() {
      return error;
    },
  };
}
