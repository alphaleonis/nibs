import { describe, it, expect } from "vitest";
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
      tags: ["one", "two"],
      body: "Body text",
      etag: "etag-1",
    });
  });

  it("normalizes null priority/estimate/tags/body/etag to empty defaults", () => {
    const snap = toNibSnapshot(
      makePayload({ priority: null, estimate: null, tags: null, body: null, etag: null }),
    );
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
    expect(next.deleted).toBe(false);
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
    const prev: NibChangeState = { deleted: false, external: null, lastExternalEtag: null };
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
    // suppresses. This is not widened by the model — it matches EditorModal today.
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
    expect(next.deleted).toBe(true);
    expect(next.external).toBeNull();
  });

  it("returns prev unchanged when already deleted (reference-stable)", () => {
    const deletedEvent: RawNibEvent = { type: "deleted", nibId: "nibs-abc1", nib: null };
    const first = classifyNibEvent(initialNibChangeState, deletedEvent, null);
    const second = classifyNibEvent(first, deletedEvent, null);
    expect(second).toBe(first); // reference-stable no-op
  });

  it("returns prev unchanged for a created/updated event with a missing payload", () => {
    const prev: NibChangeState = { deleted: false, external: null, lastExternalEtag: null };
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
    "tags",
    "body",
    "etag",
  ] as const;

  const source = NIB_CHANGED_SUBSCRIPTION.loc?.source.body ?? "";
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
