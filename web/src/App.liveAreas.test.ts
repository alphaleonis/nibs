import { render } from "@testing-library/svelte";
import { tick } from "svelte";
import { describe, it, expect, vi, beforeEach } from "vitest";
import App from "./App.svelte";
import { CONFIG_QUERY, CONFIG_CHANGED_SUBSCRIPTION, NIB_DETAIL_QUERY } from "./lib/queries";
import { prepareFilter } from "./lib/filter";
import type { ViewSpine } from "./lib/viewSpine";

// A `nibs area rename` in another terminal rewrites the store's areas.yml, the
// server reloads it and pushes the new vocabulary. Without that push the browser
// keeps rendering sections the store no longer declares, and withholds every
// `area:` filter naming a path its own nibs now carry (nibs-5cuk).

const { configStore, configChangedStore } = await vi.hoisted(async () => {
  const { writable } = await import("svelte/store");
  const empty = { fetching: false, error: undefined, data: undefined, stale: false };
  return {
    configStore: Object.assign(
      writable<Record<string, unknown>>(empty),
      { reexecute: vi.fn() },
    ),
    configChangedStore: writable<Record<string, unknown>>(empty),
  };
});

/** Settle CONFIG_QUERY to `config` — the vocabulary as the page loaded it. */
const setConfig = (config: Record<string, unknown>) =>
  configStore.set({ fetching: false, error: undefined, data: { config }, stale: false });

/** Deliver one `configChanged` event — the server's push after a reload. */
const pushConfig = (config: Record<string, unknown>) =>
  configChangedStore.set({
    fetching: false,
    error: undefined,
    data: { configChanged: config },
    stale: false,
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
  const detailData = readable({ fetching: false, error: undefined, data: { nib: null }, stale: false });
  const idleSub = readable({ fetching: false, error: undefined, data: undefined, stale: false });

  return {
    ...actual,
    getContextClient: vi.fn(),
    setContextClient: vi.fn(),
    queryStore: vi.fn().mockImplementation((opts: { query: unknown }) => {
      if (opts.query === CONFIG_QUERY) return configStore;
      if (opts.query === NIB_DETAIL_QUERY) return detailData;
      return nibsData;
    }),
    subscriptionStore: vi.fn().mockImplementation((opts: { query: unknown }) => {
      if (opts.query === CONFIG_CHANGED_SUBSCRIPTION) return configChangedStore;
      return idleSub;
    }),
  };
});

const currentSpine = (): ViewSpine => {
  const get = spineGetters.at(-1);
  if (!get) throw new Error("App did not provide a view spine");
  return get();
};

const areaNode = (path: string, depth = 0) => ({
  path,
  name: path.split("/").pop() ?? path,
  description: "",
  color: "",
  depth,
});

describe("App live areas vocabulary", () => {
  beforeEach(() => {
    window.history.replaceState(null, "", "/");
    document.documentElement.classList.remove("dark");
    delete document.documentElement.dataset.theme;
    spineGetters.length = 0;
    configStore.set({ fetching: false, error: undefined, data: undefined, stale: false });
    configChangedStore.set({ fetching: false, error: undefined, data: undefined, stale: false });
  });

  it("re-sections on a pushed vocabulary, with no page reload and no refetch", async () => {
    setConfig({ projectName: "test-project", areas: [areaNode("web"), areaNode("web/dashboard", 1)] });
    render(App);
    expect(currentSpine().areas.validity("web")).toBe("declared");

    // `nibs area rename web frontend`, as the server pushes it.
    pushConfig({
      projectName: "test-project",
      areas: [areaNode("frontend"), areaNode("frontend/dashboard", 1)],
    });
    await tick();

    expect(currentSpine().areas.validity("frontend")).toBe("declared");
    expect(currentSpine().areas.validity("web")).toBe("undeclared");
    // The symptom the push removes: a filter naming the path the nibs now
    // carry used to be withheld as undeclared, silently widening the list.
    expect(prepareFilter({ area: "frontend" }, currentSpine().areas).serverFilter.area).toBe(
      "frontend",
    );
  });

  it("takes the project name from the push too", async () => {
    setConfig({ projectName: "test-project", areas: [] });
    render(App);
    expect(document.title).toBe("Nibs - test-project");

    pushConfig({ projectName: "renamed-project", areas: [] });
    await tick();

    expect(document.title).toBe("Nibs - renamed-project");
  });

  it("renders the loaded vocabulary while no push has arrived", async () => {
    setConfig({ projectName: "test-project", areas: [areaNode("web")] });
    render(App);
    await tick();

    expect(currentSpine().areas.status).toBe("ready");
    expect(currentSpine().areas.validity("web")).toBe("declared");
  });
});
