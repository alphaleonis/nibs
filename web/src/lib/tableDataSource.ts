/**
 * Pure decision core for the tree table's live data source: it decides *when* to
 * refetch the nib list in response to a `NIB_CHANGED_SUBSCRIPTION` event. It owns
 * the synchronous branch logic that is easy to get wrong — dedup, defer-the-
 * delete-refetch-until-the-fade-plays, single-pending-timer, and throw isolation
 * — and nothing reactive.
 *
 * Framework-free by design: ZERO Svelte and ZERO urql imports. The Svelte/urql
 * concerns (the query store, the subscription store, and the `NibChangeTracker`
 * whose highlight/fade `$state` mutates on async timers) stay in the adapter
 * (`composables/useTableData.svelte.ts`) and reach this core only through the
 * injected `SourcePorts`. That keeps this decision logic provable in plain vitest
 * with a fake clock — no jsdom, no `$effect.root`, no urql mock.
 *
 * Distinct from `tableData.ts` / `buildShapedTableData`, which assembles the view rows
 * from a nib list; this decides when to (re)source that list.
 */

/** The subset of a `nibChanged` payload this core inspects. */
export interface NibChangeEvent {
  type: string;
  nibId: string;
  nib?: { etag?: string | null } | null;
}

/** Opaque timer token the core stores but never inspects (a `setTimeout`
 *  handle in production, a test token under a fake clock). */
export type DeferredHandle = unknown;

export interface SourcePorts {
  /** Refresh the nib list. Adapter injects `result.reexecute({ requestPolicy:
   *  "network-only" })`. May throw or be absent — the core isolates it. */
  requestRefetch(): void;
  /** Schedule a deferred callback. Adapter injects `setTimeout`. */
  scheduleDeferred(fn: () => void, ms: number): DeferredHandle;
  /** Cancel a previously scheduled callback. Adapter injects `clearTimeout`. */
  cancelDeferred(handle: DeferredHandle): void;
  /** Apply the change to view-side state. Adapter injects
   *  `changeTracker.handleEvent`. Called exactly ONCE per fresh event (gated by
   *  this core's dedup), and deliberately NOT wrapped in try/catch (it is total). */
  applyChange(event: NibChangeEvent): void;
  /** How long a deleted row's fade-out plays. Adapter injects
   *  `() => changeTracker.fadeDurationMs`. Read at schedule time. */
  fadeDurationMs(): number;
  /** Surface a failure. Defaults to `console.error`. Never swallows. */
  reportError?(context: string, err: unknown): void;
}

export interface TableDataSource {
  /** Route a fresh subscription event through the dedup + refetch decision. */
  onChangeEvent(event: NibChangeEvent): void;
  /** Surface a subscription-stream error (never swallowed). */
  onSubscriptionError(err: unknown): void;
  /** Clear the pending delete timer. Idempotent. */
  destroy(): void;
}

export function createTableDataSource(ports: SourcePorts): TableDataSource {
  const reportError =
    ports.reportError ?? ((context: string, err: unknown) => console.error(context, err));

  // Content key of the last-applied event. urql re-emits a fresh wrapper object
  // on every reactive cycle, so identity comparison is unreliable — compare by
  // content. The payload etag is folded in so a genuine second edit to the SAME
  // nib (new etag) is not swallowed, while a burst of duplicate emissions for one
  // commit (shared etag) collapses. `deleted`/`archived` carry a null nib → etag
  // falls back to "" so their (type:nibId) dedup is unaffected.
  let lastEventKey = "";

  // Exactly one pending deferred-delete refetch. A new delete cancels + replaces
  // the prior one (never cleared per non-delete event — that would cut a still-
  // playing fade short); `destroy()` clears it. `undefined` means none pending —
  // checked with `!== undefined` because a valid handle can be falsy (e.g. `0`).
  let pendingDelete: DeferredHandle | undefined;

  // Isolate the fragile refetch: a throwing/absent `requestRefetch` must never
  // escape `onChangeEvent`, or (in the adapter) it would abort Svelte's effect
  // flush and silently take the live bridge down for the rest of the session.
  function safeRefetch(): void {
    try {
      ports.requestRefetch();
    } catch (err) {
      reportError("Failed to refetch nibs after a change event:", err);
    }
  }

  return {
    onChangeEvent(event: NibChangeEvent): void {
      const key = `${event.type}:${event.nibId}:${event.nib?.etag ?? ""}`;
      if (key === lastEventKey) return;
      lastEventKey = key;

      // Once per fresh event, before the refetch decision. Total, so unwrapped.
      ports.applyChange(event);

      if (event.type === "deleted") {
        // Defer the refetch so the row's fade-out plays before it leaves the
        // dataset. Replace (not just add) any prior pending delete timer.
        if (pendingDelete !== undefined) ports.cancelDeferred(pendingDelete);
        pendingDelete = ports.scheduleDeferred(() => {
          pendingDelete = undefined;
          safeRefetch();
        }, ports.fadeDurationMs());
      } else {
        safeRefetch();
      }
    },

    onSubscriptionError(err: unknown): void {
      reportError("Nib subscription error:", err);
    },

    destroy(): void {
      if (pendingDelete !== undefined) {
        ports.cancelDeferred(pendingDelete);
        pendingDelete = undefined;
      }
    },
  };
}
