import { describe, it, expect, vi, afterEach } from "vitest";
import { flushSync } from "svelte";
import { writable, type Writable } from "svelte/store";
import { useTableData, type UseTableDataOptions } from "./useTableData.svelte";

// --- Fake urql stores -------------------------------------------------------
// The adapter reaches urql only through the injected `queryStore` /
// `subscriptionStore` seams (mirroring liveNib.svelte.ts), so tests drive it
// with plain writables — no @urql mock, no jsdom.

interface QueryStoreValue {
  data?: { nibs?: Array<{ id: string }> } | null;
  error?: unknown;
  fetching: boolean;
}

type FakeQueryStore = NonNullable<UseTableDataOptions["queryStore"]>;
type FakeSubStore = NonNullable<UseTableDataOptions["subscriptionStore"]>;

function makeFakeQuery() {
  const calls: Array<{ variables: { filter?: unknown } }> = [];
  const stores: Array<Writable<QueryStoreValue> & { reexecute: ReturnType<typeof vi.fn> }> = [];
  const queryStore = ((args: { variables: { filter?: unknown } }) => {
    calls.push({ variables: args.variables });
    const store = writable<QueryStoreValue>({
      fetching: false,
      error: undefined,
      data: { nibs: [] },
    });
    const withReexecute = Object.assign(store, { reexecute: vi.fn() });
    stores.push(withReexecute);
    return withReexecute;
  }) as unknown as FakeQueryStore;
  return { calls, stores, queryStore };
}

interface SubStoreValue {
  data?: { nibChanged?: { type: string; nibId: string; nib?: { etag?: string | null } | null } | null } | undefined;
  error?: unknown;
  fetching?: boolean;
}

function makeFakeSub() {
  // A writable that tracks live subscriber count so teardown can be asserted.
  let subscribers = 0;
  const store = writable<SubStoreValue>({ data: undefined });
  const wrapped = {
    subscribe(run: (v: SubStoreValue) => void) {
      subscribers++;
      const unsub = store.subscribe(run);
      return () => {
        subscribers--;
        unsub();
      };
    },
  };
  const subscriptionStore = (() => wrapped) as unknown as FakeSubStore;
  return {
    subscriptionStore,
    push: (v: SubStoreValue) => store.set(v),
    get subscribers() {
      return subscribers;
    },
  };
}

function withRoot(fn: () => void): () => void {
  return $effect.root(fn);
}

const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

afterEach(() => {
  errorSpy.mockClear();
});

describe("useTableData", () => {
  it("re-keys the query when the server filter changes", () => {
    const q = makeFakeQuery();
    const sub = makeFakeSub();
    let filter = $state<{ search?: string }>({ search: "a" });
    let view!: ReturnType<typeof useTableData>;

    const dispose = withRoot(() => {
      view = useTableData({
        client: {} as never,
        getServerFilter: () => filter,
        queryStore: q.queryStore,
        subscriptionStore: sub.subscriptionStore,
      });
    });
    flushSync();

    expect(q.calls).toHaveLength(1);
    expect(q.calls[0].variables.filter).toEqual({ search: "a" });

    // The current query store's value flows through to the passthroughs.
    q.stores[0].set({ fetching: false, error: undefined, data: { nibs: [{ id: "nibs-1" }] } });
    flushSync();
    expect(view.allNibs).toEqual([{ id: "nibs-1" }]);
    expect(view.fetching).toBe(false);

    // Changing the filter re-keys: a fresh query store is created for it.
    filter = { search: "b" };
    flushSync();
    expect(q.calls).toHaveLength(2);
    expect(q.calls[1].variables.filter).toEqual({ search: "b" });

    dispose();
  });

  it("routes a subscription change event into the core → refetches the current query", () => {
    const q = makeFakeQuery();
    const sub = makeFakeSub();
    let view!: ReturnType<typeof useTableData>;

    const dispose = withRoot(() => {
      view = useTableData({
        client: {} as never,
        getServerFilter: () => ({}),
        queryStore: q.queryStore,
        subscriptionStore: sub.subscriptionStore,
      });
    });
    flushSync();
    void view.allNibs;

    // A non-delete event refetches the live query store immediately.
    sub.push({ data: { nibChanged: { type: "created", nibId: "nibs-new" } } });
    flushSync();
    expect(q.stores[0].reexecute).toHaveBeenCalledTimes(1);
    expect(q.stores[0].reexecute).toHaveBeenCalledWith({ requestPolicy: "network-only" });

    // A burst of identical emissions coalesces (dedup lives in the core).
    for (let i = 0; i < 5; i++) {
      sub.push({ data: { nibChanged: { type: "created", nibId: "nibs-new" } } });
      flushSync();
    }
    expect(q.stores[0].reexecute).toHaveBeenCalledTimes(1);

    dispose();
  });

  it("surfaces the highlight state through `changed` after a change event", () => {
    const q = makeFakeQuery();
    const sub = makeFakeSub();
    let view!: ReturnType<typeof useTableData>;

    const dispose = withRoot(() => {
      view = useTableData({
        client: {} as never,
        getServerFilter: () => ({}),
        queryStore: q.queryStore,
        subscriptionStore: sub.subscriptionStore,
      });
    });
    flushSync();

    expect(view.changed.isHighlighted("nibs-h")).toBe(false);

    sub.push({ data: { nibChanged: { type: "updated", nibId: "nibs-h", nib: { etag: "e1" } } } });
    flushSync();

    // The adapter-side change tracker highlights the updated nib.
    expect(view.changed.isHighlighted("nibs-h")).toBe(true);

    dispose();
  });

  it("defers a delete's refetch and clears it on teardown (no ghost refetch, unsubscribes)", () => {
    const q = makeFakeQuery();
    const sub = makeFakeSub();
    let view!: ReturnType<typeof useTableData>;

    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      const dispose = withRoot(() => {
        view = useTableData({
          client: {} as never,
          getServerFilter: () => ({}),
          queryStore: q.queryStore,
          subscriptionStore: sub.subscriptionStore,
        });
      });
      flushSync();
      void view.allNibs;
      expect(sub.subscribers).toBe(1); // subscription is live

      // A delete defers its refetch — nothing fires synchronously.
      sub.push({ data: { nibChanged: { type: "deleted", nibId: "nibs-d" } } });
      flushSync();
      expect(q.stores[0].reexecute).not.toHaveBeenCalled();

      // Tear down inside the fade window, then let the deferred deadline pass.
      dispose();
      expect(sub.subscribers).toBe(0); // effect cleanup unsubscribed
      vi.advanceTimersByTime(1000);
      expect(q.stores[0].reexecute).not.toHaveBeenCalled(); // timer was cleared
    } finally {
      vi.useRealTimers();
    }
  });

  it("routes a subscription error into the core (reported, not thrown)", () => {
    const q = makeFakeQuery();
    const sub = makeFakeSub();

    const dispose = withRoot(() => {
      useTableData({
        client: {} as never,
        getServerFilter: () => ({}),
        queryStore: q.queryStore,
        subscriptionStore: sub.subscriptionStore,
      });
    });
    flushSync();

    const err = new Error("stream down");
    expect(() => {
      sub.push({ data: undefined, error: err });
      flushSync();
    }).not.toThrow();
    expect(errorSpy).toHaveBeenCalledWith("Nib subscription error:", err);

    dispose();
  });
});
