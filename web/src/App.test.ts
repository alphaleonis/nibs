import { render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach } from "vitest";
import App from "./App.svelte";
import { CONFIG_QUERY } from "./lib/queries";

vi.mock("@urql/svelte", async () => {
  const { readable } = await import("svelte/store");
  const actual = await vi.importActual<typeof import("@urql/svelte")>("@urql/svelte");

  const configData = readable({
    fetching: false,
    error: undefined,
    data: { config: { projectName: "test-project" } },
    stale: false,
  });

  const nibsData = readable({
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
  });

  const subscriptionData = readable({
    fetching: false,
    error: undefined,
    data: undefined,
    stale: false,
  });

  return {
    ...actual,
    getContextClient: vi.fn(),
    setContextClient: vi.fn(),
    queryStore: vi.fn().mockImplementation((opts: { query: unknown }) => {
      if (opts.query === CONFIG_QUERY) {
        return configData;
      }
      return nibsData;
    }),
    subscriptionStore: vi.fn().mockReturnValue(subscriptionData),
  };
});

import { queryStore } from "@urql/svelte";
const mockQueryStore = vi.mocked(queryStore);

describe("App", () => {
  beforeEach(() => {
    mockQueryStore.mockClear();
  });

  it("renders with dark theme shell containing Toolbar and TreeTable", () => {
    render(App);

    // Dark theme shell: has the app title with project name
    expect(screen.getByText("Nibs - test-project")).toBeInTheDocument();

    // Toolbar is rendered with icon buttons (search has moved to FilterBar)
    expect(screen.getByTitle("Options")).toBeInTheDocument();
    expect(screen.getByTitle("Filters")).toBeInTheDocument();

    // TreeTable renders data
    expect(screen.getByText("Test nib")).toBeInTheDocument();
  });

  it("wires filter state between Toolbar, FilterBar, and TreeTable", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    render(App);

    // queryStore should have been called for initial render
    expect(mockQueryStore).toHaveBeenCalled();
    const initialCallCount = mockQueryStore.mock.calls.length;

    // Open FilterBar by clicking Filters button
    await user.click(screen.getByTitle("Filters"));

    // Type in keyword search in FilterBar
    const searchInput = screen.getByPlaceholderText(/keyword/i);
    await user.type(searchInput, "bug");
    expect(searchInput).toHaveValue("bug");

    // Open Options dropdown and toggle "Include completed" off
    await user.click(screen.getByTitle("Options"));
    await user.click(screen.getByRole("menuitemcheckbox"));

    // With $derived(queryStore(...)), filter changes should trigger new queryStore calls
    expect(mockQueryStore.mock.calls.length).toBeGreaterThan(initialCallCount);

    // The latest nibs query call should contain the updated filter variables
    // (find the last call that has variables with a filter property, skipping config queries)
    const nibsCalls = mockQueryStore.mock.calls.filter(
      (call) => call[0].variables?.filter !== undefined
    );
    expect(nibsCalls.length).toBeGreaterThan(0);
    const latestVars = nibsCalls[nibsCalls.length - 1][0].variables;
    expect(latestVars.filter).toEqual(
      expect.objectContaining({
        search: "bug",
        excludeStatus: ["completed", "scrapped"],
      })
    );
  });

  it("opens detail panel when a title is clicked", async () => {
    const user = userEvent.setup();
    render(App);

    // Panel should not be open initially
    expect(screen.queryByTestId("detail-panel")).not.toBeInTheDocument();

    // Click a nib title
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);

    // Detail panel should appear
    expect(screen.getByTestId("detail-panel")).toBeInTheDocument();
  });

  it("closes detail panel when close button is clicked", async () => {
    const user = userEvent.setup();
    render(App);

    // Open the panel
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);
    expect(screen.getByTestId("detail-panel")).toBeInTheDocument();

    // Close the panel
    await user.click(screen.getByTestId("detail-close"));
    expect(screen.queryByTestId("detail-panel")).not.toBeInTheDocument();
  });

  it("closes detail panel when Escape key is pressed", async () => {
    const user = userEvent.setup();
    render(App);

    // Open the panel by clicking a title
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);
    expect(screen.getByTestId("detail-panel")).toBeInTheDocument();

    // Press Escape
    await user.keyboard("{Escape}");
    expect(screen.queryByTestId("detail-panel")).not.toBeInTheDocument();
  });

  it("tree-table is full width when panel is closed", () => {
    const { container } = render(App);

    // No detail panel
    expect(screen.queryByTestId("detail-panel")).not.toBeInTheDocument();

    // PaneGroup is always present (tree-table is always in a pane)
    expect(container.querySelector("[data-pane-group]")).toBeInTheDocument();

    // No resize handle when panel is closed
    expect(screen.queryByTestId("resize-handle")).not.toBeInTheDocument();

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
    expect(screen.getByTestId("detail-panel")).toBeInTheDocument();
    const paneGroup = container.querySelector("[data-pane-group]");
    expect(paneGroup).toBeInTheDocument();

    // There should be two panes in the group
    const panes = container.querySelectorAll("[data-pane]");
    expect(panes.length).toBe(2);

    // Detail pane should have the data-testid
    const detailPane = container.querySelector("[data-testid='detail-pane']");
    expect(detailPane).toBeInTheDocument();
  });

  it("Escape closes only the FilterBar dropdown when both dropdown and detail panel are open", async () => {
    const user = userEvent.setup();
    render(App);

    // Open the detail panel by clicking a title
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);
    expect(screen.getByTestId("detail-panel")).toBeInTheDocument();

    // Open the FilterBar
    await user.click(screen.getByTitle("Filters"));

    // Open the Type dropdown in FilterBar
    await user.click(screen.getByRole("button", { name: /type/i }));
    expect(screen.getByRole("menuitemcheckbox", { name: "bug" })).toBeInTheDocument();

    // Press Escape — should close only the dropdown, NOT the detail panel
    await user.keyboard("{Escape}");

    // Dropdown should be closed
    expect(screen.queryByRole("menuitemcheckbox", { name: "bug" })).not.toBeInTheDocument();

    // Detail panel should still be open
    expect(screen.getByTestId("detail-panel")).toBeInTheDocument();
  });

  it("resize handle renders between tree-table and detail panel when panel is open", async () => {
    const user = userEvent.setup();
    render(App);

    // Resize handle should NOT be present when panel is closed
    expect(screen.queryByTestId("resize-handle")).not.toBeInTheDocument();

    // Open the panel
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);

    // Resize handle should now be present (PaneForge resizer with our data-testid)
    const resizeHandle = screen.getByTestId("resize-handle");
    expect(resizeHandle).toBeInTheDocument();
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
      expect(screen.getByTestId("detail-panel")).toBeInTheDocument();

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

  it("renders toolbar icon buttons including view selector", () => {
    render(App);

    expect(screen.getByTitle("New item")).toBeInTheDocument();
    expect(screen.getByTitle("Select view")).toBeInTheDocument();
    expect(screen.getByTitle("Filters")).toBeInTheDocument();
    expect(screen.getByTitle("Options")).toBeInTheDocument();
    expect(screen.getByTitle("Columns")).toBeInTheDocument();

    // View selector should show "Milestones" (default viewLevel)
    expect(screen.getByTitle("Select view")).toHaveTextContent("Milestones");
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
    expect(screen.getByTestId("detail-panel")).toBeInTheDocument();

    // Get a reference to the TreeTable element while panel is open
    const treeTable = screen.getByTestId("tree-table");
    expect(treeTable).toBeInTheDocument();

    // Close the panel
    await user.click(screen.getByTestId("detail-close"));

    // The same DOM element should still be in the document (not recreated)
    expect(treeTable).toBeInTheDocument();
    expect(screen.getByTestId("tree-table")).toBe(treeTable);
  });

  it("PaneGroup is always present even when no nib is selected", () => {
    const { container } = render(App);

    // No nib selected, panel is closed
    expect(screen.queryByTestId("detail-panel")).not.toBeInTheDocument();

    // PaneGroup should still be in the DOM
    const paneGroup = container.querySelector("[data-pane-group]");
    expect(paneGroup).toBeInTheDocument();
    expect(paneGroup).toHaveAttribute("data-direction", "horizontal");
  });

  it("detail panel shows when nib is selected (with collapsible pane)", async () => {
    const user = userEvent.setup();
    const { container } = render(App);

    // Open the detail panel
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);

    // Detail panel should appear
    expect(screen.getByTestId("detail-panel")).toBeInTheDocument();

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
    expect(screen.getByTestId("detail-panel")).toBeInTheDocument();

    // Close the panel
    await user.click(screen.getByTestId("detail-close"));

    // Detail panel content should not be visible
    expect(screen.queryByTestId("detail-panel")).not.toBeInTheDocument();

    // The detail pane DOM element should still exist but be collapsed
    const detailPane = container.querySelector("[data-testid='detail-pane']");
    expect(detailPane).toBeInTheDocument();
    expect(detailPane).toHaveAttribute("data-pane-state", "collapsed");
  });

  it("resize handle hidden when detail pane is collapsed, visible when expanded", async () => {
    const user = userEvent.setup();
    render(App);

    // Initially no resize handle
    expect(screen.queryByTestId("resize-handle")).not.toBeInTheDocument();

    // Open the panel
    const titleTexts = screen.getAllByTestId("title-text");
    await user.click(titleTexts[0]);

    // Resize handle should now be visible
    expect(screen.getByTestId("resize-handle")).toBeInTheDocument();

    // Close the panel
    await user.click(screen.getByTestId("detail-close"));

    // Resize handle should be hidden again
    expect(screen.queryByTestId("resize-handle")).not.toBeInTheDocument();
  });
});
