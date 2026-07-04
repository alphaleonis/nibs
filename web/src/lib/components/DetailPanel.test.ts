import { render, screen, waitFor } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { readable, writable } from "svelte/store";
import DetailPanel from "./DetailPanel.svelte";
import { CONFIRM_DIALOG_KEY } from "$lib/contexts";
import type { ConfirmDialogState, ConfirmDialogOptions } from "$lib/composables/useConfirmDialog.svelte";
// bits-ui scroll lock sets pointer-events: none on <body>, so disable the check
const user = userEvent.setup({ pointerEventsCheck: 0 });

function makeMockConfirmDialog(): ConfirmDialogState & { lastOpts: ConfirmDialogOptions | null } {
  const state = {
    open: false,
    title: "",
    message: "",
    label: "",
    variant: "danger" as "danger" | "warning",
    action: null as (() => void) | null,
    lastOpts: null as ConfirmDialogOptions | null,
    showConfirm: vi.fn((opts: ConfirmDialogOptions) => {
      state.open = true;
      state.title = opts.title;
      state.message = opts.message;
      state.label = opts.label;
      state.variant = opts.variant;
      state.action = opts.action;
      state.lastOpts = opts;
    }),
    close: vi.fn(() => {
      state.open = false;
      state.action = null;
    }),
  };
  return state;
}

function makeContext(confirmDialog: ConfirmDialogState): Map<string, unknown> {
  const m = new Map<string, unknown>();
  m.set(CONFIRM_DIALOG_KEY, confirmDialog);
  return m;
}

// Mock urql (still needed for queryStore)
// Hoisted mock for subscriptionStore
const { mockSubscriptionStore } = vi.hoisted(() => {
  return { mockSubscriptionStore: vi.fn() };
});

vi.mock("@urql/svelte", async () => {
  const actual = await vi.importActual<typeof import("@urql/svelte")>("@urql/svelte");
  return {
    ...actual,
    getContextClient: vi.fn().mockReturnValue({}),
    queryStore: vi.fn(),
    subscriptionStore: mockSubscriptionStore,
  };
});

// Mock the mutations module
const { mockExecute, mockIsMutating } = vi.hoisted(() => {
  return {
    mockExecute: vi.fn().mockResolvedValue({ ok: true, data: {} }),
    mockIsMutating: vi.fn().mockReturnValue(false),
  };
});
vi.mock("$lib/mutations", () => ({
  getMutationStore: () => ({
    execute: mockExecute,
    isMutating: mockIsMutating,
    get pending() { return false; },
  }),
}));

// Mock svelte-sonner toast (some tests still check for non-mutation toasts)
const { mockToastError } = vi.hoisted(() => {
  return { mockToastError: vi.fn() };
});
vi.mock("svelte-sonner", async () => {
  const actual = await vi.importActual<typeof import("svelte-sonner")>("svelte-sonner");
  return {
    ...actual,
    toast: {
      ...actual.toast,
      error: mockToastError,
    },
  };
});

import { queryStore } from "@urql/svelte";
import { CONFIG_QUERY, NIB_DETAIL_QUERY } from "$lib/queries";
const mockQueryStore = vi.mocked(queryStore);

function makeNibData(overrides: Record<string, unknown> = {}) {
  return {
    id: "nibs-abc1",
    title: "Fix login bug",
    status: "in-progress",
    type: "bug",
    priority: "high",
    estimate: "m",
    tags: ["auth", "frontend"],
    body: "",
    documents: [],
    etag: "abc123",
    parent: null,
    children: [],
    blocking: [],
    blockedBy: [],
    mentions: [],
    mentionedBy: [],
    ...overrides,
  };
}

// Dispatch queryStore calls to the correct mock based on the query document.
// DetailPanel subscribes to both NIB_DETAIL_QUERY and CONFIG_QUERY.
function setupQueryDispatch(opts: {
  nib?: ReturnType<typeof makeNibData> | null;
  fetching?: boolean;
  error?: { message: string };
  prefix?: string;
  projectName?: string;
}) {
  const nibState = opts.fetching
    ? { fetching: true, error: undefined, data: undefined, stale: false }
    : opts.error
      ? { fetching: false, error: opts.error, data: undefined, stale: false }
      : {
          fetching: false,
          error: undefined,
          data: opts.nib !== undefined ? { nib: opts.nib } : { nib: null },
          stale: false,
        };
  const configState = {
    fetching: false,
    error: undefined,
    data: {
      config: {
        projectName: opts.projectName ?? "nibs",
        prefix: opts.prefix ?? "nibs-",
      },
    },
    stale: false,
  };
  mockQueryStore.mockImplementation((args: any) => {
    if (args?.query === CONFIG_QUERY) {
      return readable(configState) as any;
    }
    if (args?.query === NIB_DETAIL_QUERY) {
      return readable(nibState) as any;
    }
    return readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any;
  });
}

function mockNibQuery(nibData: ReturnType<typeof makeNibData> | null = null) {
  setupQueryDispatch({ nib: nibData });
}

function mockFetchingQuery() {
  setupQueryDispatch({ fetching: true });
}

/**
 * Helper: open a shadcn Select dropdown and click an option.
 * The trigger is a button; clicking it opens the listbox. Then we find the
 * option by its data-value attribute and click it.
 */
async function selectOption(testId: string, optionValue: string) {
  const trigger = screen.getByTestId(testId);
  await user.click(trigger);
  // shadcn Select items have role="option" with data-value attributes
  const options = screen.getAllByRole("option");
  const option = options.find((o) => o.getAttribute("data-value") === optionValue);
  if (!option) throw new Error(`Option with data-value="${optionValue}" not found`);
  await user.click(option);
}

describe("DetailPanel", () => {
  let mockConfirmDialog: ReturnType<typeof makeMockConfirmDialog>;

  beforeEach(() => {
    mockQueryStore.mockReset();
    mockSubscriptionStore.mockReset().mockReturnValue(
      { subscribe: readable({ data: undefined }).subscribe }
    );
    mockExecute.mockReset().mockResolvedValue({ ok: true, data: {} });
    mockIsMutating.mockReset().mockReturnValue(false);
    mockToastError.mockReset();
    mockConfirmDialog = makeMockConfirmDialog();
  });

  function renderPanel(props: Record<string, unknown>) {
    return render(DetailPanel, {
      props,
      context: makeContext(mockConfirmDialog),
    });
  }

  it("renders nib metadata fields from query data", () => {
    mockNibQuery(makeNibData());

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    // ID visible
    expect(screen.getByText("nibs-abc1")).toBeInTheDocument();

    // Title input with correct value
    const titleInput = screen.getByTestId("detail-title") as HTMLInputElement;
    expect(titleInput.value).toBe("Fix login bug");

    // Status trigger shows current value
    const statusTrigger = screen.getByTestId("detail-status");
    expect(statusTrigger).toHaveTextContent("in-progress");

    // Type trigger shows current value
    const typeTrigger = screen.getByTestId("detail-type");
    expect(typeTrigger).toHaveTextContent("bug");

    // Priority trigger shows current value
    const priorityTrigger = screen.getByTestId("detail-priority");
    expect(priorityTrigger).toHaveTextContent("high");

    // Estimate trigger shows human-readable label
    const estimateTrigger = screen.getByTestId("detail-estimate");
    expect(estimateTrigger).toHaveTextContent("Medium");

    // Tags
    const tags = screen.getAllByTestId("detail-tag");
    expect(tags).toHaveLength(2);
    expect(tags[0]).toHaveTextContent("auth");
    expect(tags[1]).toHaveTextContent("frontend");
  });

  it("shows loading state while fetching", () => {
    mockFetchingQuery();

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    expect(screen.getByTestId("detail-loading")).toBeInTheDocument();
    expect(screen.getByText("Loading...")).toBeInTheDocument();
  });

  it("fires onclose when the close button is clicked", async () => {
    mockNibQuery(makeNibData());

    const handler = vi.fn();
    renderPanel({ nibId: "nibs-abc1", onclose: handler });

    await user.click(screen.getByTestId("detail-close"));
    expect(handler).toHaveBeenCalledOnce();
  });

  it("has ARIA landmark and labels for accessibility", () => {
    mockNibQuery(makeNibData());

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const panel = screen.getByTestId("detail-panel");
    expect(panel).toHaveAttribute("role", "complementary");
    expect(panel).toHaveAttribute("aria-label", "Nib detail");

    const closeBtn = screen.getByTestId("detail-close");
    expect(closeBtn).toHaveAttribute("aria-label", "Close detail panel");
  });

  it("title input saves on blur when changed", async () => {
    mockNibQuery(makeNibData());

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const titleInput = screen.getByTestId("detail-title") as HTMLInputElement;
    await user.clear(titleInput);
    await user.type(titleInput, "Updated title");
    await user.tab(); // triggers blur

    expect(mockExecute).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: "update-nib",
        id: "nibs-abc1",
        input: { title: "Updated title" },
        ifMatch: "abc123",
      })
    );
  });

  it("title input does not save on blur when unchanged", async () => {
    mockNibQuery(makeNibData());

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const titleInput = screen.getByTestId("detail-title") as HTMLInputElement;
    // Click and blur without changing
    await user.click(titleInput);
    await user.tab();

    expect(mockExecute).not.toHaveBeenCalled();
  });

  it("status dropdown change triggers mutation", async () => {
    mockNibQuery(makeNibData());

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    await selectOption("detail-status", "completed");

    expect(mockExecute).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: "update-nib",
        id: "nibs-abc1",
        input: { status: "completed" },
        ifMatch: "abc123",
      })
    );
  });

  it("type dropdown change triggers mutation", async () => {
    mockNibQuery(makeNibData());

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    await selectOption("detail-type", "feature");

    expect(mockExecute).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: "update-nib",
        id: "nibs-abc1",
        input: { type: "feature" },
        ifMatch: "abc123",
      })
    );
  });

  it("priority dropdown change triggers mutation", async () => {
    mockNibQuery(makeNibData());

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    await selectOption("detail-priority", "critical");

    expect(mockExecute).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: "update-nib",
        id: "nibs-abc1",
        input: { priority: "critical" },
        ifMatch: "abc123",
      })
    );
  });

  it("priority dropdown clearing sends null", async () => {
    mockNibQuery(makeNibData());

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    await selectOption("detail-priority", "__none__");

    expect(mockExecute).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: "update-nib",
        id: "nibs-abc1",
        input: { priority: null },
        ifMatch: "abc123",
      })
    );
  });

  it("estimate dropdown change triggers mutation", async () => {
    mockNibQuery(makeNibData());

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    await selectOption("detail-estimate", "xl");

    expect(mockExecute).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: "update-nib",
        id: "nibs-abc1",
        input: { estimate: "xl" },
        ifMatch: "abc123",
      })
    );
  });

  it("tag remove button triggers removeTags mutation", async () => {
    mockNibQuery(makeNibData());

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const removeButtons = screen.getAllByTestId("detail-tag-remove");
    // Remove "auth" tag (first one)
    await user.click(removeButtons[0]);

    expect(mockExecute).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: "update-nib",
        id: "nibs-abc1",
        input: { removeTags: ["auth"] },
        ifMatch: "abc123",
      })
    );
  });

  it("tag input adds valid tag on Enter", async () => {
    mockNibQuery(makeNibData());

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const tagInput = screen.getByTestId("detail-tag-input") as HTMLInputElement;
    await user.type(tagInput, "new-tag{Enter}");

    expect(mockExecute).toHaveBeenCalledWith(
      expect.objectContaining({
        kind: "update-nib",
        id: "nibs-abc1",
        input: { addTags: ["new-tag"] },
        ifMatch: "abc123",
      })
    );
  });

  it("tag input rejects invalid tags", async () => {
    mockNibQuery(makeNibData());

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const tagInput = screen.getByTestId("detail-tag-input") as HTMLInputElement;
    await user.type(tagInput, "INVALID{Enter}");

    // Should not fire mutation
    expect(mockExecute).not.toHaveBeenCalled();
  });

  it("tag input clears after successful add", async () => {
    mockNibQuery(makeNibData());

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const tagInput = screen.getByTestId("detail-tag-input") as HTMLInputElement;
    await user.type(tagInput, "new-tag{Enter}");

    expect(tagInput.value).toBe("");
  });

  it("error result from mutation reverts title on failure", async () => {
    mockExecute.mockResolvedValue({ ok: false, error: "Update failed" });
    mockNibQuery(makeNibData());

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const titleInput = screen.getByTestId("detail-title") as HTMLInputElement;
    await user.clear(titleInput);
    await user.type(titleInput, "Changed");
    await user.tab();

    await waitFor(() => {
      // Title should revert to original on failure
      expect(titleInput.value).toBe("Fix login bug");
    });
  });

  it("estimate dropdown shows human-readable labels", async () => {
    mockNibQuery(makeNibData());

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    // Open the estimate dropdown to see options
    await user.click(screen.getByTestId("detail-estimate"));

    const options = screen.getAllByRole("option");
    const labels = options.map(o => o.textContent?.trim());
    expect(labels).toContain("None");
    expect(labels?.some(t => t?.includes("Small"))).toBe(true);
    expect(labels?.some(t => t?.includes("Medium"))).toBe(true);
    expect(labels?.some(t => t?.includes("Large"))).toBe(true);
    expect(labels?.some(t => t?.includes("Extra Large"))).toBe(true);
  });

  it("shows not-found state when nib is null after fetching", () => {
    mockNibQuery(null);

    renderPanel({ nibId: "nibs-xxx", onclose: vi.fn() });

    expect(screen.getByTestId("detail-not-found")).toBeInTheDocument();
    expect(screen.getByText("Nib not found")).toBeInTheDocument();
    expect(screen.queryByTestId("detail-loading")).not.toBeInTheDocument();
  });

  it("shows error state when query returns an error", () => {
    setupQueryDispatch({ error: { message: "Network error" } });

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    expect(screen.getByTestId("detail-not-found")).toBeInTheDocument();
    expect(screen.getByText("Error loading nib")).toBeInTheDocument();
  });

  it("fires onmissing(nibId) once when the nib is not found (nibs-etk3)", async () => {
    mockNibQuery(null); // settled, no error, nib === null

    const onmissing = vi.fn();
    renderPanel({ nibId: "nibs-gone", onclose: vi.fn(), onmissing });

    await waitFor(() => expect(onmissing).toHaveBeenCalledWith("nibs-gone"));
    expect(onmissing).toHaveBeenCalledTimes(1);
  });

  it("does NOT fire onmissing while the nib is loaded (nibs-etk3)", () => {
    mockNibQuery(makeNibData());

    const onmissing = vi.fn();
    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn(), onmissing });

    expect(onmissing).not.toHaveBeenCalled();
  });

  it("does NOT fire onmissing on a query error — could be transient, don't clear the URL (nibs-etk3)", () => {
    setupQueryDispatch({ error: { message: "Network error" } });

    const onmissing = vi.fn();
    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn(), onmissing });

    expect(onmissing).not.toHaveBeenCalled();
  });

  it("tag input rejects duplicate tags", async () => {
    mockNibQuery(makeNibData({ tags: ["existing-tag"] }));

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const tagInput = screen.getByTestId("detail-tag-input") as HTMLInputElement;
    await user.type(tagInput, "existing-tag{Enter}");

    // Should not fire mutation
    expect(mockExecute).not.toHaveBeenCalled();
    // Should show duplicate error
    expect(screen.getByText("Tag already exists")).toBeInTheDocument();
  });

  it("renders markdown body as HTML", () => {
    mockNibQuery(makeNibData({ body: "## Heading\n\nSome **bold** text." }));

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const bodySection = screen.getByTestId("detail-body-section");
    expect(bodySection).toBeInTheDocument();

    const prose = screen.getByTestId("detail-body-prose");
    expect(prose.innerHTML).toContain("<h2");
    expect(prose.innerHTML).toContain("Heading");
    expect(prose.innerHTML).toContain("<strong>bold</strong>");
  });

  it("does not render body section when body is empty", () => {
    mockNibQuery(makeNibData({ body: "" }));

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    expect(screen.queryByTestId("detail-body-section")).not.toBeInTheDocument();
  });

  it("shows disabled edit button in body section", () => {
    mockNibQuery(makeNibData({ body: "Some content" }));

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const editButton = screen.getByTestId("detail-body-edit");
    expect(editButton).toBeInTheDocument();
    expect(editButton).toBeDisabled();
    expect(editButton).toHaveTextContent("Edit");
  });

  it("renders documents list when documents are present", () => {
    mockNibQuery(makeNibData({ documents: ["src/main.ts", "docs/README.md"] }));

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const docsSection = screen.getByTestId("detail-documents-section");
    expect(docsSection).toBeInTheDocument();

    const docs = screen.getAllByTestId("detail-document");
    expect(docs).toHaveLength(2);
    expect(docs[0]).toHaveTextContent("src/main.ts");
    expect(docs[1]).toHaveTextContent("docs/README.md");
  });

  it("does not render documents section when documents array is empty", () => {
    mockNibQuery(makeNibData({ documents: [] }));

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    expect(screen.queryByTestId("detail-documents-section")).not.toBeInTheDocument();
  });

  it("renders body with code blocks correctly", () => {
    mockNibQuery(makeNibData({ body: "Use `inline code` and:\n\n```\ncode block\n```" }));

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const prose = screen.getByTestId("detail-body-prose");
    expect(prose.innerHTML).toContain("<code>");
    expect(prose.innerHTML).toContain("<pre>");
  });

  it("renders body with lists correctly", () => {
    mockNibQuery(makeNibData({ body: "- item one\n- item two\n- item three" }));

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const prose = screen.getByTestId("detail-body-prose");
    expect(prose.innerHTML).toContain("<ul>");
    expect(prose.innerHTML).toContain("<li>");
    expect(prose.innerHTML).toContain("item one");
  });

  it("strips dangerous HTML from body", () => {
    mockNibQuery(makeNibData({ body: '<script>alert("xss")</script>Hello' }));

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const prose = screen.getByTestId("detail-body-prose");
    expect(prose.innerHTML).not.toContain("<script>");
    expect(prose.innerHTML).toContain("Hello");
  });

  it("renders parent group with clickable link when nib has a parent", async () => {
    mockNibQuery(makeNibData({
      parent: { id: "nibs-parent1", title: "Parent Epic", type: "epic", status: "in-progress" },
    }));

    const nibSelectHandler = vi.fn();
    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn(), onnibselect: nibSelectHandler });

    const parentGroup = screen.getByTestId("detail-related-parent");
    expect(parentGroup).toBeInTheDocument();

    const link = parentGroup.querySelector('[data-testid="detail-related-link"]') as HTMLElement;
    expect(link).toBeInTheDocument();
    expect(link).toHaveTextContent("Parent Epic");

    await user.click(link);
    expect(nibSelectHandler).toHaveBeenCalledWith("nibs-parent1");
  });

  it("renders children group with clickable child items", async () => {
    mockNibQuery(makeNibData({
      children: [
        { id: "nibs-child1", title: "Child Task 1", type: "task", status: "todo" },
        { id: "nibs-child2", title: "Child Task 2", type: "task", status: "completed" },
      ],
    }));

    const nibSelectHandler = vi.fn();
    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn(), onnibselect: nibSelectHandler });

    const childrenGroup = screen.getByTestId("detail-related-children");
    expect(childrenGroup).toBeInTheDocument();

    const links = childrenGroup.querySelectorAll('[data-testid="detail-related-link"]');
    expect(links).toHaveLength(2);
    expect(links[0]).toHaveTextContent("Child Task 1");
    expect(links[1]).toHaveTextContent("Child Task 2");

    await user.click(links[0] as HTMLElement);
    expect(nibSelectHandler).toHaveBeenCalledWith("nibs-child1");

    await user.click(links[1] as HTMLElement);
    expect(nibSelectHandler).toHaveBeenCalledWith("nibs-child2");
  });

  it("renders blocked-by group with clickable items", async () => {
    mockNibQuery(makeNibData({
      blockedBy: [
        { id: "nibs-blocker1", title: "Blocking Issue", type: "bug", status: "in-progress" },
      ],
    }));

    const nibSelectHandler = vi.fn();
    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn(), onnibselect: nibSelectHandler });

    const blockedByGroup = screen.getByTestId("detail-related-blocked-by");
    expect(blockedByGroup).toBeInTheDocument();

    const link = blockedByGroup.querySelector('[data-testid="detail-related-link"]') as HTMLElement;
    expect(link).toHaveTextContent("Blocking Issue");

    await user.click(link);
    expect(nibSelectHandler).toHaveBeenCalledWith("nibs-blocker1");
  });

  it("renders blocking group with clickable items", async () => {
    mockNibQuery(makeNibData({
      blocking: [
        { id: "nibs-blocked1", title: "Blocked Feature", type: "feature", status: "draft" },
      ],
    }));

    const nibSelectHandler = vi.fn();
    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn(), onnibselect: nibSelectHandler });

    const blockingGroup = screen.getByTestId("detail-related-blocking");
    expect(blockingGroup).toBeInTheDocument();

    const link = blockingGroup.querySelector('[data-testid="detail-related-link"]') as HTMLElement;
    expect(link).toHaveTextContent("Blocked Feature");

    await user.click(link);
    expect(nibSelectHandler).toHaveBeenCalledWith("nibs-blocked1");
  });

  it("does not render empty relationship groups", () => {
    mockNibQuery(makeNibData({
      parent: { id: "nibs-parent1", title: "Parent", type: "epic", status: "todo" },
      children: [],
      blocking: [],
      blockedBy: [],
    }));

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    // Parent group should exist
    expect(screen.getByTestId("detail-related-parent")).toBeInTheDocument();

    // Empty groups should NOT be rendered
    expect(screen.queryByTestId("detail-related-children")).not.toBeInTheDocument();
    expect(screen.queryByTestId("detail-related-blocked-by")).not.toBeInTheDocument();
    expect(screen.queryByTestId("detail-related-blocking")).not.toBeInTheDocument();
  });

  it("group headers toggle content visibility when clicked", async () => {
    mockNibQuery(makeNibData({
      children: [
        { id: "nibs-child1", title: "Child Task", type: "task", status: "todo" },
      ],
    }));

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const childrenGroup = screen.getByTestId("detail-related-children");

    // Items should be visible by default (expanded)
    const items = childrenGroup.querySelector('[data-testid="detail-related-link"]');
    expect(items).toBeInTheDocument();

    // Click the group toggle to collapse
    const toggle = childrenGroup.querySelector('[data-testid="detail-group-toggle"]') as HTMLElement;
    expect(toggle).toBeInTheDocument();
    await user.click(toggle);

    // Items should be hidden after collapse
    expect(childrenGroup.querySelector('[data-testid="detail-related-link"]')).not.toBeInTheDocument();

    // Click again to expand
    await user.click(toggle);
    expect(childrenGroup.querySelector('[data-testid="detail-related-link"]')).toBeInTheDocument();
  });

  it("shows add-child button only in children group when onaddchild is provided", () => {
    const addChildHandler = vi.fn();
    mockNibQuery(makeNibData({
      parent: { id: "nibs-parent1", title: "Parent", type: "epic", status: "todo" },
      children: [
        { id: "nibs-child1", title: "Child", type: "task", status: "todo" },
      ],
      blockedBy: [
        { id: "nibs-blocker1", title: "Blocker", type: "bug", status: "in-progress" },
      ],
    }));

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn(), onaddchild: addChildHandler });

    // Add-child button should exist in the children group
    const childrenGroup = screen.getByTestId("detail-related-children");
    const addChildBtn = childrenGroup.querySelector('[data-testid="detail-related-add-child"]') as HTMLElement;
    expect(addChildBtn).toBeInTheDocument();

    // Other groups should NOT have the add-child button
    const parentGroup = screen.getByTestId("detail-related-parent");
    expect(parentGroup.querySelector('[data-testid="detail-related-add-child"]')).not.toBeInTheDocument();

    const blockedByGroup = screen.getByTestId("detail-related-blocked-by");
    expect(blockedByGroup.querySelector('[data-testid="detail-related-add-child"]')).not.toBeInTheDocument();
  });

  it("hides add-child button when onaddchild is not provided", () => {
    mockNibQuery(makeNibData({
      children: [
        { id: "nibs-child1", title: "Child", type: "task", status: "todo" },
      ],
    }));

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    expect(screen.queryByTestId("detail-related-add-child")).not.toBeInTheDocument();
  });

  it("related items show colored status dots", () => {
    mockNibQuery(makeNibData({
      children: [
        { id: "nibs-child1", title: "Todo Child", type: "task", status: "todo" },
        { id: "nibs-child2", title: "Done Child", type: "task", status: "completed" },
      ],
    }));

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const links = screen.getAllByTestId("detail-related-link");
    // Each link should have a status dot with the correct color
    const dot1 = links[0].querySelector('.status-dot') as HTMLElement;
    expect(dot1).toBeInTheDocument();
    expect(dot1.style.backgroundColor).toBe("var(--status-todo-text)");

    const dot2 = links[1].querySelector('.status-dot') as HTMLElement;
    expect(dot2).toBeInTheDocument();
    expect(dot2.style.backgroundColor).toBe("var(--status-completed-text)");
  });

  it("does not render related section when nib has no relationships", () => {
    mockNibQuery(makeNibData({
      parent: null,
      children: [],
      blocking: [],
      blockedBy: [],
      mentions: [],
      mentionedBy: [],
    }));

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    expect(screen.queryByTestId("detail-related-section")).not.toBeInTheDocument();
  });

  it("renders mentions group with clickable items", async () => {
    mockNibQuery(makeNibData({
      mentions: [
        { id: "nibs-mentioned1", title: "Mentioned Nib", type: "feature", status: "todo" },
      ],
    }));

    const nibSelectHandler = vi.fn();
    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn(), onnibselect: nibSelectHandler });

    const group = screen.getByTestId("detail-related-mentions");
    expect(group).toBeInTheDocument();

    const link = group.querySelector('[data-testid="detail-related-link"]') as HTMLElement;
    expect(link).toHaveTextContent("Mentioned Nib");

    await user.click(link);
    expect(nibSelectHandler).toHaveBeenCalledTimes(1);
    expect(nibSelectHandler).toHaveBeenCalledWith("nibs-mentioned1");
  });

  it("renders mentioned-by group with clickable items", async () => {
    mockNibQuery(makeNibData({
      mentionedBy: [
        { id: "nibs-mentor1", title: "Referring Nib", type: "task", status: "in-progress" },
      ],
    }));

    const nibSelectHandler = vi.fn();
    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn(), onnibselect: nibSelectHandler });

    const group = screen.getByTestId("detail-related-mentioned-by");
    expect(group).toBeInTheDocument();

    const link = group.querySelector('[data-testid="detail-related-link"]') as HTMLElement;
    expect(link).toHaveTextContent("Referring Nib");

    await user.click(link);
    expect(nibSelectHandler).toHaveBeenCalledTimes(1);
    expect(nibSelectHandler).toHaveBeenCalledWith("nibs-mentor1");
  });

  it("renders related section when only mentions are present", () => {
    mockNibQuery(makeNibData({
      parent: null,
      children: [],
      blocking: [],
      blockedBy: [],
      mentions: [
        { id: "nibs-mentioned1", title: "Only Mention", type: "feature", status: "todo" },
      ],
      mentionedBy: [],
    }));

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    expect(screen.getByTestId("detail-related-section")).toBeInTheDocument();
    expect(screen.getByTestId("detail-related-mentions")).toBeInTheDocument();
    expect(screen.queryByTestId("detail-related-mentioned-by")).not.toBeInTheDocument();
  });

  it("renders body mentions as clickable links and invokes onnibselect on click", async () => {
    mockNibQuery(makeNibData({
      body: "see #gx0f for details",
      mentions: [
        { id: "nibs-gx0f", title: "Mentioned Nib", type: "feature", status: "todo" },
      ],
    }));

    const nibSelectHandler = vi.fn();
    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn(), onnibselect: nibSelectHandler });

    const prose = screen.getByTestId("detail-body-prose");
    const anchor = prose.querySelector('a[data-nib-id="nibs-gx0f"]') as HTMLAnchorElement;
    expect(anchor).toBeInTheDocument();
    expect(anchor).toHaveTextContent("#gx0f");

    await user.click(anchor);
    expect(nibSelectHandler).toHaveBeenCalledTimes(1);
    expect(nibSelectHandler).toHaveBeenCalledWith("nibs-gx0f");
  });

  it("resolves both short-form and full-form mentions against nib.mentions", async () => {
    mockNibQuery(makeNibData({
      body: "short #gx0f and full #nibs-gx0f",
      mentions: [
        { id: "nibs-gx0f", title: "Mentioned Nib", type: "feature", status: "todo" },
      ],
    }));

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const prose = screen.getByTestId("detail-body-prose");
    const anchors = prose.querySelectorAll('a[data-nib-id="nibs-gx0f"]');
    expect(anchors.length).toBe(2);
  });

  it("resolves short-form mentions using the configured prefix", () => {
    setupQueryDispatch({
      nib: makeNibData({
        body: "see #gx0f",
        mentions: [
          { id: "myproj-gx0f", title: "Target", type: "task", status: "todo" },
        ],
      }),
      prefix: "myproj-",
      projectName: "Test",
    });

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const prose = screen.getByTestId("detail-body-prose");
    const anchor = prose.querySelector('a[data-nib-id="myproj-gx0f"]') as HTMLAnchorElement;
    expect(anchor).toBeInTheDocument();
    expect(anchor).toHaveTextContent("#gx0f");
  });

  it("Enter key on a focused mention link invokes onnibselect", async () => {
    mockNibQuery(makeNibData({
      body: "see #gx0f",
      mentions: [
        { id: "nibs-gx0f", title: "Mentioned Nib", type: "feature", status: "todo" },
      ],
    }));

    const nibSelectHandler = vi.fn();
    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn(), onnibselect: nibSelectHandler });

    const prose = screen.getByTestId("detail-body-prose");
    const anchor = prose.querySelector('a[data-nib-id="nibs-gx0f"]') as HTMLAnchorElement;
    anchor.focus();
    await user.keyboard("{Enter}");
    expect(nibSelectHandler).toHaveBeenCalledTimes(1);
    expect(nibSelectHandler).toHaveBeenCalledWith("nibs-gx0f");
  });

  it("does not intercept clicks on non-mention anchors", async () => {
    mockNibQuery(makeNibData({
      body: "[external](https://example.com) link",
      mentions: [],
    }));

    const nibSelectHandler = vi.fn();
    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn(), onnibselect: nibSelectHandler });

    const prose = screen.getByTestId("detail-body-prose");
    const link = prose.querySelector('a[href="https://example.com"]') as HTMLAnchorElement;
    expect(link).toBeInTheDocument();
    expect(link.hasAttribute("data-nib-id")).toBe(false);

    // Prevent jsdom navigation warning by adding default-preventing handler on document
    const noop = (e: Event) => e.preventDefault();
    document.addEventListener("click", noop, true);
    try {
      await user.click(link);
    } finally {
      document.removeEventListener("click", noop, true);
    }
    // Handler should NOT be called for non-mention anchors
    expect(nibSelectHandler).toHaveBeenCalledTimes(0);
  });

  it("hides mentions and mentioned-by groups when both are empty", () => {
    mockNibQuery(makeNibData({
      parent: { id: "nibs-parent1", title: "Parent", type: "epic", status: "todo" },
      mentions: [],
      mentionedBy: [],
    }));

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    expect(screen.queryByTestId("detail-related-mentions")).not.toBeInTheDocument();
    expect(screen.queryByTestId("detail-related-mentioned-by")).not.toBeInTheDocument();
  });

  it("resets collapsed groups when nibId changes", async () => {
    mockNibQuery(makeNibData({
      children: [
        { id: "nibs-child1", title: "Child Task", type: "task", status: "todo" },
      ],
    }));

    const { rerender } = renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    const childrenGroup = screen.getByTestId("detail-related-children");

    // Items should be visible by default (expanded)
    expect(childrenGroup.querySelector('[data-testid="detail-related-link"]')).toBeInTheDocument();

    // Collapse the children group
    const toggle = childrenGroup.querySelector('[data-testid="detail-group-toggle"]') as HTMLElement;
    await user.click(toggle);

    // Items should be hidden after collapse
    expect(childrenGroup.querySelector('[data-testid="detail-related-link"]')).not.toBeInTheDocument();

    // Re-render with a different nibId (simulating nib navigation)
    mockNibQuery(makeNibData({
      id: "nibs-xyz9",
      title: "Different Nib",
      etag: "xyz789",
      children: [
        { id: "nibs-child2", title: "Another Child", type: "task", status: "todo" },
      ],
    }));

    await rerender({ nibId: "nibs-xyz9", onclose: vi.fn() });

    // After nib change, children group should be expanded (collapsed state reset)
    await waitFor(() => {
      const newChildrenGroup = screen.getByTestId("detail-related-children");
      expect(newChildrenGroup.querySelector('[data-testid="detail-related-link"]')).toBeInTheDocument();
    });
  });

  it("renders delete and archive action buttons", () => {
    mockNibQuery(makeNibData());

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    expect(screen.getByTestId("detail-delete-button")).toBeInTheDocument();
    expect(screen.getByTestId("detail-delete-button")).toHaveTextContent("Delete");
    expect(screen.getByTestId("detail-archive-button")).toBeInTheDocument();
    expect(screen.getByTestId("detail-archive-button")).toHaveTextContent("Archive");
  });

  it("clicking delete button calls showConfirm with danger variant", async () => {
    mockNibQuery(makeNibData());

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    // showConfirm should not have been called initially
    expect(mockConfirmDialog.showConfirm).not.toHaveBeenCalled();

    // Click delete button
    await user.click(screen.getByTestId("detail-delete-button"));

    // showConfirm should be called with correct args
    expect(mockConfirmDialog.showConfirm).toHaveBeenCalledWith(
      expect.objectContaining({
        title: "Delete nib?",
        label: "Delete",
        variant: "danger",
      })
    );
    expect(mockConfirmDialog.lastOpts?.message).toContain("nibs-abc1");
  });

  it("delete confirm action calls deleteNib command and fires onclose", async () => {
    mockExecute.mockResolvedValue({ ok: true, data: { deleteNib: true } });
    mockNibQuery(makeNibData());

    const onclose = vi.fn();
    renderPanel({ nibId: "nibs-abc1", onclose });

    // Click delete button to trigger showConfirm
    await user.click(screen.getByTestId("detail-delete-button"));

    // Invoke the action callback that was passed to showConfirm
    const action = mockConfirmDialog.lastOpts!.action;
    await action();

    await waitFor(() => {
      expect(mockExecute).toHaveBeenCalledWith(
        expect.objectContaining({
          kind: "delete-nib",
          id: "nibs-abc1",
        })
      );
      expect(mockConfirmDialog.close).toHaveBeenCalled();
      expect(onclose).toHaveBeenCalledOnce();
    });
  });

  it("clicking archive button calls showConfirm with warning variant", async () => {
    mockNibQuery(makeNibData());

    renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

    // showConfirm should not have been called initially
    expect(mockConfirmDialog.showConfirm).not.toHaveBeenCalled();

    // Click archive button
    await user.click(screen.getByTestId("detail-archive-button"));

    // showConfirm should be called with correct args
    expect(mockConfirmDialog.showConfirm).toHaveBeenCalledWith(
      expect.objectContaining({
        title: "Archive nib?",
        label: "Archive",
        variant: "warning",
      })
    );
    expect(mockConfirmDialog.lastOpts?.message).toContain("nibs-abc1");
  });

  it("archive confirm action calls archiveNib command and fires onclose", async () => {
    mockExecute.mockResolvedValue({ ok: true, data: { archiveNib: true } });
    mockNibQuery(makeNibData());

    const onclose = vi.fn();
    renderPanel({ nibId: "nibs-abc1", onclose });

    // Click archive button to trigger showConfirm
    await user.click(screen.getByTestId("detail-archive-button"));

    // Invoke the action callback that was passed to showConfirm
    const action = mockConfirmDialog.lastOpts!.action;
    await action();

    await waitFor(() => {
      expect(mockExecute).toHaveBeenCalledWith(
        expect.objectContaining({
          kind: "archive-nib",
          id: "nibs-abc1",
        })
      );
      expect(mockConfirmDialog.close).toHaveBeenCalled();
      expect(onclose).toHaveBeenCalledOnce();
    });
  });

  it("delete action does not fire onclose on mutation failure", async () => {
    mockExecute.mockResolvedValue({ ok: false, error: "Delete failed" });
    mockNibQuery(makeNibData());

    const onclose = vi.fn();
    renderPanel({ nibId: "nibs-abc1", onclose });

    // Click delete button and invoke action
    await user.click(screen.getByTestId("detail-delete-button"));
    await mockConfirmDialog.lastOpts!.action();

    await waitFor(() => {
      // onclose should NOT be called on failure
      expect(onclose).not.toHaveBeenCalled();
    });
  });

  it("archive action does not fire onclose on mutation failure", async () => {
    mockExecute.mockResolvedValue({ ok: false, error: "Archive failed" });
    mockNibQuery(makeNibData());

    const onclose = vi.fn();
    renderPanel({ nibId: "nibs-abc1", onclose });

    // Click archive button and invoke action
    await user.click(screen.getByTestId("detail-archive-button"));
    await mockConfirmDialog.lastOpts!.action();

    await waitFor(() => {
      // onclose should NOT be called on failure
      expect(onclose).not.toHaveBeenCalled();
    });
  });

  describe("real-time updates", () => {
    it("shows deleted banner when subscription emits a delete event", async () => {
      mockNibQuery(makeNibData());
      mockSubscriptionStore.mockReturnValue(
        { subscribe: readable({
          data: { nibChanged: { type: "deleted", nibId: "nibs-abc1", nib: null } },
        }).subscribe }
      );

      renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

      await waitFor(() => {
        expect(screen.getByTestId("detail-deleted-notice")).toBeInTheDocument();
        expect(screen.getByTestId("detail-deleted-notice")).toHaveTextContent("This nib was deleted");
      });
    });

    it("update event triggers highlight that auto-clears", async () => {
      vi.useFakeTimers();
      mockNibQuery(makeNibData());
      mockSubscriptionStore.mockReturnValue(
        { subscribe: readable({
          data: { nibChanged: { type: "updated", nibId: "nibs-abc1", nib: makeNibData() } },
        }).subscribe }
      );

      renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

      await waitFor(() => {
        const body = screen.getByTestId("detail-body-container");
        expect(body.classList.contains("nib-detail-highlighted")).toBe(true);
      });

      // Advance time by 1s to clear highlight
      vi.advanceTimersByTime(1000);

      await waitFor(() => {
        const body = screen.getByTestId("detail-body-container");
        expect(body.classList.contains("nib-detail-highlighted")).toBe(false);
      });

      vi.useRealTimers();
    });

    it("subscribes with the current nibId", () => {
      mockNibQuery(makeNibData());

      renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

      expect(mockSubscriptionStore).toHaveBeenCalledWith(
        expect.objectContaining({
          variables: { id: "nibs-abc1" },
        })
      );
    });

    it("disables editing controls after delete event", async () => {
      mockNibQuery(makeNibData());
      mockSubscriptionStore.mockReturnValue(
        { subscribe: readable({
          data: { nibChanged: { type: "deleted", nibId: "nibs-abc1", nib: null } },
        }).subscribe }
      );

      renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

      await waitFor(() => {
        // Title input should be disabled
        expect(screen.getByTestId("detail-title")).toBeDisabled();

        // Status, type, priority, estimate selects should be disabled
        expect(screen.getByTestId("detail-status")).toBeDisabled();
        expect(screen.getByTestId("detail-type")).toBeDisabled();
        expect(screen.getByTestId("detail-priority")).toBeDisabled();
        expect(screen.getByTestId("detail-estimate")).toBeDisabled();

        // Delete and archive buttons should be disabled
        expect(screen.getByTestId("detail-delete-button")).toBeDisabled();
        expect(screen.getByTestId("detail-archive-button")).toBeDisabled();
      });
    });

    it("disables tag editor after delete event", async () => {
      mockNibQuery(makeNibData());
      mockSubscriptionStore.mockReturnValue(
        { subscribe: readable({
          data: { nibChanged: { type: "deleted", nibId: "nibs-abc1", nib: null } },
        }).subscribe }
      );

      renderPanel({ nibId: "nibs-abc1", onclose: vi.fn() });

      await waitFor(() => {
        // Tag input should be disabled
        expect(screen.getByTestId("detail-tag-input")).toBeDisabled();

        // Tag remove buttons should not be present when disabled
        expect(screen.queryAllByTestId("detail-tag-remove")).toHaveLength(0);
      });
    });

    it("disables edit button after delete event", async () => {
      mockNibQuery(makeNibData({ body: "Some content" }));
      mockSubscriptionStore.mockReturnValue(
        { subscribe: readable({
          data: { nibChanged: { type: "deleted", nibId: "nibs-abc1", nib: null } },
        }).subscribe }
      );

      renderPanel({ nibId: "nibs-abc1", onclose: vi.fn(), onedit: vi.fn() });

      await waitFor(() => {
        expect(screen.getByTestId("detail-body-edit")).toBeDisabled();
      });
    });
  });
});
