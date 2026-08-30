import { render, screen, waitFor } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { readable, writable } from "svelte/store";
import { tick } from "svelte";
import TreeTable from "./TreeTable.svelte";
import type { TreeTableNib, ViewLevel, ColumnKey } from "../types";
import { DEFAULT_COLUMN_WIDTHS } from "../types";
import { containingSectionRowId, isSyntheticRowId } from "../tree";
import { OPEN_STATUSES } from "../constants";
import { SelectionState } from "../selection.svelte";
import { DragState } from "../drag.svelte";
import { TreeViewState } from "../treeView.svelte";
import { makeTestContext } from "../contexts";
import { switchViewLevel } from "../resolvePrefs";
import { NibChangeTracker } from "../changeTracker.svelte";
import { Preferences } from "../preferences.svelte";

function makeTreeTableNib(overrides: Partial<TreeTableNib> = {}): TreeTableNib {
  return {
    id: "nibs-abc1",
    title: "Fix login bug",
    status: "in-progress",
    type: "bug",
    priority: "high",
    estimate: "m",
    tags: ["auth"],
    createdAt: "2026-03-15T10:00:00Z",
    updatedAt: "2026-03-20T10:00:00Z",
    parentId: null,
    milestone: "",
    milestoneOrder: "",
    blockingIds: [],
    blockedByIds: [],
    etag: "etag-test",
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

// jsdom doesn't implement elementFromPoint, which the drag move handler calls.
if (typeof document.elementFromPoint !== "function") {
  document.elementFromPoint = () => null;
}

const { mockToastInfo } = vi.hoisted(() => ({ mockToastInfo: vi.fn() }));
vi.mock("svelte-sonner", async () => {
  const actual = await vi.importActual<typeof import("svelte-sonner")>("svelte-sonner");
  return { ...actual, toast: { ...actual.toast, info: mockToastInfo } };
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

  it("renders column headers including ID, Type, Title, Status, Estimate, Tags", () => {
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
    expect(headers).toContain("Status");
    expect(headers).toContain("Estimate");
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

  it("shows loading indicator on the initial load (fetching, no data yet)", () => {
    mockQueryStore.mockReturnValue(
      readable({ fetching: true, error: undefined, data: undefined, stale: false }) as any
    );

    renderTreeTable({ filter: {} });

    expect(screen.getByText(/loading/i)).toBeInTheDocument();
  });

  // A background refetch is `fetching: true` while data is already present (the
  // NIB_CHANGED_SUBSCRIPTION-driven live refetch). The table must stay mounted so
  // an in-progress column drag/resize, an open inline editor, and scroll position
  // survive — "Loading..." must NOT replace the populated rows.
  // Bites: reverting the gate to the unguarded `{#if dataSource.fetching}` shows
  // "Loading..." over the existing data and drops the rows, failing this test.
  it("keeps the table mounted during a background refetch (fetching with data present)", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
      makeTreeTableNib({ id: "nibs-001", title: "First task", parentId: "nibs-m1" }),
    ];

    // fetching=true AND non-empty data: a live background refetch, not initial load.
    mockQueryStore.mockReturnValue(
      readable({ fetching: true, error: undefined, data: { nibs }, stale: false }) as any
    );

    const { container } = renderTreeTable({ filter: {} });

    // The populated table stays rendered — rows are still present.
    const dataRows = container.querySelectorAll("tr[data-testid='tree-row']");
    expect(dataRows).toHaveLength(2);
    expect(screen.getByText("First task")).toBeInTheDocument();

    // The initial-load "Loading..." state must NOT replace the table.
    expect(screen.queryByText(/loading/i)).not.toBeInTheDocument();
  });

  it("shows empty state when no nibs returned", () => {
    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs: [] }, stale: false }) as any
    );

    renderTreeTable({ filter: {} });

    expect(screen.getByText(/no nibs found/i)).toBeInTheDocument();
  });

  // An empty result under two or more hierarchy predicates is a dead end the generic
  // message cannot explain: "Descendants of this" on a nib followed by "Ancestors of
  // this" on one of its now-visible children yields `ancestor:<parent>
  // descendant:<child>`, which nothing can satisfy (no node lies strictly between a
  // direct parent and child). The replacement names the active relationships and
  // offers a way back out.
  describe("empty state", () => {
    function renderEmpty(props: Record<string, unknown>) {
      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: { nibs: [] }, stale: false }) as any
      );
      return renderTreeTable(props);
    }

    it("names the active hierarchy relationships when two of them combine to nothing", () => {
      renderEmpty({ filter: { ancestorId: "tnib-p", descendantId: "tnib-c" } });

      const explanation = screen.getByTestId("empty-hierarchy");
      expect(explanation).toHaveTextContent("ancestor:tnib-p");
      expect(explanation).toHaveTextContent("descendant:tnib-c");
      // The hierarchy-specific state REPLACES the generic one; both at once would
      // read as two unrelated failures.
      expect(screen.queryByText(/no nibs found/i)).not.toBeInTheDocument();
    });

    // Guard against the hierarchy message swallowing the generic one: an ordinary
    // empty result must still say "No nibs found".
    it("keeps the generic message when a non-hierarchy filter matches nothing", () => {
      renderEmpty({ filter: { type: ["bug"] } });

      expect(screen.getByText(/no nibs found/i)).toBeInTheDocument();
      expect(screen.queryByTestId("empty-hierarchy")).not.toBeInTheDocument();
    });

    // One hierarchy predicate empties for ordinary reasons (a leaf has no
    // descendants); there is no combination to explain.
    it("keeps the generic message for a single hierarchy predicate", () => {
      renderEmpty({ filter: { ancestorId: "tnib-p" } });

      expect(screen.getByText(/no nibs found/i)).toBeInTheDocument();
      expect(screen.queryByTestId("empty-hierarchy")).not.toBeInTheDocument();
    });

    it("clears only the hierarchy fields when the escape hatch is used", async () => {
      const user = userEvent.setup();
      const onfilterchange = vi.fn();
      renderEmpty({
        filter: { ancestorId: "tnib-p", descendantId: "tnib-c", type: ["bug"], search: "login" },
        onfilterchange,
      });

      await user.click(screen.getByRole("button", { name: /clear hierarchy filters/i }));

      expect(onfilterchange).toHaveBeenCalledWith({ type: ["bug"], search: "login" });
    });
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

  // The server refuses a filter naming a nib that does not exist rather than
  // answering it with an empty list — otherwise "nothing is under that nib" and
  // "no such nib" are the same response. That refusal arrives on the read path as
  // a NOT_FOUND-coded GraphQL error, and it arrives on EVERY keystroke while an
  // id is being typed, so presenting it the way a network failure is presented
  // would flash a red error through the list for ids that are merely incomplete.
  describe("unknown filter id", () => {
    // A CombinedError-shaped value: the code lives on graphQLErrors[].extensions,
    // and urql's aggregate `message` carries a "[GraphQL] " prefix the user must
    // never see. Both details are what the component has to get right.
    function notFoundError(message: string) {
      return {
        message: `[GraphQL] ${message}`,
        graphQLErrors: [{ message, extensions: { code: "NOT_FOUND" } }],
      };
    }

    function renderWithError(error: unknown, props: Record<string, unknown> = { filter: {} }) {
      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error, data: undefined, stale: false }) as any
      );
      return renderTreeTable(props);
    }

    it("explains a refused filter id inline instead of as an error", () => {
      renderWithError(notFoundError('parentId filter: no nib with id "zz"'), {
        filter: { parentId: "zz" },
      });

      const explanation = screen.getByTestId("empty-unknown-id");
      expect(explanation).toHaveTextContent('parentId filter: no nib with id "zz"');
      // The generic error branch must not also fire: it is the branch that
      // renders the destructive styling and the raw transport prefix.
      expect(screen.queryByText(/\[GraphQL\]/)).not.toBeInTheDocument();
      expect(screen.queryByText(/^Error:/)).not.toBeInTheDocument();
    });

    it("offers the hierarchy escape hatch when a tree filter carried the id", async () => {
      const user = userEvent.setup();
      const onfilterchange = vi.fn();
      renderWithError(notFoundError('ancestorId filter: no nib with id "zz"'), {
        filter: { ancestorId: "zz", type: ["bug"] },
        onfilterchange,
      });

      await user.click(screen.getByRole("button", { name: /clear hierarchy filters/i }));

      expect(onfilterchange).toHaveBeenCalledWith({ type: ["bug"] });
    });

    // The escape hatch clears hierarchy fields only, so offering it when no
    // hierarchy field is set would give the user a button that changes nothing.
    it("omits the escape hatch when no hierarchy filter is set", () => {
      renderWithError(notFoundError('mentionsId filter: no nib with id "zz"'), {
        filter: { mentionsId: "zz" },
      });

      expect(screen.getByTestId("empty-unknown-id")).toBeInTheDocument();
      expect(screen.queryByRole("button", { name: /clear hierarchy filters/i })).not.toBeInTheDocument();
    });

    // The calm treatment is earned by the NOT_FOUND code, not by being an error
    // during filtering. An uncoded failure is still a failure.
    it("keeps the destructive error state for an uncoded failure", () => {
      renderWithError({ message: "Network error" }, { filter: { parentId: "zz" } });

      expect(screen.queryByTestId("empty-unknown-id")).not.toBeInTheDocument();
      expect(screen.getByText(/network error/i)).toBeInTheDocument();
    });

    // The destructive branch reads the server's message the same way the calm one
    // does. A real failure is exactly when the user is most likely to read the
    // text closely, so the "[GraphQL] " transport prefix belongs there least.
    it("strips the transport prefix from an uncoded GraphQL failure", () => {
      const message = "querying nibs: reading .nibs: permission denied";
      renderWithError(
        { message: `[GraphQL] ${message}`, graphQLErrors: [{ message, extensions: {} }] },
        { filter: { parentId: "zz" } }
      );

      expect(screen.queryByTestId("empty-unknown-id")).not.toBeInTheDocument();
      expect(screen.getByText(`Error: ${message}`)).toBeInTheDocument();
      expect(screen.queryByText(/\[GraphQL\]/)).not.toBeInTheDocument();
    });
  });

  // `parent:<id>` and `no:parent` are independent tokens in the query box and
  // nothing client-side rejects the combination, so the pair reaches the server,
  // which refuses it. It is reachable by hand-typing and in two clicks — drilling
  // into "Children of this" from a `no:parent` view ANDs a parentId onto the
  // existing filter. That is a query the user can fix, not a failure, and it had a
  // recoverable empty state before the server started refusing it.
  describe("contradictory filter pair", () => {
    function contradictionError(message: string) {
      return {
        message: `[GraphQL] ${message}`,
        graphQLErrors: [{ message, extensions: { code: "FILTER_CONTRADICTION" } }],
      };
    }

    function renderWithError(error: unknown, props: Record<string, unknown>) {
      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error, data: undefined, stale: false }) as any
      );
      return renderTreeTable(props);
    }

    // The backend speaks GraphQL field names (`hasParent: false`); the box the
    // user typed into speaks tokens (`no:parent`). Naming the pair the user can
    // actually see and edit is the whole point of routing this branch by code.
    it("names the pair in the query box's vocabulary, not the schema's", () => {
      renderWithError(
        contradictionError(
          'parentId filter: contradicts hasParent: false — every nib matching parentId "nibs-a" satisfies hasParent: true, so nothing can match both'
        ),
        { filter: { parentId: "nibs-a", hasParent: false } }
      );

      const explanation = screen.getByTestId("empty-contradiction");
      expect(explanation).toHaveTextContent("parent:nibs-a");
      expect(explanation).toHaveTextContent("no:parent");
      expect(explanation).not.toHaveTextContent("hasParent");
      // The generic error branch renders the destructive box and the raw
      // transport prefix; it must not fire alongside this one.
      expect(screen.queryByText(/^Error:/)).not.toBeInTheDocument();
      expect(screen.queryByText(/\[GraphQL\]/)).not.toBeInTheDocument();
    });

    it("offers the hierarchy escape hatch, which clears both halves", async () => {
      const user = userEvent.setup();
      const onfilterchange = vi.fn();
      renderWithError(contradictionError("parentId filter: contradicts hasParent: false"), {
        filter: { parentId: "nibs-a", hasParent: false, type: ["bug"] },
        onfilterchange,
      });

      await user.click(screen.getByRole("button", { name: /clear hierarchy filters/i }));

      expect(onfilterchange).toHaveBeenCalledWith({ type: ["bug"] });
    });

    // The blocking dimension contradicts too, and the escape hatch does not reach
    // it — offering a button that leaves the query exactly as refused would be
    // worse than offering none.
    it("names a blocked-by pair but omits the hierarchy escape hatch", () => {
      renderWithError(contradictionError("blockedById filter: contradicts hasBlockedBy: false"), {
        filter: { blockedById: "nibs-b", hasBlockedBy: false },
      });

      const explanation = screen.getByTestId("empty-contradiction");
      expect(explanation).toHaveTextContent("blocked-by:nibs-b");
      expect(explanation).toHaveTextContent("no:blocked-by");
      expect(screen.queryByRole("button", { name: /clear hierarchy filters/i })).not.toBeInTheDocument();
    });

    // The calm treatment is earned by the code AND by the client being able to
    // name the pair. If the two disagree — a filter that no longer holds the pair
    // the server refused — falling through to the generic error shows the server's
    // own sentence rather than an empty explanation.
    it("falls through to the generic error when the filter holds no pair to name", () => {
      renderWithError(contradictionError("parentId filter: contradicts hasParent: false"), {
        filter: { type: ["bug"] },
      });

      expect(screen.queryByTestId("empty-contradiction")).not.toBeInTheDocument();
      expect(screen.getByText(/^Error: parentId filter: contradicts/)).toBeInTheDocument();
    });
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

  // Content-key dedup must NOT collapse two genuinely-distinct edits to the same
  // nib. A burst re-emission of ONE commit shares an etag; a real second edit
  // carries a NEW etag. Keying dedup on (type:nibId) alone drops the second edit
  // and the tree goes stale relative to it. The key includes the payload etag so
  // a distinct-etag edit still refetches.
  it("refetches for a second distinct-etag edit to the same nib (does not over-dedup)", async () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-x", title: "Original", type: "task" }),
    ];

    const reexecute = vi.fn();
    mockQueryStore.mockReturnValue({
      ...readable({ fetching: false, error: undefined, data: { nibs }, stale: false }),
      reexecute,
    } as any);

    const subStore = writable<any>({ fetching: true, error: undefined, data: undefined, stale: false });
    mockSubscriptionStore.mockReturnValue(subStore as any);

    renderTreeTable({ filter: {} });
    await tick();

    // Mirror the real subscription payload: type + nibId + a nib object carrying
    // an etag (NIB_CHANGED_SUBSCRIPTION selects nib { ... etag ... }).
    const makeEvent = (etag: string) => ({
      type: "updated",
      nibId: "nibs-x",
      nib: {
        id: "nibs-x",
        title: "Edited",
        status: "in-progress",
        type: "task",
        priority: "normal",
        estimate: "m",
        tags: [],
        body: "",
        etag,
        updatedAt: "2026-03-20T10:00:00Z",
        parentId: null,
        blockingIds: [],
        blockedByIds: [],
      },
    });

    // Edit A commits (etag e1).
    subStore.set({ fetching: true, error: undefined, data: { nibChanged: makeEvent("e1") }, stale: false });
    await tick();

    // A genuinely distinct edit B to the SAME nib commits (etag e2).
    subStore.set({ fetching: true, error: undefined, data: { nibChanged: makeEvent("e2") }, stale: false });
    await tick();

    // Both distinct edits must refetch — the second must not be swallowed.
    expect(reexecute).toHaveBeenCalledTimes(2);
  });

  // Complement to the two-distinct-edits case: a burst re-emission of ONE commit
  // shares an etag, so even WITH the nib payload present the identical repeats
  // must still coalesce to a single refetch (no effect_update_depth loop).
  it("still coalesces 20 identical emissions that carry the same etag to one refetch", async () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-x", title: "Original", type: "task" }),
    ];

    const reexecute = vi.fn();
    mockQueryStore.mockReturnValue({
      ...readable({ fetching: false, error: undefined, data: { nibs }, stale: false }),
      reexecute,
    } as any);

    const subStore = writable<any>({ fetching: true, error: undefined, data: undefined, stale: false });
    mockSubscriptionStore.mockReturnValue(subStore as any);

    renderTreeTable({ filter: {} });
    await tick();

    const evt = {
      type: "updated",
      nibId: "nibs-x",
      nib: {
        id: "nibs-x",
        title: "Edited",
        status: "in-progress",
        type: "task",
        priority: "normal",
        estimate: "m",
        tags: [],
        body: "",
        etag: "same-etag",
        updatedAt: "2026-03-20T10:00:00Z",
        parentId: null,
        blockingIds: [],
        blockedByIds: [],
      },
    };
    for (let i = 0; i < 20; i++) {
      subStore.set({
        fetching: true,
        error: undefined,
        data: { nibChanged: { type: evt.type, nibId: evt.nibId, nib: { ...evt.nib } } },
        stale: false,
      });
      await tick();
    }

    expect(reexecute).toHaveBeenCalledTimes(1);
  });

  // Regression: TreeTable used to call `result.reexecute(...)` synchronously in
  // the subscription $effect (inside untrack(), which does not catch). A
  // throwing/absent reexecute therefore propagated out of the effect body and
  // aborted Svelte's whole effect flush — silently killing the live bridge, so
  // NO subsequent change event was ever processed for the rest of the session.
  // reexecute is now isolated in a try/catch: one failing refetch is surfaced
  // (console.error) and the effect keeps running for the next event.
  it("survives a throwing reexecute: a later change event is still processed and the failure is surfaced", async () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
    ];

    // The non-deleted branch calls reexecute synchronously; make it always throw.
    const reexecute = vi.fn(() => {
      throw new TypeError("reexecute exploded");
    });
    mockQueryStore.mockReturnValue({
      ...readable({ fetching: false, error: undefined, data: { nibs }, stale: false }),
      reexecute,
    } as any);

    const subStore = writable<any>({ fetching: true, error: undefined, data: undefined, stale: false });
    mockSubscriptionStore.mockReturnValue(subStore as any);

    const errorSpy = vi.spyOn(console, "error").mockImplementation(() => {});

    try {
      renderTreeTable({ filter: {} });
      await tick();

      // First non-deleted event: the effect flush DID run and reexecute was
      // called (guards against a "flush never ran" false pass) — and it threw.
      subStore.set({
        fetching: true,
        error: undefined,
        data: { nibChanged: { type: "created", nibId: "nibs-a" } },
        stale: false,
      });
      await tick();
      expect(reexecute).toHaveBeenCalledTimes(1);

      // The load-bearing behavior: that throw did NOT take the bridge down. A
      // SECOND, distinct event still reaches the effect and refetches. Without
      // the try/catch the aborted flush leaves this stuck at 1 (bridge dead).
      subStore.set({
        fetching: true,
        error: undefined,
        data: { nibChanged: { type: "created", nibId: "nibs-b" } },
        stale: false,
      });
      await tick();
      expect(reexecute).toHaveBeenCalledTimes(2);

      // The failure is surfaced, not silently swallowed — once per throwing
      // refetch (two events, two throws), with the thrown error passed through.
      expect(errorSpy).toHaveBeenCalledTimes(2);
      expect(errorSpy).toHaveBeenLastCalledWith(
        "Failed to refetch nibs after a change event:",
        expect.any(TypeError),
      );
    } finally {
      errorSpy.mockRestore();
    }
  });

  // A "deleted" event defers its refetch by ~fadeDurationMs so the row's
  // fade-out animation can play before the row leaves the dataset. The timer
  // must be cleared on unmount — otherwise it outlives the component and fires
  // reexecute on a torn-down urql store.
  it("clears the deferred delete refetch on unmount so it never fires after teardown", async () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
    ];

    const reexecute = vi.fn();
    mockQueryStore.mockReturnValue({
      ...readable({ fetching: false, error: undefined, data: { nibs }, stale: false }),
      reexecute,
    } as any);

    const subStore = writable<any>({ fetching: true, error: undefined, data: undefined, stale: false });
    mockSubscriptionStore.mockReturnValue(subStore as any);

    const fadeMs = new NibChangeTracker().fadeDurationMs;
    // Fake ONLY the timer fns so Svelte's microtask-based tick() stays real.
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      const { unmount } = renderTreeTable({ filter: {} });
      await tick();

      // A delete defers its refetch — nothing fires synchronously.
      subStore.set({
        fetching: true,
        error: undefined,
        data: { nibChanged: { type: "deleted", nibId: "nibs-m1" } },
        stale: false,
      });
      await tick();
      expect(reexecute).not.toHaveBeenCalled();

      // Unmount inside the fade window, then let the deferred deadline pass.
      unmount();
      vi.advanceTimersByTime(fadeMs);

      // The deferred timer was cleared on teardown, so reexecute never ran.
      expect(reexecute).not.toHaveBeenCalled();
    } finally {
      vi.useRealTimers();
    }
  });

  // The complement to the unmount guard: while the component stays mounted, the
  // deferred delete refetch DOES fire once the fade window elapses.
  it("fires the deferred delete refetch once the fade window elapses while mounted", async () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
    ];

    const reexecute = vi.fn();
    mockQueryStore.mockReturnValue({
      ...readable({ fetching: false, error: undefined, data: { nibs }, stale: false }),
      reexecute,
    } as any);

    const subStore = writable<any>({ fetching: true, error: undefined, data: undefined, stale: false });
    mockSubscriptionStore.mockReturnValue(subStore as any);

    const fadeMs = new NibChangeTracker().fadeDurationMs;
    vi.useFakeTimers({ toFake: ["setTimeout", "clearTimeout"] });
    try {
      renderTreeTable({ filter: {} });
      await tick();

      subStore.set({
        fetching: true,
        error: undefined,
        data: { nibChanged: { type: "deleted", nibId: "nibs-m1" } },
        stale: false,
      });
      await tick();
      // Deferred: the refetch has not fired yet.
      expect(reexecute).not.toHaveBeenCalled();

      // The fade window elapses — the deferred refetch fires exactly once.
      vi.advanceTimersByTime(fadeMs);
      expect(reexecute).toHaveBeenCalledTimes(1);
    } finally {
      vi.useRealTimers();
    }
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

    const visibleColumns: ColumnKey[] = ["title", "status"];
    const { container } = renderTreeTable({
      filter: {},
      viewLevel: "epics" as ViewLevel,
      visibleColumns,
    });

    const thead = container.querySelector("thead")!;
    const headers = Array.from(thead.querySelectorAll("th")).map(th => th.textContent?.trim());
    expect(headers).toContain("Title");
    expect(headers).toContain("Status");
    expect(headers).not.toContain("ID");
    expect(headers).not.toContain("Type");
    expect(headers).not.toContain("Estimate");
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

    const visibleColumns: ColumnKey[] = ["title", "status"];
    const { container } = renderTreeTable({
      filter: {},
      viewLevel: "epics" as ViewLevel,
      visibleColumns,
    });

    // ID and type cells should not be rendered
    expect(container.querySelector("[data-testid='nib-id']")).not.toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-type']")).not.toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-estimate']")).not.toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-tags']")).not.toBeInTheDocument();

    // Title and state cells should be rendered
    expect(container.querySelector("[data-testid='nib-title']")).toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-status']")).toBeInTheDocument();
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
    const visibleColumns: ColumnKey[] = ["id", "parent", "type", "title", "status", "estimate", "tags"];
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

  it("renders Created and Modified headers when those columns are visible", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-001", title: "Epic A", type: "epic" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    const visibleColumns: ColumnKey[] = ["title", "created", "modified"];
    const { container } = renderTreeTable({
      filter: {},
      viewLevel: "epics" as ViewLevel,
      visibleColumns,
    });

    const thead = container.querySelector("thead")!;
    const headers = Array.from(thead.querySelectorAll("th")).map(th => th.textContent?.trim());
    expect(headers).toContain("Created");
    expect(headers).toContain("Modified");
  });

  it("omits Created and Modified headers when those columns are not visible", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-001", title: "Epic A", type: "epic" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    // A visible set without created / modified.
    const visibleColumns: ColumnKey[] = ["id", "parent", "type", "title", "status", "estimate", "tags"];
    const { container } = renderTreeTable({
      filter: {},
      viewLevel: "epics" as ViewLevel,
      visibleColumns,
    });

    const thead = container.querySelector("thead")!;
    const headers = Array.from(thead.querySelectorAll("th")).map(th => th.textContent?.trim());
    expect(headers).not.toContain("Created");
    expect(headers).not.toContain("Modified");
  });

  it("table width grows by the created/modified column widths when they are shown", () => {
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
      visibleColumns: ["title", "created", "modified"] as ColumnKey[],
    });
    const grownWidth = parseInt((withCols.container.querySelector("table") as HTMLElement).style.width, 10);

    // created + modified default widths added to the base table width.
    expect(grownWidth).toBe(baseWidth + DEFAULT_COLUMN_WIDTHS.created + DEFAULT_COLUMN_WIDTHS.modified);
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
    const visibleColumns: ColumnKey[] = ["id", "title", "type", "status", "estimate", "tags"];
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

  it("rows keep draggable class when a hide-filter is active (filters never reorder rows)", () => {
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

    // A hide-filter keeps matching rows in tree order, so drag stays allowed.
    const draggableRows = container.querySelectorAll("tr.draggable");
    expect(draggableRows.length).toBeGreaterThan(0);
  });

  it("rows lack draggable class when search is active", () => {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
      makeTreeTableNib({ id: "nibs-001", title: "Child Task", type: "task", parentId: "nibs-m1" }),
    ];

    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );

    const { container } = renderTreeTable({
      filter: { search: "child" },
      viewLevel: "milestones" as ViewLevel,
    });

    // Search flattens results out of tree order, so drag must be disabled.
    const draggableRows = container.querySelectorAll("tr.draggable");
    expect(draggableRows).toHaveLength(0);
  });

  describe("table sort — date columns", () => {
    // Two root tasks whose created/modified ordering differ, so a sort visibly
    // reorders the rows away from the incoming (manual `order`) sequence.
    function renderFlat(props: Record<string, unknown> = {}, viewLevel: ViewLevel = "flat" as ViewLevel) {
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "nibs-001", title: "First", type: "task", createdAt: "2026-01-01T00:00:00Z", updatedAt: "2026-03-01T00:00:00Z" }),
        makeTreeTableNib({ id: "nibs-002", title: "Second", type: "task", createdAt: "2026-02-01T00:00:00Z", updatedAt: "2026-01-01T00:00:00Z" }),
      ];
      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
      );
      return renderTreeTable({
        filter: {},
        viewLevel,
        visibleColumns: ["title", "created", "modified"] as ColumnKey[],
        ...props,
      });
    }

    it("renders the Created/Modified headers as sortable columnheaders in flat view", () => {
      renderFlat();
      expect(screen.getByRole("columnheader", { name: "Created" })).toBeInTheDocument();
      expect(screen.getByRole("columnheader", { name: "Modified" })).toBeInTheDocument();
    });

    it("clicking Modified with no active sort emits ascending", async () => {
      const user = userEvent.setup();
      const ontablesortchange = vi.fn();
      renderFlat({ tableSort: null, ontablesortchange });
      await user.click(screen.getByRole("columnheader", { name: "Modified" }));
      expect(ontablesortchange).toHaveBeenLastCalledWith({ field: "modified", direction: "asc" });
    });

    it("clicking Modified while ascending emits descending", async () => {
      const user = userEvent.setup();
      const ontablesortchange = vi.fn();
      renderFlat({ tableSort: { field: "modified", direction: "asc" }, ontablesortchange });
      await user.click(screen.getByRole("columnheader", { name: "Modified" }));
      expect(ontablesortchange).toHaveBeenLastCalledWith({ field: "modified", direction: "desc" });
    });

    it("clicking Modified while descending turns the sort off (null)", async () => {
      const user = userEvent.setup();
      const ontablesortchange = vi.fn();
      renderFlat({ tableSort: { field: "modified", direction: "desc" }, ontablesortchange });
      await user.click(screen.getByRole("columnheader", { name: "Modified" }));
      expect(ontablesortchange).toHaveBeenLastCalledWith(null);
    });

    it("shows an up arrow and aria-sort=ascending when Modified is sorted ascending", () => {
      const { container } = renderFlat({ tableSort: { field: "modified", direction: "asc" } });
      const arrow = container.querySelector("[data-testid='table-sort-arrow-modified']");
      expect(arrow).toBeInTheDocument();
      expect(arrow!.classList.contains("lucide-arrow-up")).toBe(true);
      const modifiedTh = Array.from(container.querySelectorAll("th")).find((th) => th.textContent?.trim() === "Modified")!;
      expect(modifiedTh.getAttribute("aria-sort")).toBe("ascending");
      // The other sortable field reads "none" while inactive.
      const createdTh = Array.from(container.querySelectorAll("th")).find((th) => th.textContent?.trim() === "Created")!;
      expect(createdTh.getAttribute("aria-sort")).toBe("none");
    });

    it("shows a down arrow and aria-sort=descending when Modified is sorted descending", () => {
      const { container } = renderFlat({ tableSort: { field: "modified", direction: "desc" } });
      const arrow = container.querySelector("[data-testid='table-sort-arrow-modified']");
      expect(arrow).toBeInTheDocument();
      expect(arrow!.classList.contains("lucide-arrow-down")).toBe(true);
      const modifiedTh = Array.from(container.querySelectorAll("th")).find((th) => th.textContent?.trim() === "Modified")!;
      expect(modifiedTh.getAttribute("aria-sort")).toBe("descending");
    });

    it("reorders rows by the active flat sort (created descending)", () => {
      const { container } = renderFlat({ tableSort: { field: "created", direction: "desc" } });
      const titles = Array.from(container.querySelectorAll("[data-testid='title-text']")).map((e) => e.textContent);
      // createdAt desc: Second (Feb) before First (Jan).
      expect(titles).toEqual(["Second", "First"]);
    });

    it("keeps manual order when the flat sort is off (null)", () => {
      const { container } = renderFlat({ tableSort: null });
      const titles = Array.from(container.querySelectorAll("[data-testid='title-text']")).map((e) => e.textContent);
      expect(titles).toEqual(["First", "Second"]);
    });

    it("deactivates the sort when the sorted column is hidden (reverts to manual order)", () => {
      // Sort by Created desc, but hide the Created column: with no reachable header
      // to clear it, the sort would otherwise leave rows ordered by an invisible
      // field. Instead it deactivates and rows fall back to manual `order`.
      const { container } = renderFlat({
        tableSort: { field: "created", direction: "desc" },
        visibleColumns: ["title", "modified"] as ColumnKey[],
      });
      const titles = Array.from(container.querySelectorAll("[data-testid='title-text']")).map((e) => e.textContent);
      expect(titles).toEqual(["First", "Second"]); // manual order, NOT created-desc ("Second","First")
      // The still-visible Modified header must not falsely claim an active sort.
      const modifiedTh = Array.from(container.querySelectorAll("th")).find((th) => th.textContent?.trim() === "Modified")!;
      expect(modifiedTh.getAttribute("aria-sort")).toBe("none");
    });

    it("date headers ARE sortable columnheaders in the tree (none) view too (sort lifted from Flat-only)", () => {
      const { container } = renderFlat({ tableSort: { field: "modified", direction: "asc" } }, "none" as ViewLevel);
      // The whole header is the click-to-sort control in the tree view, not a plain label.
      expect(screen.getByRole("columnheader", { name: "Modified" })).toBeInTheDocument();
      expect(screen.getByRole("columnheader", { name: "Created" })).toBeInTheDocument();
      // The active field shows its direction arrow + aria-sort in the tree view.
      const arrow = container.querySelector("[data-testid='table-sort-arrow-modified']");
      expect(arrow).toBeInTheDocument();
      expect(arrow!.classList.contains("lucide-arrow-up")).toBe(true);
      const modifiedTh = Array.from(container.querySelectorAll("th")).find((th) => th.textContent?.trim() === "Modified")!;
      expect(modifiedTh.getAttribute("aria-sort")).toBe("ascending");
      const createdTh = Array.from(container.querySelectorAll("th")).find((th) => th.textContent?.trim() === "Created")!;
      expect(createdTh.getAttribute("aria-sort")).toBe("none");
    });

    it("rows are not draggable in flat view (browse-only), sort on or off", () => {
      expect(renderFlat().container.querySelectorAll("tr.draggable")).toHaveLength(0);
    });
  });

  describe("table sort — every sortable column", () => {
    // Three roots whose type ordering (milestone → bug → task) differs from their
    // incoming manual order, so a Type sort visibly reorders the rows.
    function renderCols(props: Record<string, unknown> = {}, viewLevel: ViewLevel = "flat" as ViewLevel) {
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "nibs-001", title: "Task", type: "task", status: "todo" }),
        makeTreeTableNib({ id: "nibs-002", title: "Milestone", type: "milestone", status: "draft" }),
        makeTreeTableNib({ id: "nibs-003", title: "Bug", type: "bug", status: "completed" }),
      ];
      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
      );
      return renderTreeTable({
        filter: {},
        viewLevel,
        visibleColumns: ["title", "type", "status"] as ColumnKey[],
        ...props,
      });
    }

    it("renders a click-to-sort columnheader for every visible column in flat view (not just dates)", () => {
      renderCols();
      expect(screen.getByRole("columnheader", { name: "Title" })).toBeInTheDocument();
      expect(screen.getByRole("columnheader", { name: "Type" })).toBeInTheDocument();
      expect(screen.getByRole("columnheader", { name: "Status" })).toBeInTheDocument();
    });

    it("clicking a non-date header (Type) with no active sort emits that field ascending", async () => {
      const user = userEvent.setup();
      const ontablesortchange = vi.fn();
      renderCols({ tableSort: null, ontablesortchange });
      await user.click(screen.getByRole("columnheader", { name: "Type" }));
      expect(ontablesortchange).toHaveBeenLastCalledWith({ field: "type", direction: "asc" });
    });

    it("reorders rows by a non-date column (type ascending, canonical rank)", () => {
      const { container } = renderCols({ tableSort: { field: "type", direction: "asc" } });
      const titles = Array.from(container.querySelectorAll("[data-testid='title-text']")).map((e) => e.textContent);
      // Canonical rank: milestone → bug → task.
      expect(titles).toEqual(["Milestone", "Bug", "Task"]);
    });

    it("shows an arrow and aria-sort on a non-date header (State descending)", () => {
      const { container } = renderCols({ tableSort: { field: "status", direction: "desc" } });
      const arrow = container.querySelector("[data-testid='table-sort-arrow-status']");
      expect(arrow).toBeInTheDocument();
      expect(arrow!.classList.contains("lucide-arrow-down")).toBe(true);
      const stateTh = Array.from(container.querySelectorAll("th")).find((th) => th.textContent?.trim() === "Status")!;
      expect(stateTh.getAttribute("aria-sort")).toBe("descending");
      // A different sortable header reads "none" while inactive.
      const typeTh = Array.from(container.querySelectorAll("th")).find((th) => th.textContent?.trim() === "Type")!;
      expect(typeTh.getAttribute("aria-sort")).toBe("none");
    });

    it("deactivates the sort when a NON-date sorted column (type) is hidden", () => {
      const { container } = renderCols({
        tableSort: { field: "type", direction: "asc" },
        visibleColumns: ["title", "status"] as ColumnKey[],
      });
      const titles = Array.from(container.querySelectorAll("[data-testid='title-text']")).map((e) => e.textContent);
      // Type column hidden → sort deactivates → manual order (Task, Milestone, Bug).
      expect(titles).toEqual(["Task", "Milestone", "Bug"]);
    });

    it("non-date headers ARE sortable in the tree (none) view and reorder roots by rank", () => {
      const { container } = renderCols({ tableSort: { field: "type", direction: "asc" } }, "none" as ViewLevel);
      // The whole header is the click-to-sort control in the tree view.
      expect(screen.getByRole("columnheader", { name: "Type" })).toBeInTheDocument();
      const typeTh = Array.from(container.querySelectorAll("th")).find((th) => th.textContent?.trim() === "Type")!;
      expect(typeTh.getAttribute("aria-sort")).toBe("ascending");
      // These three are all roots, so the sort reorders them by canonical rank
      // (milestone → bug → task) exactly as in flat view.
      const titles = Array.from(container.querySelectorAll("[data-testid='title-text']")).map((e) => e.textContent);
      expect(titles).toEqual(["Milestone", "Bug", "Task"]);
    });
  });

  describe("table sort — all views (headers, arrows, sibling-sort, drag gating)", () => {
    // An epic with a child task yields at least one visible row in every view
    // level (none/flat show both; the grouping lenses promote/bucket them), so
    // the sortable <thead> renders and its buttons are assertable everywhere.
    function renderView(viewLevel: ViewLevel, props: Record<string, unknown> = {}) {
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "nibs-e1", title: "Epic", type: "epic", status: "todo" }),
        makeTreeTableNib({ id: "nibs-t1", title: "Task", type: "task", status: "draft", parentId: "nibs-e1" }),
      ];
      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
      );
      return renderTreeTable({
        filter: {},
        viewLevel,
        visibleColumns: ["title", "status", "modified"] as ColumnKey[],
        ...props,
      });
    }

    const ALL_VIEWS: ViewLevel[] = ["none", "milestones", "epics", "features", "flat"] as ViewLevel[];

    it.each(ALL_VIEWS)("renders a click-to-sort columnheader in the %s view", (viewLevel) => {
      renderView(viewLevel);
      expect(screen.getByRole("columnheader", { name: "Status" })).toBeInTheDocument();
    });

    it.each(ALL_VIEWS)("clicking a header cycles off → asc in the %s view", async (viewLevel) => {
      const user = userEvent.setup();
      const ontablesortchange = vi.fn();
      renderView(viewLevel, { tableSort: null, ontablesortchange });
      await user.click(screen.getByRole("columnheader", { name: "Status" }));
      expect(ontablesortchange).toHaveBeenLastCalledWith({ field: "status", direction: "asc" });
    });

    it.each(ALL_VIEWS)("clicking asc → desc in the %s view", async (viewLevel) => {
      const user = userEvent.setup();
      const ontablesortchange = vi.fn();
      renderView(viewLevel, { tableSort: { field: "status", direction: "asc" }, ontablesortchange });
      await user.click(screen.getByRole("columnheader", { name: "Status" }));
      expect(ontablesortchange).toHaveBeenLastCalledWith({ field: "status", direction: "desc" });
    });

    it.each(ALL_VIEWS)("clicking desc → off (null) in the %s view", async (viewLevel) => {
      const user = userEvent.setup();
      const ontablesortchange = vi.fn();
      renderView(viewLevel, { tableSort: { field: "status", direction: "desc" }, ontablesortchange });
      await user.click(screen.getByRole("columnheader", { name: "Status" }));
      expect(ontablesortchange).toHaveBeenLastCalledWith(null);
    });

    it.each(ALL_VIEWS)("shows the direction arrow + aria-sort for the active field in the %s view", (viewLevel) => {
      const { container } = renderView(viewLevel, { tableSort: { field: "status", direction: "desc" } });
      const arrow = container.querySelector("[data-testid='table-sort-arrow-status']");
      expect(arrow).toBeInTheDocument();
      expect(arrow!.classList.contains("lucide-arrow-down")).toBe(true);
      const stateTh = Array.from(container.querySelectorAll("th")).find((th) => th.textContent?.trim() === "Status")!;
      expect(stateTh.getAttribute("aria-sort")).toBe("descending");
    });

    // --- Sibling-sort: nesting preserved, each sibling group reordered ---
    // Two milestone roots (input Z-before-A) each a potential parent; one carries
    // two child tasks (input Zeta-before-Alpha). A title sort must reorder the
    // ROOTS (A before Z) AND the CHILDREN (Alpha before Zeta) while keeping the
    // children nested under their parent — NOT flatten everything into one global
    // title order (which would interleave "Alpha" ahead of the "Root A" parent).
    function renderNested(props: Record<string, unknown> = {}) {
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "nibs-m2", title: "Root Z", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-m1", title: "Root A", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-c2", title: "Zeta", type: "task", parentId: "nibs-m1" }),
        makeTreeTableNib({ id: "nibs-c1", title: "Alpha", type: "task", parentId: "nibs-m1" }),
      ];
      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
      );
      return renderTreeTable({
        filter: {},
        viewLevel: "none" as ViewLevel,
        visibleColumns: ["title"] as ColumnKey[],
        ...props,
      });
    }

    it("sibling-sorts the tree (none) view: roots and children reorder, nesting preserved", () => {
      const { container } = renderNested({ tableSort: { field: "title", direction: "asc" } });
      const titles = Array.from(container.querySelectorAll("[data-testid='title-text']")).map((e) => e.textContent);
      // Sibling-sort: Root A (with its now-sorted children Alpha, Zeta) then Root Z.
      // A global flat title sort would be ["Alpha","Root A","Root Z","Zeta"] — the
      // DIFFERENT expected order below proves the children stayed nested.
      expect(titles).toEqual(["Root A", "Alpha", "Zeta", "Root Z"]);
    });

    it("keeps manual (input) order in the tree view when the sort is off", () => {
      const { container } = renderNested({ tableSort: null });
      const titles = Array.from(container.querySelectorAll("[data-testid='title-text']")).map((e) => e.textContent);
      // Input order, nested: Root Z (no children), Root A, then its children as entered.
      expect(titles).toEqual(["Root Z", "Root A", "Zeta", "Alpha"]);
    });

    // --- Drag gating: an active sort disables drag-reorder in Tree/lens views ---
    // A real parent + child so rows are draggable when the gate is open.
    function renderDrag(viewLevel: ViewLevel, props: Record<string, unknown> = {}) {
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-001", title: "Child Task", type: "task", parentId: "nibs-m1" }),
      ];
      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
      );
      return renderTreeTable({
        filter: {},
        viewLevel,
        visibleColumns: ["title", "status"] as ColumnKey[],
        ...props,
      });
    }

    it("tree (none) view: rows draggable when the sort is off", () => {
      const { container } = renderDrag("none" as ViewLevel, { tableSort: null });
      expect(container.querySelectorAll("tr.draggable").length).toBeGreaterThan(0);
    });

    it("tree (none) view: drag DISABLED while a sort is active", () => {
      const { container } = renderDrag("none" as ViewLevel, { tableSort: { field: "status", direction: "asc" } });
      expect(container.querySelectorAll("tr.draggable")).toHaveLength(0);
    });

    it("grouping lens (milestones): rows draggable when the sort is off", () => {
      const { container } = renderDrag("milestones" as ViewLevel, { tableSort: null });
      expect(container.querySelectorAll("tr.draggable").length).toBeGreaterThan(0);
    });

    it("grouping lens (milestones): drag DISABLED while a sort is active", () => {
      const { container } = renderDrag("milestones" as ViewLevel, { tableSort: { field: "status", direction: "asc" } });
      expect(container.querySelectorAll("tr.draggable")).toHaveLength(0);
    });

    it("re-enabling: drag returns when a hidden sorted column deactivates the sort", () => {
      // Sort by a column that is NOT visible → activeSort is null → drag allowed,
      // even though a tableSort preference is set. Confirms the gate keys off the
      // EFFECTIVE (active) sort, not the raw preference.
      const { container } = renderDrag("none" as ViewLevel, {
        tableSort: { field: "created", direction: "asc" },
        visibleColumns: ["title", "status"] as ColumnKey[],
      });
      expect(container.querySelectorAll("tr.draggable").length).toBeGreaterThan(0);
    });

    // The gate above is deliberate but was entirely silent: a blocked row just
    // refused to move, which reads as a broken app (and, because the sort is
    // persisted per browser profile, survives a restart and differs between
    // browsers). Attempting a drag must now say why.
    describe("blocked-drag explanation", () => {
      /** Press on a row and move past the 5px drag threshold. */
      function attemptDrag(container: HTMLElement) {
        const row = container.querySelector("tr[data-nib-id]") as HTMLElement;
        row.dispatchEvent(new PointerEvent("pointerdown", {
          clientX: 100, clientY: 100, bubbles: true, button: 0,
        }));
        window.dispatchEvent(new PointerEvent("pointermove", {
          clientX: 130, clientY: 100, bubbles: true,
        }));
        window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
      }

      beforeEach(() => mockToastInfo.mockReset());

      it("explains the active sort when a drag is attempted", () => {
        const { container } = renderDrag("none" as ViewLevel, {
          tableSort: { field: "status", direction: "asc" },
        });
        attemptDrag(container);

        expect(mockToastInfo).toHaveBeenCalledTimes(1);
        expect(mockToastInfo.mock.calls[0][0]).toBe("Reordering is off while sorted by Status");
      });

      it("offers an action that clears the sort", () => {
        const ontablesortchange = vi.fn();
        const { container } = renderDrag("none" as ViewLevel, {
          tableSort: { field: "status", direction: "asc" },
          ontablesortchange,
        });
        attemptDrag(container);

        const action = mockToastInfo.mock.calls[0][1]?.action;
        expect(action?.label).toBe("Clear sort");
        action.onClick(new MouseEvent("click"));
        expect(ontablesortchange).toHaveBeenCalledWith(null);
      });

      it("explains the Flat view when a drag is attempted there", () => {
        const { container } = renderDrag("flat" as ViewLevel, { tableSort: null });
        attemptDrag(container);

        expect(mockToastInfo.mock.calls[0][0]).toBe("Reordering is off in the Flat view");
      });

      it("offers an action that leaves the Flat view", () => {
        const onviewlevelchange = vi.fn();
        const { container } = renderDrag("flat" as ViewLevel, { tableSort: null, onviewlevelchange });
        attemptDrag(container);

        const action = mockToastInfo.mock.calls[0][1]?.action;
        expect(action?.label).toBe("Switch to Tree");
        action.onClick(new MouseEvent("click"));
        expect(onviewlevelchange).toHaveBeenCalledWith("none");
      });

      it("stays silent on a right-click, which never begins a drag", () => {
        const { container } = renderDrag("none" as ViewLevel, {
          tableSort: { field: "status", direction: "asc" },
        });
        const row = container.querySelector("tr[data-nib-id]") as HTMLElement;
        row.dispatchEvent(new PointerEvent("pointerdown", {
          clientX: 100, clientY: 100, bubbles: true, button: 2,
        }));
        window.dispatchEvent(new PointerEvent("pointermove", {
          clientX: 130, clientY: 100, bubbles: true,
        }));

        expect(mockToastInfo).not.toHaveBeenCalled();
      });

      it("offers an action that clears the search", () => {
        const onfilterchange = vi.fn();
        const { container } = renderDrag("none" as ViewLevel, {
          tableSort: null,
          filter: { search: "api", type: ["task"] },
          onfilterchange,
        });
        attemptDrag(container);

        const action = mockToastInfo.mock.calls[0][1]?.action;
        expect(action?.label).toBe("Clear search");
        action.onClick(new MouseEvent("click"));
        // Only the search term is dropped — the token filters the user built up survive.
        expect(onfilterchange).toHaveBeenCalledWith({ type: ["task"] });
      });

      // The collapsing itself happens inside svelte-sonner (it dedupes by id), so
      // this can only guard that a STABLE id is handed over — e2e/drag-block-toast
      // proves the toasts actually stop stacking.
      it("reuses one toast id across attempts, so repeats replace rather than stack", () => {
        const { container } = renderDrag("none" as ViewLevel, {
          tableSort: { field: "status", direction: "asc" },
        });
        attemptDrag(container);
        attemptDrag(container);

        expect(mockToastInfo).toHaveBeenCalledTimes(2);
        const [first, second] = mockToastInfo.mock.calls.map((c) => c[1]?.id);
        expect(first).toBeDefined();
        expect(second).toBe(first);
      });

      it("stays silent when a row is merely clicked", () => {
        const { container } = renderDrag("none" as ViewLevel, {
          tableSort: { field: "status", direction: "asc" },
        });
        const row = container.querySelector("tr[data-nib-id]") as HTMLElement;
        row.dispatchEvent(new PointerEvent("pointerdown", {
          clientX: 100, clientY: 100, bubbles: true, button: 0,
        }));
        window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

        expect(mockToastInfo).not.toHaveBeenCalled();
      });

      it("stays silent when drag is available", () => {
        const { container } = renderDrag("none" as ViewLevel, { tableSort: null });
        attemptDrag(container);

        expect(mockToastInfo).not.toHaveBeenCalled();
      });
    });

    it("flat view stays browse-only even with the sort off", () => {
      const { container } = renderDrag("flat" as ViewLevel, { tableSort: null });
      expect(container.querySelectorAll("tr.draggable")).toHaveLength(0);
    });
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

    it("Space toggles the arrow-focused row (focusedNibId fallback path)", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      sel.focus("nibs-001");
      const nibs = makeKeyboardTestNibs(2);

      setupWithNibs(nibs, {}, { selection: sel });
      const scrollContainer = getScrollContainer();
      // DOM focus on the grid container (not a row) — the key event has no row
      // ancestor, so Space must fall back to the virtual focusedNibId.
      scrollContainer.focus();

      await user.keyboard(" ");

      expect(sel.selectedIds.has("nibs-001")).toBe(true);
    });

    // Regression (nibs-5ela finding #1): a sortable header is a real tab stop
    // (tabindex=0) whose keydown bubbles to the grid's keyboard-nav handler on the
    // scroll container. The header must consume its own Enter/Space so a key-repeat
    // or a modifier chord can never leak through and act on the background,
    // virtually-focused row. Only a clean press sorts; non-sort keys still pass to
    // grid nav.
    describe("sortable header does not leak Enter/Space to grid nav", () => {
      function titleHeader(container: HTMLElement): HTMLElement {
        // The always-visible Title column is a sortable columnheader in every view.
        return container.querySelector("thead th[data-col-key='title']") as HTMLElement;
      }

      it("held/repeat Space on a focused header does NOT toggle-select the focused row", () => {
        const sel = new SelectionState();
        sel.focus("nibs-m1"); // a row is virtually focused, but not selected
        const nibs = makeKeyboardTestNibs(2);
        const { container } = setupWithNibs(nibs, {}, { selection: sel });
        const th = titleHeader(container);
        th.focus();

        // Autorepeat Space: the grid Space handler would toggle-select focusedNibId.
        th.dispatchEvent(new KeyboardEvent("keydown", { key: " ", repeat: true, bubbles: true, cancelable: true }));

        expect(sel.selectedIds.has("nibs-m1")).toBe(false);
      });

      it("modifier+Enter on a focused header does NOT navigate the focused row", () => {
        const sel = new SelectionState();
        sel.focus("nibs-m1");
        const nibs = makeKeyboardTestNibs(2);
        const { container } = setupWithNibs(nibs, {}, { selection: sel });
        const th = titleHeader(container);
        th.focus();

        // Ctrl+Enter: the grid Enter handler would open/select focusedNibId.
        th.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", ctrlKey: true, bubbles: true, cancelable: true }));

        expect(sel.selectedNibId).toBeNull();
      });

      it("a clean Enter on a focused header sorts and leaves the focused row untouched", () => {
        const ontablesortchange = vi.fn();
        const sel = new SelectionState();
        sel.focus("nibs-m1");
        const nibs = makeKeyboardTestNibs(2);
        const { container } = setupWithNibs(nibs, { tableSort: null, ontablesortchange }, { selection: sel });
        const th = titleHeader(container);
        th.focus();

        th.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));

        // The clean press sorts...
        expect(ontablesortchange).toHaveBeenCalledWith({ field: "title", direction: "asc" });
        // ...and does not leak to grid nav (no navigate/select of the focused row).
        expect(sel.selectedNibId).toBeNull();
        expect(sel.selectedIds.has("nibs-m1")).toBe(false);
      });

      it("ArrowDown on a focused header still reaches grid nav (not over-swallowed)", () => {
        const sel = new SelectionState();
        sel.focus("nibs-m1");
        const nibs = makeKeyboardTestNibs(2);
        const { container } = setupWithNibs(nibs, {}, { selection: sel });
        const th = titleHeader(container);
        th.focus();

        // ArrowDown is NOT a sort key, so the header ignores it and it bubbles to
        // grid nav, which advances the virtual focus to the next row.
        th.dispatchEvent(new KeyboardEvent("keydown", { key: "ArrowDown", bubbles: true, cancelable: true }));

        expect(sel.focusedNibId).toBe("nibs-001");
      });
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

    it("Ctrl+click on the title text toggles nib in selection via context", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      sel.select("nibs-m1"); // Pre-select milestone
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      // Ctrl+click the title text itself — the same gesture as on the row body.
      const titleText = container.querySelector(
        "tr[data-nib-id='nibs-001'] [data-action='title']",
      ) as HTMLElement;
      await user.keyboard("{Control>}");
      await user.click(titleText);
      await user.keyboard("{/Control}");

      expect(sel.selectedIds.has("nibs-001")).toBe(true);
      expect(sel.selectedIds.has("nibs-m1")).toBe(true);
    });

    it("Shift+click on the title text range-selects via context", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      sel.select("nibs-m1"); // Anchor at milestone
      const nibs = [
        makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-001", title: "Task 1", type: "task", parentId: "nibs-m1" }),
        makeTreeTableNib({ id: "nibs-002", title: "Task 2", type: "task", parentId: "nibs-m1" }),
      ];

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      // Shift+click the title text of Task 2 — range from the milestone anchor.
      const titleText = container.querySelector(
        "tr[data-nib-id='nibs-002'] [data-action='title']",
      ) as HTMLElement;
      await user.keyboard("{Shift>}");
      await user.click(titleText);
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
      // An epic can take children, so its row carries the [+] affordance
      // (a milestone's no longer does).
      const nibs = [
        makeTreeTableNib({ id: "nibs-e1", title: "Epic", type: "epic" }),
        makeTreeTableNib({ id: "nibs-001", title: "Child Task", type: "task", parentId: "nibs-e1" }),
      ];

      const { container } = setupWithNibs(nibs, { onaddchild }, { selection: sel });

      const addChildBtn = container.querySelector("tr[data-nib-id='nibs-e1'] [data-action='add-child']") as HTMLElement;
      await user.click(addChildBtn);

      expect(onaddchild).toHaveBeenCalledOnce();
      // Third arg is the clicked [+]'s viewport rect (the type picker anchors to it).
      expect(onaddchild).toHaveBeenCalledWith(
        "nibs-e1",
        "epic",
        expect.objectContaining({ x: expect.any(Number), y: expect.any(Number) }),
      );

      // selection should NOT have changed
      expect(sel.selectedNibId).toBeNull();
    });

    it("milestone rows render no add-child affordance (a milestone takes no children)", () => {
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, {});

      expect(container.querySelector("tr[data-nib-id='nibs-m1'] [data-action='add-child']")).toBeNull();
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
        expect.objectContaining({ id: "nibs-001", title: "Child Task" }),
        expect.objectContaining({
          hasChildren: false, // Child Task is a leaf
          expandChildren: expect.any(Function),
          collapseChildren: expect.any(Function),
        }),
        // The etag resolver: the table owns the loaded nibs, so the batch
        // mutations' ifMatch lookup has to travel with the event.
        expect.any(Function),
      );
    });

    it("context menu supplies hasChildren=true for a parent row", async () => {
      const user = userEvent.setup();
      const onrowcontextmenu = vi.fn();
      const nibs = makeTestNibs(); // nibs-m1 (milestone) has a child

      const { container } = setupWithNibs(nibs, { onrowcontextmenu });

      const row = container.querySelector("tr[data-nib-id='nibs-m1']") as HTMLElement;
      await user.pointer({ target: row, keys: "[MouseRight]" });

      expect(onrowcontextmenu).toHaveBeenCalledWith(
        "nibs-m1",
        expect.any(MouseEvent),
        expect.objectContaining({ id: "nibs-m1" }),
        expect.objectContaining({ hasChildren: true }),
        expect.any(Function),
      );
    });

    it("subtree collapseChildren/expandChildren toggle the whole subtree", async () => {
      const user = userEvent.setup();
      let captured: import("../types").RowSubtreeActions | undefined;
      const onrowcontextmenu = vi.fn((_id, _e, _nib, subtree) => { captured = subtree; });
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-e1", title: "Epic", type: "epic", parentId: "nibs-m1" }),
        makeTreeTableNib({ id: "nibs-t1", title: "Deep task", type: "task", parentId: "nibs-e1" }),
      ];

      const { container } = setupWithNibs(nibs, { onrowcontextmenu });
      // Check row presence by id ("Epic" also appears in the task's Parent column,
      // so text queries are ambiguous).
      const rowVisible = (id: string) => container.querySelector(`tr[data-nib-id='${id}']`) !== null;

      // All three rows visible initially.
      expect(rowVisible("nibs-e1")).toBe(true);
      expect(rowVisible("nibs-t1")).toBe(true);

      // Right-click the milestone to capture its subtree actions.
      const row = container.querySelector("tr[data-nib-id='nibs-m1']") as HTMLElement;
      await user.pointer({ target: row, keys: "[MouseRight]" });
      expect(captured?.hasChildren).toBe(true);

      // Collapse the whole subtree — the milestone collapses, hiding everything below.
      captured!.collapseChildren();
      await tick();
      expect(rowVisible("nibs-e1")).toBe(false);
      expect(rowVisible("nibs-t1")).toBe(false);

      // Expanding just the milestone reveals exactly ONE level (the epic stays collapsed).
      const toggle = container.querySelector("tr[data-nib-id='nibs-m1'] [data-action='toggle']") as HTMLElement;
      await user.click(toggle);
      await tick();
      expect(rowVisible("nibs-e1")).toBe(true);
      expect(rowVisible("nibs-t1")).toBe(false);

      // Expand-children fully expands the subtree again.
      captured!.expandChildren();
      await tick();
      expect(rowVisible("nibs-e1")).toBe(true);
      expect(rowVisible("nibs-t1")).toBe(true);
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

    // Regression: a synthetic "No X" grouping bucket row is not a
    // real nib. Routing its synthetic id through view.open resolves an empty
    // detail query and fires the missing-nib ("no longer exists") heal path.
    // A bucket row must instead toggle/collapse its group, mirroring its caret.
    function makeBucketTestNibs(): TreeTableNib[] {
      return [
        makeTreeTableNib({ id: "nibs-m1", title: "Milestone A", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-e1", title: "Epic under A", type: "epic", parentId: "nibs-m1" }),
        // No milestone parent -> falls into the synthetic "No milestone" bucket.
        makeTreeTableNib({ id: "nibs-loose", title: "Loose Task", type: "task" }),
      ];
    }

    function milestoneBucketId(nibs: TreeTableNib[]): string {
      const bucketId = containingSectionRowId(new Map(nibs.map(n => [n.id, n])), "nibs-loose", "milestones");
      expect(bucketId).not.toBeNull();
      expect(isSyntheticRowId(bucketId!)).toBe(true);
      return bucketId!;
    }

    it("clicking a bucket row title does NOT open/select it and toggles its group instead", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeBucketTestNibs();
      const bucketId = milestoneBucketId(nibs);

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      // The bucket row and its child are visible initially.
      expect(screen.getByText("Loose Task")).toBeInTheDocument();
      const bucketTitle = container.querySelector(
        `tr[data-nib-id="${bucketId}"] [data-action="title"]`,
      ) as HTMLElement;
      expect(bucketTitle).toBeInTheDocument();

      // Click the bucket's title (the exact path that used to route to view.open).
      await user.click(bucketTitle);

      // It must NOT open/select the synthetic bucket id (no "no longer exists").
      expect(sel.selectedNibId).not.toBe(bucketId);
      expect(sel.selectedNibId).toBeNull();

      // Instead the group collapses — its child is hidden.
      expect(screen.queryByText("Loose Task")).not.toBeInTheDocument();

      // Clicking again re-expands the group.
      const bucketTitleAfter = container.querySelector(
        `tr[data-nib-id="${bucketId}"] [data-action="title"]`,
      ) as HTMLElement;
      await user.click(bucketTitleAfter);
      expect(screen.getByText("Loose Task")).toBeInTheDocument();
    });

    it("clicking a bucket row body does NOT open/select it and toggles its group instead", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeBucketTestNibs();
      const bucketId = milestoneBucketId(nibs);

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      expect(screen.getByText("Loose Task")).toBeInTheDocument();

      // Click the row body (a non-action cell), not the title/caret.
      const bucketBodyCell = container.querySelector(
        `tr[data-nib-id="${bucketId}"] [data-testid="nib-type"]`,
      ) as HTMLElement;
      expect(bucketBodyCell).toBeInTheDocument();
      await user.click(bucketBodyCell);

      expect(sel.selectedNibId).not.toBe(bucketId);
      expect(sel.selectedNibId).toBeNull();
      expect(screen.queryByText("Loose Task")).not.toBeInTheDocument();
    });

    it("Ctrl+click on a bucket row title toggles its group and keeps the synthetic id out of the selection", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      sel.select("nibs-m1");
      const nibs = makeBucketTestNibs();
      const bucketId = milestoneBucketId(nibs);

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      expect(screen.getByText("Loose Task")).toBeInTheDocument();
      const bucketTitle = container.querySelector(
        `tr[data-nib-id="${bucketId}"] [data-action="title"]`,
      ) as HTMLElement;

      await user.keyboard("{Control>}");
      await user.click(bucketTitle);
      await user.keyboard("{/Control}");

      // A bucket is not a nib, so it can never join the bulk-action set.
      expect(sel.selectedIds.has(bucketId)).toBe(false);
      expect(sel.selectedIds.has("nibs-m1")).toBe(true);
      // The group collapses instead — its child is hidden.
      expect(screen.queryByText("Loose Task")).not.toBeInTheDocument();
    });

    it("Shift+click on a bucket row body toggles its group and keeps the synthetic id out of the selection", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      sel.select("nibs-m1");
      const nibs = makeBucketTestNibs();
      const bucketId = milestoneBucketId(nibs);

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      expect(screen.getByText("Loose Task")).toBeInTheDocument();
      const bucketBodyCell = container.querySelector(
        `tr[data-nib-id="${bucketId}"] [data-testid="nib-type"]`,
      ) as HTMLElement;

      await user.keyboard("{Shift>}");
      await user.click(bucketBodyCell);
      await user.keyboard("{/Shift}");

      expect(sel.selectedIds.has(bucketId)).toBe(false);
      expect(screen.queryByText("Loose Task")).not.toBeInTheDocument();
    });

    it("Shift+click whose range SPANS a bucket selects the nibs in range and NOT the bucket's synthetic id", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeBucketTestNibs();
      const bucketId = milestoneBucketId(nibs);
      // Anchor on the epic, which sits BEFORE the bucket in view order.
      sel.select("nibs-e1");

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      // View order is [m1, e1, <bucket>, loose]. Shift-click the loose task's row
      // body (after the bucket) so the range spans the interleaved bucket row.
      const looseBodyCell = container.querySelector(
        `tr[data-nib-id="nibs-loose"] [data-testid="nib-type"]`,
      ) as HTMLElement;
      expect(looseBodyCell).toBeInTheDocument();
      await user.keyboard("{Shift>}");
      await user.click(looseBodyCell);
      await user.keyboard("{/Shift}");

      expect(sel.selectedIds.has("nibs-e1")).toBe(true);
      expect(sel.selectedIds.has("nibs-loose")).toBe(true);
      expect(sel.selectedIds.has(bucketId)).toBe(false);
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

    // Space/Enter on a row title resolve WHICH row to act on from the key event's
    // own DOM row ancestor (`tr[data-nib-id]`), falling back to the virtual
    // `focusedNibId` only when the event has no row ancestor (arrow-key nav, where
    // DOM focus sits on the grid container). No DOM->virtual focus sync exists.
    it("Space on a Tab-focused title toggles THAT row (resolved from the DOM), preserving a multi-select", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = [
        makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-001", title: "Task 1", type: "task", parentId: "nibs-m1" }),
        makeTreeTableNib({ id: "nibs-002", title: "Task 2", type: "task", parentId: "nibs-m1" }),
        makeTreeTableNib({ id: "nibs-003", title: "Task 3", type: "task", parentId: "nibs-m1" }),
      ];

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      // Seed a pre-existing multi-select (003) and a STALE virtual focus on a
      // DIFFERENT row (001) than the one we tab to. The row that Space acts on must
      // come from the DOM event, not this stale focusedNibId.
      sel.toggleSelect("nibs-003", "follow");
      sel.focus("nibs-001");

      // Tab lands DOM focus on row 002's title.
      const title2 = container.querySelector(
        "tr[data-nib-id='nibs-002'] [data-action='title']",
      ) as HTMLElement;
      expect(title2.tagName).toBe("BUTTON");
      title2.focus();

      // Real keypress: userEvent synthesizes the native button click that a dropped
      // preventDefault() would let through, so this catches a native open leaking in.
      await user.keyboard(" ");

      // The tabbed-to row (002) toggles in — resolved from the DOM, NOT the stale
      // focusedNibId (001), which a naive focusedNibId-only fix would have toggled.
      expect(sel.selectedIds.has("nibs-002")).toBe(true);
      expect(sel.selectedIds.has("nibs-001")).toBe(false);
      // The pre-existing multi-select survives — a native open would have collapsed
      // the selection to a single 002, dropping 003.
      expect(sel.selectedIds.has("nibs-003")).toBe(true);
      // toggleSelect also moves focus/anchor to the toggled row (like Ctrl-click).
      expect(sel.focusedNibId).toBe("nibs-002");
    });

    // When focusedNibId is null (e.g. Escape cleared it) but DOM focus is still on
    // a row title, Enter must still open that row — the keyboard path resolves the
    // target from the event's DOM row, not from the (null) focusedNibId.
    it("after focus is cleared, Enter on a still-DOM-focused title still opens the nib", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      const title = container.querySelector(
        "tr[data-nib-id='nibs-001'] [data-action='title']",
      ) as HTMLElement;
      title.focus();
      // Simulate the Escape hierarchy clearing virtual focus WITHOUT blurring DOM
      // focus (SelectionState.clearFocus, what the Escape handler calls last).
      sel.clearFocus();
      expect(sel.focusedNibId).toBeNull();

      await user.keyboard("{Enter}");

      // Enter still opens the nib — the target is resolved from the DOM row, not
      // the now-null focusedNibId.
      expect(sel.selectedNibId).toBe("nibs-001");
    });

    // Enter on a Tab-focused bucket title toggles its group: the target resolves
    // from the DOM row (a bucket id), and Enter routes a bucket to toggleNode
    // rather than navigateToNib.
    it("Enter on a Tab-focused bucket title toggles its group", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeBucketTestNibs();
      const bucketId = milestoneBucketId(nibs);

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      // The bucket's child is visible initially.
      expect(screen.getByText("Loose Task")).toBeInTheDocument();

      const bucketTitle = container.querySelector(
        `tr[data-nib-id="${bucketId}"] [data-action="title"]`,
      ) as HTMLElement;
      bucketTitle.focus();
      await user.keyboard("{Enter}");

      // Enter toggled the group closed — its child is hidden — and the synthetic
      // bucket id never entered the selection.
      expect(screen.queryByText("Loose Task")).not.toBeInTheDocument();
      expect(sel.selectedIds.has(bucketId)).toBe(false);
      expect(sel.selectedNibId).toBeNull();
    });

    // Passive focus must NOT arm a destructive action: getActionTargetIds()
    // (Delete/Edit target resolver) falls back to focusedNibId, so merely tabbing
    // to a row must not set focusedNibId — only an explicit selection gesture may.
    it("focusing a row title does not arm it as the Delete target (focus alone sets no action target)", async () => {
      const sel = new SelectionState();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      const title = container.querySelector(
        "tr[data-nib-id='nibs-001'] [data-action='title']",
      ) as HTMLElement;
      title.focus();

      // getActionTargetIds() keys the Delete/Edit target off focusedNibId when
      // there is no multi-select or context-menu target. Passive focus must leave
      // focusedNibId (and the selection) empty so nothing is armed.
      expect(sel.focusedNibId).toBeNull();
      expect(sel.selectedIds.size).toBe(0);
    });

    it("Enter on a focused row title opens it (keyboard-nav path)", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      const title = container.querySelector(
        "tr[data-nib-id='nibs-001'] [data-action='title']",
      ) as HTMLElement;
      title.focus();
      await user.keyboard("{Enter}");

      expect(sel.selectedNibId).toBe("nibs-001");
    });

    it("Space on a title toggles that same row back out after a click selected it", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      const title = container.querySelector(
        "tr[data-nib-id='nibs-001'] [data-action='title']",
      ) as HTMLElement;

      // A click selects the row.
      await user.click(title);
      expect(sel.selectedIds.has("nibs-001")).toBe(true);
      expect(sel.focusedNibId).toBe("nibs-001");

      // Space on that same title toggles the row back out — the keyboard path
      // resolves its target from the event's DOM row (001). Use a real keypress
      // (focus + user.keyboard) so a dropped preventDefault would let the native
      // click through and be caught, rather than a raw synthetic keydown.
      title.focus();
      await user.keyboard(" ");
      expect(sel.selectedIds.has("nibs-001")).toBe(false);
    });

    it("Space on the toggle button keeps native activation (collapses, does NOT toggle selection)", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, {}, { selection: sel });

      expect(screen.getByText("Child Task")).toBeInTheDocument();

      const toggle = container.querySelector(
        "tr[data-nib-id='nibs-m1'] [data-action='toggle']",
      ) as HTMLElement;
      toggle.focus();
      await user.keyboard(" ");

      // Native activation still fires the collapse...
      expect(screen.queryByText("Child Task")).not.toBeInTheDocument();
      // ...and Space did NOT toggle selection — the exemption still covers toggle.
      expect(sel.selectedIds.size).toBe(0);
    });

    it("Space on the add-child button keeps native activation (opens type picker, does NOT toggle selection)", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const onaddchild = vi.fn();
      // An epic row carries the [+] affordance (a milestone's no longer does).
      const nibs = [
        makeTreeTableNib({ id: "nibs-e1", title: "Epic", type: "epic" }),
        makeTreeTableNib({ id: "nibs-001", title: "Child Task", type: "task", parentId: "nibs-e1" }),
      ];

      const { container } = setupWithNibs(nibs, { onaddchild }, { selection: sel });

      const addChild = container.querySelector(
        "tr[data-nib-id='nibs-e1'] [data-action='add-child']",
      ) as HTMLElement;
      addChild.focus();
      await user.keyboard(" ");

      expect(onaddchild).toHaveBeenCalledOnce();
      expect(sel.selectedIds.size).toBe(0);
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
      // An epic row carries every delegated affordance, add-child included
      // (a milestone's no longer does).
      const nibs = [
        makeTreeTableNib({ id: "nibs-e1", title: "Epic", type: "epic" }),
        makeTreeTableNib({ id: "nibs-001", title: "Child Task", type: "task", parentId: "nibs-e1" }),
      ];

      const { container } = setupWithNibs(nibs, { onaddchild }, { selection: sel });

      // Click various interactive elements — all should be handled via delegation
      // Toggle: should collapse without selecting
      const toggle = container.querySelector("tr[data-nib-id='nibs-e1'] [data-action='toggle']") as HTMLElement;
      await user.click(toggle);
      expect(sel.selectedNibId).toBeNull();

      // Add-child: should call onaddchild without selecting
      const addChildBtn = container.querySelector("tr[data-nib-id='nibs-e1'] [data-action='add-child']") as HTMLElement;
      await user.click(addChildBtn);
      expect(onaddchild).toHaveBeenCalledOnce();
      expect(sel.selectedNibId).toBeNull();

      // Title: should select via context
      const titleText = container.querySelector("tr[data-nib-id='nibs-e1'] [data-action='title']") as HTMLElement;
      await user.click(titleText);
      expect(sel.selectedNibId).toBe("nibs-e1");
    });
  });

  describe("openDetailOn gesture", () => {
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
        makeTreeTableNib({ id: "nibs-002", title: "Other Task", type: "task", parentId: "nibs-m1" }),
      ];
    }

    it("double mode: a plain single click selects and focuses without opening the panel", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, { openDetailOn: "double" }, { selection: sel });

      const row = container.querySelector("tr[data-nib-id='nibs-001']") as HTMLElement;
      await user.click(row);

      expect(sel.selectedNibId).toBeNull();
      expect(sel.panelOpen).toBe(false);
      expect(sel.selectedIds.has("nibs-001")).toBe(true);
      expect(sel.focusedNibId).toBe("nibs-001");
      expect(sel.anchorId).toBe("nibs-001");
    });

    it("double mode: a single click on the title text also selects without opening", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, { openDetailOn: "double" }, { selection: sel });

      const titleText = container.querySelector(
        "tr[data-nib-id='nibs-001'] [data-action='title']",
      ) as HTMLElement;
      await user.click(titleText);

      expect(sel.selectedNibId).toBeNull();
      expect(sel.selectedIds.has("nibs-001")).toBe(true);
    });

    it("double mode: a double-click opens the nib", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, { openDetailOn: "double" }, { selection: sel });

      const row = container.querySelector("tr[data-nib-id='nibs-001']") as HTMLElement;
      await user.dblClick(row);

      expect(sel.selectedNibId).toBe("nibs-001");
    });

    it("double mode: an already-open nib survives a single click on a different row", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      sel.select("nibs-001");
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, { openDetailOn: "double" }, { selection: sel });

      const otherRow = container.querySelector("tr[data-nib-id='nibs-002']") as HTMLElement;
      await user.click(otherRow);

      // The panel keeps showing what the user was reading; only the selection moves.
      expect(sel.selectedNibId).toBe("nibs-001");
      expect(sel.selectedIds.has("nibs-002")).toBe(true);
      expect(sel.selectedIds.has("nibs-001")).toBe(false);
      expect(sel.focusedNibId).toBe("nibs-002");
    });

    it("double mode: a single click on a bucket row still toggles its group", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "nibs-001", title: "Loose Task", type: "task", parentId: null }),
      ];
      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
      );
      const { container } = renderTreeTable(
        { filter: {}, viewLevel: "milestones" as ViewLevel, openDetailOn: "double" },
        { selection: sel },
      );

      const bucketRow = Array.from(container.querySelectorAll("tr[data-nib-id]")).find((tr) =>
        isSyntheticRowId(tr.getAttribute("data-nib-id")!),
      ) as HTMLElement;
      expect(bucketRow).toBeTruthy();
      expect(screen.getByText("Loose Task")).toBeInTheDocument();

      await user.click(bucketRow);

      expect(screen.queryByText("Loose Task")).not.toBeInTheDocument();
      expect(sel.selectedIds.size).toBe(0);
      expect(sel.selectedNibId).toBeNull();
    });

    it("double mode: shift+click still range-selects (unchanged by the gate)", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      sel.select("nibs-m1");
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, { openDetailOn: "double" }, { selection: sel });

      const row = container.querySelector("tr[data-nib-id='nibs-002']") as HTMLElement;
      await user.keyboard("{Shift>}");
      await user.click(row);
      await user.keyboard("{/Shift}");

      expect(sel.selectedIds.has("nibs-m1")).toBe(true);
      expect(sel.selectedIds.has("nibs-001")).toBe(true);
      expect(sel.selectedIds.has("nibs-002")).toBe(true);
    });

    it("single mode (default): a single click opens the nib", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, { openDetailOn: "single" }, { selection: sel });

      const row = container.querySelector("tr[data-nib-id='nibs-001']") as HTMLElement;
      await user.click(row);

      expect(sel.selectedNibId).toBe("nibs-001");
    });

    it("single mode (default): a double-click also opens the nib", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      const nibs = makeTestNibs();

      const { container } = setupWithNibs(nibs, { openDetailOn: "single" }, { selection: sel });

      const row = container.querySelector("tr[data-nib-id='nibs-001']") as HTMLElement;
      await user.dblClick(row);

      expect(sel.selectedNibId).toBe("nibs-001");
    });

    // App always passes `prefs`, so `resolvedOpenDetailOn` takes the prefs arm in
    // production and the `openDetailOn` prop arm exists only for tests. These two
    // cases drive the real path — persisted blob → Preferences → resolver → click
    // branch — inside the automated gate, where the prop-based cases above cannot
    // reach it.
    describe("via prefs (the production path)", () => {
      function withPersistedPreferences<T>(blob: Record<string, unknown>, body: (prefs: Preferences) => T): T {
        localStorage.setItem("nibs-filter-preferences", JSON.stringify(blob));
        try {
          return body(new Preferences());
        } finally {
          localStorage.removeItem("nibs-filter-preferences");
        }
      }

      it("double mode: a single click selects without opening the panel", async () => {
        const user = userEvent.setup();
        const sel = new SelectionState();

        await withPersistedPreferences(
          { q: "", viewLevel: "milestones", openDetailOn: "double" },
          async (prefs) => {
            expect(prefs.openDetailOn).toBe("double");
            const { container } = setupWithNibs(makeTestNibs(), { prefs }, { selection: sel });

            await user.click(container.querySelector("tr[data-nib-id='nibs-001']") as HTMLElement);

            expect(sel.selectedNibId).toBeNull();
            expect(sel.selectedIds.has("nibs-001")).toBe(true);
          },
        );
      });

      it("single mode: a single click opens the panel", async () => {
        const user = userEvent.setup();
        const sel = new SelectionState();

        await withPersistedPreferences(
          { q: "", viewLevel: "milestones", openDetailOn: "single" },
          async (prefs) => {
            const { container } = setupWithNibs(makeTestNibs(), { prefs }, { selection: sel });

            await user.click(container.querySelector("tr[data-nib-id='nibs-001']") as HTMLElement);

            expect(sel.selectedNibId).toBe("nibs-001");
          },
        );
      });

      it("prefs wins over the prop, so the prop cannot mask a broken prefs read", async () => {
        const user = userEvent.setup();
        const sel = new SelectionState();

        await withPersistedPreferences(
          { q: "", viewLevel: "milestones", openDetailOn: "double" },
          async (prefs) => {
            const { container } = setupWithNibs(
              makeTestNibs(),
              { prefs, openDetailOn: "single" },
              { selection: sel },
            );

            await user.click(container.querySelector("tr[data-nib-id='nibs-001']") as HTMLElement);

            expect(sel.selectedNibId).toBeNull();
          },
        );
      });
    });

    // Bulk-selection gestures (ctrl/meta+click, shift+click) collapse to exactly
    // one id often enough that, left alone, they become a second way for a SINGLE
    // click to open the panel — and a collapse to zero-or-many tears down a panel
    // showing an unrelated nib. In "double" mode the panel has exactly one writer
    // path (the explicit open gestures), so these cases pin that it stays put.
    describe("double mode: bulk gestures never move the detail panel", () => {
      async function ctrlClick(user: ReturnType<typeof userEvent.setup>, row: HTMLElement) {
        await user.keyboard("{Control>}");
        await user.click(row);
        await user.keyboard("{/Control}");
      }

      async function shiftClick(user: ReturnType<typeof userEvent.setup>, row: HTMLElement) {
        await user.keyboard("{Shift>}");
        await user.click(row);
        await user.keyboard("{/Shift}");
      }

      it("ctrl+click with nothing selected selects without opening the panel", async () => {
        const user = userEvent.setup();
        const sel = new SelectionState();

        const { container } = setupWithNibs(makeTestNibs(), { openDetailOn: "double" }, { selection: sel });

        await ctrlClick(user, container.querySelector("tr[data-nib-id='nibs-001']") as HTMLElement);

        expect(sel.selectedNibId).toBeNull();
        expect(sel.panelOpen).toBe(false);
        expect(sel.selectedIds.has("nibs-001")).toBe(true);
        expect(sel.focusedNibId).toBe("nibs-001");
      });

      it("shift+click on the anchor row itself selects without opening the panel", async () => {
        const user = userEvent.setup();
        const sel = new SelectionState();

        const { container } = setupWithNibs(makeTestNibs(), { openDetailOn: "double" }, { selection: sel });

        const row = container.querySelector("tr[data-nib-id='nibs-001']") as HTMLElement;
        // A plain click sets the anchor; shift-clicking the same row makes the
        // range collapse to exactly one id.
        await user.click(row);
        await shiftClick(user, row);

        expect(sel.selectedNibId).toBeNull();
        expect(sel.selectedIds.has("nibs-001")).toBe(true);
      });

      it("ctrl+clicking two other rows leaves an open panel on its own nib", async () => {
        const user = userEvent.setup();
        const sel = new SelectionState();
        sel.select("nibs-m1");

        const { container } = setupWithNibs(makeTestNibs(), { openDetailOn: "double" }, { selection: sel });

        await ctrlClick(user, container.querySelector("tr[data-nib-id='nibs-001']") as HTMLElement);
        await ctrlClick(user, container.querySelector("tr[data-nib-id='nibs-002']") as HTMLElement);

        expect(sel.selectedNibId).toBe("nibs-m1");
        expect(sel.selectedIds.has("nibs-001")).toBe(true);
        expect(sel.selectedIds.has("nibs-002")).toBe(true);
      });

      it("a ctrl+click that collapses the selection to zero leaves the panel alone", async () => {
        const user = userEvent.setup();
        const sel = new SelectionState();
        sel.select("nibs-m1");
        sel.deselectAll(); // panel still on nibs-m1, nothing selected

        const { container } = setupWithNibs(makeTestNibs(), { openDetailOn: "double" }, { selection: sel });

        const row = container.querySelector("tr[data-nib-id='nibs-001']") as HTMLElement;
        await ctrlClick(user, row); // in
        await ctrlClick(user, row); // back out — selection is now empty

        expect(sel.selectedIds.size).toBe(0);
        expect(sel.selectedNibId).toBe("nibs-m1");
      });

      it("a shift+click range spanning several rows leaves an open panel alone", async () => {
        const user = userEvent.setup();
        const sel = new SelectionState();
        sel.select("nibs-m1");

        const { container } = setupWithNibs(makeTestNibs(), { openDetailOn: "double" }, { selection: sel });

        await user.click(container.querySelector("tr[data-nib-id='nibs-001']") as HTMLElement);
        await shiftClick(user, container.querySelector("tr[data-nib-id='nibs-002']") as HTMLElement);

        expect(sel.selectedIds.has("nibs-001")).toBe(true);
        expect(sel.selectedIds.has("nibs-002")).toBe(true);
        expect(sel.selectedNibId).toBe("nibs-m1");
      });

      it("single mode is unchanged: ctrl+click collapsing to one still opens the panel", async () => {
        const user = userEvent.setup();
        const sel = new SelectionState();

        const { container } = setupWithNibs(makeTestNibs(), { openDetailOn: "single" }, { selection: sel });

        await ctrlClick(user, container.querySelector("tr[data-nib-id='nibs-001']") as HTMLElement);

        expect(sel.selectedNibId).toBe("nibs-001");
      });

      it("single mode is unchanged: a multi-row ctrl+click still closes the panel", async () => {
        const user = userEvent.setup();
        const sel = new SelectionState();
        sel.select("nibs-m1");

        const { container } = setupWithNibs(makeTestNibs(), { openDetailOn: "single" }, { selection: sel });

        await ctrlClick(user, container.querySelector("tr[data-nib-id='nibs-001']") as HTMLElement);

        expect(sel.selectedNibId).toBeNull();
      });

      it("single mode is unchanged: shift+click on the anchor row still opens the panel", async () => {
        const user = userEvent.setup();
        const sel = new SelectionState();

        const { container } = setupWithNibs(makeTestNibs(), { openDetailOn: "single" }, { selection: sel });

        const row = container.querySelector("tr[data-nib-id='nibs-001']") as HTMLElement;
        await user.click(row);
        await shiftClick(user, row);

        expect(sel.selectedNibId).toBe("nibs-001");
      });

      it("shift+ArrowDown collapsing to one row does not open the panel", async () => {
        const user = userEvent.setup();
        const sel = new SelectionState();
        sel.focus("nibs-m1");
        sel.anchorId = "nibs-001";

        setupWithNibs(makeTestNibs(), { openDetailOn: "double" }, { selection: sel });

        screen.getByRole("grid").focus();
        await user.keyboard("{Shift>}{ArrowDown}{/Shift}");

        expect(sel.focusedNibId).toBe("nibs-001");
        expect(sel.selectedIds.has("nibs-001")).toBe(true);
        expect(sel.selectedNibId).toBeNull();
      });

      it("Space on a focused row does not open the panel", async () => {
        const user = userEvent.setup();
        const sel = new SelectionState();
        sel.focus("nibs-001");

        setupWithNibs(makeTestNibs(), { openDetailOn: "double" }, { selection: sel });

        screen.getByRole("grid").focus();
        await user.keyboard("{ }");

        expect(sel.selectedIds.has("nibs-001")).toBe(true);
        expect(sel.selectedNibId).toBeNull();
      });
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

    // Regression: ensureVisible for a nib that is in the
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

    // Regression: a cold deep-link runs syncFromUrl on
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
    it("re-queries when a server-side filter (search) changes, after the debounce", async () => {
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

      // A `search` change is server-side, so it re-keys the list query — but the
      // refetch is debounced (nibs-rv7c), so it lands shortly after, not
      // synchronously. waitFor rides out the debounce window.
      await rerender({ filter: { search: "login" } });
      await waitFor(() => {
        expect(mockQueryStore.mock.calls.length).toBeGreaterThan(initialCallCount);
      });
      const latestCall = mockQueryStore.mock.calls[mockQueryStore.mock.calls.length - 1];
      expect(latestCall[0].variables!.filter).toMatchObject({ search: "login" });
    });

    it("does NOT re-query when only a client-side facet (status) changes", async () => {
      const nibs: TreeTableNib[] = [
        makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-001", title: "Task", type: "task", parentId: "nibs-m1" }),
      ];

      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
      );

      const { rerender } = renderTreeTable({ filter: {} });
      const initialCallCount = mockQueryStore.mock.calls.length;

      // The status include-list is stripped from the server filter, so the server
      // data is unchanged — status is applied client-side. No re-query fires, even
      // after the debounce window elapses (the server filter is content-equal).
      await rerender({ filter: { status: [...OPEN_STATUSES] } });
      await new Promise((resolve) => setTimeout(resolve, 300)); // > the 250ms refetch debounce
      expect(mockQueryStore.mock.calls.length).toBe(initialCallCount);
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

      await rerender({ filter: { status: [...OPEN_STATUSES] } });

      // Should have 2 rows after filter change
      rows = container.querySelectorAll("[data-testid='tree-row']");
      expect(rows).toHaveLength(2);
      expect(screen.getByText("Active Task")).toBeInTheDocument();
      expect(screen.queryByText("Completed Task")).not.toBeInTheDocument();
    });
  });

  // Scroll-restore lifecycle. These drive the real mount → scroll →
  // remount → restore path through TreeTable + TreeViewState — the coverage gap
  // the prior review flagged, where the two confirmed defects lived.
  //
  // jsdom has NO layout, so it cannot reproduce real scrollTop CLAMPING
  // (scrollHeight - clientHeight) and does not fire a native scroll event on a
  // programmatic scrollTop assignment. jsdom stores scrollTop verbatim, so these
  // tests verify the restore-effect WIRING (mount/remount → restore) end-to-end;
  // the clamp-echo defect's trigger (#2) is covered at the unit level via
  // simulation in useScrollRestore.test.ts.
  describe("scroll restore", () => {
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

  describe("prune multi-select on filter change", () => {
    function makeNibs(): TreeTableNib[] {
      return [
        makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
        makeTreeTableNib({ id: "nibs-t1", title: "Task one", type: "task", parentId: "nibs-m1" }),
        makeTreeTableNib({ id: "nibs-t2", title: "Task two", type: "task", parentId: "nibs-m1" }),
        makeTreeTableNib({ id: "nibs-b1", title: "A bug", type: "bug", parentId: "nibs-m1" }),
      ];
    }

    it("drops selected nibs that no longer match the active client filter", async () => {
      const sel = new SelectionState();
      sel.toggleSelect("nibs-t1", "follow");
      sel.toggleSelect("nibs-t2", "follow");
      sel.toggleSelect("nibs-b1", "follow");

      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: { nibs: makeNibs() }, stale: false }) as any
      );

      const { rerender } = renderTreeTable(
        { filter: {}, viewLevel: "milestones" as ViewLevel },
        { selection: sel },
      );
      await tick();

      // No filter yet — all three remain selected.
      expect(sel.selectedIds.size).toBe(3);

      // Filter to tasks only — the bug is no longer selectable and must be pruned.
      await rerender({ filter: { type: ["task"] }, viewLevel: "milestones" as ViewLevel });
      await tick();

      expect(sel.selectedIds.has("nibs-t1")).toBe(true);
      expect(sel.selectedIds.has("nibs-t2")).toBe(true);
      expect(sel.selectedIds.has("nibs-b1")).toBe(false);
      expect(sel.selectedIds.size).toBe(2);
    });

    it("resets anchor/focus that fall out of the filter", async () => {
      const sel = new SelectionState();
      sel.toggleSelect("nibs-t1", "follow");
      sel.toggleSelect("nibs-b1", "follow"); // anchor + focus land on the bug

      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: { nibs: makeNibs() }, stale: false }) as any
      );

      const { rerender } = renderTreeTable(
        { filter: {}, viewLevel: "milestones" as ViewLevel },
        { selection: sel },
      );
      await tick();
      expect(sel.anchorId).toBe("nibs-b1");
      expect(sel.focusedNibId).toBe("nibs-b1");

      await rerender({ filter: { type: ["task"] }, viewLevel: "milestones" as ViewLevel });
      await tick();

      expect(sel.anchorId).toBeNull();
      expect(sel.focusedNibId).toBeNull();
    });

    it("keeps the multi-selection intact when a parent row is collapsed (no filter)", async () => {
      const user = userEvent.setup();
      const sel = new SelectionState();
      sel.toggleSelect("nibs-t1", "follow");
      sel.toggleSelect("nibs-t2", "follow");

      mockQueryStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: { nibs: makeNibs() }, stale: false }) as any
      );

      const { container } = renderTreeTable(
        { filter: {}, viewLevel: "milestones" as ViewLevel },
        { selection: sel },
      );
      await tick();
      expect(sel.selectedIds.size).toBe(2);

      // Collapse the milestone — hides the two selected tasks from view.
      const toggle = container.querySelector("tr[data-nib-id='nibs-m1'] [data-action='toggle']") as HTMLElement;
      await user.click(toggle);
      await tick();

      // Rows are hidden…
      expect(screen.queryByText("Task one")).not.toBeInTheDocument();
      // …but the selection is preserved — collapse is not a filter.
      expect(sel.selectedIds.has("nibs-t1")).toBe(true);
      expect(sel.selectedIds.has("nibs-t2")).toBe(true);
      expect(sel.selectedIds.size).toBe(2);
    });

    it("does not prune while the query is still fetching (cold deep-link guard)", async () => {
      const sel = new SelectionState();
      sel.select("nibs-deep");
      sel.toggleSelect("nibs-deep2", "follow");

      // Query in flight: fetching, no data yet.
      mockQueryStore.mockReturnValue(
        readable({ fetching: true, error: undefined, data: undefined, stale: false }) as any
      );

      renderTreeTable(
        { filter: {}, viewLevel: "milestones" as ViewLevel },
        { selection: sel },
      );
      await tick();

      // Data hasn't landed — selection must be left intact, not wiped to empty.
      expect(sel.selectedIds.has("nibs-deep")).toBe(true);
      expect(sel.selectedIds.has("nibs-deep2")).toBe(true);
      expect(sel.selectedIds.size).toBe(2);
    });
  });
});

describe("TreeTable — per-view column order + reorder drag", () => {
  beforeEach(() => {
    mockQueryStore.mockReset();
    mockSubscriptionStore.mockReset();
    mockSubscriptionStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any
    );
  });

  function renderOrdered(props: Record<string, unknown> = {}) {
    const nibs: TreeTableNib[] = [
      makeTreeTableNib({ id: "nibs-001", title: "Row one", type: "task", status: "todo" }),
    ];
    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );
    return renderTreeTable({
      filter: {},
      viewLevel: "flat" as ViewLevel,
      visibleColumns: ["id", "title", "status"] as ColumnKey[],
      columnOrder: ["id", "title", "status"] as ColumnKey[],
      ...props,
    });
  }

  it("renders headers in the per-view columnOrder (not canonical)", () => {
    const { container } = renderOrdered({ columnOrder: ["status", "title", "id"] as ColumnKey[] });
    const headers = Array.from(container.querySelectorAll("thead th[data-col-key]")).map((th) => th.getAttribute("data-col-key"));
    expect(headers).toEqual(["status", "title", "id"]);
  });

  it("renders row cells in the same order as the reordered headers", () => {
    const { container } = renderOrdered({ columnOrder: ["status", "title", "id"] as ColumnKey[] });
    const row = container.querySelector("tr[data-testid='tree-row']")!;
    const cellTestids = Array.from(row.querySelectorAll("td[data-testid]")).map((td) => td.getAttribute("data-testid"));
    expect(cellTestids).toEqual(["nib-status", "nib-title", "nib-id"]);
  });

  it("tableWidth sums the visible column widths (actions 32px + widths), order-independent", () => {
    const { container } = renderOrdered({ columnOrder: ["status", "title", "id"] as ColumnKey[] });
    const width = parseInt((container.querySelector("table") as HTMLElement).style.width, 10);
    expect(width).toBe(32 + DEFAULT_COLUMN_WIDTHS.id + DEFAULT_COLUMN_WIDTHS.title + DEFAULT_COLUMN_WIDTHS.status);
  });

  it("dragging a header past the threshold writes the reordered columnOrder and suppresses the sort click", () => {
    const oncolumnorderchange = vi.fn();
    const ontablesortchange = vi.fn();
    const { container } = renderOrdered({
      columnOrder: ["id", "title", "status"] as ColumnKey[],
      oncolumnorderchange,
      ontablesortchange,
      tableSort: null,
    });

    const idTh = container.querySelector("thead th[data-col-key='id']") as HTMLElement;
    const stateTh = container.querySelector("thead th[data-col-key='status']") as HTMLElement;
    stateTh.getBoundingClientRect = () =>
      ({ top: 0, bottom: 40, left: 0, right: 100, width: 100, height: 40, x: 0, y: 0, toJSON: () => {} }) as DOMRect;

    const orig = document.elementFromPoint;
    document.elementFromPoint = () => stateTh;
    try {
      idTh.dispatchEvent(new PointerEvent("pointerdown", { clientX: 10, clientY: 10, button: 0, bubbles: true }));
      // 70px move crosses the threshold; cursor x=80 is the right half of the
      // state header (mid=50) → drop "after".
      window.dispatchEvent(new PointerEvent("pointermove", { clientX: 80, clientY: 10, bubbles: true }));
      window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

      expect(oncolumnorderchange).toHaveBeenCalledTimes(1);
      expect(oncolumnorderchange).toHaveBeenCalledWith(["title", "status", "id"]);

      // The click a browser fires right after the drag must NOT toggle the sort.
      const idSortBtn = container.querySelector("[data-testid='table-sort-id']") as HTMLElement;
      idSortBtn.dispatchEvent(new MouseEvent("click", { bubbles: true }));
      expect(ontablesortchange).not.toHaveBeenCalled();
    } finally {
      document.elementFromPoint = orig;
    }
  });

  it("a plain header-body click (no drag) still toggles the sort (below-threshold disambiguation)", () => {
    const ontablesortchange = vi.fn();
    const { container } = renderOrdered({ ontablesortchange, tableSort: null });
    // Click the <th> body itself — the whole header is the sort control now.
    const idTh = container.querySelector("thead th[data-col-key='id']") as HTMLElement;
    idTh.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(ontablesortchange).toHaveBeenCalledWith({ field: "id", direction: "asc" });
  });

  it("a pointerdown on the resize edge-handle resizes and does NOT start a column reorder", () => {
    const oncolumnorderchange = vi.fn();
    const oncolumnwidthschange = vi.fn();
    const { container } = renderOrdered({ oncolumnorderchange, oncolumnwidthschange });
    const idTh = container.querySelector("thead th[data-col-key='id']") as HTMLElement;
    const handle = idTh.querySelector(".resize-handle") as HTMLElement;

    handle.dispatchEvent(new PointerEvent("pointerdown", { clientX: 100, clientY: 5, button: 0, bubbles: true }));
    handle.dispatchEvent(new PointerEvent("pointermove", { clientX: 150, clientY: 5, bubbles: true }));
    handle.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));

    // The resize path fired (a width change was emitted)...
    expect(oncolumnwidthschange).toHaveBeenCalled();
    // ...and the header-drag guard bailed on the resize handle — no reorder.
    expect(oncolumnorderchange).not.toHaveBeenCalled();
  });
});

// A grouping lens is lossless in WORK ITEMS but not in ROWS: buildViewTree hides
// a container ranked above the lens's tier while descending into it, so a
// milestone selected in the Tree view has no row at all under the Epics lens.
// These drive the whole seam — switchViewLevel → TreeViewState slot → the
// applier effect → SelectionState/useScrollRestore — through the real component.
describe("view transition reconcile", () => {
  const nibs: TreeTableNib[] = [
    makeTreeTableNib({ id: "nibs-m1", title: "Milestone", type: "milestone" }),
    makeTreeTableNib({ id: "nibs-e1", title: "Epic", type: "epic", parentId: "nibs-m1" }),
    makeTreeTableNib({ id: "nibs-t1", title: "Task", type: "task", parentId: "nibs-e1" }),
  ];

  async function withPersistedPreferences(
    blob: Record<string, unknown>,
    body: (prefs: Preferences) => Promise<void>,
  ): Promise<void> {
    localStorage.setItem("nibs-filter-preferences", JSON.stringify(blob));
    try {
      await body(new Preferences());
    } finally {
      localStorage.removeItem("nibs-filter-preferences");
    }
  }

  function mountTree(selection: SelectionState, treeView: TreeViewState, prefs: Preferences) {
    // Self-contained rather than leaning on a sibling describe's beforeEach, so a
    // filtered run (`-t`) of this block alone still mounts a working table.
    mockSubscriptionStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any
    );
    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any
    );
    return renderTreeTable({ prefs }, { selection, treeView });
  }

  it("has no row for the milestone under the Epics lens (the premise)", async () => {
    await withPersistedPreferences({ q: "", viewLevel: "epics" }, async (prefs) => {
      const { container } = mountTree(new SelectionState(), new TreeViewState("epics"), prefs);
      await tick();

      expect(container.querySelector("tr[data-nib-id='nibs-e1']")).not.toBeNull();
      expect(container.querySelector("tr[data-nib-id='nibs-m1']")).toBeNull();
    });
  });

  it("drops the selection, focus and anchor the incoming lens has no row for", async () => {
    const selection = new SelectionState();
    const treeView = new TreeViewState("none");

    await withPersistedPreferences({ q: "", viewLevel: "none" }, async (prefs) => {
      mountTree(selection, treeView, prefs);
      await tick();

      selection.select("nibs-m1");
      await tick();
      expect(selection.selectedIds.has("nibs-m1")).toBe(true);

      switchViewLevel(prefs, undefined, treeView, "none", "epics");
      await tick();

      expect(selection.selectedIds.has("nibs-m1")).toBe(false);
      expect(selection.focusedNibId).toBeNull();
      expect(selection.anchorId).toBeNull();
    });
  });

  it("leaves the detail panel open on that nib — nib-keyed state is not per-view", async () => {
    // `selectedNibId` is also the `?nib=` URL, and closing it would need the
    // replaceClosed() heal plus the unsaved-body-buffer guard. Reconciling
    // selection and focus is deliberately the whole job.
    const selection = new SelectionState();
    const treeView = new TreeViewState("none");

    await withPersistedPreferences({ q: "", viewLevel: "none" }, async (prefs) => {
      mountTree(selection, treeView, prefs);
      await tick();

      selection.select("nibs-m1");
      await tick();

      switchViewLevel(prefs, undefined, treeView, "none", "epics");
      await tick();

      expect(selection.selectedNibId).toBe("nibs-m1");
      expect(selection.panelOpen).toBe(true);
    });
  });

  it("keeps a selection the incoming lens still has a row for", async () => {
    const selection = new SelectionState();
    const treeView = new TreeViewState("none");

    await withPersistedPreferences({ q: "", viewLevel: "none" }, async (prefs) => {
      mountTree(selection, treeView, prefs);
      await tick();

      selection.select("nibs-t1");
      await tick();

      switchViewLevel(prefs, undefined, treeView, "none", "epics");
      await tick();

      expect(selection.selectedIds.has("nibs-t1")).toBe(true);
      expect(selection.focusedNibId).toBe("nibs-t1");
    });
  });

  it("reveals the surviving row, expanding whatever hides it in the new view", async () => {
    // The anchor is handed to the existing ensureVisible sink, which expands
    // collapsed ancestors — so a row that survives the switch but sits inside a
    // collapsed branch is brought back on screen rather than merely kept selected.
    const selection = new SelectionState();
    const treeView = new TreeViewState("none");

    await withPersistedPreferences({ q: "", viewLevel: "none" }, async (prefs) => {
      const { container } = mountTree(selection, treeView, prefs);
      await tick();

      selection.select("nibs-t1");
      treeView.setCollapsed(["nibs-e1"]);
      await tick();
      expect(container.querySelector("tr[data-nib-id='nibs-t1']")).toBeNull();

      switchViewLevel(prefs, undefined, treeView, "none", "epics");
      await waitFor(() => {
        expect(container.querySelector("tr[data-nib-id='nibs-t1']")).not.toBeNull();
      });
      expect(treeView.isCollapsed("nibs-e1")).toBe(false);
    });
  });

  it("consumes the transition, so a later unrelated re-render does not re-reconcile", async () => {
    const selection = new SelectionState();
    const treeView = new TreeViewState("none");

    await withPersistedPreferences({ q: "", viewLevel: "none" }, async (prefs) => {
      mountTree(selection, treeView, prefs);
      await tick();

      switchViewLevel(prefs, undefined, treeView, "none", "epics");
      await tick();
      expect(treeView.pendingTransition).toBeNull();
      expect(treeView.activeLevel).toBe("epics");

      // A selection made AFTER the switch must survive: nothing is pending, so
      // the applier has no reason to run again.
      selection.select("nibs-t1");
      await tick();
      expect(selection.selectedIds.has("nibs-t1")).toBe(true);
    });
  });

  it("resets the scroll offset instead of carrying the outgoing view's into the new one", async () => {
    // The container is NOT recreated by a view switch, so element identity cannot
    // see it — the epoch is what retires ownership and re-arms the restore.
    const treeView = new TreeViewState("none");
    treeView.scrollTop = 500;

    await withPersistedPreferences({ q: "", viewLevel: "none" }, async (prefs) => {
      const { container } = mountTree(new SelectionState(), treeView, prefs);
      await tick();

      const sc = container.querySelector(".scroll-container") as HTMLElement;
      expect(sc.scrollTop).toBe(500);

      switchViewLevel(prefs, undefined, treeView, "none", "epics");
      await tick();

      expect(treeView.scrollTop).toBe(0);
      // Same element, reset offset — proving the applier's epoch bump reached the
      // restore effect in the same flush.
      expect(container.querySelector(".scroll-container")).toBe(sc);
      expect(sc.scrollTop).toBe(0);
    });
  });

  it("resets the scroll even when the incoming view has the same number of rows", async () => {
    // The Tree and Milestones lenses emit the same three rows for this fixture,
    // so neither the container binding nor the row count changes — the epoch is
    // the only thing that can tell the restore effect a switch happened.
    const treeView = new TreeViewState("none");
    treeView.scrollTop = 500;

    await withPersistedPreferences({ q: "", viewLevel: "none" }, async (prefs) => {
      const { container } = mountTree(new SelectionState(), treeView, prefs);
      await tick();

      const sc = container.querySelector(".scroll-container") as HTMLElement;
      const before = container.querySelectorAll("tr[data-nib-id]").length;
      expect(sc.scrollTop).toBe(500);

      switchViewLevel(prefs, undefined, treeView, "none", "milestones");
      await tick();

      expect(container.querySelectorAll("tr[data-nib-id]").length).toBe(before);
      expect(sc.scrollTop).toBe(0);
    });
  });

  it("does not reconcile when the current lens is re-picked", async () => {
    const selection = new SelectionState();
    const treeView = new TreeViewState("epics");

    await withPersistedPreferences({ q: "", viewLevel: "epics" }, async (prefs) => {
      mountTree(selection, treeView, prefs);
      await tick();

      selection.select("nibs-t1");
      treeView.scrollTop = 300;
      await tick();

      switchViewLevel(prefs, undefined, treeView, "epics", "epics");
      await tick();

      expect(selection.selectedIds.has("nibs-t1")).toBe(true);
      expect(treeView.scrollTop).toBe(300);
    });
  });

  // Until the list query lands there is no membership to reconcile against —
  // viewMemberIds is empty, and reconciling would drop a selection nothing
  // invalidated (a cold deep-link populates it via syncFromUrl before data
  // arrives). The switch is still owed a reconcile, so the slot has to survive
  // the wait rather than be consumed by it.
  describe("with the list query still in flight", () => {
    /** Mount before the result lands; `settle` delivers it mid-test. */
    function mountLoadingTree(selection: SelectionState, treeView: TreeViewState, prefs: Preferences) {
      mockSubscriptionStore.mockReturnValue(
        readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any
      );
      const store = writable<any>({ fetching: true, error: undefined, data: undefined, stale: false });
      mockQueryStore.mockReturnValue(store as any);
      const rendered = renderTreeTable({ prefs }, { selection, treeView });
      return {
        ...rendered,
        settle: () => store.set({ fetching: false, error: undefined, data: { nibs }, stale: false }),
      };
    }

    it("holds the transition rather than pruning against the empty dataset", async () => {
      const selection = new SelectionState();
      const treeView = new TreeViewState("none");

      await withPersistedPreferences({ q: "", viewLevel: "none" }, async (prefs) => {
        mountLoadingTree(selection, treeView, prefs);
        await tick();

        selection.select("nibs-t1");
        await tick();

        switchViewLevel(prefs, undefined, treeView, "none", "epics");
        await tick();

        expect(selection.selectedIds.has("nibs-t1")).toBe(true);
        expect(selection.focusedNibId).toBe("nibs-t1");
        expect(selection.anchorId).toBe("nibs-t1");
        // Unconsumed — deferred, not skipped.
        expect(treeView.pendingTransition).toEqual({ from: "none", to: "epics" });
      });
    });

    it("reconciles the held transition once the result lands", async () => {
      const selection = new SelectionState();
      const treeView = new TreeViewState("none");

      await withPersistedPreferences({ q: "", viewLevel: "none" }, async (prefs) => {
        const { settle } = mountLoadingTree(selection, treeView, prefs);
        await tick();

        selection.select("nibs-m1");
        await tick();

        switchViewLevel(prefs, undefined, treeView, "none", "epics");
        await tick();
        expect(selection.selectedIds.has("nibs-m1")).toBe(true);

        settle();

        // The Epics lens has no row for the milestone: a hold that never fired
        // would leave it selected, focused and off screen — the very state the
        // reconcile exists to clear.
        await waitFor(() => {
          expect(selection.selectedIds.has("nibs-m1")).toBe(false);
        });
        expect(selection.focusedNibId).toBeNull();
        expect(selection.anchorId).toBeNull();
        expect(treeView.pendingTransition).toBeNull();
        expect(treeView.activeLevel).toBe("epics");
      });
    });

    it("keeps a survivor and resets the scroll when the held transition fires", async () => {
      const selection = new SelectionState();
      const treeView = new TreeViewState("none");
      treeView.scrollTop = 500;

      await withPersistedPreferences({ q: "", viewLevel: "none" }, async (prefs) => {
        const { settle } = mountLoadingTree(selection, treeView, prefs);
        await tick();

        selection.select("nibs-t1");
        await tick();

        switchViewLevel(prefs, undefined, treeView, "none", "epics");
        await tick();
        // Deferring the scroll reset with the prune is safe: restore() bails at
        // hasContent() while there are no rows, so nothing is clamped meanwhile.
        expect(treeView.scrollTop).toBe(500);

        settle();

        await waitFor(() => {
          expect(treeView.pendingTransition).toBeNull();
        });
        expect(selection.selectedIds.has("nibs-t1")).toBe(true);
        expect(selection.focusedNibId).toBe("nibs-t1");
        expect(treeView.scrollTop).toBe(0);
      });
    });

    it("reconciles immediately while a REFETCH is in flight, because the rows are already on screen", async () => {
      const selection = new SelectionState();
      const treeView = new TreeViewState("none");

      await withPersistedPreferences({ q: "", viewLevel: "none" }, async (prefs) => {
        mockSubscriptionStore.mockReturnValue(
          readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any
        );
        // queryStore MERGES emissions, so a refetch keeps the previous data and
        // only a re-key starts at undefined. The table refetches on every
        // nibChanged event, so this is the state after any create, update or
        // delete — the incoming lens is rendered and there is real membership to
        // reconcile against, so holding the transition here would leave the
        // milestone selected with no row, which is the bug the seam closes.
        const store = writable<any>({ fetching: true, error: undefined, data: { nibs }, stale: true });
        mockQueryStore.mockReturnValue(store as any);
        renderTreeTable({ prefs }, { selection, treeView });
        await tick();

        selection.select("nibs-m1");
        await tick();

        switchViewLevel(prefs, undefined, treeView, "none", "epics");

        await waitFor(() => {
          expect(treeView.pendingTransition).toBeNull();
        });
        expect(selection.selectedIds.has("nibs-m1")).toBe(false);
        expect(selection.focusedNibId).toBeNull();
        expect(selection.anchorId).toBeNull();
      });
    });
  });
});
