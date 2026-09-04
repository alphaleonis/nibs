import { render } from "@testing-library/svelte";
import { tick } from "svelte";
import { describe, it, expect, vi, beforeEach } from "vitest";
import App from "./App.svelte";
import { CONFIG_QUERY, NIB_DETAIL_QUERY } from "./lib/queries";
import { prepareFilter } from "./lib/filter";
import type { ViewSpine } from "./lib/viewSpine";

// CONFIG_QUERY runs once, so a failed one costs the areas vocabulary for the
// rest of the session unless something re-asks: `withSendableArea` withholds
// every `area` against the "unknown" that UNAVAILABLE_AREAS answers, so the list
// silently widens to the whole store while the filter says otherwise.
//
// Two re-asks exist, and this file covers App's wiring of both. The socket one
// needs the socket seam, which is why this file mocks `./lib/graphql` and
// App.test.ts does not; the automatic one is `useLiveConfig`'s backoff, whose
// policy is tested at that unit and whose reaching the store is tested here.

const { socket, configStore } = await vi.hoisted(async () => {
  const { writable } = await import("svelte/store");
  return {
    // The hooks App hands `createClient`. Calling them is how a test plays the
    // socket dropping and coming back.
    socket: { hooks: null as { onConnected?: () => void; onClosed?: () => void } | null },
    // CONFIG_QUERY's store. `reexecute` is the assertion target AND a
    // requirement: App calls it, and a bare writable would throw.
    configStore: Object.assign(
      writable<{
        fetching: boolean;
        error: CombinedError | undefined;
        data: { config: Record<string, unknown> } | undefined;
        stale: boolean;
      }>({ fetching: false, error: undefined, data: undefined, stale: false }),
      { reexecute: vi.fn() },
    ),
  };
});

/** Settle CONFIG_QUERY to `config`. */
const setConfig = (config: Record<string, unknown>) =>
  configStore.set({ fetching: false, error: undefined, data: { config }, stale: false });

/** Settle CONFIG_QUERY to a failure: urql reports a network error with no data. */
const failConfig = () =>
  configStore.set({
    fetching: false,
    error: new CombinedError({ networkError: new Error("offline") }),
    data: undefined,
    stale: false,
  });

// App builds its own client, and the socket hooks it passes are the only way in.
vi.mock("./lib/graphql", async () => {
  const actual = await vi.importActual<typeof import("./lib/graphql")>("./lib/graphql");
  return {
    ...actual,
    createClient: (hooks: { onConnected?: () => void; onClosed?: () => void }) => {
      socket.hooks = hooks;
      return { client: { mutation: vi.fn() }, reconnect: vi.fn() };
    },
  };
});

const { spineGetters } = vi.hoisted(() => ({ spineGetters: [] as (() => ViewSpine)[] }));

vi.mock("./lib/contexts", async () => {
  const actual = await vi.importActual<typeof import("./lib/contexts")>("./lib/contexts");
  return {
    ...actual,
    provideViewSpine: (get: () => ViewSpine) => {
      spineGetters.push(get);
      actual.provideViewSpine(get);
    },
  };
});

vi.mock("@urql/svelte", async () => {
  const { readable } = await import("svelte/store");
  const actual = await vi.importActual<typeof import("@urql/svelte")>("@urql/svelte");

  const nibsData = Object.assign(
    readable({ fetching: false, error: undefined, data: { nibs: [] }, stale: false }),
    { reexecute: vi.fn() },
  );
  const detailData = readable({
    fetching: false,
    error: undefined,
    data: { nib: null },
    stale: false,
  });

  return {
    ...actual,
    getContextClient: vi.fn(),
    setContextClient: vi.fn(),
    queryStore: vi.fn().mockImplementation((opts: { query: unknown }) => {
      if (opts.query === CONFIG_QUERY) return configStore;
      if (opts.query === NIB_DETAIL_QUERY) return detailData;
      return nibsData;
    }),
    subscriptionStore: vi
      .fn()
      .mockReturnValue(readable({ fetching: false, error: undefined, data: undefined, stale: false })),
  };
});

import { CombinedError } from "@urql/svelte";
import { CONFIG_RETRY_DELAYS } from "./lib/composables/useLiveConfig.svelte";

/** The spine App provided on the most recent render. */
const currentSpine = (): ViewSpine => {
  const get = spineGetters.at(-1);
  if (!get) throw new Error("App did not provide a view spine");
  return get();
};

/** The socket hooks App handed `createClient` on the most recent render. */
const hooks = () => {
  if (!socket.hooks) throw new Error("App did not build a live client");
  return socket.hooks;
};

/** Drop the socket and bring it back — the sequence that fires `onRecovered`.
 *  The first connect is a cold one: there is no gap yet to have missed. */
const reconnectAfterGap = () => {
  hooks().onConnected?.();
  hooks().onClosed?.();
  hooks().onConnected?.();
};

const areas = [
  { path: "web", name: "web", description: "", color: "", depth: 0 },
  { path: "web/dashboard", name: "dashboard", description: "", color: "", depth: 1 },
];

describe("App areas vocabulary recovery", () => {
  beforeEach(() => {
    window.history.replaceState(null, "", "/");
    document.documentElement.classList.remove("dark");
    delete document.documentElement.dataset.theme;
    spineGetters.length = 0;
    socket.hooks = null;
    configStore.reexecute.mockClear();
    failConfig();
  });

  it("re-asks the failed config query when the socket returns, and the area filter re-applies", async () => {
    render(App);

    // The failure state: no vocabulary, so the value is withheld and the list
    // the server answers is a superset of what the filter asked for.
    expect(currentSpine().areas.status).toBe("unavailable");
    expect(prepareFilter({ area: "web" }, currentSpine().areas).serverFilter).not.toHaveProperty(
      "area",
    );

    hooks().onConnected?.();
    expect(configStore.reexecute).not.toHaveBeenCalled();

    hooks().onClosed?.();
    hooks().onConnected?.();
    expect(configStore.reexecute).toHaveBeenCalledWith({ requestPolicy: "network-only" });

    // The re-ask lands.
    setConfig({ projectName: "test-project", areas });
    await tick();

    expect(currentSpine().areas.status).toBe("ready");
    expect(currentSpine().areas.validity("web")).toBe("declared");
    expect(prepareFilter({ area: "web" }, currentSpine().areas).serverFilter.area).toBe("web");
  });

  // The re-ask used to be gated on holding NO vocabulary, because a failed
  // re-ask took away the one in hand. `useLiveConfig` latches the last good
  // config, so the gate is gone — and it has to be, or a vocabulary edited while
  // the socket was down is never picked up (the gap left open by nibs-5cuk).
  it("re-asks on a reconnect even with a vocabulary already in hand", async () => {
    setConfig({ projectName: "test-project", areas });
    render(App);
    expect(currentSpine().areas.status).toBe("ready");

    reconnectAfterGap();
    await tick();

    expect(configStore.reexecute).toHaveBeenCalledWith({ requestPolicy: "network-only" });
  });

  // The half that makes the un-gating safe. urql drops `data` on a failed
  // network-only re-execution, so without the latch this reconnect would end
  // with the session holding no vocabulary at all — strictly worse than the
  // staleness the re-ask exists to clear.
  it("keeps the vocabulary when the reconnect's own re-ask fails", async () => {
    setConfig({ projectName: "test-project", areas });
    render(App);

    reconnectAfterGap();
    failConfig();
    await tick();

    expect(currentSpine().areas.status).toBe("ready");
    expect(currentSpine().areas.validity("web")).toBe("declared");
    expect(prepareFilter({ area: "web" }, currentSpine().areas).serverFilter.area).toBe("web");
  });

  // nibs-zwnm: a config query that fails while the socket stays healthy fires no
  // `onRecovered` at all — queries travel over HTTP and only subscriptions use
  // the socket — so before the backoff nothing ever re-asked.
  it("re-asks a config query that failed with the socket healthy, with no socket event", async () => {
    vi.useFakeTimers();
    try {
      render(App);
      hooks().onConnected?.(); // healthy, and it stays that way
      expect(currentSpine().areas.status).toBe("unavailable");
      configStore.reexecute.mockClear();

      await vi.advanceTimersByTimeAsync(CONFIG_RETRY_DELAYS[0]);

      expect(configStore.reexecute).toHaveBeenCalledWith({ requestPolicy: "network-only" });
    } finally {
      vi.useRealTimers();
    }
  });

  it("re-asks when a reconnect lands while the FIRST config query is still open", () => {
    // The cell an `error`-gated guard misses: no result yet, so no error yet.
    // Skipping here spends the one healing signal on a request that may still
    // fail, and the session then holds UNAVAILABLE_SPINE for the rest of it.
    configStore.set({ fetching: true, error: undefined, data: undefined, stale: false });
    render(App);
    expect(currentSpine().areas.status).toBe("loading");
    configStore.reexecute.mockClear();

    reconnectAfterGap();

    expect(configStore.reexecute).toHaveBeenCalledTimes(1);
  });
});
