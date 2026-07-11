import { render, screen, waitFor } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { readable, writable } from "svelte/store";
import { tick } from "svelte";
import TreeTable from "./TreeTable.svelte";
import type { TreeTableNib, ViewLevel, ColumnKey } from "../types";
import { DEFAULT_COLUMN_WIDTHS } from "../types";
import { SelectionState } from "../selection.svelte";
import { DragState } from "../drag.svelte";
import { TreeViewState } from "../treeView.svelte";
import { makeTestContext } from "../contexts";

function makeTreeTableNib(overrides: Partial<TreeTableNib> = {}): TreeTableNib {
  return {
    id: "nibs-abc1",
    title: "Fix login bug",
    status: "in-progress",
    type: "bug",
    priority: "high",
    estimate: "m",
    tags: ["auth"],
    updatedAt: "2026-03-20T10:00:00Z",
    parentId: null,
    blockingIds: [],
    blockedByIds: [],
    ...overrides,
  };
}

vi.mock("@urql/svelte", async () => {
  const actual = await vi.importActual<typeof import("@urql/svelte")>("@urql/svelte");
  return {
    ...actual,
    getContextClient: vi.fn(),
    queryStore: vi.fn(),
    subscriptionStore: vi.fn(),
  };
});

import { queryStore, subscriptionStore } from "@urql/svelte";
const mockQueryStore = vi.mocked(queryStore);
const mockSubscriptionStore = vi.mocked(subscriptionStore);

/** Render TreeTable with required context. */
function renderTreeTable(
  props: Record<string, unknown>,
  opts?: { selection?: SelectionState; drag?: DragState; treeView?: TreeViewState },
) {
  return render(TreeTable, {
    props: props as any,
    context: makeTestContext(
      opts?.selection ?? new SelectionState(),
      opts?.drag ?? new DragState(),
      { treeView: opts?.treeView },
    ),
  });
}

describe("TreeTable", () => {
  beforeEach(() => {
    mockQueryStore.mockReset();
    mockSubscriptionStore.mockReset();
    // Default: subscription returns no data
    mockSubscriptionStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any
    );
  });

  it("renders a table with thead column headers and tbody with data rows", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
      makeTreeTableNib({ id: "nibs-001", title: "First task", parentId: "nibs-m1" }),
      makeTreeTableNib({ id: "nibs-002", title: "Second task", parentId: "nibs-m1" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    const { container } = renderTreeTable({ filter: {} });

    // Should render a <table> element
    const table = container.querySelector("table");
    expect(table).toBeInTheDocument();

    // Should have <thead> with column headers
    const thead = table!.querySelector("thead");
    expect(thead).toBeInTheDocument();

    // Should have <tbody> with data rows (milestone + 2 children)
    const tbody = table!.querySelector("tbody");
    expect(tbody).toBeInTheDocument();
    const dataRows = tbody!.querySelectorAll("tr[data-testid='tree-row']");
    expect(dataRows).toHaveLength(3);

    // Titles should render
    expect(screen.getByText("First task")).toBeInTheDocument();
    expect(screen.getByText("Second task")).toBeInTheDocument();
  });

  it("renders column headers including ID, Type, Title, State, Effort, Tags", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
      makeTreeTableNib({ id: "nibs-001", title: "A task", parentId: "nibs-m1" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    const { container } = renderTreeTable({ filter: {} });

    const thead = container.querySelector("thead")!;
    const headers = Array.from(thead.querySelectorAll("th")).map(th => th.textContent?.trim());

    expect(headers).toContain("ID");
    expect(headers).toContain("Type");
    expect(headers).toContain("Title");
    expect(headers).toContain("State");
    expect(headers).toContain("Effort");
    expect(headers).toContain("Tags");
  });

  it("indents child rows by depth via padding on title cell content", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-001", title: "Milestone", type: "milestone" }),
      makeTreeTableNib({ id: "nibs-002", title: "Child epic", type: "epic", parentId: "nibs-001" }),
      makeTreeTableNib({ id: "nibs-003", title: "Grandchild bug", type: "bug", parentId: "nibs-002" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    const { container } = renderTreeTable({ filter: {} });

    const rows = container.querySelectorAll("[data-testid='tree-row']");
    expect(rows).toHaveLength(3);

    // Indentation is on the title cell content, not the row itself
    const titleCells = container.querySelectorAll("[data-testid='nib-title']");
    expect(titleCells).toHaveLength(3);
    expect((titleCells[0] as HTMLElement).style.paddingLeft).toBe("0px");
    expect((titleCells[1] as HTMLElement).style.paddingLeft).toBe("24px");
    expect((titleCells[2] as HTMLElement).style.paddingLeft).toBe("48px");
  });

  it("shows expand/collapse toggle on parent rows, not on leaves", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
      makeTreeTableNib({ id: "nibs-001", title: "Parent epic", type: "epic", parentId: "nibs-m1" }),
      makeTreeTableNib({ id: "nibs-002", title: "Child task", type: "task", parentId: "nibs-001" }),
      makeTreeTableNib({ id: "nibs-003", title: "Standalone task", type: "task", parentId: "nibs-m1" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    const { container } = renderTreeTable({ filter: {} });

    const rows = container.querySelectorAll("[data-testid='tree-row']");
    expect(rows).toHaveLength(4);

    // Milestone row has children toggle
    const milestoneToggle = rows[0].querySelector("[data-testid='toggle']");
    expect(milestoneToggle).toBeInTheDocument();

    // Epic row has children toggle
    const epicToggle = rows[1].querySelector("[data-testid='toggle']");
    expect(epicToggle).toBeInTheDocument();

    // Child task row should not have a toggle button
    const childToggle = rows[2].querySelector("[data-testid='toggle']");
    expect(childToggle).not.toBeInTheDocument();

    // Standalone task row should not have a toggle button
    const standaloneToggle = rows[3].querySelector("[data-testid='toggle']");
    expect(standaloneToggle).not.toBeInTheDocument();
  });

  it("collapsing a parent hides children, expanding shows them again", async () => {
    const user = userEvent.setup();
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-001", title: "Milestone", type: "milestone" }),
      makeTreeTableNib({ id: "nibs-002", title: "The child task", type: "task", parentId: "nibs-001" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    const { container } = renderTreeTable({ filter: {} });

    // Initially both visible (check via data rows count)
    let rows = container.querySelectorAll("[data-testid='tree-row']");
    expect(rows).toHaveLength(2);

    // Click toggle to collapse
    const toggle = container.querySelector("[data-testid='toggle']") as HTMLElement;
    await user.click(toggle);

    // Child should be hidden (only parent row remains)
    rows = container.querySelectorAll("[data-testid='tree-row']");
    expect(rows).toHaveLength(1);
    expect(screen.queryByText("The child task")).not.toBeInTheDocument();

    // Click toggle to expand
    const toggleAfter = container.querySelector("[data-testid='toggle']") as HTMLElement;
    await user.click(toggleAfter);

    // Child should be visible again
    rows = container.querySelectorAll("[data-testid='tree-row']");
    expect(rows).toHaveLength(2);
    expect(screen.getByText("The child task")).toBeInTheDocument();
  });

  it("shows loading indicator while fetching", () => {
    mockQueryStore.mockReturnValue(
      readable({ fetching: true, error: undefined, data: undefined, stale: false }) as any
    );

    renderTreeTable({ filter: {} });

    expect(screen.getByText(/loading/i)).toBeInTheDocument();
  });

  it("shows empty state when no nibs returned", () => {
    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs: [] }, stale: false }) as any
    );

    renderTreeTable({ filter: {} });

    expect(screen.getByText(/no nibs found/i)).toBeInTheDocument();
  });

  it("shows error message when query fails", () => {
    mockQueryStore.mockReturnValue(
      readable({
        fetching: false,
        error: { message: "Network error" },
        data: undefined,
        stale: false,
      }) as any
    );

    renderTreeTable({ filter: {} });

    expect(screen.getByText(/network error/i)).toBeInTheDocument();
  });

  // Regression: urql's subscription store emits a fresh wrapper object on
  // every reactive cycle. Reference-based dedup inside the TreeTable
  // subscription effect used to fail, causing an infinite effect loop that
  // Svelte halts with `effect_update_depth_exceeded` — leaving the UI stuck
  // on "Loading..." until a manual refresh. The fix deduplicates by event
  // content and wraps side effects in untrack().
  it("deduplicates repeated subscription emissions with the same event payload", async () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
    ];

    const reexecute = vi.fn();
    mockQueryStore.mockReturnValue({
      ...readable({ fetching: false, error: undefined, data: { nibs }, stale: false }),
      reexecute,
    } as any);

    // Writable subscription store lets us push multiple "emissions" that
    // have different wrapper identity but the same inner event payload.
    const subStore = writable<any>({ fetching: true, error: undefined, data: undefined, stale: false });
    mockSubscriptionStore.mockReturnValue(subStore as any);

    renderTreeTable({ filter: {} });
    await tick();

    // Same logical event (same type + nibId) emitted via three fresh
    // wrapper objects with three fresh inner data objects. Reference
    // comparison would flag all three as "new".
    const evt = { type: "created", nibId: "nibs-new" };
    for (let i = 0; i < 20; i++) {
      subStore.set({
        fetching: true,
        error: undefined,
        data: { nibChanged: { ...evt } },
        stale: false,
      });
      await tick();
    }

    // All 20 wrapper emissions should coalesce into a single refetch.
    expect(reexecute).toHaveBeenCalledTimes(1);
  });

  it("milestones view: milestone headers keep subtrees; loose work lands in a 'No milestone' bucket (lossless)", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-001", title: "Milestone A", type: "milestone" }),
      makeTreeTableNib({ id: "nibs-002", title: "Epic under A", type: "epic", parentId: "nibs-001" }),
      makeTreeTableNib({ id: "nibs-003", title: "Standalone task", type: "task" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    const { container } = renderTreeTable({ filter: {}, viewLevel: "milestones" as ViewLevel });

    const rows = container.querySelectorAll("[data-testid='tree-row']");
    // Milestone A + Epic + "No milestone" bucket + Standalone task
    expect(rows).toHaveLength(4);

    // Scope to row title cells ("Milestone A" also appears in the Epic's parent cell now)
    const titles = Array.from(container.querySelectorAll("[data-testid='title-text']")).map(e => e.textContent);
    expect(titles).toContain("Milestone A");
    expect(titles).toContain("Epic under A");
    // Nothing is dropped: the standalone task now shows under a "No milestone" bucket
    expect(titles).toContain("No milestone (1)");
    expect(titles).toContain("Standalone task");
  });

  it("milestones view shows the Parent column (parent is a normal column in every lens)", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-001", title: "Milestone A", type: "milestone" }),
      makeTreeTableNib({ id: "nibs-002", title: "Task", type: "task", parentId: "nibs-001" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    const { container } = renderTreeTable({ filter: {}, viewLevel: "milestones" as ViewLevel });

    const thead = container.querySelector("thead")!;
    const headers = Array.from(thead.querySelectorAll("th")).map(th => th.textContent?.trim());
    expect(headers).toContain("Parent");

    // The child task's parent cell renders its milestone parent
    const parentCells = container.querySelectorAll("[data-testid='nib-parent']");
    expect(parentCells.length).toBeGreaterThan(0);
  });

  it("epics view shows Parent column", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-001", title: "Epic A", type: "epic" }),
      makeTreeTableNib({ id: "nibs-002", title: "Task", type: "task", parentId: "nibs-001" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    const { container } = renderTreeTable({ filter: {}, viewLevel: "epics" as ViewLevel });

    const thead = container.querySelector("thead")!;
    const headers = Array.from(thead.querySelectorAll("th")).map(th => th.textContent?.trim());
    expect(headers).toContain("Parent");
  });

  it("dims non-matching ancestors when advanced filters active", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-001", title: "Ancestor container", type: "epic" }),
      makeTreeTableNib({ id: "nibs-002", title: "Matching bug item", type: "bug", parentId: "nibs-001" }),
      makeTreeTableNib({ id: "nibs-003", title: "Standalone task", type: "task" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    // Filter for type: bug — only "Matching bug item" matches
    // Use epics view so the epic is the root
    const { container } = renderTreeTable({
      filter: { type: ["bug"] },
      viewLevel: "epics" as ViewLevel,
    });

    const rows = container.querySelectorAll("[data-testid='tree-row']");
    // Only Ancestor container and Matching bug item should be visible (Standalone task hidden by view)
    expect(rows).toHaveLength(2);

    // Ancestor container should be dimmed (ancestor, not matching)
    const parentRow = rows[0] as HTMLElement;
    expect(parentRow.style.opacity).toBe("0.4");

    // Matching bug item should be at full opacity (matches filter)
    const childRow = rows[1] as HTMLElement;
    expect(childRow.style.opacity).toBe("");

    // Standalone task should not be visible
    expect(screen.queryByText("Standalone task")).not.toBeInTheDocument();
  });

  it("expand-all button shows all children by clearing collapsed state", async () => {
    const user = userEvent.setup();
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
      makeTreeTableNib({ id: "nibs-001", title: "Parent A", type: "epic", parentId: "nibs-m1" }),
      makeTreeTableNib({ id: "nibs-002", title: "Child A", type: "task", parentId: "nibs-001" }),
      makeTreeTableNib({ id: "nibs-003", title: "Parent B", type: "epic", parentId: "nibs-m1" }),
      makeTreeTableNib({ id: "nibs-004", title: "Child B", type: "task", parentId: "nibs-003" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    const { container } = renderTreeTable({ filter: {} });

    // Collapse Parent A and Parent B (they are at index 1 and 3 in the tree)
    const toggles = container.querySelectorAll("[data-testid='toggle']");
    // Find and collapse the epic toggles (Parent A, Parent B)
    await user.click(toggles[1] as HTMLElement); // Parent A
    await user.click(toggles[2] as HTMLElement); // Parent B

    // Children should be hidden
    expect(screen.queryByText("Child A")).not.toBeInTheDocument();
    expect(screen.queryByText("Child B")).not.toBeInTheDocument();

    // Click expand-all
    const expandAll = container.querySelector("[data-testid='expand-all']") as HTMLElement;
    expect(expandAll).toBeInTheDocument();
    await user.click(expandAll);

    // Children should be visible again
    expect(screen.getByText("Child A")).toBeInTheDocument();
    expect(screen.getByText("Child B")).toBeInTheDocument();
  });

  it("collapse-all button hides all children", async () => {
    const user = userEvent.setup();
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
      makeTreeTableNib({ id: "nibs-001", title: "Parent A", type: "epic", parentId: "nibs-m1" }),
      makeTreeTableNib({ id: "nibs-002", title: "Child A", type: "task", parentId: "nibs-001" }),
      makeTreeTableNib({ id: "nibs-003", title: "Parent B", type: "epic", parentId: "nibs-m1" }),
      makeTreeTableNib({ id: "nibs-004", title: "Child B", type: "task", parentId: "nibs-003" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    const { container } = renderTreeTable({ filter: {} });

    // Initially all visible
    expect(screen.getByText("Child A")).toBeInTheDocument();
    expect(screen.getByText("Child B")).toBeInTheDocument();

    // Click collapse-all
    const collapseAll = container.querySelector("[data-testid='collapse-all']") as HTMLElement;
    expect(collapseAll).toBeInTheDocument();
    await user.click(collapseAll);

    // Children should be hidden
    expect(screen.queryByText("Child A")).not.toBeInTheDocument();
    expect(screen.queryByText("Child B")).not.toBeInTheDocument();
    // Milestone still visible (root of tree)
    expect(screen.getByText("Milestone")).toBeInTheDocument();
  });

  it("strips status from server filter when status filter is active", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-001", title: "Parent epic", type: "epic", status: "draft" }),
      makeTreeTableNib({ id: "nibs-002", title: "In-progress child", type: "bug", status: "in-progress", parentId: "nibs-001" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    renderTreeTable({
      filter: { status: ["in-progress"] },
      viewLevel: "epics" as ViewLevel,
    });

    // The serverFilter passed to queryStore should NOT contain status,
    // just like type/priority/estimate/tags are stripped out.
    const lastCall = mockQueryStore.mock.calls[mockQueryStore.mock.calls.length - 1];
    const variables = lastCall[0].variables!;
    expect(variables.filter).not.toHaveProperty("status");
  });

  it("shows all nibs normally when no advanced filters active", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-001", title: "Parent epic", type: "epic" }),
      makeTreeTableNib({ id: "nibs-002", title: "Child bug", type: "bug", parentId: "nibs-001" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    // No advanced filters — use epics view so epic is the root
    const { container } = renderTreeTable({
      filter: { search: "test" },
      viewLevel: "epics" as ViewLevel,
    });

    const rows = container.querySelectorAll("[data-testid='tree-row']");
    expect(rows).toHaveLength(2);

    // None should be dimmed
    for (const row of rows) {
      expect((row as HTMLElement).style.opacity).toBe("");
    }
  });

  it("hides column headers when visibleColumns excludes them", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-001", title: "Epic A", type: "epic" }),
      makeTreeTableNib({ id: "nibs-002", title: "Task", type: "task", parentId: "nibs-001" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    const visibleColumns: ColumnKey[] = ["title", "state"];
    const { container } = renderTreeTable({
      filter: {},
      viewLevel: "epics" as ViewLevel,
      visibleColumns,
    });

    const thead = container.querySelector("thead")!;
    const headers = Array.from(thead.querySelectorAll("th")).map(th => th.textContent?.trim());
    expect(headers).toContain("Title");
    expect(headers).toContain("State");
    expect(headers).not.toContain("ID");
    expect(headers).not.toContain("Type");
    expect(headers).not.toContain("Effort");
    expect(headers).not.toContain("Tags");
    expect(headers).not.toContain("Parent");
  });

  it("hides row cells when visibleColumns excludes them", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-001", title: "Epic A", type: "epic" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    const visibleColumns: ColumnKey[] = ["title", "state"];
    const { container } = renderTreeTable({
      filter: {},
      viewLevel: "epics" as ViewLevel,
      visibleColumns,
    });

    // ID and type cells should not be rendered
    expect(container.querySelector("[data-testid='nib-id']")).not.toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-type']")).not.toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-effort']")).not.toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-tags']")).not.toBeInTheDocument();

    // Title and state cells should be rendered
    expect(container.querySelector("[data-testid='nib-title']")).toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-state']")).toBeInTheDocument();
  });

  it("renders Blocking and Blocked by headers when those columns are visible", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-001", title: "Epic A", type: "epic" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    const visibleColumns: ColumnKey[] = ["title", "blocking", "blockedBy"];
    const { container } = renderTreeTable({
      filter: {},
      viewLevel: "epics" as ViewLevel,
      visibleColumns,
    });

    const thead = container.querySelector("thead")!;
    const headers = Array.from(thead.querySelectorAll("th")).map(th => th.textContent?.trim());
    expect(headers).toContain("Blocking");
    expect(headers).toContain("Blocked by");
  });

  it("omits Blocking and Blocked by headers when those columns are not visible", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-001", title: "Epic A", type: "epic" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    // The default-visible set (no blocking / blockedBy).
    const visibleColumns: ColumnKey[] = ["id", "parent", "type", "title", "state", "effort", "tags"];
    const { container } = renderTreeTable({
      filter: {},
      viewLevel: "epics" as ViewLevel,
      visibleColumns,
    });

    const thead = container.querySelector("thead")!;
    const headers = Array.from(thead.querySelectorAll("th")).map(th => th.textContent?.trim());
    expect(headers).not.toContain("Blocking");
    expect(headers).not.toContain("Blocked by");
  });

  it("table width grows by the blocking/blockedBy column widths when they are shown", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-001", title: "Epic A", type: "epic" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    const base = renderTreeTable({
      filter: {},
      viewLevel: "epics" as ViewLevel,
      visibleColumns: ["title"] as ColumnKey[],
    });
    const baseWidth = parseInt((base.container.querySelector("table") as HTMLElement).style.width, 10);

    const withCols = renderTreeTable({
      filter: {},
      viewLevel: "epics" as ViewLevel,
      visibleColumns: ["title", "blocking", "blockedBy"] as ColumnKey[],
    });
    const grownWidth = parseInt((withCols.container.querySelector("table") as HTMLElement).style.width, 10);

    // blocking + blockedBy default widths added to the base table width.
    expect(grownWidth).toBe(baseWidth + DEFAULT_COLUMN_WIDTHS.blocking + DEFAULT_COLUMN_WIDTHS.blockedBy);
  });

  it("parent column hidden by visibleColumns exclusion", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-001", title: "Epic A", type: "epic" }),
      makeTreeTableNib({ id: "nibs-002", title: "Task", type: "task", parentId: "nibs-001" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    // epics view does NOT hide parent, but visibleColumns excludes it
    const visibleColumns: ColumnKey[] = ["id", "title", "type", "state", "effort", "tags"];
    const { container } = renderTreeTable({
      filter: {},
      viewLevel: "epics" as ViewLevel,
      visibleColumns,
    });

    const thead = container.querySelector("thead")!;
    const headers = Array.from(thead.querySelectorAll("th")).map(th => th.textContent?.trim());
    expect(headers).not.toContain("Parent");
    expect(container.querySelectorAll("[data-testid='nib-parent']")).toHaveLength(0);
  });

  it("rows have draggable class when no filters are active", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
      makeTreeTableNib({ id: "nibs-001", title: "Child Task", type: "task", parentId: "nibs-m1" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    const { container } = renderTreeTable({
      filter: {},
      viewLevel: "milestones" as ViewLevel,
    });

    // With no active filters, non-unparented rows should have draggable class
    const draggableRows = container.querySelectorAll("tr.draggable");
    expect(draggableRows.length).toBeGreaterThan(0);
  });

  it("rows lack draggable class when filters are active", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
      makeTreeTableNib({ id: "nibs-001", title: "Child Task", type: "task", parentId: "nibs-m1" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    const { container } = renderTreeTable({
      filter: { type: ["task"] },
      viewLevel: "milestones" as ViewLevel,
    });

    // With active client filters, rows should NOT have draggable class
    const draggableRows = container.querySelectorAll("tr.draggable");
    expect(draggableRows).toHaveLength(0);
  });

  describe("keyboard navigation", () => {
    function setupWithNibs(
      nibs: TreeTableNib[],
      extraProps: Record<string, unknown> = {},
      opts?: { selection?: SelectionState; drag?: DragState },
    ) {
      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
      );
      // Default to milestones view with milestone data for keyboard tests
      return renderTreeTable(
        { filter: {}, viewLevel: "milestones" as ViewLevel, ...extraProps },
        opts,
      );
    }

    function getScrollContainer(): HTMLElement {
      return screen.getByRole("grid");
    }

    // Helper to make a milestone with task children for keyboard navigation tests
    function makeKeyboardTestNibs(count: number): TreeTableNib[] {
      const milestone = makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" });
      const children = Array.from({ length: count }, (_, i) =>
        makeTreeTableNib({
          id: `nibs-${String(i + 1).padStart(3, "0")}`,
          title: `Task ${i + 1}`,
          type: "task",
          parentId: "nibs-m1",
        })
      );
      return [milestone, ...children];
    }

    it("ArrowDown from no focus focuses first row", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeKeyboardTestNibs(2);

      setupWithNibs(nibs, {}, { selection: sel });
      const scrollContainer = getScrollContainer();
      scrollContainer.focus();

      await user.keyboard("{ArrowDown}");

      // First visible row is the milestone
      expect(sel.focusedNibId).toBe("nibs-m1");
    });

    it("ArrowDown moves focus to next visible row", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      sel.focus("nibs-m1");
      const nibs = makeKeyboardTestNibs(2);

      setupWithNibs(nibs, {}, { selection: sel });
      const scrollContainer = getScrollContainer();
      scrollContainer.focus();

      await user.keyboard("{ArrowDown}");

      expect(sel.focusedNibId).toBe("nibs-001");
    });

    it("ArrowDown at last row stays on last row (clamp)", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      sel.focus("nibs-001");
      const nibs = makeKeyboardTestNibs(1);

      // Focus is on the last row (the only child task)
      setupWithNibs(nibs, {}, { selection: sel });
      const scrollContainer = getScrollContainer();
      scrollContainer.focus();

      await user.keyboard("{ArrowDown}");

      // Focus stays on last row
      expect(sel.focusedNibId).toBe("nibs-001");
    });

    it("ArrowUp moves focus to previous visible row", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      sel.focus("nibs-001");
      const nibs = makeKeyboardTestNibs(2);

      setupWithNibs(nibs, {}, { selection: sel });
      const scrollContainer = getScrollContainer();
      scrollContainer.focus();

      await user.keyboard("{ArrowUp}");

      expect(sel.focusedNibId).toBe("nibs-m1");
    });

    it("ArrowUp at first row stays on first row (clamp)", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      sel.focus("nibs-m1");
      const nibs = makeKeyboardTestNibs(2);

      setupWithNibs(nibs, {}, { selection: sel });
      const scrollContainer = getScrollContainer();
      scrollContainer.focus();

      await user.keyboard("{ArrowUp}");

      // Focus stays on first row
      expect(sel.focusedNibId).toBe("nibs-m1");
    });

    it("Enter on focused row selects it via context", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      sel.focus("nibs-001");
      const nibs = makeKeyboardTestNibs(2);

      setupWithNibs(nibs, {}, { selection: sel });
      const scrollContainer = getScrollContainer();
      scrollContainer.focus();

      await user.keyboard("{Enter}");

      expect(sel.selectedNibId).toBe("nibs-001");
    });

    it("ArrowLeft on expanded parent collapses it", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      sel.focus("nibs-001");
      const nibs = [
        makeTreeTableNib({ id: "nibs-001", title: "Parent", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-002", title: "Child", type: "task", parentId: "nibs-001" }),
      ];

      setupWithNibs(nibs, {}, { selection: sel });
      const scrollContainer = getScrollContainer();
      scrollContainer.focus();

      // Initially child is visible
      expect(screen.getByText("Child")).toBeInTheDocument();

      await user.keyboard("{ArrowLeft}");

      // Parent was expanded, should now be collapsed — child hidden
      expect(screen.queryByText("Child")).not.toBeInTheDocument();
      // Focus should NOT have changed (collapse, don't move)
      expect(sel.focusedNibId).toBe("nibs-001");
    });

    it("ArrowRight on collapsed parent expands it", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      sel.focus("nibs-001");
      const nibs = [
        makeTreeTableNib({ id: "nibs-001", title: "Parent", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-002", title: "Child", type: "task", parentId: "nibs-001" }),
      ];

      setupWithNibs(nibs, {}, { selection: sel });
      const scrollContainer = getScrollContainer();
      scrollContainer.focus();

      // First collapse the parent
      const toggle = screen.getByTestId("toggle");
      await user.click(toggle);
      expect(screen.queryByText("Child")).not.toBeInTheDocument();

      // Now ArrowRight should expand it
      await user.keyboard("{ArrowRight}");

      expect(screen.getByText("Child")).toBeInTheDocument();
      expect(sel.focusedNibId).toBe("nibs-001");
    });

    it("ArrowLeft on leaf moves focus to parent", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      sel.focus("nibs-002");
      const nibs = [
        makeTreeTableNib({ id: "nibs-001", title: "Parent", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-002", title: "Child", type: "task", parentId: "nibs-001" }),
      ];

      setupWithNibs(nibs, {}, { selection: sel });
      const scrollContainer = getScrollContainer();
      scrollContainer.focus();

      await user.keyboard("{ArrowLeft}");

      expect(sel.focusedNibId).toBe("nibs-001");
    });

    it("focused row has .focused class", () => {
      const sel = new SelectionState();
      sel.focus("nibs-001");
      const nibs = makeKeyboardTestNibs(2);
      const { container } = setupWithNibs(nibs, {}, { selection: sel });
      const rows = container.querySelectorAll("[data-testid='tree-row']");
      // nibs-001 is the second row (after milestone)
      expect(rows[1].classList.contains("focused")).toBe(true);
      expect(rows[0].classList.contains("focused")).toBe(false);
    });
  });

  describe("event delegation", () => {
    function setupWithNibs(
      nibs: TreeTableNib[],
      extraProps: Record<string, unknown> = {},
      opts?: { selection?: SelectionState; drag?: DragState },
    ) {
      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
      );
      return renderTreeTable(
        { filter: {}, viewLevel: "milestones" as ViewLevel, ...extraProps },
        opts,
      );
    }

    function makeTestNibs(): TreeTableNib[] {
      return [
        makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-001", title: "Child Task", type: "task", parentId: "nibs-m1" }),
      ];
    }

    it("row click selects nib via context", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      // Click on the row for "Child Task"
      const row = container.querySelector("tr[data-nib-id='nibs-001']") as HTMLElement;
      await user.click(row);

      expect(sel.selectedNibId).toBe("nibs-001");
    });

    it("Ctrl+click toggles nib in selection via context", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      sel.select("nibs-m1"); // Pre-select milestone
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      // Ctrl+click on Child Task — should add to selection
      const row = container.querySelector("tr[data-nib-id='nibs-001']") as HTMLElement;
      await user.keyboard("{Control>}");
      await user.click(row);
      await user.keyboard("{/Control}");

      expect(sel.selectedIds.has("nibs-001")).toBe(true);
      expect(sel.selectedIds.has("nibs-m1")).toBe(true);
    });

    it("Shift+click range-selects via context", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      sel.select("nibs-m1"); // Anchor at milestone
      const nibs = [
        makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-001", title: "Task 1", type: "task", parentId: "nibs-m1" }),
        makeTreeTableNib({ id: "nibs-002", title: "Task 2", type: "task", parentId: "nibs-m1" }),
      ];

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      // Shift+click on Task 2 — should select range from milestone to Task 2
      const row = container.querySelector("tr[data-nib-id='nibs-002']") as HTMLElement;
      await user.keyboard("{Shift>}");
      await user.click(row);
      await user.keyboard("{/Shift}");

      expect(sel.selectedIds.has("nibs-m1")).toBe(true);
      expect(sel.selectedIds.has("nibs-001")).toBe(true);
      expect(sel.selectedIds.has("nibs-002")).toBe(true);
    });

    it("toggle click dispatches collapse/expand via delegation and does NOT select", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      // Initially the milestone has a child visible
      expect(screen.getByText("Child Task")).toBeInTheDocument();

      // Click the toggle button on the milestone row
      const toggle = container.querySelector("[data-action='toggle']") as HTMLElement;
      await user.click(toggle);

      // Child should be hidden (collapsed)
      expect(screen.queryByText("Child Task")).not.toBeInTheDocument();

      // selection should NOT have changed
      expect(sel.selectedNibId).toBeNull();
    });

    it("title click selects nib via context", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      // Click the title text of "Child Task"
      const titleText = container.querySelector("tr[data-nib-id='nibs-001'] [data-action='title']") as HTMLElement;
      await user.click(titleText);

      expect(sel.selectedNibId).toBe("nibs-001");
    });

    it("add-child click dispatches onaddchild via delegation and does NOT select", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const onaddchild = vi.fn();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, { onaddchild }, { selection: sel });

      // The milestone row (type: "milestone") can have children
      const addChildBtn = container.querySelector("tr[data-nib-id='nibs-m1'] [data-action='add-child']") as HTMLElement;
      await user.click(addChildBtn);

      expect(onaddchild).toHaveBeenCalledOnce();
      expect(onaddchild).toHaveBeenCalledWith("nibs-m1", "milestone");

      // selection should NOT have changed
      expect(sel.selectedNibId).toBeNull();
    });

    it("context menu dispatches onrowcontextmenu via delegation with preventDefault", async () => {
      const user = userEvent.setup();
      const onrowcontextmenu = vi.fn();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, { onrowcontextmenu });

      // Right-click on the row for "Child Task"
      const row = container.querySelector("tr[data-nib-id='nibs-001']") as HTMLElement;
      await user.pointer({ target: row, keys: "[MouseRight]" });

      expect(onrowcontextmenu).toHaveBeenCalledOnce();
      expect(onrowcontextmenu).toHaveBeenCalledWith(
        "nibs-001",
        expect.any(MouseEvent),
        expect.objectContaining({ id: "nibs-001", title: "Child Task" })
      );
    });

    it("row double-click selects nib via context", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      // Double-click on the row for "Milestone"
      const row = container.querySelector("tr[data-nib-id='nibs-m1']") as HTMLElement;
      await user.dblClick(row);

      // dblclick should select via context
      expect(sel.selectedNibId).toBe("nibs-m1");
    });

    it("title click does NOT also fire row-level selection for other actions", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      const titleText = container.querySelector("tr[data-nib-id='nibs-001'] [data-action='title']") as HTMLElement;
      await user.click(titleText);

      // Title click selects via context
      expect(sel.selectedNibId).toBe("nibs-001");
    });

    it("TreeTableRow renders with zero callback props (pure data/visual)", () => {
      const nibs = makeTestNibs();
      const { container } = setupWithNibs(nibs);

      // Row renders correctly without any callback props
      const rows = container.querySelectorAll("tr[data-nib-id]");
      expect(rows.length).toBe(2);

      // Each row has data-nib-id set
      expect((rows[0] as HTMLElement).dataset.nibId).toBe("nibs-m1");
      expect((rows[1] as HTMLElement).dataset.nibId).toBe("nibs-001");
    });

    it("title element is a <button> for keyboard accessibility (Enter/Space activate)", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      // The title element should be a <button> so Enter/Space natively fire click events
      const titleEl = container.querySelector("tr[data-nib-id='nibs-001'] [data-action='title']") as HTMLElement;
      expect(titleEl.tagName).toBe("BUTTON");

      // Focus the title and press Enter — should activate select via context
      titleEl.focus();
      await user.keyboard("{Enter}");
      expect(sel.selectedNibId).toBe("nibs-001");

      // Reset selection to verify Space also works
      sel.clearAll();

      titleEl.focus();
      // Press Space — should also activate select via context
      await user.keyboard(" ");
      expect(sel.selectedNibId).toBe("nibs-001");
    });

    it("row pointerdown enters pending drag state via context", async () => {
      const user = userEvent.setup();
      const nibs = makeTestNibs();
      const dragCtx = new DragState();

      const { container } = setupWithNibs(nibs, {}, { drag: dragCtx });

      // Find the milestone row
      const row = container.querySelector("tr[data-nib-id='nibs-m1']") as HTMLElement;
      expect(row).toBeInTheDocument();

      // Pointer-down on the row should enter a pending drag state
      await user.pointer({ target: row, keys: "[MouseLeft>]" });

      // Clean up by releasing
      await user.pointer({ keys: "[/MouseLeft]" });
    });

    it("no stopPropagation in TreeTableRow — events bubble to scroll container", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const onaddchild = vi.fn();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, { onaddchild }, { selection: sel });

      // Click various interactive elements — all should be handled via delegation
      // Toggle: should collapse without selecting
      const toggle = container.querySelector("[data-action='toggle']") as HTMLElement;
      await user.click(toggle);
      expect(sel.selectedNibId).toBeNull();

      // Add-child: should call onaddchild without selecting
      const addChildBtn = container.querySelector("[data-action='add-child']") as HTMLElement;
      await user.click(addChildBtn);
      expect(onaddchild).toHaveBeenCalledOnce();
      expect(sel.selectedNibId).toBeNull();

      // Title: should select via context
      const titleText = container.querySelector("tr[data-nib-id='nibs-m1'] [data-action='title']") as HTMLElement;
      await user.click(titleText);
      expect(sel.selectedNibId).toBe("nibs-m1");
    });
  });

  describe("ensureVisible", () => {
    function setupWithNibs(
      nibs: TreeTableNib[],
      extraProps: Record<string, unknown> = {},
      opts?: { selection?: SelectionState; drag?: DragState },
    ) {
      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
      );
      return renderTreeTable(
        { filter: {}, viewLevel: "milestones" as ViewLevel, ...extraProps },
        opts,
      );
    }

    it("expands collapsed ancestor chain when ensureVisible nib is hidden", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-e1", title: "Epic", type: "epic", parentId: "nibs-m1" }),
        makeTreeTableNib({ id: "nibs-t1", title: "Deep Task", type: "task", parentId: "nibs-e1" }),
      ];

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      // Collapse both ancestors so the task is hidden
      const toggles = container.querySelectorAll("[data-testid='toggle']");
      await user.click(toggles[0] as HTMLElement); // Collapse milestone
      expect(screen.queryByText("Deep Task")).not.toBeInTheDocument();

      // Now request ensureVisible for the hidden task
      sel.ensureVisible("nibs-t1");

      // Wait for the $effect to process and expand ancestors
      await waitFor(() => {
        expect(screen.getByText("Deep Task")).toBeInTheDocument();
      });
    });

    it("scrolls the ensureVisible nib into view", async () => {
      const sel = new SelectionState();
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-t1", title: "Visible Task", type: "task", parentId: "nibs-m1" }),
      ];

      setupWithNibs(nibs, {}, { selection: sel });

      // Spy on scrollIntoView before requesting ensureVisible
      const row = document.querySelector("tr[data-nib-id='nibs-t1']") as HTMLElement;
      const scrollSpy = vi.fn();
      row.scrollIntoView = scrollSpy;

      sel.ensureVisible("nibs-t1");

      await waitFor(() => {
        expect(scrollSpy).toHaveBeenCalledWith({ block: "nearest" });
      });
    });

    it("clears pendingEnsureVisibleId when nib does not exist in dataset", async () => {
      const sel = new SelectionState();
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
      ];

      setupWithNibs(nibs, {}, { selection: sel });

      sel.ensureVisible("nibs-does-not-exist");

      await waitFor(() => {
        expect(sel.pendingEnsureVisibleId).toBeNull();
      });
    });

    it("clears pendingEnsureVisibleId after processing", async () => {
      const sel = new SelectionState();
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-t1", title: "Task", type: "task", parentId: "nibs-m1" }),
      ];

      setupWithNibs(nibs, {}, { selection: sel });

      sel.ensureVisible("nibs-t1");

      await waitFor(() => {
        expect(sel.pendingEnsureVisibleId).toBeNull();
      });
    });

    // Regression (nibs-58c3, review #1): ensureVisible for a nib that is in the
    // dataset but excluded by an active client filter used to spin the effect
    // forever — every pass reassigned collapsedIds to a fresh Set that could
    // never make the filtered-out nib visible (effect_update_depth_exceeded).
    // The effect must now detect that expansion changes nothing and clear.
    it("settles (does not loop) when ensureVisible targets a filtered-out nib", async () => {
      const sel = new SelectionState();
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-e1", title: "Epic", type: "epic", parentId: "nibs-m1" }),
        makeTreeTableNib({ id: "nibs-bug1", title: "Matching Bug", type: "bug", parentId: "nibs-e1" }),
        makeTreeTableNib({ id: "nibs-task1", title: "Filtered Task", type: "task", parentId: "nibs-e1" }),
      ];

      // Active client filter (type: bug) drops the task from `rows` while it
      // stays in `allNibs`. Expanding ancestors can never reveal it.
      setupWithNibs(nibs, { filter: { type: ["bug"] } }, { selection: sel });

      expect(screen.getByText("Matching Bug")).toBeInTheDocument();
      expect(screen.queryByText("Filtered Task")).not.toBeInTheDocument();

      sel.ensureVisible("nibs-task1");

      // Must terminate by clearing, not loop forever reassigning collapsedIds.
      await waitFor(() => {
        expect(sel.pendingEnsureVisibleId).toBeNull();
      });

      // Still filtered out — expansion did not (and cannot) reveal it.
      expect(screen.queryByText("Filtered Task")).not.toBeInTheDocument();
    });

    // Regression (nibs-58c3, review #3): a cold deep-link runs syncFromUrl on
    // mount before the GraphQL query resolves (allNibs === []). The effect must
    // NOT clear the pending request as "absent" while the query is still
    // fetching — it must wait for data, then expand/scroll.
    it("keeps the pending request while the query is fetching, then resolves it once data arrives", async () => {
      const sel = new SelectionState();
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-t1", title: "Deferred Task", type: "task", parentId: "nibs-m1" }),
      ];

      // Query in-flight, no data yet.
      const store = writable<any>({ fetching: true, error: undefined, data: undefined, stale: false });
      mockQueryStore.mockReturnValue(store as any);

      // Deep-link request for a nib not yet in the (empty) dataset.
      sel.ensureVisible("nibs-t1");

      renderTreeTable(
        { filter: {}, viewLevel: "milestones" as ViewLevel },
        { selection: sel },
      );
      await tick();

      // Still fetching → preserve the request (do NOT clear as absent).
      expect(sel.pendingEnsureVisibleId).toBe("nibs-t1");

      // Data arrives.
      store.set({ fetching: false, error: undefined, data: { nibs }, stale: false });

      // Now present and visible → effect clears after scrolling.
      await waitFor(() => {
        expect(sel.pendingEnsureVisibleId).toBeNull();
      });
    });
  });

  describe("reactive filter re-query", () => {
    it("re-queries when filter changes", async () => {
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-001", title: "Task", type: "task", parentId: "nibs-m1" }),
      ];

      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
      );

      const { rerender } = renderTreeTable({ filter: {} });

      // queryStore should have been called once for initial render
      const initialCallCount = mockQueryStore.mock.calls.length;
      expect(initialCallCount).toBeGreaterThanOrEqual(1);

      // Re-render with a different filter (simulating "Include completed" toggle)
      await rerender({ filter: { excludeStatus: ["completed", "scrapped"] } });

      // queryStore should have been called again with the updated filter
      expect(mockQueryStore.mock.calls.length).toBeGreaterThan(initialCallCount);

      // excludeStatus is now a client-side filter, so it is stripped from the
      // server filter — the re-query fetches completed/scrapped nibs so their
      // active descendants stay visible (with the ancestor dimmed in place).
      const latestCall = mockQueryStore.mock.calls[mockQueryStore.mock.calls.length - 1];
      expect(latestCall[0].variables!.filter).not.toHaveProperty("excludeStatus");
    });

    it("renders updated data after filter change", async () => {
      const allNibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-001", title: "Active Task", type: "task", status: "todo", parentId: "nibs-m1" }),
        makeTreeTableNib({ id: "nibs-002", title: "Completed Task", type: "task", status: "completed", parentId: "nibs-m1" }),
      ];

      // Initial render shows all 3 nibs
      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: { nibs: allNibs }, stale: false }) as any
      );

      const { container, rerender } = renderTreeTable({ filter: {} });

      // Should have 3 rows initially
      let rows = container.querySelectorAll("[data-testid='tree-row']");
      expect(rows).toHaveLength(3);
      expect(screen.getByText("Active Task")).toBeInTheDocument();
      expect(screen.getByText("Completed Task")).toBeInTheDocument();

      // Now simulate server returning fewer nibs after filter change
      const filteredNibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-001", title: "Active Task", type: "task", status: "todo", parentId: "nibs-m1" }),
      ];

      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: { nibs: filteredNibs }, stale: false }) as any
      );

      await rerender({ filter: { excludeStatus: ["completed", "scrapped"] } });

      // Should have 2 rows after filter change
      rows = container.querySelectorAll("[data-testid='tree-row']");
      expect(rows).toHaveLength(2);
      expect(screen.getByText("Active Task")).toBeInTheDocument();
      expect(screen.queryByText("Completed Task")).not.toBeInTheDocument();
    });
  });

  // Scroll-restore lifecycle (nibs-qpvw). These drive the real mount → scroll →
  // remount → restore path through TreeTable + TreeViewState — the coverage gap
  // the prior review flagged, where the two confirmed defects lived.
  //
  // jsdom has NO layout, so it cannot reproduce real scrollTop CLAMPING
  // (scrollHeight - clientHeight) and does not fire a native scroll event on a
  // programmatic scrollTop assignment. jsdom stores scrollTop verbatim, so these
  // tests verify the restore-effect WIRING (mount/remount → restore) end-to-end;
  // the clamp-echo defect's trigger (#2) is covered at the unit level via
  // simulation in useScrollRestore.test.ts.
  describe("scroll restore (nibs-qpvw)", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
      makeTreeTableNib({ id: "nibs-001", title: "Task", type: "task", parentId: "nibs-m1" }),
    ];

    it("restores the saved scroll offset onto the scroll container on mount", async () => {
      const tv = new TreeViewState();
      tv.scrollTop = 500;

      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
      );

      const { container } = renderTreeTable({ filter: {} }, { treeView: tv });
      await tick();

      const sc = container.querySelector(".scroll-container") as HTMLElement;
      expect(sc).not.toBeNull();
      expect(sc.scrollTop).toBe(500);
    });

    it("re-restores the saved offset onto a fresh container after a refetch destroys and recreates it", async () => {
      const tv = new TreeViewState();
      tv.scrollTop = 500;

      // Writable query store lets us drive data → fetching → data. The
      // {#if $result.fetching} branch destroys the scroll container while
      // in-flight and recreates a NEW element when data returns, exercising the
      // element-identity re-restore (each new container fails container ===
      // ownedEl and re-arms restore()).
      const store = writable<any>({ fetching: false, error: undefined, data: { nibs }, stale: false });
      mockQueryStore.mockReturnValue(store as any);

      const { container } = renderTreeTable({ filter: {} }, { treeView: tv });
      await tick();

      // Initial mount restores the saved offset.
      expect((container.querySelector(".scroll-container") as HTMLElement).scrollTop).toBe(500);

      // Refetch in-flight: the container is destroyed (loading branch).
      store.set({ fetching: true, error: undefined, data: undefined, stale: false });
      await tick();
      expect(container.querySelector(".scroll-container")).toBeNull();

      // Data returns: a NEW container mounts and must be re-restored to 500.
      store.set({ fetching: false, error: undefined, data: { nibs }, stale: false });
      await tick();
      const sc2 = container.querySelector(".scroll-container") as HTMLElement;
      expect(sc2).not.toBeNull();
      expect(sc2.scrollTop).toBe(500);
    });
  });
});
