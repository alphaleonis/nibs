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

export type NibChangeType = "created" | "updated" | "deleted" | "archived" | "unarchived";

/**
 * Why the viewed nib is no longer at its old location. The two causes are NOT
 * interchangeable: a deleted nib is gone, while an archived one still exists at
 * its archive path and still accepts writes. The presenter keys savability off
 * this, so collapsing them back to a boolean silently forecloses a real save.
 */
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
  /** Why the viewed nib left its location, or null while it is still there
   *  (resets on nib change). */
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
    tags: nib.tags ?? [],
    body: nib.body ?? "",
    etag: nib.etag ?? "",
  };
}

/**
 * Reduce a subscription event onto the previous state.
 *
 * - `deleted` / `archived` → `{ gone: <reason>, external: null }` (returns `prev`
 *   when the reason would not change, so it is reference-stable). The reason is
 *   carried rather than collapsed to a flag: an archived nib still exists and
 *   still accepts a save, a deleted one does not. A deletion is terminal —
 *   nothing downgrades `deleted` back to `archived` — matching the view
 *   reducer's contract in `activeView.ts`.
 * - `unarchived` reopens the view: the nib is back at its main path, so `gone` is
 *   CLEARED (unless it is `deleted` — a deletion is terminal and the watcher never
 *   emits an unarchive for a file that no longer exists). The clear happens
 *   independent of the etag dedup below: archiving/unarchiving is a pure move that
 *   leaves the content etag unchanged, so routing the reopen through the dedup
 *   would swallow the very event that must take the banner down (nibs-2fgz). A
 *   fresh snapshot rides along only when it is genuinely new (not a self-echo or a
 *   duplicate revision).
 * - `created` / `updated` collapse to the same path: build a snapshot UNLESS
 *   (a) it is a self-echo (`nib.etag === selfEtag`) or (b) it duplicates the last
 *   external etag (`nib.etag === prev.lastExternalEtag && etag !== ""`); either
 *   case returns `prev` unchanged. A missing payload also returns `prev`. Such an
 *   event never clears `gone`: archiving into a WATCHED archive directory emits
 *   the rename and the archive-path create in one batch, in either order, and
 *   neither ordering may leave the buffer looking live.
 */
export function classifyNibEvent(
  prev: NibChangeState,
  event: RawNibEvent,
  selfEtag: string | null,
): NibChangeState {
  if (event.type === "deleted" || event.type === "archived") {
    // A deletion is terminal, so only an unchanged reason or a late `archived`
    // over an already-`deleted` nib is a no-op.
    if (prev.gone === event.type || prev.gone === "deleted") return prev;
    return { gone: event.type, external: null, lastExternalEtag: prev.lastExternalEtag };
  }

  if (event.type === "unarchived") {
    // Deletion is terminal — nothing resurrects a deleted nib.
    if (prev.gone === "deleted") return prev;
    // Reference-stable clear of `gone` (no-op when it was already null). Kept
    // separate from the snapshot adoption below so the reopen never rides on the
    // etag dedup — a move leaves the content etag unchanged, so an unarchive
    // routed through the dedup would be swallowed and the banner would stick.
    const cleared: NibChangeState = prev.gone === null ? prev : { ...prev, gone: null };
    const nib = event.nib;
    // Self-echo, missing payload, or a duplicate revision: nothing fresh to
    // adopt, but STILL reopen the view.
    if (!nib || nib.etag === selfEtag) return cleared;
    const etag = nib.etag ?? "";
    if (nib.etag === prev.lastExternalEtag && etag !== "") return cleared;
    // A genuinely new revision arrived with the unarchive: adopt it AND reopen.
    return { gone: null, external: toNibSnapshot(nib), lastExternalEtag: etag };
  }

  const nib = event.nib;
  if (!nib) return prev;

  // (a) Self-echo: our own save reflected back — ignore.
  if (nib.etag === selfEtag) return prev;

  const etag = nib.etag ?? "";
  // (b) Dedup: the same non-empty revision emitted more than once.
  if (nib.etag === prev.lastExternalEtag && etag !== "") return prev;

  return {
    gone: prev.gone,
    external: toNibSnapshot(nib),
    lastExternalEtag: etag,
  };
}
