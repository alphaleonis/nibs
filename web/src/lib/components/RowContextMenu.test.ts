import { render, screen, waitFor } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach } from "vitest";
import RowContextMenu from "./RowContextMenu.svelte";
import { SelectionState } from "../selection.svelte";
import { DragState } from "../drag.svelte";
import { makeTestContext } from "$lib/contexts";
import { openSubmenu } from "$lib/testing/menu";
import type {
  ConfirmDialogState,
  ConfirmDialogOptions,
} from "$lib/composables/useConfirmDialog.svelte";
import type { ActiveView } from "$lib/composables/useActiveView.svelte";
import type { HistoryNav } from "$lib/composables/useHistoryNav.svelte";
import type { TreeTableNib } from "../types";
import type { RelIdKey } from "$lib/query";

// bits-ui scroll lock sets pointer-events: none on <body>, so disable the check
const user = userEvent.setup({ pointerEventsCheck: 0 });

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
    get pending() {
      return false;
    },
  }),
}));

function makeMockConfirmDialog(): ConfirmDialogState & {
  lastOpts: ConfirmDialogOptions | null;
} {
  const state = {
    open: false,
    title: "",
    message: "",
    label: "",
    variant: "danger" as "danger" | "warning",
    action: null as (() => void) | null,
    saveLabel: null as string | null,
    saveAction: null as (() => void) | null,
    lastOpts: null as ConfirmDialogOptions | null,
    showConfirm: vi.fn((opts: ConfirmDialogOptions) => {
      state.open = true;
      state.title = opts.title;
      state.message = opts.message;
      state.label = opts.label;
      state.variant = opts.variant;
      state.action = opts.action;
      state.saveLabel = opts.saveLabel ?? null;
      state.saveAction = opts.saveAction ?? null;
      state.lastOpts = opts;
    }),
    close: vi.fn(() => {
      state.open = false;
      state.action = null;
      state.saveLabel = null;
      state.saveAction = null;
    }),
    // These tests drive showConfirm/close only; dismiss() is never exercised here.
    dismiss: vi.fn(),
  };
  return state;
}

/** A spyable ActiveView stub whose `open` mirrors selection (so "Open" tests can
 *  assert selectedNibId) while `startCreateChild` is observable for "Add child". */
function makeMockActiveView(selection: SelectionState) {
  return {
    state: { kind: "closed" as const },
    form: null,
    detail: null,
    isOpen: false,
    presentation: "docked" as const,
    blocksHistoryNav: false,
    open: vi.fn(async (id: string) => { selection.select(id); }),
    expand: vi.fn(),
    collapse: vi.fn(),
    startCreate: vi.fn(async () => {}),
    startCreateChild: vi.fn(async () => {}),
    chooseType: vi.fn(),
    cancelType: vi.fn(),
    save: vi.fn(async () => undefined),
    requestClose: vi.fn(async () => { selection.close(); }),
    syncTo: vi.fn(),
    noteMissing: vi.fn(() => "closed"),
    dispose: vi.fn(),
  };
}

/** A HistoryNav stub whose `replaceClosed` is observable — the URL-healing call
 *  that must fire only when the mutation took out the nib the panel is showing. */
function makeHistoryNav(replaceClosed: () => void): HistoryNav {
  return {
    navigateToNib: vi.fn(),
    closePanel: vi.fn(),
    replaceClosed,
    handlePopState: vi.fn(),
    syncFromUrl: vi.fn(),
  } satisfies HistoryNav;
}

function makeNib(overrides: Partial<TreeTableNib> = {}): TreeTableNib {
  return {
    id: "nibs-abc1",
    title: "Test nib",
    status: "todo",
    type: "task",
    priority: "normal",
    estimate: "m",
    tags: [],
    createdAt: "2026-03-15T10:00:00Z",
    updatedAt: "2026-03-20T10:00:00Z",
    parentId: null,
    blockingIds: [],
    blockedByIds: [],
    ...overrides,
  };
}

describe("RowContextMenu", () => {
  let mockConfirmDialog: ReturnType<typeof makeMockConfirmDialog>;
  let mockView: ReturnType<typeof makeMockActiveView>;
  let selection: SelectionState;

  beforeEach(() => {
    mockExecute.mockReset().mockResolvedValue({ ok: true, data: {} });
    mockIsMutating.mockReset().mockReturnValue(false);
    mockConfirmDialog = makeMockConfirmDialog();
    selection = new SelectionState();
    mockView = makeMockActiveView(selection);
  });

  function renderMenu(
    props: {
      nib?: TreeTableNib;
      selectedCount?: number;
      hasChildren?: boolean;
      onexpandchildren?: () => void;
      oncollapsechildren?: () => void;
      onfilterrelated?: (field: RelIdKey, id: string) => void;
      historyNav?: HistoryNav;
    } = {},
  ) {
    const nib = props.nib ?? makeNib();
    return render(RowContextMenu, {
      props: {
        open: true,
        position: { x: 100, y: 100 },
        nib,
        selectedCount: props.selectedCount ?? 1,
        hasChildren: props.hasChildren ?? false,
        onexpandchildren: props.onexpandchildren,
        oncollapsechildren: props.oncollapsechildren,
        onfilterrelated: props.onfilterrelated,
      },
      context: makeTestContext(selection, new DragState(), {
        confirmDialog: mockConfirmDialog,
        activeView: mockView as unknown as ActiveView,
        historyNav: props.historyNav,
      }),
    });
  }

  // ─── Single mode rendering ────────────────────────────────────

  describe("single mode rendering", () => {
    it("shows Open, Edit, Copy ID items in single mode", async () => {
      renderMenu();

      await waitFor(() => {
        expect(screen.getByTestId("ctx-open")).toBeInTheDocument();
        expect(screen.getByTestId("ctx-edit")).toBeInTheDocument();
        expect(screen.getByTestId("ctx-copy-id")).toBeInTheDocument();
      });
    });

    it("shows Delete and Archive with single labels", async () => {
      renderMenu();

      await waitFor(() => {
        expect(screen.getByTestId("ctx-delete")).toHaveTextContent("Delete");
        expect(screen.getByTestId("ctx-archive")).toHaveTextContent("Archive");
      });
    });
  });

  // ─── Bulk mode rendering ──────────────────────────────────────

  describe("bulk mode rendering", () => {
    it("hides Open, Edit, Copy ID items in bulk mode", async () => {
      renderMenu({ selectedCount: 3 });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-delete")).toBeInTheDocument();
      });

      expect(screen.queryByTestId("ctx-open")).not.toBeInTheDocument();
      expect(screen.queryByTestId("ctx-edit")).not.toBeInTheDocument();
      expect(screen.queryByTestId("ctx-copy-id")).not.toBeInTheDocument();
    });

    it("shows count in Delete and Archive labels in bulk mode", async () => {
      renderMenu({ selectedCount: 3 });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-delete")).toHaveTextContent(
          "Delete 3 items",
        );
        expect(screen.getByTestId("ctx-archive")).toHaveTextContent(
          "Archive 3 items",
        );
      });
    });
  });

  // ─── showAddChild type check ──────────────────────────────────

  describe("showAddChild visibility", () => {
    it("shows Add child for epic type in single mode", async () => {
      renderMenu({ nib: makeNib({ type: "epic" }) });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-add-child")).toBeInTheDocument();
      });
    });

    it("shows Add child for feature type in single mode", async () => {
      renderMenu({ nib: makeNib({ type: "feature" }) });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-add-child")).toBeInTheDocument();
      });
    });

    it("shows Add child for milestone type in single mode", async () => {
      renderMenu({ nib: makeNib({ type: "milestone" }) });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-add-child")).toBeInTheDocument();
      });
    });

    it("hides Add child for task type (leaf)", async () => {
      renderMenu({ nib: makeNib({ type: "task" }) });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-delete")).toBeInTheDocument();
      });

      expect(screen.queryByTestId("ctx-add-child")).not.toBeInTheDocument();
    });

    it("shows Add child for bug type (a bug can parent task/research)", async () => {
      renderMenu({ nib: makeNib({ type: "bug" }) });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-add-child")).toBeInTheDocument();
      });
    });

    it("hides Add child in bulk mode even for epic type", async () => {
      renderMenu({ nib: makeNib({ type: "epic" }), selectedCount: 2 });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-delete")).toBeInTheDocument();
      });

      expect(screen.queryByTestId("ctx-add-child")).not.toBeInTheDocument();
    });
  });

  // ─── expand/collapse children ─────────────────────

  describe("expand/collapse children", () => {
    it("shows both options when the row has children in single mode", async () => {
      renderMenu({ nib: makeNib({ type: "epic" }), hasChildren: true });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-expand-children")).toBeInTheDocument();
        expect(screen.getByTestId("ctx-collapse-children")).toBeInTheDocument();
      });
    });

    it("hides both options when the row has no children", async () => {
      renderMenu({ nib: makeNib({ type: "epic" }), hasChildren: false });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-delete")).toBeInTheDocument();
      });

      expect(screen.queryByTestId("ctx-expand-children")).not.toBeInTheDocument();
      expect(screen.queryByTestId("ctx-collapse-children")).not.toBeInTheDocument();
    });

    it("hides both options in bulk mode even when the row has children", async () => {
      renderMenu({ nib: makeNib({ type: "epic" }), hasChildren: true, selectedCount: 2 });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-delete")).toBeInTheDocument();
      });

      expect(screen.queryByTestId("ctx-expand-children")).not.toBeInTheDocument();
      expect(screen.queryByTestId("ctx-collapse-children")).not.toBeInTheDocument();
    });

    it("invokes onexpandchildren when Expand children is clicked", async () => {
      const onexpandchildren = vi.fn();
      renderMenu({ nib: makeNib({ type: "epic" }), hasChildren: true, onexpandchildren });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-expand-children")).toBeInTheDocument();
      });

      await user.click(screen.getByTestId("ctx-expand-children"));

      expect(onexpandchildren).toHaveBeenCalledOnce();
    });

    it("invokes oncollapsechildren when Collapse children is clicked", async () => {
      const oncollapsechildren = vi.fn();
      renderMenu({ nib: makeNib({ type: "epic" }), hasChildren: true, oncollapsechildren });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-collapse-children")).toBeInTheDocument();
      });

      await user.click(screen.getByTestId("ctx-collapse-children"));

      expect(oncollapsechildren).toHaveBeenCalledOnce();
    });
  });

  // ─── getActionTargetIds priority chain ────────────────────────

  describe("getActionTargetIds priority chain", () => {
    it("targets selectedIds when multi-select is active", async () => {
      selection.select("nibs-abc1");
      selection.toggleSelect("nibs-def2");
      // Now hasMultiSelect = true, selectedIds = {nibs-abc1, nibs-def2}

      renderMenu({ nib: makeNib(), selectedCount: 2 });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-delete")).toBeInTheDocument();
      });

      // Clicking delete triggers getActionTargetIds -> uses selectedIds
      await user.click(screen.getByTestId("ctx-delete"));

      expect(mockConfirmDialog.showConfirm).toHaveBeenCalledOnce();
      // The confirm title should say "Delete 2 items" since ids.length = 2
      expect(mockConfirmDialog.lastOpts?.title).toBe("Delete 2 items");
    });

    it("targets focusedNibId when no multi-select", async () => {
      selection.focus("nibs-focused1");
      // hasMultiSelect = false, focusedNibId = "nibs-focused1"

      renderMenu({ nib: makeNib({ id: "nibs-abc1" }) });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-delete")).toBeInTheDocument();
      });

      await user.click(screen.getByTestId("ctx-delete"));

      expect(mockConfirmDialog.showConfirm).toHaveBeenCalledOnce();
      // With one focused id, title should be single delete
      expect(mockConfirmDialog.lastOpts?.title).toBe("Delete nib");
    });

    it("falls back to nib.id when neither multi-select nor focusedNibId", async () => {
      // Neither focusedNibId nor hasMultiSelect set

      renderMenu({ nib: makeNib({ id: "nibs-fallback" }) });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-delete")).toBeInTheDocument();
      });

      await user.click(screen.getByTestId("ctx-delete"));

      expect(mockConfirmDialog.showConfirm).toHaveBeenCalledOnce();
      expect(mockConfirmDialog.lastOpts?.title).toBe("Delete nib");
    });

    it("does NOT resolve a synthetic bucket id as the delete target (nibs-oxaq)", async () => {
      // Right-clicking a "No X" grouping-bucket row sets nib to the bucket. Its id
      // is unresolvable for any bulk action, so getActionTargetIds must exclude it —
      // delete then early-returns and never opens the confirm (which would otherwise
      // dispatch a phantom deleteBatch(["__no_milestone__"])).
      renderMenu({ nib: makeNib({ id: "__no_milestone__" }) });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-delete")).toBeInTheDocument();
      });

      await user.click(screen.getByTestId("ctx-delete"));

      expect(mockConfirmDialog.showConfirm).not.toHaveBeenCalled();
    });
  });

  // ─── Confirm dialog for delete/archive ────────────────────────

  describe("confirm dialog for delete", () => {
    it("triggers showConfirm with danger variant on single delete", async () => {
      renderMenu();

      await waitFor(() => {
        expect(screen.getByTestId("ctx-delete")).toBeInTheDocument();
      });

      await user.click(screen.getByTestId("ctx-delete"));

      expect(mockConfirmDialog.showConfirm).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "Delete nib",
          variant: "danger",
          label: "Delete",
        }),
      );
      expect(mockConfirmDialog.lastOpts?.message).toContain("cannot be undone");
    });

    it("triggers showConfirm with correct bulk delete message", async () => {
      selection.select("nibs-abc1");
      selection.toggleSelect("nibs-def2");
      selection.toggleSelect("nibs-ghi3");

      renderMenu({ selectedCount: 3 });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-delete")).toBeInTheDocument();
      });

      await user.click(screen.getByTestId("ctx-delete"));

      expect(mockConfirmDialog.showConfirm).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "Delete 3 items",
          variant: "danger",
          label: "Delete 3 items",
        }),
      );
      expect(mockConfirmDialog.lastOpts?.message).toContain("3 items");
    });
  });

  describe("confirm dialog for archive", () => {
    it("triggers showConfirm with warning variant on single archive", async () => {
      renderMenu();

      await waitFor(() => {
        expect(screen.getByTestId("ctx-archive")).toBeInTheDocument();
      });

      await user.click(screen.getByTestId("ctx-archive"));

      expect(mockConfirmDialog.showConfirm).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "Archive nib",
          variant: "warning",
          label: "Archive",
        }),
      );
    });

    it("triggers showConfirm with correct bulk archive message", async () => {
      selection.select("nibs-abc1");
      selection.toggleSelect("nibs-def2");

      renderMenu({ selectedCount: 2 });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-archive")).toBeInTheDocument();
      });

      await user.click(screen.getByTestId("ctx-archive"));

      expect(mockConfirmDialog.showConfirm).toHaveBeenCalledWith(
        expect.objectContaining({
          title: "Archive 2 items",
          variant: "warning",
          label: "Archive 2 items",
        }),
      );
      expect(mockConfirmDialog.lastOpts?.message).toContain("2 items");
    });
  });

  // ─── What a completed mutation clears ─────────────────────────
  //
  // `selectedNibId` (the detail panel) and the action target can point at
  // different rows once "open on double-click" is on: `selectOnly` moves the
  // selection and focus while the panel stays where it was. A mutation must
  // therefore clear only what it actually invalidated — the selection set and
  // focus always, the panel and the `?nib=` URL only when the mutated ids
  // include the nib the panel is showing.
  describe("post-mutation cleanup", () => {
    /** Run the confirm dialog's action, i.e. what happens after the user confirms. */
    async function confirmAction() {
      await mockConfirmDialog.lastOpts!.action!();
    }

    it("deleting the focused row leaves a panel showing a different nib open", async () => {
      selection.select("nibs-open");      // panel on nibs-open
      selection.selectOnly("nibs-abc1");  // selection/focus move; panel does not
      const replaceClosed = vi.fn();
      renderMenu({ historyNav: makeHistoryNav(replaceClosed) });

      await waitFor(() => expect(screen.getByTestId("ctx-delete")).toBeInTheDocument());
      await user.click(screen.getByTestId("ctx-delete"));
      await confirmAction();

      expect(mockExecute).toHaveBeenCalled();
      expect(selection.selectedNibId).toBe("nibs-open");
      expect(selection.panelOpen).toBe(true);
      expect(selection.selectedIds.size).toBe(0);
      expect(selection.focusedNibId).toBeNull();
      expect(replaceClosed).not.toHaveBeenCalled();
    });

    it("deleting the nib the panel is showing closes it and heals the URL", async () => {
      selection.select("nibs-abc1"); // panel and target are the same row
      const replaceClosed = vi.fn();
      renderMenu({ historyNav: makeHistoryNav(replaceClosed) });

      await waitFor(() => expect(screen.getByTestId("ctx-delete")).toBeInTheDocument());
      await user.click(screen.getByTestId("ctx-delete"));
      await confirmAction();

      expect(selection.selectedNibId).toBeNull();
      expect(selection.panelOpen).toBe(false);
      expect(replaceClosed).toHaveBeenCalled();
    });

    it("archiving the focused row leaves a panel showing a different nib open", async () => {
      selection.select("nibs-open");
      selection.selectOnly("nibs-abc1");
      const replaceClosed = vi.fn();
      renderMenu({ historyNav: makeHistoryNav(replaceClosed) });

      await waitFor(() => expect(screen.getByTestId("ctx-archive")).toBeInTheDocument());
      await user.click(screen.getByTestId("ctx-archive"));
      await confirmAction();

      expect(mockExecute).toHaveBeenCalled();
      expect(selection.selectedNibId).toBe("nibs-open");
      expect(selection.selectedIds.size).toBe(0);
      expect(replaceClosed).not.toHaveBeenCalled();
    });

    it("archiving the nib the panel is showing closes it and heals the URL", async () => {
      selection.select("nibs-abc1");
      const replaceClosed = vi.fn();
      renderMenu({ historyNav: makeHistoryNav(replaceClosed) });

      await waitFor(() => expect(screen.getByTestId("ctx-archive")).toBeInTheDocument());
      await user.click(screen.getByTestId("ctx-archive"));
      await confirmAction();

      expect(selection.selectedNibId).toBeNull();
      expect(replaceClosed).toHaveBeenCalled();
    });

    it("a bulk delete that includes the open nib still closes the panel", async () => {
      selection.select("nibs-open");
      selection.toggleSelect("nibs-abc1"); // set is now { nibs-open, nibs-abc1 }
      const replaceClosed = vi.fn();
      renderMenu({ selectedCount: 2, historyNav: makeHistoryNav(replaceClosed) });

      await waitFor(() => expect(screen.getByTestId("ctx-delete")).toBeInTheDocument());
      await user.click(screen.getByTestId("ctx-delete"));
      // toggleSelect nulled selectedNibId on the collapse to two; put the panel
      // back on one of the doomed rows to model the mid-mutation state.
      selection.selectedNibId = "nibs-open";
      await confirmAction();

      expect(selection.selectedNibId).toBeNull();
      expect(replaceClosed).toHaveBeenCalled();
    });

    it("a failed mutation clears nothing", async () => {
      mockExecute.mockResolvedValue({ ok: false, error: new Error("nope") });
      selection.select("nibs-abc1");
      const replaceClosed = vi.fn();
      renderMenu({ historyNav: makeHistoryNav(replaceClosed) });

      await waitFor(() => expect(screen.getByTestId("ctx-delete")).toBeInTheDocument());
      await user.click(screen.getByTestId("ctx-delete"));
      await confirmAction();

      expect(selection.selectedNibId).toBe("nibs-abc1");
      expect(selection.selectedIds.has("nibs-abc1")).toBe(true);
      expect(replaceClosed).not.toHaveBeenCalled();
    });
  });

  // ─── Status submenu interaction ───────────────────────────────

  describe("status submenu", () => {
    it("clicking a status item calls mutations.execute with setStatusBatch", async () => {
      renderMenu();

      await waitFor(() => {
        expect(screen.getByTestId("ctx-status-trigger")).toBeInTheDocument();
      });

      await openSubmenu(user, screen.getByTestId("ctx-status-trigger"));

      // Wait for submenu items to appear and click one
      const completedItem = await screen.findByTestId("ctx-status-completed");
      await user.click(completedItem);

      await waitFor(() => {
        expect(mockExecute).toHaveBeenCalledWith(
          expect.objectContaining({
            kind: "batch",
            commands: expect.arrayContaining([
              expect.objectContaining({
                kind: "update-nib",
                input: { status: "completed" },
              }),
            ]),
          }),
        );
      });
    });

    it("lists statuses in transition order", async () => {
      // Same order as the status select and the TUI picker: the path work
      // takes, not the STATUSES order that sorting and the facets use.
      renderMenu();

      await waitFor(() => {
        expect(screen.getByTestId("ctx-status-trigger")).toBeInTheDocument();
      });

      await openSubmenu(user, screen.getByTestId("ctx-status-trigger"));
      await screen.findByTestId("ctx-status-draft");

      const labels = Array.from(
        document.querySelectorAll<HTMLElement>('[data-testid^="ctx-status-"]'),
      )
        .filter((el) => el.dataset.testid !== "ctx-status-trigger")
        .map((item) => item.textContent?.trim());
      expect(labels).toEqual([
        "draft",
        "todo",
        "in-progress",
        "completed",
        "deferred",
        "scrapped",
      ]);
    });
  });

  // ─── Priority submenu interaction ─────────────────────────────

  describe("priority submenu", () => {
    it("clicking a priority item calls mutations.execute with setPriorityBatch", async () => {
      renderMenu();

      await waitFor(() => {
        expect(screen.getByTestId("ctx-priority-trigger")).toBeInTheDocument();
      });

      await openSubmenu(user, screen.getByTestId("ctx-priority-trigger"));

      // Wait for submenu items to appear and click one
      const criticalItem = await screen.findByTestId("ctx-priority-critical");
      await user.click(criticalItem);

      await waitFor(() => {
        expect(mockExecute).toHaveBeenCalledWith(
          expect.objectContaining({
            kind: "batch",
            commands: expect.arrayContaining([
              expect.objectContaining({
                kind: "update-nib",
                input: { priority: "critical" },
              }),
            ]),
          }),
        );
      });
    });
  });

  // ─── Open and Edit actions ────────────────────────────────────

  describe("Open action", () => {
    it("clicking Open selects the nib", async () => {
      renderMenu();

      await waitFor(() => {
        expect(screen.getByTestId("ctx-open")).toBeInTheDocument();
      });

      await user.click(screen.getByTestId("ctx-open"));

      expect(selection.selectedNibId).toBe("nibs-abc1");
    });
  });

  describe("Edit action", () => {
    it("clicking Edit opens the unified view via view.open", async () => {
      renderMenu();

      await waitFor(() => {
        expect(screen.getByTestId("ctx-edit")).toBeInTheDocument();
      });

      await user.click(screen.getByTestId("ctx-edit"));

      expect(mockView.open).toHaveBeenCalledWith("nibs-abc1");
    });
  });

  describe("Add child action", () => {
    it("clicking Add child calls view.startCreateChild with nib id, type, and an anchor rect", async () => {
      renderMenu({ nib: makeNib({ id: "nibs-epic1", type: "epic" }) });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-add-child")).toBeInTheDocument();
      });

      await user.click(screen.getByTestId("ctx-add-child"));

      // The third arg is the clicked item's rect (all-zero DOMRect under jsdom).
      expect(mockView.startCreateChild).toHaveBeenCalledWith(
        "nibs-epic1",
        "epic",
        expect.objectContaining({ x: expect.any(Number), y: expect.any(Number) }),
      );
    });
  });

  // ─── Copy ID action ─────────────────────────────────────────────

  describe("Copy ID action", () => {
    it("clicking Copy ID writes nib ID to clipboard", async () => {
      const writeText = vi.fn().mockResolvedValue(undefined);
      Object.defineProperty(navigator, "clipboard", {
        value: { writeText },
        writable: true,
        configurable: true,
      });

      renderMenu({ nib: makeNib({ id: "nibs-xyz" }) });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-copy-id")).toBeInTheDocument();
      });

      await user.click(screen.getByTestId("ctx-copy-id"));

      expect(writeText).toHaveBeenCalledWith("nibs-xyz");
    });
  });

  // ─── Delete confirm action executes deleteBatch ───────────────

  describe("delete confirm action", () => {
    it("executes deleteBatch and clears selection on success", async () => {
      selection.select("nibs-abc1");

      renderMenu();

      await waitFor(() => {
        expect(screen.getByTestId("ctx-delete")).toBeInTheDocument();
      });

      await user.click(screen.getByTestId("ctx-delete"));

      // Execute the confirm action
      const action = mockConfirmDialog.lastOpts!.action;
      await action();

      await waitFor(() => {
        expect(mockConfirmDialog.close).toHaveBeenCalled();
        expect(mockExecute).toHaveBeenCalledWith(
          expect.objectContaining({
            kind: "batch",
            commands: expect.arrayContaining([
              expect.objectContaining({ kind: "delete-nib" }),
            ]),
          }),
        );
      });

      // Selection should be cleared on success
      expect(selection.selectedIds.size).toBe(0);
    });

    it("does not clear selection when deleteBatch fails", async () => {
      selection.select("nibs-abc1");
      mockExecute.mockResolvedValueOnce({ ok: false, error: "some error" });

      renderMenu();

      await waitFor(() => {
        expect(screen.getByTestId("ctx-delete")).toBeInTheDocument();
      });

      await user.click(screen.getByTestId("ctx-delete"));

      const action = mockConfirmDialog.lastOpts!.action;
      await action();

      await waitFor(() => {
        expect(mockConfirmDialog.close).toHaveBeenCalled();
        expect(mockExecute).toHaveBeenCalled();
      });

      // Selection should NOT be cleared on failure
      expect(selection.selectedIds.size).toBe(1);
      expect(selection.selectedIds.has("nibs-abc1")).toBe(true);
    });
  });

  // ─── Archive confirm action executes archiveBatch ─────────────

  describe("archive confirm action", () => {
    it("executes archiveBatch and clears selection on success", async () => {
      selection.select("nibs-abc1");

      renderMenu();

      await waitFor(() => {
        expect(screen.getByTestId("ctx-archive")).toBeInTheDocument();
      });

      await user.click(screen.getByTestId("ctx-archive"));

      // Execute the confirm action
      const action = mockConfirmDialog.lastOpts!.action;
      await action();

      await waitFor(() => {
        expect(mockConfirmDialog.close).toHaveBeenCalled();
        expect(mockExecute).toHaveBeenCalledWith(
          expect.objectContaining({
            kind: "batch",
            commands: expect.arrayContaining([
              expect.objectContaining({ kind: "archive-nib" }),
            ]),
          }),
        );
      });

      // Selection should be cleared on success
      expect(selection.selectedIds.size).toBe(0);
    });

    it("does not clear selection when archiveBatch fails", async () => {
      selection.select("nibs-abc1");
      mockExecute.mockResolvedValueOnce({ ok: false, error: "some error" });

      renderMenu();

      await waitFor(() => {
        expect(screen.getByTestId("ctx-archive")).toBeInTheDocument();
      });

      await user.click(screen.getByTestId("ctx-archive"));

      const action = mockConfirmDialog.lastOpts!.action;
      await action();

      await waitFor(() => {
        expect(mockConfirmDialog.close).toHaveBeenCalled();
        expect(mockExecute).toHaveBeenCalled();
      });

      // Selection should NOT be cleared on failure
      expect(selection.selectedIds.size).toBe(1);
      expect(selection.selectedIds.has("nibs-abc1")).toBe(true);
    });
  });

  // ─── Filter related submenu ───────────────────────────────────

  describe("Filter related submenu", () => {
    it("shows the submenu trigger in single mode", async () => {
      renderMenu();

      await waitFor(() => {
        expect(screen.getByTestId("ctx-filter-related-trigger")).toBeInTheDocument();
      });
    });

    it("hides the submenu in bulk mode (single-target gating)", async () => {
      renderMenu({ selectedCount: 3 });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-delete")).toBeInTheDocument();
      });

      expect(screen.queryByTestId("ctx-filter-related-trigger")).not.toBeInTheDocument();
    });

    // The visible-LABEL → field DIRECTION mapping is the known trap (blocking vs
    // blocked-by was swapped in the original nib draft). Each case locates the item
    // by its LABEL TEXT (NOT a field-derived testid, which would move with the field
    // and mask a swap) and asserts the field it emits. Swapping any label↔field pair
    // in FILTER_RELATIONS turns the matching row red.
    const directionCases: { label: string; field: RelIdKey }[] = [
      { label: "Items blocking this", field: "blockingId" },
      { label: "Items this blocks", field: "blockedById" },
      { label: "Children of this", field: "parentId" },
      // Hierarchy: the label names the RESULT set, the field names the relationship
      // the results hold toward this row. `ancestorId` keeps nibs whose ancestor is
      // this row — its descendants — and `descendantId` keeps this row's ancestors.
      { label: "Descendants of this", field: "ancestorId" },
      { label: "Ancestors of this", field: "descendantId" },
      { label: "Siblings of this", field: "siblingId" },
      { label: "Items mentioning this", field: "mentionsId" },
      { label: "Items this mentions", field: "mentionedById" },
    ];

    for (const { label, field } of directionCases) {
      it(`"${label}" emits onfilterrelated(${field}, nib.id)`, async () => {
        const onfilterrelated = vi.fn();
        renderMenu({ nib: makeNib({ id: "nibs-target" }), onfilterrelated });

        await waitFor(() => {
          expect(screen.getByTestId("ctx-filter-related-trigger")).toBeInTheDocument();
        });

        await openSubmenu(user, screen.getByTestId("ctx-filter-related-trigger"));

        // Anchor on the human label so a swapped mapping is actually caught.
        const item = await screen.findByText(label);
        await user.click(item);

        expect(onfilterrelated).toHaveBeenCalledExactlyOnceWith(field, "nibs-target");
      });
    }

    it("composes onto the current filter (ANDs) and overwrites the same-kind field", async () => {
      // Mirror App.svelte's composition contract: prefs.filter = { ...prefs.filter, [field]: id }.
      let filter: Record<string, unknown> = { status: ["todo"], parentId: "old-parent" };
      const onfilterrelated = (field: RelIdKey, id: string) => {
        filter = { ...filter, [field]: id };
      };

      renderMenu({ nib: makeNib({ id: "nibs-target" }), onfilterrelated });

      await waitFor(() => {
        expect(screen.getByTestId("ctx-filter-related-trigger")).toBeInTheDocument();
      });

      await openSubmenu(user, screen.getByTestId("ctx-filter-related-trigger"));
      await user.click(await screen.findByTestId("ctx-filter-parentId"));

      // ANDs: the existing status filter is preserved (not replaced).
      // Overwrites: the same-kind parentId is replaced with the row's id.
      expect(filter).toEqual({ status: ["todo"], parentId: "nibs-target" });
    });

    // A different-dimension key survives the pick, so the item narrows the current
    // query instead of resetting it. One blocking field and one hierarchy field
    // share this case rather than repeating the pattern per dimension. The real
    // composition lives in App.svelte's handler and is covered end-to-end in
    // App.test.ts; this only pins the (field, id) pair the menu emits. Items are
    // reached by LABEL TEXT, like directionCases, so a swapped mapping cannot hide
    // behind a field-derived testid that moves with it.
    const composeCases: { label: string; field: RelIdKey }[] = [
      { label: "Items blocking this", field: "blockingId" },
      { label: "Descendants of this", field: "ancestorId" },
    ];

    for (const { label, field } of composeCases) {
      it(`"${label}" ANDs onto the current filter without disturbing other facets`, async () => {
        let filter: Record<string, unknown> = { status: ["todo"], parentId: "old-parent" };
        const onfilterrelated = (f: RelIdKey, id: string) => {
          filter = { ...filter, [f]: id };
        };

        renderMenu({ nib: makeNib({ id: "nibs-target" }), onfilterrelated });

        await waitFor(() => {
          expect(screen.getByTestId("ctx-filter-related-trigger")).toBeInTheDocument();
        });

        await openSubmenu(user, screen.getByTestId("ctx-filter-related-trigger"));
        await user.click(await screen.findByText(label));

        expect(filter).toEqual({ status: ["todo"], parentId: "old-parent", [field]: "nibs-target" });
      });
    }
  });

  // ─── Menu does not render when open=false or nib=null ─────────

  describe("conditional rendering", () => {
    it("does not render context menu when nib is null", () => {
      render(RowContextMenu, {
        props: {
          open: true,
          position: { x: 100, y: 100 },
          nib: null,
          selectedCount: 1,
        },
        context: makeTestContext(selection, new DragState(), {
          confirmDialog: mockConfirmDialog,
          activeView: mockView as unknown as ActiveView,
        }),
      });

      expect(screen.queryByTestId("context-menu")).not.toBeInTheDocument();
    });
  });
});
