/**
 * Decides when the tree table refetches the nib list in response to change
 * events: dedup, deferring a delete's refetch until its fade plays, one pending
 * timer, and refetch error isolation.
 *
 * No Svelte or urql imports: the adapter (`composables/useTableData.svelte.ts`)
 * supplies those through `SourcePorts`, so this runs under a fake clock.
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
  /** Refresh the nib list. A throw is caught and reported. */
  requestRefetch(): void;
  scheduleDeferred(fn: () => void, ms: number): DeferredHandle;
  cancelDeferred(handle: DeferredHandle): void;
  /** Apply the change to view-side state, once per fresh event. Must not throw:
   *  it is not wrapped in try/catch. */
  applyChange(event: NibChangeEvent): void;
  /** How long a deleted row's fade-out plays. Read at schedule time. */
  fadeDurationMs(): number;
  /** Surface a failure. Defaults to `console.error`. */
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

  // Compared by content, not identity. The etag keeps a second edit to the same
  // nib distinct while repeated emissions of one change collapse; a `deleted`
  // event carries no nib, so its etag part is "".
  let lastEventKey = "";

  // The single pending deferred-delete refetch; a new delete replaces it.
  // Checked with `!== undefined` because a valid handle can be falsy (`0`).
  let pendingDelete: DeferredHandle | undefined;

  // A throw must not escape `onChangeEvent`: in the adapter it would abort
  // Svelte's effect flush.
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

      ports.applyChange(event);

      if (event.type === "deleted") {
        // Defer so the row's fade-out plays before it leaves the dataset.
        if (pendingDelete !== undefined) ports.cancelDeferred(pendingDelete);
        pendingDelete = ports.scheduleDeferred(() => {
          pendingDelete = undefined;
          safeRefetch();
        }, ports.fadeDurationMs());
      } else if (pendingDelete === undefined) {
        // While a fade is pending, its deferred refetch covers this change too;
        // refetching now would drop the fading row mid-fade.
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
