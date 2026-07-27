import { render, screen, within, waitFor } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach } from "vitest";
import App from "./App.svelte";
import { CONFIG_QUERY, NIB_DETAIL_QUERY } from "./lib/queries";

// The detail query for a nib that exists NOW but can be made to vanish mid-session
// (deleted / archived while the user has it open). Hoisted so the mock factory and
// the tests share one store instance: the factory serves it for `nibs-vanish`, and
// a test settles it to `{ nib: null }` once the buffer is dirty.
const { vanishingNib, vanishingDetailData, nibSubData } = await vi.hoisted(async () => {
  const { writable } = await import("svelte/store");
  const nib = {
    id: "nibs-vanish",
    title: "Vanishing nib",
    status: "todo",
    type: "task",
    priority: "normal",
    estimate: "",
    tags: [] as string[],
    body: "",
    documents: [] as string[],
    etag: "etag-vanish",
    parent: null,
    children: [] as unknown[],
    blocking: [] as unknown[],
    blockedBy: [] as unknown[],
    mentions: [] as unknown[],
    mentionedBy: [] as unknown[],
  };
  type DetailStore = {
    fetching: boolean;
    error: undefined;
    data: { nib: typeof nib | null };
    stale: boolean;
  };
  return {
    vanishingNib: nib,
    vanishingDetailData: writable<DetailStore>({
      fetching: false,
      error: undefined,
      data: { nib },
      stale: false,
    }),
    // The live NIB_CHANGED subscription. Writable so a test can push a real
    // server event (the only way a buffer reaches `gone`/"archived" — an archived
    // nib still RESOLVES, so the detail query never goes null for it).
    nibSubData: writable<{ fetching: boolean; error: undefined; data: unknown; stale: boolean }>({
      fetching: false,
      error: undefined,
      data: undefined,
      stale: false,
    }),
  };
});

/** Settle the vanishing nib's detail query to "no such nib". */
const vanishNib = () =>
  vanishingDetailData.set({ fetching: false, error: undefined, data: { nib: null }, stale: false });
/** Restore the vanishing nib so the shared store doesn't leak between tests. */
const restoreVanishingNib = () =>
  vanishingDetailData.set({
    fetching: false,
    error: undefined,
    data: { nib: vanishingNib },
    stale: false,
  });

/** Push a live `archived` event for `id` — the nib moved to the archive, and
 *  still exists there. */
const archiveNib = (id: string) =>
  nibSubData.set({
    fetching: false,
    error: undefined,
    data: { nibChanged: { type: "archived", nibId: id, nib: vanishingNib } },
    stale: false,
  });
/** Reset the shared subscription store so an event doesn't leak between tests. */
const resetNibSub = () =>
  nibSubData.set({ fetching: false, error: undefined, data: undefined, stale: false });

vi.mock("@urql/svelte", async () => {
  const { readable } = await import("svelte/store");
  const actual = await vi.importActual<typeof import("@urql/svelte")>("@urql/svelte");

  const configData = readable({
    fetching: false,
    error: undefined,
    data: { config: { projectName: "test-project" } },
    stale: false,
  });

  // `reexecute`: TreeTable calls it on every change event that is not a delete
  // (a delete defers it behind the fade timer). A bare `readable` has no such
  // method, so it would throw a TypeError. TreeTable isolates the reexecute call
  // in a try/catch, so a throw is caught (surfaced as a console.error, refetch
  // lost) rather than aborting the effect flush — but a real reexecute is still
  // supplied here so subscription-driven tests exercise the live refetch path.
  const nibsData = Object.assign(readable({
    fetching: false,
    error: undefined,
    data: {
      nibs: [
        {
          id: "nibs-m1",
          title: "Test milestone",
          status: "in-progress",
          type: "milestone",
          priority: "normal",
          estimate: "",
          tags: [],
          updatedAt: "2026-03-20T10:00:00Z",
          parentId: null,
          blockingIds: [],
          blockedByIds: [],
        },
        {
          id: "nibs-abc1",
          title: "Test nib",
          status: "todo",
          type: "task",
          priority: "normal",
          estimate: "",
          tags: [],
          updatedAt: "2026-03-20T10:00:00Z",
          parentId: "nibs-m1",
          blockingIds: [],
          blockedByIds: [],
        },
      ],
    },
    stale: false,
  }), { reexecute: vi.fn() });

  // The active-nib view runs NIB_DETAIL_QUERY (to seed the edit form + render
  // relations); it must resolve to a real nib or App treats it as not-found and
  // self-closes. Layout tests only need the panel to render, so a
  // single static nib suffices.
  const nibDetailData = readable({
    fetching: false,
    error: undefined,
    data: {
      nib: {
        id: "nibs-m1",
        title: "Test milestone",
        status: "in-progress",
        type: "milestone",
        priority: "normal",
        estimate: "",
        tags: [],
        body: "",
        documents: [],
        etag: "etag-m1",
        parent: null,
        children: [],
        blocking: [],
        blockedBy: [],
        mentions: [],
        mentionedBy: [],
      },
    },
    stale: false,
  });

  // A settled detail query that resolves to NO nib — a deleted / archived /
  // stale ?nib= link. App treats this as missing and self-closes.
  const missingDetailData = readable({
    fetching: false,
    error: undefined,
    data: { nib: null },
    stale: false,
  });

  return {
    ...actual,
    getContextClient: vi.fn(),
    setContextClient: vi.fn(),
    queryStore: vi.fn().mockImplementation((opts: { query: unknown; variables?: { id?: string } }) => {
      if (opts.query === CONFIG_QUERY) {
        return configData;
      }
      if (opts.query === NIB_DETAIL_QUERY) {
        if (opts.variables?.id === "nibs-gone") return missingDetailData;
        if (opts.variables?.id === "nibs-vanish") return vanishingDetailData;
        return nibDetailData;
      }
      return nibsData;
    }),
    subscriptionStore: vi.fn().mockReturnValue(nibSubData),
  };
});

import { queryStore } from "@urql/svelte";
const mockQueryStore = vi.mocked(queryStore);

describe("App", () => {
  beforeEach(() => {
    mockQueryStore.mockClear();
    // App now syncs selection from the URL on mount, and clicks push history.
    // jsdom shares window.history/location across tests in a file, so reset to a
    // clean URL before each test to mirror a fresh page load.
    window.history.replaceState(null, "", "/");
    // App's $effect runs applyTheme, which mutates <html> class/dataset. jsdom
    // shares document.documentElement across tests in a file (index.html's FOUC
    // guard never runs here), so reset the light/dark seam to a clean slate.
    document.documentElement.classList.remove("dark");
    delete document.documentElement.dataset.theme;
    // The vanishing nib's detail store is module-level, so a test that deletes it
    // would leak into the next one. Same for the shared subscription store.
    restoreVanishingNib();
    resetNibSub();
  });

  it("renders with dark theme shell containing Toolbar and TreeTable", () => {
    render(App);

    // Default prefs resolve to the dark Graphite palette, so App's $effect adds
    // `.dark` to <html> — index.html does not hardcode it.
    expect(document.documentElement.classList.contains("dark")).toBe(true);

    // Dark theme shell: has the app title with project name
    expect(screen.getByText("Nibs - test-project")).toBeInTheDocument();

    // Toolbar is rendered with controls
    expect(screen.getByRole("button", { name: "Settings" })).toBeInTheDocument();
    expect(screen.getByTestId("filter-keyword")).toBeInTheDocument();

    // TreeTable renders data
    expect(screen.getByText("Test nib")).toBeInTheDocument();
  });

  it("applies prefs.theme to document.documentElement as the app renders", () => {
    // The only runtime call site of applyTheme is App.svelte's $effect on
    // prefs.theme — the glue from persisted Preferences to the live DOM. Seed a
    // non-default theme, render App, and assert data-theme reflects it. Uses the
    // same defineProperty localStorage pattern as the panel-width tests below.
    const savedStorage = globalThis.localStorage;
    const savedTheme = document.documentElement.dataset.theme;
    const mockStore: Record<string, string> = {
      "nibs-filter-preferences": JSON.stringify({
        filter: {},
        viewLevel: "none",
        theme: "dracula",
      }),
    };
    Object.defineProperty(globalThis, "localStorage", {
      value: {
        getItem: (key: string) => mockStore[key] ?? null,
        setItem: (key: string, value: string) => { mockStore[key] = value; },
        removeItem: (key: string) => { delete mockStore[key]; },
      },
      writable: true,
      configurable: true,
    });

    try {
      render(App);
      expect(document.documentElement.dataset.theme).toBe("dracula");
      // Dracula is a dark theme, so applyTheme keeps `.dark` present.
      expect(document.documentElement.classList.contains("dark")).toBe(true);
    } finally {
      Object.defineProperty(globalThis, "localStorage", {
        value: savedStorage,
        writable: true,
        configurable: true,
      });
      if (savedTheme === undefined) delete document.documentElement.dataset.theme;
      else document.documentElement.dataset.theme = savedTheme;
    }
  });

  it("renders WITHOUT `.dark` when the seeded theme is the light Daylight palette", () => {
    // The complement of the dark-theme assertions: a seeded light theme must clear
    // `.dark` so shadcn `dark:` utilities switch off and the app renders light.
    // Uses the same defineProperty localStorage pattern.
    //
    // Force the opposite starting state so this can only pass if App actively
    // clears `.dark` (the shared beforeEach already removes it, which would make
    // the assertion vacuous). Mirrors theme.test.ts's "removes `.dark`" unit test.
    const hadDark = document.documentElement.classList.contains("dark");
    document.documentElement.classList.add("dark");

    const savedStorage = globalThis.localStorage;
    const savedTheme = document.documentElement.dataset.theme;
    const mockStore: Record<string, string> = {
      "nibs-filter-preferences": JSON.stringify({
        filter: {},
        viewLevel: "none",
        theme: "daylight",
      }),
    };
    Object.defineProperty(globalThis, "localStorage", {
      value: {
        getItem: (key: string) => mockStore[key] ?? null,
        setItem: (key: string, value: string) => { mockStore[key] = value; },
        removeItem: (key: string) => { delete mockStore[key]; },
      },
      writable: true,
      configurable: true,
    });

    try {
      render(App);
      expect(document.documentElement.dataset.theme).toBe("daylight");
      expect(document.documentElement.classList.contains("dark")).toBe(false);
    } finally {
      Object.defineProperty(globalThis, "localStorage", {
        value: savedStorage,
        writable: true,
        configurable: true,
      });
      if (savedTheme === undefined) delete document.documentElement.dataset.theme;
      else document.documentElement.dataset.theme = savedTheme;
      // Restore the `.dark` class we forced on above, so this test leaves the
      // shared document element as it found it (not reliant on the next beforeEach).
      document.documentElement.classList.toggle("dark", hadDark);
    }
  });

  it("applies a persisted large fontSize to --font-scale as the app renders", async () => {
    // The only runtime call site of applyFontScale is App.svelte's $effect on
    // prefs.fontSize — the glue from persisted Preferences to the live DOM,
    // mirroring the applyTheme effect above. Seed a non-default fontSize, render
    // App, and assert --font-scale reflects it. Same defineProperty localStorage
    // pattern as the theme tests.
    const savedStorage = globalThis.localStorage;
    const savedFontScale = document.documentElement.style.getPropertyValue("--font-scale");
    const mockStore: Record<string, string> = {
      "nibs-filter-preferences": JSON.stringify({
        filter: {},
        viewLevel: "none",
        fontSize: "large",
      }),
    };
    Object.defineProperty(globalThis, "localStorage", {
      value: {
        getItem: (key: string) => mockStore[key] ?? null,
        setItem: (key: string, value: string) => { mockStore[key] = value; },
        removeItem: (key: string) => { delete mockStore[key]; },
      },
      writable: true,
      configurable: true,
    });
    // Force a sentinel so this can only pass if App's $effect actively writes the
    // scale — otherwise a stale/default value could satisfy the assertion and make
    // it vacuous (mirrors the light-theme test forcing the opposite `.dark` state).
    document.documentElement.style.setProperty("--font-scale", "999");

    try {
      render(App);
      // large → FONT_SCALES.large (1.15), written by App.svelte's $effect.
      await waitFor(() =>
        expect(document.documentElement.style.getPropertyValue("--font-scale")).toBe("1.15"),
      );
    } finally {
      Object.defineProperty(globalThis, "localStorage", {
        value: savedStorage,
        writable: true,
        configurable: true,
      });
      // Restore the shared documentElement's --font-scale so no other test in this
      // file sees a stale value (jsdom shares the element across tests).
      if (savedFontScale === "") document.documentElement.style.removeProperty("--font-scale");
      else document.documentElement.style.setProperty("--font-scale", savedFontScale);
    }
  });

  it("applies a persisted small fontSize to --font-scale as the app renders", async () => {
    // Complement of the large case: a seeded small fontSize resolves to the 0.9
    // multiplier, proving the effect reads the real preference (not a constant).
    const savedStorage = globalThis.localStorage;
    const savedFontScale = document.documentElement.style.getPropertyValue("--font-scale");
    const mockStore: Record<string, string> = {
      "nibs-filter-preferences": JSON.stringify({
        filter: {},
        viewLevel: "none",
        fontSize: "small",
      }),
    };
    Object.defineProperty(globalThis, "localStorage", {
      value: {
        getItem: (key: string) => mockStore[key] ?? null,
        setItem: (key: string, value: string) => { mockStore[key] = value; },
        removeItem: (key: string) => { delete mockStore[key]; },
      },
      writable: true,
      configurable: true,
    });
    document.documentElement.style.setProperty("--font-scale", "999");

    try {
      render(App);
      await waitFor(() =>
        expect(document.documentElement.style.getPropertyValue("--font-scale")).toBe("0.9"),
      );
    } finally {
      Object.defineProperty(globalThis, "localStorage", {
        value: savedStorage,
        writable: true,
        configurable: true,
      });
      if (savedFontScale === "") document.documentElement.style.removeProperty("--font-scale");
      else document.documentElement.style.setProperty("--font-scale", savedFontScale);
    }
  });

  it("wires filter state between Toolbar and TreeTable", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(App);

    // queryStore should have been called for initial render
    expect(mockQueryStore).toHaveBeenCalled();
    const initialCallCount = mockQueryStore.mock.calls.length;

    // Type in keyword search (always visible in toolbar)
    const searchInput = screen.getByTestId("filter-keyword");
    await user.type(searchInput, "bug");
    expect(searchInput).toHaveValue("bug");

    // Hide completed via the Status-facet "Open + deferred" preset: open the
    // Status dropdown, then click the preset. Anchor the name to the start so it
    // targets the Toolbar's "Status" facet trigger, not the table's new
    // "Sort by Status" header button (headers are now sortable in every view).
    await user.click(screen.getByRole("button", { name: /^status/i }));
    await user.click(screen.getByTestId("status-preset-open-deferred"));

    // `search` is server-side, so typing re-queries — but the list refetch is
    // debounced (nibs-rv7c), landing shortly after typing rather than on each
    // keystroke. waitFor rides out the debounce window.
    await waitFor(() => {
      expect(mockQueryStore.mock.calls.length).toBeGreaterThan(initialCallCount);
      const nibsCalls = mockQueryStore.mock.calls.filter(
        (call) => call[0].variables?.filter !== undefined
      );
      // search stays server-side; the status include-list is applied client-side (so
      // the server still returns completed/scrapped ancestors for in-place dimming)
      // and must NOT be forwarded to the GraphQL server.
      const latestVars = nibsCalls[nibsCalls.length - 1][0].variables!;
      expect(latestVars.filter).toEqual(expect.objectContaining({ search: "bug" }));
      expect(latestVars.filter).not.toHaveProperty("status");
    });
  });

  it("URL ?q= wins over a different stored query on load (shared link reproduces its filter)", () => {
    // localStorage holds one filter; the URL carries a DIFFERENT one. The shared
    // `?q=` link must win, so the box (and thus the filter) reflects the URL.
    const savedStorage = globalThis.localStorage;
    const mockStore: Record<string, string> = {
      "nibs-filter-preferences": JSON.stringify({ q: "type:feature", viewLevel: "none" }),
    };
    Object.defineProperty(globalThis, "localStorage", {
      value: {
        getItem: (key: string) => mockStore[key] ?? null,
        setItem: (key: string, value: string) => { mockStore[key] = value; },
        removeItem: (key: string) => { delete mockStore[key]; },
      },
      writable: true,
      configurable: true,
    });
    window.history.replaceState(null, "", "/?q=type%3Abug");

    try {
      render(App);
      const box = screen.getByTestId("filter-keyword") as HTMLInputElement;
      // URL query (type:bug) wins over the stored one (type:feature).
      expect(box.value).toBe("type:bug");
    } finally {
      Object.defineProperty(globalThis, "localStorage", {
        value: savedStorage, writable: true, configurable: true,
      });
    }
  });

  it("uses the stored query when the URL has no ?q= (falls back to localStorage)", () => {
    const savedStorage = globalThis.localStorage;
    const mockStore: Record<string, string> = {
      "nibs-filter-preferences": JSON.stringify({ q: "type:feature", viewLevel: "none" }),
    };
    Object.defineProperty(globalThis, "localStorage", {
      value: {
        getItem: (key: string) => mockStore[key] ?? null,
        setItem: (key: string, value: string) => { mockStore[key] = value; },
        removeItem: (key: string) => { delete mockStore[key]; },
      },
      writable: true,
      configurable: true,
    });
    // No ?q= in the URL — the stored query is used.
    window.history.replaceState(null, "", "/");

    try {
      render(App);
      expect((screen.getByTestId("filter-keyword") as HTMLInputElement).value).toBe("type:feature");
    } finally {
      Object.defineProperty(globalThis, "localStorage", {
        value: savedStorage, writable: true, configurable: true,
      });
    }
  });

  it("opens detail panel when a title is clicked", async () => {
    const user = userEvent.setup();
    render(App);

    // Panel should not be open initially
    expect(screen.queryByTestId("active-nib-view")).not.toBeInTheDocument();

    // Click a nib title
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);

    // Detail panel should appear
    expect(screen.getByTestId("active-nib-view")).toBeInTheDocument();
  });

  it("closes detail panel when close button is clicked", async () => {
    const user = userEvent.setup();
    render(App);

    // Open the panel
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);
    expect(screen.getByTestId("active-nib-view")).toBeInTheDocument();

    // Close the panel
    await user.click(screen.getByTestId("anv-close"));
    expect(screen.queryByTestId("active-nib-view")).not.toBeInTheDocument();
  });

  it("closes detail panel when Escape key is pressed", async () => {
    const user = userEvent.setup();
    render(App);

    // Open the panel by clicking a title
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);
    expect(screen.getByTestId("active-nib-view")).toBeInTheDocument();

    // Press Escape
    await user.keyboard("{Escape}");
    expect(screen.queryByTestId("active-nib-view")).not.toBeInTheDocument();
  });

  it("tree-table is full width when panel is closed", () => {
    const { container } = render(App);

    // No detail panel
    expect(screen.queryByTestId("active-nib-view")).not.toBeInTheDocument();

    // PaneGroup is always present (tree-table is always in a pane)
    expect(container.querySelector("[data-pane-group]")).toBeInTheDocument();

    // Resize handle is hidden when panel is closed
    expect(screen.getByTestId("resize-handle")).toHaveClass("hidden");

    // Detail pane should be collapsed
    const detailPane = container.querySelector("[data-testid='detail-pane']");
    expect(detailPane).toBeInTheDocument();
    expect(detailPane).toHaveAttribute("data-pane-state", "collapsed");
  });

  it("tree-table shares space with detail panel when open", async () => {
    const user = userEvent.setup();
    const { container } = render(App);

    // Open the panel
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);

    // Both panes should exist within a PaneForge group
    expect(screen.getByTestId("active-nib-view")).toBeInTheDocument();
    const paneGroup = container.querySelector("[data-pane-group]");
    expect(paneGroup).toBeInTheDocument();

    // There should be two panes in the group
    const panes = container.querySelectorAll("[data-pane]");
    expect(panes.length).toBe(2);

    // Detail pane should have the data-testid
    const detailPane = container.querySelector("[data-testid='detail-pane']");
    expect(detailPane).toBeInTheDocument();
  });

  it("Escape closes only the filter dropdown when both dropdown and detail panel are open", async () => {
    const user = userEvent.setup();
    render(App);

    // Open the detail panel by clicking a title
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);
    expect(screen.getByTestId("active-nib-view")).toBeInTheDocument();

    // Open the Type dropdown in toolbar. Anchor the name to the start so it
    // targets the Toolbar's "Type" facet trigger, not the table's new
    // "Sort by Type" header button (headers are now sortable in every view).
    await user.click(screen.getByRole("button", { name: /^type/i }));
    expect(screen.getByRole("menuitemcheckbox", { name: "bug" })).toBeInTheDocument();

    // Press Escape — should close only the dropdown, NOT the detail panel
    await user.keyboard("{Escape}");

    // Dropdown should be closed
    expect(screen.queryByRole("menuitemcheckbox", { name: "bug" })).not.toBeInTheDocument();

    // Detail panel should still be open
    expect(screen.getByTestId("active-nib-view")).toBeInTheDocument();
  });

  it("resize handle renders between tree-table and detail panel when panel is open", async () => {
    const user = userEvent.setup();
    render(App);

    // Resize handle is hidden when panel is closed
    expect(screen.getByTestId("resize-handle")).toHaveClass("hidden");

    // Open the panel
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);

    // Resize handle should now be visible (PaneForge resizer with our data-testid)
    const resizeHandle = screen.getByTestId("resize-handle");
    expect(resizeHandle).not.toHaveClass("hidden");
    expect(resizeHandle).toHaveAttribute("data-pane-resizer");
  });

  it("resize handle is focusable via PaneForge", async () => {
    const user = userEvent.setup();
    render(App);

    // Open the panel
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);

    const resizeHandle = screen.getByTestId("resize-handle");

    // PaneForge sets tabindex on the resizer
    expect(resizeHandle).toHaveAttribute("tabindex", "0");
  });

  it("double-click on resize handle resets panel width to default", async () => {
    // Set up localStorage with a non-default detailPanelWidth
    const savedStorage = globalThis.localStorage;
    const mockStore: Record<string, string> = {
      "nibs-filter-preferences": JSON.stringify({
        filter: {},
        viewLevel: "milestones",
        detailPanelWidth: 600,
      }),
    };
    const setItemSpy = vi.fn((key: string, value: string) => { mockStore[key] = value; });
    Object.defineProperty(globalThis, "localStorage", {
      value: {
        getItem: (key: string) => mockStore[key] ?? null,
        setItem: setItemSpy,
        removeItem: (key: string) => { delete mockStore[key]; },
      },
      writable: true,
      configurable: true,
    });

    try {
      const user = userEvent.setup();
      render(App);

      // Open the panel
      const titleTexts = screen.getAllByTestId("title-text");
      await user.click(titleTexts[0]);

      // Clear saves from initial render
      setItemSpy.mockClear();

      // Double-click the resize handle
      const resizeHandle = screen.getByTestId("resize-handle");
      await user.dblClick(resizeHandle);

      // Should persist the default width (400) to localStorage
      expect(setItemSpy).toHaveBeenCalled();
      const lastSave = JSON.parse(setItemSpy.mock.calls[setItemSpy.mock.calls.length - 1][1]);
      expect(lastSave.detailPanelWidth).toBe(400);
    } finally {
      // Restore original localStorage
      Object.defineProperty(globalThis, "localStorage", {
        value: savedStorage,
        writable: true,
        configurable: true,
      });
    }
  });

  it("detail panel initializes with stored detailPanelWidth from preferences", async () => {
    // Set up localStorage with a custom detailPanelWidth
    const savedStorage = globalThis.localStorage;
    const mockStore: Record<string, string> = {
      "nibs-filter-preferences": JSON.stringify({
        filter: {},
        viewLevel: "milestones",
        detailPanelWidth: 500,
      }),
    };
    Object.defineProperty(globalThis, "localStorage", {
      value: {
        getItem: (key: string) => mockStore[key] ?? null,
        setItem: vi.fn((key: string, value: string) => { mockStore[key] = value; }),
        removeItem: (key: string) => { delete mockStore[key]; },
      },
      writable: true,
      configurable: true,
    });

    try {
      const user = userEvent.setup();
      const { container } = render(App);

      // Open the panel
      const titleTexts = screen.getAllByTestId("title-text");
      await user.click(titleTexts[0]);

      // PaneGroup should be mounted with a detail pane
      const paneGroup = container.querySelector("[data-pane-group]");
      expect(paneGroup).toBeInTheDocument();

      // The detail pane should exist (PaneForge uses the defaultSize prop on mount)
      const panes = container.querySelectorAll("[data-pane]");
      expect(panes.length).toBe(2);
    } finally {
      Object.defineProperty(globalThis, "localStorage", {
        value: savedStorage,
        writable: true,
        configurable: true,
      });
    }
  });

  it("detail panel renders with resize handle when opened with non-default stored width", async () => {
    // Verifies that the detail panel and resize handle mount correctly when the user has
    // a non-default stored width. PaneForge internals don't fire resize callbacks in jsdom,
    // so this test validates structural wiring rather than the onResize pipeline.
    const savedStorage = globalThis.localStorage;
    const mockStore: Record<string, string> = {
      "nibs-filter-preferences": JSON.stringify({
        filter: {},
        viewLevel: "milestones",
        detailPanelWidth: 350,
      }),
    };
    const setItemSpy = vi.fn((key: string, value: string) => { mockStore[key] = value; });
    Object.defineProperty(globalThis, "localStorage", {
      value: {
        getItem: (key: string) => mockStore[key] ?? null,
        setItem: setItemSpy,
        removeItem: (key: string) => { delete mockStore[key]; },
      },
      writable: true,
      configurable: true,
    });

    try {
      const user = userEvent.setup();
      render(App);

      // Open the panel
      const titleTexts = screen.getAllByTestId("title-text");
      await user.click(titleTexts[0]);

      // The PaneGroup should exist
      expect(screen.getByTestId("active-nib-view")).toBeInTheDocument();

      // The resize handle should exist with the onDraggingChange callback wired
      const resizeHandle = screen.getByTestId("resize-handle");
      expect(resizeHandle).toBeInTheDocument();
    } finally {
      Object.defineProperty(globalThis, "localStorage", {
        value: savedStorage,
        writable: true,
        configurable: true,
      });
    }
  });

  it("PaneForge pane group uses horizontal direction", async () => {
    const user = userEvent.setup();
    const { container } = render(App);

    // Open the panel
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);

    const paneGroup = container.querySelector("[data-pane-group]");
    expect(paneGroup).toHaveAttribute("data-direction", "horizontal");
  });

  it("PaneForge pane group uses vertical direction when detailPanelPosition is bottom", async () => {
    // Seed a "bottom" dock preference. The PaneGroup's split direction is resolved
    // at mount from the pref (vertical = table on top, preview below), independent
    // of whether the detail panel is open — so no panel-open interaction is needed.
    const savedStorage = globalThis.localStorage;
    const mockStore: Record<string, string> = {
      "nibs-filter-preferences": JSON.stringify({
        filter: {},
        viewLevel: "none",
        detailPanelPosition: "bottom",
      }),
    };
    Object.defineProperty(globalThis, "localStorage", {
      value: {
        getItem: (key: string) => mockStore[key] ?? null,
        setItem: (key: string, value: string) => { mockStore[key] = value; },
        removeItem: (key: string) => { delete mockStore[key]; },
      },
      writable: true,
      configurable: true,
    });

    try {
      const { container } = render(App);

      const paneGroup = container.querySelector("[data-pane-group]");
      expect(paneGroup).toHaveAttribute("data-direction", "vertical");
    } finally {
      Object.defineProperty(globalThis, "localStorage", {
        value: savedStorage,
        writable: true,
        configurable: true,
      });
    }
  });

  it("preserves tree collapse state across a detail-panel dock toggle", async () => {
    // Toggling the dock position remounts the PaneGroup (PaneForge fixes split
    // direction at creation), which remounts TreeTable. Collapse state therefore
    // cannot be TreeTable-local $state — that resets on the remount and silently
    // re-expands every branch. It lives in a TreeViewState provided outside the
    // {#key position} block, so it survives the remount.
    const user = userEvent.setup();
    render(App);

    // Collapse the milestone so its child ("Test nib") is hidden.
    await user.click(screen.getByTestId("collapse-all"));
    expect(screen.queryByText("Test nib")).not.toBeInTheDocument();

    // Toggle the dock position to "bottom" at runtime — this remounts the PaneGroup.
    await user.click(screen.getByRole("button", { name: "Settings" }));
    const group = screen.getByRole("radiogroup", { name: /detail panel position/i });
    await user.click(within(group).getByRole("radio", { name: /bottom/i }));

    // The collapsed branch must stay collapsed — the child is still hidden and the
    // root milestone is still shown.
    expect(screen.queryByText("Test nib")).not.toBeInTheDocument();
    expect(screen.getByText("Test milestone")).toBeInTheDocument();
  });

  it("renders toolbar icon buttons including the group-by control", () => {
    render(App);

    expect(screen.getByRole("button", { name: "New item" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /^Group by/ })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Settings" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Columns" })).toBeInTheDocument();

    // Group-by control should show "Tree" (default lens — the full hierarchy)
    expect(screen.getByRole("button", { name: /^Group by/ })).toHaveTextContent("Tree");
  });

  it("TreeTable DOM element persists when panel opens", async () => {
    const user = userEvent.setup();
    render(App);

    // Get a reference to a TreeTable child element before opening panel
    const treeTable = screen.getByTestId("tree-table");
    expect(treeTable).toBeInTheDocument();

    // Open the detail panel
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);

    // The same DOM element should still be in the document (not recreated)
    expect(treeTable).toBeInTheDocument();
    // Also verify it's the exact same reference
    expect(screen.getByTestId("tree-table")).toBe(treeTable);
  });

  it("TreeTable DOM element persists when panel closes", async () => {
    const user = userEvent.setup();
    render(App);

    // Open the detail panel
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);
    expect(screen.getByTestId("active-nib-view")).toBeInTheDocument();

    // Get a reference to the TreeTable element while panel is open
    const treeTable = screen.getByTestId("tree-table");
    expect(treeTable).toBeInTheDocument();

    // Close the panel
    await user.click(screen.getByTestId("anv-close"));

    // The same DOM element should still be in the document (not recreated)
    expect(treeTable).toBeInTheDocument();
    expect(screen.getByTestId("tree-table")).toBe(treeTable);
  });

  it("PaneGroup is always present even when no nib is selected", () => {
    // Seed the default "right" dock explicitly. The PaneGroup's split direction is
    // resolved from prefs.detailPanelPosition; when localStorage is functional and
    // shared (as on CI), a dock preference persisted by an earlier test can leak in
    // and flip this to "vertical". Seeding a scoped store makes it deterministic —
    // symmetric to the "bottom" seeding above.
    const savedStorage = globalThis.localStorage;
    const mockStore: Record<string, string> = {
      "nibs-filter-preferences": JSON.stringify({
        filter: {},
        viewLevel: "none",
        detailPanelPosition: "right",
      }),
    };
    Object.defineProperty(globalThis, "localStorage", {
      value: {
        getItem: (key: string) => mockStore[key] ?? null,
        setItem: (key: string, value: string) => { mockStore[key] = value; },
        removeItem: (key: string) => { delete mockStore[key]; },
      },
      writable: true,
      configurable: true,
    });

    try {
      const { container } = render(App);

      // No nib selected, panel is closed
      expect(screen.queryByTestId("active-nib-view")).not.toBeInTheDocument();

      // PaneGroup should still be in the DOM
      const paneGroup = container.querySelector("[data-pane-group]");
      expect(paneGroup).toBeInTheDocument();
      expect(paneGroup).toHaveAttribute("data-direction", "horizontal");
    } finally {
      Object.defineProperty(globalThis, "localStorage", {
        value: savedStorage,
        writable: true,
        configurable: true,
      });
    }
  });

  it("detail panel shows when nib is selected (with collapsible pane)", async () => {
    const user = userEvent.setup();
    const { container } = render(App);

    // Open the detail panel
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);

    // Detail panel should appear
    expect(screen.getByTestId("active-nib-view")).toBeInTheDocument();

    // The detail pane should not be in collapsed state
    const detailPane = container.querySelector("[data-testid='detail-pane']");
    expect(detailPane).toBeInTheDocument();
    // When expanded, PaneForge sets data-pane-state to something other than "collapsed"
    expect(detailPane?.getAttribute("data-pane-state")).not.toBe("collapsed");
  });

  it("detail pane collapses when panel is closed", async () => {
    const user = userEvent.setup();
    const { container } = render(App);

    // Open the detail panel
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);
    expect(screen.getByTestId("active-nib-view")).toBeInTheDocument();

    // Close the panel
    await user.click(screen.getByTestId("anv-close"));

    // Detail panel content should not be visible
    expect(screen.queryByTestId("active-nib-view")).not.toBeInTheDocument();

    // The detail pane DOM element should still exist but be collapsed
    const detailPane = container.querySelector("[data-testid='detail-pane']");
    expect(detailPane).toBeInTheDocument();
    expect(detailPane).toHaveAttribute("data-pane-state", "collapsed");
  });

  it("resize handle hidden when detail pane is collapsed, visible when expanded", async () => {
    const user = userEvent.setup();
    render(App);

    // Initially resize handle is hidden
    expect(screen.getByTestId("resize-handle")).toHaveClass("hidden");

    // Open the panel
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);

    // Resize handle should now be visible
    expect(screen.getByTestId("resize-handle")).not.toHaveClass("hidden");

    // Close the panel
    await user.click(screen.getByTestId("anv-close"));

    // Resize handle should be hidden again
    expect(screen.getByTestId("resize-handle")).toHaveClass("hidden");
  });

  // ─── Unified active-nib-view wiring ───────────────

  it("keyboard 'n' opens a docked create view through the presenter", async () => {
    const user = userEvent.setup();
    render(App);

    expect(screen.queryByTestId("active-nib-view")).not.toBeInTheDocument();

    // 'n' routes to view.startCreate({ type: "task" }); a create instance renders
    // in the docked pane with a "Create" primary button and no overflow menu.
    await user.keyboard("n");

    expect(screen.getByTestId("active-nib-view")).toBeInTheDocument();
    expect(screen.getByTestId("anv-save")).toHaveTextContent("Create");
    expect(screen.queryByTestId("anv-overflow")).not.toBeInTheDocument();
  });

  it("expand routes the view into a full-screen modal; collapse returns it to the dock", async () => {
    const user = userEvent.setup();
    render(App);

    // Open a nib (docked).
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);
    expect(screen.getByTestId("active-nib-view")).toBeInTheDocument();
    expect(screen.queryByTestId("active-nib-modal")).not.toBeInTheDocument();

    // Expand → the view moves to the modal overlay (docked pane no longer hosts it).
    await user.click(screen.getByTestId("anv-expand"));
    expect(screen.getByTestId("active-nib-modal")).toBeInTheDocument();

    // Collapse (from inside the modal) → back to the dock, modal gone.
    await user.click(screen.getByTestId("anv-collapse"));
    expect(screen.queryByTestId("active-nib-modal")).not.toBeInTheDocument();
    expect(screen.getByTestId("active-nib-view")).toBeInTheDocument();
  });

  it("heals a stale ?nib=<gone> deep link: self-closes the view", async () => {
    // Land on a URL whose nib no longer exists. The detail query settles with a
    // null nib, so App fires the missing-nib heal (closes the view + toast).
    window.history.replaceState(null, "", "/?nib=nibs-gone");

    render(App);

    // The view must not stay open on a nib that resolves to nothing.
    await waitFor(() => {
      expect(screen.queryByTestId("active-nib-view")).not.toBeInTheDocument();
    });
    // The stale query string is healed off the URL.
    expect(window.location.search).toBe("");
  });

  it("keeps a DIRTY buffer when the viewed nib vanishes: shows the deleted notice, no self-close", async () => {
    const user = userEvent.setup();
    // Open a nib that exists, then type into it so the buffer holds unsaved edits.
    window.history.replaceState(null, "", "/?nib=nibs-vanish");
    render(App);

    const title = await screen.findByTestId("anv-title");
    await user.type(title, " edited");
    expect(title).toHaveValue("Vanishing nib edited");

    // The nib is deleted out from under the open editor: the detail query settles
    // with a null nib while the buffer is dirty.
    vanishNib();

    // The unsaved edits must survive behind the read-only deleted notice — the
    // view stays open rather than silently dropping the buffer.
    await waitFor(() => {
      expect(screen.getByTestId("anv-gone-notice")).toBeInTheDocument();
    });
    expect(screen.getByTestId("active-nib-view")).toBeInTheDocument();
    expect(screen.getByTestId("anv-title")).toHaveValue("Vanishing nib edited");
    // The ?nib= URL still points at what is on screen, so it must not be healed.
    expect(window.location.search).toBe("?nib=nibs-vanish");
  });

  it("closing a dirty buffer whose nib vanished prompts Discard-only, then closes", async () => {
    const user = userEvent.setup();
    window.history.replaceState(null, "", "/?nib=nibs-vanish");
    render(App);

    const title = await screen.findByTestId("anv-title");
    await user.type(title, " edited");
    vanishNib();
    await waitFor(() => {
      expect(screen.getByTestId("anv-gone-notice")).toBeInTheDocument();
    });

    await user.click(screen.getByTestId("anv-close"));

    // The prompt MUST still fire — closing does abandon the unsaved edits — but
    // the nib is deleted, so a Save could only dispatch against nothing and fail.
    // App withdraws the option instead of offering that dead end.
    await waitFor(() => {
      expect(screen.getByTestId("confirm-dialog")).toBeInTheDocument();
    });
    expect(screen.getByTestId("confirm-dialog-title")).toHaveTextContent("This nib was deleted");
    expect(screen.queryByTestId("confirm-dialog-save")).not.toBeInTheDocument();
    // Discard/Cancel — the only outcomes a deleted nib can honor — remain.
    expect(screen.getByTestId("confirm-dialog-confirm")).toHaveTextContent("Discard");
    expect(screen.getByTestId("confirm-dialog-cancel")).toBeInTheDocument();

    // Discard resolves the guard and the panel closes: not stranded.
    await user.click(screen.getByTestId("confirm-dialog-confirm"));
    await waitFor(() => {
      expect(screen.queryByTestId("active-nib-view")).not.toBeInTheDocument();
    });
  });

  it("closing a dirty buffer whose nib was ARCHIVED still offers Save", async () => {
    const user = userEvent.setup();
    window.history.replaceState(null, "", "/?nib=nibs-vanish");
    render(App);

    const title = await screen.findByTestId("anv-title");
    await user.type(title, " edited");
    // Archived, not deleted: the nib moved into the archive and still exists
    // there, so Core.Update writes to its archive path and a save really lands.
    archiveNib("nibs-vanish");
    await waitFor(() => {
      expect(screen.getByTestId("anv-gone-notice")).toHaveTextContent("This nib was archived");
    });

    await user.click(screen.getByTestId("anv-close"));

    await waitFor(() => {
      expect(screen.getByTestId("confirm-dialog")).toBeInTheDocument();
    });
    // The regression this pins: `gone` alone once withdrew Save, so archiving a
    // nib with unsaved edits left destroying them as the only way forward — and
    // said "This nib was deleted" while doing it. Both are false for an archive.
    expect(screen.getByTestId("confirm-dialog-title")).toHaveTextContent("Unsaved changes");
    expect(screen.getByTestId("confirm-dialog-save")).toHaveTextContent("Save");

    await user.click(screen.getByTestId("confirm-dialog-cancel"));
  });

  it("closing a dirty buffer whose nib is still live offers Save", async () => {
    const user = userEvent.setup();
    window.history.replaceState(null, "", "/?nib=nibs-vanish");
    render(App);

    const title = await screen.findByTestId("anv-title");
    await user.type(title, " edited");

    await user.click(screen.getByTestId("anv-close"));

    // The boundary's other side: nothing was deleted, so the guard keeps its
    // recommended Save. Only a `gone` buffer withdraws it.
    await waitFor(() => {
      expect(screen.getByTestId("confirm-dialog")).toBeInTheDocument();
    });
    expect(screen.getByTestId("confirm-dialog-title")).toHaveTextContent("Unsaved changes");
    expect(screen.getByTestId("confirm-dialog-save")).toHaveTextContent("Save");

    // Cancel out to close the dialog before the test ends, rather than leaving it
    // open at teardown. (Not leak avoidance: the per-test unmount discards the App
    // closure and nothing awaits the pending promise. Closing here keeps teardown
    // clear of bits-ui's deferred body-scroll-lock timer — see test-setup.ts.)
    await user.click(screen.getByTestId("confirm-dialog-cancel"));
  });

  it("gates 'n' while a discard confirm is open, so the pending close is not stranded (nibs-an5d)", async () => {
    // The dirty-guard's confirm reuses the single dialog. Before the fix, pressing
    // 'n' while it was open fired a SECOND confirmDiscard that overwrote the single
    // pending-resolver slot: the original requestClose promise leaked, and
    // answering the dialog ran the create instead — stranding the close forever.
    const user = userEvent.setup();
    window.history.replaceState(null, "", "/?nib=nibs-vanish");
    render(App);

    const title = await screen.findByTestId("anv-title");
    await user.type(title, " edited"); // dirty the buffer

    // Request close → the dirty-guard opens the discard confirm.
    await user.click(screen.getByTestId("anv-close"));
    await waitFor(() => {
      expect(screen.getByTestId("confirm-dialog")).toBeInTheDocument();
    });
    expect(screen.getByTestId("confirm-dialog-title")).toHaveTextContent("Unsaved changes");

    // Press 'n' while the confirm is open. The gate must swallow it — no second
    // confirm, no competing create guard hijacking the pending close.
    await user.keyboard("n");

    // Discard resolves the ORIGINAL requestClose, so the panel closes. In the
    // unfixed code the create guard would own the slot here and a create view
    // would open instead (and the close would leak).
    await user.click(screen.getByTestId("confirm-dialog-confirm"));
    await waitFor(() => {
      expect(screen.queryByTestId("active-nib-view")).not.toBeInTheDocument();
    });
    // No create view leaked open in place of the close.
    expect(screen.queryByTestId("anv-save")).not.toBeInTheDocument();
  });

  it("Back/Forward (popstate) retargets the docked view to the history nib", async () => {
    // Composed path: App's onPopState runs nav.handlePopState (updates selection
    // from the owned history state) then view.syncTo(selection.selectedNibId),
    // which skips the dirty guard. A real popstate must retarget the docked view
    // to the history nib without looping or desyncing.
    const user = userEvent.setup();
    render(App);

    // Open a nib in the docked view.
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);
    expect(screen.getByTestId("active-nib-view")).toBeInTheDocument();

    const firstId = screen.getByTestId("anv-id").textContent?.trim();
    expect(firstId).toBeTruthy();
    // Pick a DIFFERENT known nib as the Back/Forward target.
    const target = firstId === "nibs-m1" ? "nibs-abc1" : "nibs-m1";

    // Dispatch a real popstate carrying the owned history state for `target`.
    window.dispatchEvent(new PopStateEvent("popstate", { state: { nibId: target } }));

    // The docked view retargets to the history nib (single, non-duplicated view).
    await waitFor(() => {
      expect(screen.getByTestId("anv-id")).toHaveTextContent(target);
    });
    expect(screen.getAllByTestId("active-nib-view")).toHaveLength(1);
  });

  it("Back/Forward (popstate) re-syncs the filter query from the restored URL", async () => {
    // The filter query lives in `?q=` and is written with replaceState onto the
    // CURRENT history entry, while nib navigation pushState-snapshots whatever `?q=`
    // was in the address bar at that moment. Back/Forward restores an entry whose
    // `?q=` can differ from the in-memory filter, so onPopState must re-sync the
    // query from the just-restored URL (URL-wins, exactly as on initial load) — not
    // just the `?nib=` selection.
    window.history.replaceState(null, "", "/?q=type%3Abug");
    render(App);

    // Sanity: the initial `?q=` seeds the box.
    expect((screen.getByTestId("filter-keyword") as HTMLInputElement).value).toBe("type:bug");

    // Simulate Back to a history entry carrying a DIFFERENT `?q=`. jsdom does not
    // update location.search on a synthetic popstate, so set it first (mirrors what
    // a real browser restores), then dispatch the event.
    window.history.replaceState({ nibId: null }, "", "/?q=type%3Afeature");
    window.dispatchEvent(new PopStateEvent("popstate", { state: { nibId: null } }));

    // The box (and thus prefs.query) now reflects the RESTORED entry's query.
    await waitFor(() => {
      expect((screen.getByTestId("filter-keyword") as HTMLInputElement).value).toBe("type:feature");
    });
  });

  it("Back/Forward (popstate) to an entry with NO ?q= clears the filter to empty", async () => {
    // The `?q=` writer removes the param entirely when the query is empty, so a
    // restored entry WITHOUT `?q=` genuinely means "empty filter". onPopState must
    // clear the query (not preserve the current filter) — the `?? ""` fallback, not
    // `?? prefs.query`.
    window.history.replaceState(null, "", "/?q=type%3Abug");
    render(App);

    expect((screen.getByTestId("filter-keyword") as HTMLInputElement).value).toBe("type:bug");

    // Back to a no-filter entry (no `?q=` param).
    window.history.replaceState({ nibId: null }, "", "/");
    window.dispatchEvent(new PopStateEvent("popstate", { state: { nibId: null } }));

    await waitFor(() => {
      expect((screen.getByTestId("filter-keyword") as HTMLInputElement).value).toBe("");
    });
  });
});
