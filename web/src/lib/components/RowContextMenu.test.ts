import { render, screen, waitFor } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach } from "vitest";
import RowContextMenu from "./RowContextMenu.svelte";
import { SelectionState } from "../selection.svelte";
import { DragState } from "../drag.svelte";
import { makeTestContext } from "$lib/contexts";
import type {
  ConfirmDialogState,
  ConfirmDialogOptions,
} from "$lib/composables/useConfirmDialog.svelte";
import type { ActiveView } from "$lib/composables/useActiveView.svelte";
import type { TreeTableNib } from "../types";

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
    dismissAction: null as (() => void) | null,
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
      state.dismissAction = opts.onDismiss ?? null;
      state.lastOpts = opts;
    }),
    close: vi.fn(() => {
      state.open = false;
      state.action = null;
      state.saveLabel = null;
      state.saveAction = null;
      state.dismissAction = null;
    }),
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

function makeNib(overrides: Partial<TreeTableNib> = {}): TreeTableNib {
  return {
    id: "nibs-abc1",
    title: "Test nib",
    status: "todo",
    type: "task",
    priority: "normal",
    estimate: "m",
    tags: [],
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
      },
      context: makeTestContext(selection, new DragState(), {
        confirmDialog: mockConfirmDialog,
        activeView: mockView as unknown as ActiveView,
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

  // ─── Status submenu interaction ───────────────────────────────

  describe("status submenu", () => {
    it("clicking a status item calls mutations.execute with setStatusBatch", async () => {
      renderMenu();

      await waitFor(() => {
        expect(screen.getByTestId("ctx-status-trigger")).toBeInTheDocument();
      });

      // Hover over the status sub-trigger to open the submenu (bits-ui opens on pointerenter)
      await user.pointer({
        target: screen.getByTestId("ctx-status-trigger"),
      });

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
  });

  // ─── Priority submenu interaction ─────────────────────────────

  describe("priority submenu", () => {
    it("clicking a priority item calls mutations.execute with setPriorityBatch", async () => {
      renderMenu();

      await waitFor(() => {
        expect(screen.getByTestId("ctx-priority-trigger")).toBeInTheDocument();
      });

      // Hover over the priority sub-trigger to open the submenu (bits-ui opens on pointerenter)
      await user.pointer({
        target: screen.getByTestId("ctx-priority-trigger"),
      });

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
