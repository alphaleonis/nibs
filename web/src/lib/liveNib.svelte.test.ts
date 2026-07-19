import { describe, it, expect, vi, afterEach } from "vitest";
import { flushSync } from "svelte";
import { writable, type Writable } from "svelte/store";
import { createLiveNib, type LiveNibOptions } from "./liveNib.svelte";
import type { RawNibEvent } from "./nibChange";

// --- Fake subscriptionStore -------------------------------------------------
// Each call returns a fresh writable so a test can push events into the store
// belonging to a specific subscription (re-subscribe scenarios).

interface SubResult {
  data?: { nibChanged?: RawNibEvent } | undefined;
  error?: unknown;
  fetching?: boolean;
}

type FakeSubStore = NonNullable<LiveNibOptions["subscriptionStore"]>;

function makeFakeSub() {
  const stores: Array<Writable<SubResult>> = [];
  const calls: Array<{ variables: { id?: unknown } }> = [];
  // Cast through `unknown`: urql's `subscriptionStore` is heavily generic and we
  // only exercise `{ variables }` + the readable `.subscribe` contract here.
  const subscriptionStore = ((args: { variables: { id?: unknown } }) => {
    calls.push({ variables: args.variables });
    const store = writable<SubResult>({ data: undefined });
    stores.push(store);
    return store;
  }) as unknown as FakeSubStore;
  return { stores, calls, subscriptionStore };
}

function payload(overrides: Record<string, unknown> = {}) {
  return {
    id: "nibs-abc1",
    title: "Title",
    status: "todo",
    type: "task",
    priority: "high",
    estimate: "M",
    tags: ["one"],
    body: "Body",
    etag: "etag-1",
    ...overrides,
  };
}

// Run a body inside a fresh effect root; returns a disposer to tear it down.
function withRoot(fn: () => void): () => void {
  return $effect.root(fn);
}

const warnSpy = vi.spyOn(console, "warn").mockImplementation(() => {});

afterEach(() => {
  warnSpy.mockClear();
});

describe("createLiveNib", () => {
  it("surfaces an external snapshot when the subscription emits an update", () => {
    const fake = makeFakeSub();
    let live!: ReturnType<typeof createLiveNib>;

    const dispose = withRoot(() => {
      live = createLiveNib({
        client: {} as never,
        nibId: () => "nibs-abc1",
        selfEtag: () => "local-etag",
        subscriptionStore: fake.subscriptionStore,
      });
    });
    flushSync();

    expect(fake.calls).toHaveLength(1);
    expect(fake.calls[0].variables.id).toBe("nibs-abc1");
    expect(live.external).toBeNull();

    fake.stores[0].set({ data: { nibChanged: { type: "updated", nibId: "nibs-abc1", nib: payload() as never } } });
    flushSync();

    expect(live.external).not.toBeNull();
    expect(live.external?.title).toBe("Title");
    expect(live.external?.etag).toBe("etag-1");
    expect(live.gone).toBeNull();

    dispose();
  });

  it("sets gone: 'deleted' when the subscription emits a delete", () => {
    const fake = makeFakeSub();
    let live!: ReturnType<typeof createLiveNib>;

    const dispose = withRoot(() => {
      live = createLiveNib({
        client: {} as never,
        nibId: () => "nibs-abc1",
        selfEtag: () => undefined,
        subscriptionStore: fake.subscriptionStore,
      });
    });
    flushSync();

    fake.stores[0].set({ data: { nibChanged: { type: "deleted", nibId: "nibs-abc1", nib: null } } });
    flushSync();

    expect(live.gone).toBe("deleted");
    expect(live.external).toBeNull();

    dispose();
  });

  it("sets gone: 'archived' when the subscription emits an archive", () => {
    const fake = makeFakeSub();
    let live!: ReturnType<typeof createLiveNib>;

    const dispose = withRoot(() => {
      live = createLiveNib({
        client: {} as never,
        nibId: () => "nibs-abc1",
        selfEtag: () => undefined,
        subscriptionStore: fake.subscriptionStore,
      });
    });
    flushSync();

    // The wire carries the nib for an archive (it still exists), unlike a delete.
    fake.stores[0].set({
      data: {
        nibChanged: { type: "archived", nibId: "nibs-abc1", nib: payload() as never },
      },
    });
    flushSync();

    // Reported as its own reason, never collapsed into "deleted" — the presenter
    // keys savability off this.
    expect(live.gone).toBe("archived");

    dispose();
  });

  it("suppresses a self-echo read LIVE from selfEtag at event time", () => {
    const fake = makeFakeSub();
    let currentEtag = "e0";
    let live!: ReturnType<typeof createLiveNib>;

    const dispose = withRoot(() => {
      live = createLiveNib({
        client: {} as never,
        nibId: () => "nibs-abc1",
        selfEtag: () => currentEtag,
        subscriptionStore: fake.subscriptionStore,
      });
    });
    flushSync();

    // After a local save the etag advances to "e1"; the echo of that save must be
    // suppressed using the LIVE etag, without re-subscribing.
    currentEtag = "e1";
    fake.stores[0].set({ data: { nibChanged: { type: "updated", nibId: "nibs-abc1", nib: payload({ etag: "e1" }) as never } } });
    flushSync();

    expect(live.external).toBeNull();
    expect(fake.calls).toHaveLength(1); // no re-subscribe from the etag change

    // A genuinely external change (different etag) still comes through.
    fake.stores[0].set({ data: { nibChanged: { type: "updated", nibId: "nibs-abc1", nib: payload({ etag: "e2", title: "Theirs" }) as never } } });
    flushSync();
    expect(live.external?.title).toBe("Theirs");

    dispose();
  });

  it("re-subscribes and resets `gone` when nibId changes", () => {
    const fake = makeFakeSub();
    // $state so a reassignment actually re-runs the binder's effect (a plain
    // `let` would not notify Svelte, mirroring how nibId flows as a reactive prop).
    let id = $state("nibs-abc1");
    let live!: ReturnType<typeof createLiveNib>;

    const dispose = withRoot(() => {
      live = createLiveNib({
        client: {} as never,
        nibId: () => id,
        selfEtag: () => undefined,
        subscriptionStore: fake.subscriptionStore,
      });
    });
    flushSync();

    fake.stores[0].set({ data: { nibChanged: { type: "deleted", nibId: "nibs-abc1", nib: null } } });
    flushSync();
    expect(live.gone).toBe("deleted");

    id = "nibs-xyz9";
    flushSync();

    expect(fake.calls).toHaveLength(2);
    expect(fake.calls[1].variables.id).toBe("nibs-xyz9");
    expect(live.gone).toBeNull(); // reset on nibId change
    expect(live.external).toBeNull();

    // The new subscription drives state independently.
    fake.stores[1].set({ data: { nibChanged: { type: "updated", nibId: "nibs-xyz9", nib: payload({ id: "nibs-xyz9", etag: "z1" }) as never } } });
    flushSync();
    expect(live.external?.id).toBe("nibs-xyz9");

    dispose();
  });

  it("does not open a subscription in create mode (undefined nibId)", () => {
    const fake = makeFakeSub();
    let id: string | undefined = $state(undefined);
    let live!: ReturnType<typeof createLiveNib>;

    const dispose = withRoot(() => {
      live = createLiveNib({
        client: {} as never,
        nibId: () => id,
        selfEtag: () => undefined,
        subscriptionStore: fake.subscriptionStore,
      });
    });
    flushSync();

    expect(fake.calls).toHaveLength(0); // no open
    expect(live.external).toBeNull();
    expect(live.gone).toBeNull();

    // Transitioning to an id opens the subscription.
    id = "nibs-abc1";
    flushSync();
    expect(fake.calls).toHaveLength(1);
    expect(fake.calls[0].variables.id).toBe("nibs-abc1");

    dispose();
  });

  it("captures and warns on subscription errors", () => {
    const fake = makeFakeSub();
    let live!: ReturnType<typeof createLiveNib>;

    const dispose = withRoot(() => {
      live = createLiveNib({
        client: {} as never,
        nibId: () => "nibs-abc1",
        selfEtag: () => undefined,
        subscriptionStore: fake.subscriptionStore,
      });
    });
    flushSync();

    const err = new Error("boom");
    fake.stores[0].set({ data: undefined, error: err });
    flushSync();

    expect(live.error).toBe(err);
    expect(warnSpy).toHaveBeenCalled();

    dispose();
  });

  it("dedupes repeated identical emissions (reference guard)", () => {
    const fake = makeFakeSub();
    let live!: ReturnType<typeof createLiveNib>;

    const dispose = withRoot(() => {
      live = createLiveNib({
        client: {} as never,
        nibId: () => "nibs-abc1",
        selfEtag: () => undefined,
        subscriptionStore: fake.subscriptionStore,
      });
    });
    flushSync();

    const evt: SubResult = { data: { nibChanged: { type: "updated", nibId: "nibs-abc1", nib: payload({ etag: "e1" }) as never } } };
    fake.stores[0].set(evt);
    flushSync();
    const firstExternal = live.external;
    expect(firstExternal).not.toBeNull();

    // Re-emitting the same etag must not produce a new external snapshot.
    fake.stores[0].set({ data: { nibChanged: { type: "updated", nibId: "nibs-abc1", nib: payload({ etag: "e1" }) as never } } });
    flushSync();
    expect(live.external).toBe(firstExternal);

    dispose();
  });
});
