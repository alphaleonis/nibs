/**
 * Pure classifier for server-side nib-change subscription events.
 *
 * No urql, no runes, no DOM — every rule here is a plain function, unit-testable
 * with direct calls. The reactive binder (`liveNib.svelte.ts`) drives this
 * reducer; the components own the resulting banners/highlights.
 *
 * Reference-stability contract: `classifyNibEvent` returns `prev` UNCHANGED
 * (same reference) on any no-op, and a fresh object only on a real change. The
 * binder relies on this — `state = classifyNibEvent(state, …)` becomes a `$state`
 * `!==` no-op on duplicates/self-echoes, so reactivity fires exactly once per
 * real external change (the old `lastSubData` guard becomes structural).
 */

import type { NibSnapshot } from "./nibForm.svelte";

export type NibChangeType = "created" | "updated" | "deleted";

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
  // Present in the subscription selection but not part of NibSnapshot; kept
  // optional so callers may read them without widening the mapper.
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
  /** The viewed nib was deleted on the server (resets on nib change). */
  deleted: boolean;
  /** Latest non-self, de-duped external snapshot (null until one arrives). */
  external: NibSnapshot | null;
  /** Etag of the last external snapshot surfaced — used for dedup. */
  lastExternalEtag: string | null;
}

export const initialNibChangeState: NibChangeState = {
  deleted: false,
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
    tags: nib.tags ?? [],
    body: nib.body ?? "",
    etag: nib.etag ?? "",
  };
}

/**
 * Reduce a subscription event onto the previous state.
 *
 * - `deleted` → `{ deleted: true, external: null }` (returns `prev` when already
 *   deleted, so it is reference-stable).
 * - `created` / `updated` collapse to the same path: build a snapshot UNLESS
 *   (a) it is a self-echo (`nib.etag === selfEtag`) or (b) it duplicates the last
 *   external etag (`nib.etag === prev.lastExternalEtag && etag !== ""`); either
 *   case returns `prev` unchanged. A missing payload also returns `prev`.
 */
export function classifyNibEvent(
  prev: NibChangeState,
  event: RawNibEvent,
  selfEtag: string | null,
): NibChangeState {
  if (event.type === "deleted") {
    if (prev.deleted) return prev;
    return { deleted: true, external: null, lastExternalEtag: prev.lastExternalEtag };
  }

  const nib = event.nib;
  if (!nib) return prev;

  // (a) Self-echo: our own save reflected back — ignore.
  if (nib.etag === selfEtag) return prev;

  const etag = nib.etag ?? "";
  // (b) Dedup: the same non-empty revision emitted more than once.
  if (nib.etag === prev.lastExternalEtag && etag !== "") return prev;

  return {
    deleted: prev.deleted,
    external: toNibSnapshot(nib),
    lastExternalEtag: etag,
  };
}
