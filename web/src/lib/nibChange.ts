/**
 * Pure reducer for nib-change subscription events, driven by `liveNib.svelte.ts`.
 * No urql, runes or DOM.
 *
 * `classifyNibEvent` returns `prev` itself on a no-op and a new object only on a
 * real change; the binder's `$state` assignment relies on that.
 */

import type { NibSnapshot } from "./nibForm.svelte";

export type NibChangeType = "created" | "updated" | "deleted" | "archived" | "unarchived";

/** Why the viewed nib left its location. An archived nib still accepts a save and
 *  a deleted one does not; the presenter keys savability off the difference. */
export type NibGoneReason = Extract<NibChangeType, "deleted" | "archived">;

/** A committed nib as it arrives over `NIB_CHANGED_SUBSCRIPTION` (nullable fields). */
export interface RawNibPayload {
  id: string;
  title: string;
  status: string;
  type: string;
  priority: string | null;
  estimate: string | null;
  tags: string[] | null;
  body: string | null;
  etag: string | null;
  // `String!` on the wire; optional so a hand-built payload may omit them.
  milestone?: string | null;
  area?: string | null;
  // Selected by the subscription but not part of NibSnapshot.
  updatedAt?: string | null;
  parentId?: string | null;
  blockingIds?: string[] | null;
  blockedByIds?: string[] | null;
}

export interface RawNibEvent {
  type: NibChangeType;
  nibId: string;
  nib: RawNibPayload | null;
}

export interface NibChangeState {
  /** Why the viewed nib left its location, or null while it is still there. */
  gone: NibGoneReason | null;
  /** Latest non-self, de-duped external snapshot (null until one arrives). */
  external: NibSnapshot | null;
  /** Etag of the last external snapshot surfaced — used for dedup. */
  lastExternalEtag: string | null;
}

export const initialNibChangeState: NibChangeState = {
  gone: null,
  external: null,
  lastExternalEtag: null,
};

/** Map a raw subscription payload to a committed `NibSnapshot` (null → defaults). */
export function toNibSnapshot(nib: RawNibPayload): NibSnapshot {
  return {
    id: nib.id,
    title: nib.title,
    status: nib.status,
    type: nib.type,
    priority: nib.priority ?? "",
    estimate: nib.estimate ?? "",
    milestone: nib.milestone ?? "",
    area: nib.area ?? "",
    tags: nib.tags ?? [],
    body: nib.body ?? "",
    etag: nib.etag ?? "",
  };
}

/**
 * Reduce a subscription event onto the previous state.
 *
 * - `deleted` / `archived` set `gone` and drop `external`. A deletion is
 *   terminal, as in composables/activeView.ts.
 * - `unarchived` clears `gone` (unless deleted) before any etag dedup, since a
 *   move leaves the etag unchanged, and adopts the snapshot only if it is new.
 * - `created` / `updated` adopt a snapshot unless the payload is missing, a
 *   self-echo (`etag === selfEtag`) or a repeat of `lastExternalEtag`. They never
 *   clear `gone`: archiving into a watched archive directory emits the rename and
 *   the archive-path create in either order.
 */
export function classifyNibEvent(
  prev: NibChangeState,
  event: RawNibEvent,
  selfEtag: string | null,
): NibChangeState {
  if (event.type === "deleted" || event.type === "archived") {
    if (prev.gone === event.type || prev.gone === "deleted") return prev;
    return { gone: event.type, external: null, lastExternalEtag: prev.lastExternalEtag };
  }

  if (event.type === "unarchived") {
    if (prev.gone === "deleted") return prev;
    const cleared: NibChangeState = prev.gone === null ? prev : { ...prev, gone: null };
    const nib = event.nib;
    // Nothing new to adopt, but still reopen.
    if (!nib || nib.etag === selfEtag) return cleared;
    const etag = nib.etag ?? "";
    if (nib.etag === prev.lastExternalEtag && etag !== "") return cleared;
    return { gone: null, external: toNibSnapshot(nib), lastExternalEtag: etag };
  }

  const nib = event.nib;
  if (!nib) return prev;

  // Self-echo of our own save.
  if (nib.etag === selfEtag) return prev;

  const etag = nib.etag ?? "";
  // The same non-empty revision emitted again.
  if (nib.etag === prev.lastExternalEtag && etag !== "") return prev;

  return {
    gone: prev.gone,
    external: toNibSnapshot(nib),
    lastExternalEtag: etag,
  };
}
