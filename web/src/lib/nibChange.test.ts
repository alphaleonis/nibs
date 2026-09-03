import { describe, it, expect } from "vitest";
import { print } from "graphql";
import {
  classifyNibEvent,
  toNibSnapshot,
  initialNibChangeState,
  type NibChangeState,
  type RawNibEvent,
  type RawNibPayload,
} from "./nibChange";
import { NIB_CHANGED_SUBSCRIPTION } from "./queries";

// A committed payload as it arrives over the subscription (nullable optional fields).
function makePayload(overrides: Partial<RawNibPayload> = {}): RawNibPayload {
  return {
    id: "nibs-abc1",
    title: "Title",
    status: "todo",
    type: "task",
    priority: "high",
    estimate: "M",
    milestone: "nibs-m1",
    tags: ["one", "two"],
    body: "Body text",
    etag: "etag-1",
    ...overrides,
  };
}

function updatedEvent(nib: RawNibPayload): RawNibEvent {
  return { type: "updated", nibId: nib.id, nib };
}

describe("toNibSnapshot", () => {
  it("maps a full payload straight through (incl. id)", () => {
    const snap = toNibSnapshot(makePayload());
    expect(snap).toEqual({
      id: "nibs-abc1",
      title: "Title",
      status: "todo",
      type: "task",
      priority: "high",
      estimate: "M",
      milestone: "nibs-m1",
      tags: ["one", "two"],
      body: "Body text",
      etag: "etag-1",
    });
  });

  it("normalizes null priority/estimate/milestone/tags/body/etag to empty defaults", () => {
    const snap = toNibSnapshot(
      makePayload({ priority: null, estimate: null, milestone: null, tags: null, body: null, etag: null }),
    );
    expect(snap.milestone).toBe("");
    expect(snap.priority).toBe("");
    expect(snap.estimate).toBe("");
    expect(snap.tags).toEqual([]);
    expect(snap.body).toBe("");
    expect(snap.etag).toBe("");
  });
});

describe("classifyNibEvent", () => {
  it("builds an external snapshot for an updated event (no self match)", () => {
    const next = classifyNibEvent(initialNibChangeState, updatedEvent(makePayload()), null);
    expect(next).not.toBe(initialNibChangeState);
    expect(next.gone).toBeNull();
    expect(next.external).toEqual(toNibSnapshot(makePayload()));
    expect(next.lastExternalEtag).toBe("etag-1");
  });

  it("treats a created event exactly like an updated event", () => {
    const created: RawNibEvent = { type: "created", nibId: "nibs-abc1", nib: makePayload() };
    const next = classifyNibEvent(initialNibChangeState, created, null);
    expect(next.external).toEqual(toNibSnapshot(makePayload()));
    expect(next.lastExternalEtag).toBe("etag-1");
  });

  it("suppresses a self-echo (nib.etag === selfEtag) by returning prev unchanged", () => {
    const prev: NibChangeState = { gone: null, external: null, lastExternalEtag: null };
    const next = classifyNibEvent(prev, updatedEvent(makePayload({ etag: "mine" })), "mine");
    expect(next).toBe(prev); // reference-stable no-op
  });

  it("dedupes a repeated external etag by returning prev unchanged", () => {
    const first = classifyNibEvent(initialNibChangeState, updatedEvent(makePayload()), null);
    // Same etag arriving again — a duplicate emission of the same revision.
    const second = classifyNibEvent(first, updatedEvent(makePayload()), null);
    expect(second).toBe(first); // reference-stable no-op
  });

  it("treats a null-etag event as a self-echo when selfEtag is also null", () => {
    // Documented edge: rule (a) is a literal `nib.etag === selfEtag`, so null === null
    // suppresses. This is not widened by the model — it matches the legacy editor.
    const next = classifyNibEvent(initialNibChangeState, updatedEvent(makePayload({ etag: null })), null);
    expect(next).toBe(initialNibChangeState);
  });

  it("does NOT dedupe an empty etag (empty-etag corner is not widened)", () => {
    // A non-null selfEtag keeps the null-etag event off the self-echo path so the
    // dedup guard (`etag !== ""`) is exercised directly.
    const first = classifyNibEvent(initialNibChangeState, updatedEvent(makePayload({ etag: null })), "mine");
    expect(first.external).not.toBeNull();
    expect(first.lastExternalEtag).toBe("");
    const second = classifyNibEvent(first, updatedEvent(makePayload({ etag: null })), "mine");
    // An empty etag never equals a stored empty lastExternalEtag under the guard,
    // so a fresh snapshot is produced (a real change, not a dedup).
    expect(second).not.toBe(first);
  });

  it("marks deleted and clears external on a deleted event", () => {
    const withExternal = classifyNibEvent(initialNibChangeState, updatedEvent(makePayload()), null);
    const deleted: RawNibEvent = { type: "deleted", nibId: "nibs-abc1", nib: null };
    const next = classifyNibEvent(withExternal, deleted, null);
    expect(next.gone).toBe("deleted");
    expect(next.external).toBeNull();
  });

  it("returns prev unchanged when already deleted (reference-stable)", () => {
    const deletedEvent: RawNibEvent = { type: "deleted", nibId: "nibs-abc1", nib: null };
    const first = classifyNibEvent(initialNibChangeState, deletedEvent, null);
    const second = classifyNibEvent(first, deletedEvent, null);
    expect(second).toBe(first); // reference-stable no-op
  });

  it("marks gone with reason 'archived' and clears external on an archived event", () => {
    const withExternal = classifyNibEvent(initialNibChangeState, updatedEvent(makePayload()), null);
    const archived: RawNibEvent = { type: "archived", nibId: "nibs-abc1", nib: makePayload() };
    const next = classifyNibEvent(withExternal, archived, null);
    // An archived nib still EXISTS, at its archive path — the reason is what the
    // presenter needs to keep Save on offer for it.
    expect(next.gone).toBe("archived");
    expect(next.external).toBeNull();
  });

  it("distinguishes archived from deleted", () => {
    const archived: RawNibEvent = { type: "archived", nibId: "nibs-abc1", nib: makePayload() };
    const deleted: RawNibEvent = { type: "deleted", nibId: "nibs-abc1", nib: null };
    expect(classifyNibEvent(initialNibChangeState, archived, null).gone).toBe("archived");
    expect(classifyNibEvent(initialNibChangeState, deleted, null).gone).toBe("deleted");
  });

  it("returns prev unchanged when already gone for the same reason (reference-stable)", () => {
    // Distinct etags deliberately: an identical payload would be swallowed by the
    // etag dedup, so the no-op would prove nothing about the `gone` branch.
    const first = classifyNibEvent(
      initialNibChangeState,
      { type: "archived", nibId: "nibs-abc1", nib: makePayload({ etag: "e1" }) },
      null,
    );
    const second = classifyNibEvent(
      first,
      { type: "archived", nibId: "nibs-abc1", nib: makePayload({ etag: "e2" }) },
      null,
    );
    expect(second).toBe(first); // reference-stable no-op
  });

  it("lets a deletion supersede an archive (an archived nib can still be deleted)", () => {
    const archived: RawNibEvent = { type: "archived", nibId: "nibs-abc1", nib: makePayload() };
    const deleted: RawNibEvent = { type: "deleted", nibId: "nibs-abc1", nib: null };
    const first = classifyNibEvent(initialNibChangeState, archived, null);
    const second = classifyNibEvent(first, deleted, null);
    expect(second.gone).toBe("deleted");
  });

  it("treats a deletion as terminal: a later archived event does not downgrade it", () => {
    // The mirror of the supersede rule above, and the same contract the view
    // reducer states. A downgrade would re-offer a save against a nib that no
    // longer exists, so the classifier must refuse it on its own rather than
    // leaning on the reducer one layer up to catch it.
    const deleted: RawNibEvent = { type: "deleted", nibId: "nibs-abc1", nib: null };
    // A distinct etag: an identical payload would be swallowed by the etag
    // dedup, so the no-op would prove nothing about the `gone` branch.
    const archived: RawNibEvent = {
      type: "archived",
      nibId: "nibs-abc1",
      nib: makePayload({ etag: "e2" }),
    };
    const first = classifyNibEvent(initialNibChangeState, deleted, null);
    const second = classifyNibEvent(first, archived, null);
    expect(second.gone).toBe("deleted");
    expect(second).toBe(first); // reference-stable no-op
  });

  it("does not clear `gone` when a later updated event arrives", () => {
    // Order-independence: archiving a nib into a WATCHED archive directory emits
    // both `archived` (the rename) and `updated` (the create at the archive path),
    // and the batch may carry them either way round. Neither ordering may leave
    // the buffer looking live.
    const archived: RawNibEvent = { type: "archived", nibId: "nibs-abc1", nib: makePayload() };
    const afterArchive = classifyNibEvent(initialNibChangeState, archived, null);
    const afterUpdate = classifyNibEvent(afterArchive, updatedEvent(makePayload({ etag: "e2" })), null);
    expect(afterUpdate.gone).toBe("archived");
  });

  it("clears `gone` when an unarchived event arrives (the unarchive reopens the view)", () => {
    // The inverse of the archive-then-update case above: an external UNARCHIVE
    // brings the nib back to its main path, so the classifier must drop the stale
    // "archived" banner — the created/updated family's `gone`-preserving rule is
    // the wrong one for the one event that genuinely means "it's live again".
    const archived: RawNibEvent = { type: "archived", nibId: "nibs-abc1", nib: makePayload() };
    const afterArchive = classifyNibEvent(initialNibChangeState, archived, null);
    expect(afterArchive.gone).toBe("archived");

    const unarchived: RawNibEvent = {
      type: "unarchived",
      nibId: "nibs-abc1",
      nib: makePayload({ etag: "e2", title: "Back" }),
    };
    const afterUnarchive = classifyNibEvent(afterArchive, unarchived, null);
    expect(afterUnarchive.gone).toBeNull();
    expect(afterUnarchive.external).toEqual(toNibSnapshot(makePayload({ etag: "e2", title: "Back" })));
    expect(afterUnarchive.lastExternalEtag).toBe("e2");
  });

  it("clears `gone` on unarchive even when the etag is unchanged (a move keeps the content etag)", () => {
    // Archiving/unarchiving is a pure file move: the content — and thus the FNV
    // etag of Render() — is unchanged. The unarchive therefore carries the SAME
    // etag the last `updated` recorded, so reopening the view must NOT be gated by
    // the etag dedup, or the banner sticks until reload (the exact nibs-2fgz bug).
    const seeded = classifyNibEvent(
      initialNibChangeState,
      updatedEvent(makePayload({ etag: "same" })),
      null,
    );
    expect(seeded.lastExternalEtag).toBe("same");
    const archived: RawNibEvent = {
      type: "archived",
      nibId: "nibs-abc1",
      nib: makePayload({ etag: "same" }),
    };
    const afterArchive = classifyNibEvent(seeded, archived, null);
    expect(afterArchive.gone).toBe("archived");
    // Unarchive carries the same "same" etag as the pre-archive revision.
    const unarchived: RawNibEvent = {
      type: "unarchived",
      nibId: "nibs-abc1",
      nib: makePayload({ etag: "same" }),
    };
    const afterUnarchive = classifyNibEvent(afterArchive, unarchived, null);
    expect(afterUnarchive.gone).toBeNull();
  });

  it("does not resurrect a deleted nib on an unarchive (deletion is terminal)", () => {
    // The watcher never emits an unarchive for a file that no longer exists, but
    // the classifier must refuse the downgrade on its own so an out-of-order
    // stream can never re-offer a save against a deleted nib.
    const deleted: RawNibEvent = { type: "deleted", nibId: "nibs-abc1", nib: null };
    const first = classifyNibEvent(initialNibChangeState, deleted, null);
    const unarchived: RawNibEvent = {
      type: "unarchived",
      nibId: "nibs-abc1",
      nib: makePayload({ etag: "e2" }),
    };
    const second = classifyNibEvent(first, unarchived, null);
    expect(second.gone).toBe("deleted");
    expect(second).toBe(first); // reference-stable no-op
  });

  it("returns prev unchanged for a created/updated event with a missing payload", () => {
    const prev: NibChangeState = { gone: null, external: null, lastExternalEtag: null };
    const next = classifyNibEvent(prev, { type: "updated", nibId: "nibs-abc1", nib: null }, null);
    expect(next).toBe(prev);
  });

  it("produces a fresh external when a genuinely new etag arrives after a prior one", () => {
    const first = classifyNibEvent(initialNibChangeState, updatedEvent(makePayload({ etag: "e1" })), null);
    const second = classifyNibEvent(first, updatedEvent(makePayload({ etag: "e2", title: "New" })), null);
    expect(second).not.toBe(first);
    expect(second.external?.title).toBe("New");
    expect(second.lastExternalEtag).toBe("e2");
  });
});

describe("NIB_CHANGED_SUBSCRIPTION field coverage guard", () => {
  // Every field toNibSnapshot reads must be present in the subscription's `nib`
  // selection, or a dropped field would be silently defaulted at the boundary.
  const NIB_SNAPSHOT_FIELDS = [
    "id",
    "title",
    "status",
    "type",
    "priority",
    "estimate",
    "milestone",
    "tags",
    "body",
    "etag",
  ] as const;

  // NIB_CHANGED_SUBSCRIPTION is now a generated TypedDocumentNode whose serialized
  // AST carries no `.loc`; reconstruct the query text from the AST via `print`.
  const source = print(NIB_CHANGED_SUBSCRIPTION);
  // Scope the check to the `nib { ... }` sub-selection only. `id` and `type` also
  // appear elsewhere in the document (the `$id` operation var, `nibChanged(id: $id)`,
  // and the sibling top-level `type` field), so matching the whole source would let a
  // drop from inside `nib { }` pass unnoticed — defeating the guard. The nib selection
  // has no nested braces, so `[^}]*` captures it up to its closing brace.
  const nibBlock = source.match(/nib\s*\{([^}]*)\}/)?.[1] ?? "";

  it.each(NIB_SNAPSHOT_FIELDS)("selects the %s field", (field) => {
    expect(nibBlock).toMatch(new RegExp(`\\b${field}\\b`));
  });
});
