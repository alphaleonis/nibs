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
// silently widens to the whole store while the filter says otherwise. The
// re-ask is App's, on connection recovery, and it needs the socket seam — which
// is why this file mocks `./lib/graphql` and App.test.ts does not.

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

  it("leaves a config query that answered alone", async () => {
    // A vocabulary in hand is never re-fetched, so a re-ask that failed could
    // not take one away.
    setConfig({ projectName: "test-project", areas });
    render(App);
    expect(currentSpine().areas.status).toBe("ready");

    reconnectAfterGap();
    await tick();

    expect(configStore.reexecute).not.toHaveBeenCalled();
    expect(currentSpine().areas.status).toBe("ready");
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
