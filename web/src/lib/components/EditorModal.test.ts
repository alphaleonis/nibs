import { render, screen, waitFor, fireEvent } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { readable } from "svelte/store";
import { describe, it, expect, vi, beforeEach } from "vitest";
import EditorModal from "./EditorModal.svelte";

// bits-ui scroll lock sets pointer-events: none on <body>, so disable the check
const user = userEvent.setup({ pointerEventsCheck: 0 });

// Mock the mutations module
const { mockExecute } = vi.hoisted(() => {
  return {
    mockExecute: vi.fn().mockResolvedValue({ ok: true, data: { createNib: { id: "nibs-new1" } } }),
  };
});
vi.mock("$lib/mutations", () => ({
  getMutationStore: () => ({
    execute: mockExecute,
    isMutating: () => false,
    get pending() { return false; },
  }),
}));

// Mock svelte-sonner toast
const { mockToastError, mockToastSuccess } = vi.hoisted(() => {
  return { mockToastError: vi.fn(), mockToastSuccess: vi.fn() };
});
vi.mock("svelte-sonner", async () => {
  const actual = await vi.importActual<typeof import("svelte-sonner")>("svelte-sonner");
  return {
    ...actual,
    toast: {
      ...actual.toast,
      error: mockToastError,
      success: mockToastSuccess,
    },
  };
});

// Mock CodeMirror modules (used by MarkdownEditor child component)
vi.mock("codemirror", () => ({
  basicSetup: [],
}));
vi.mock("@codemirror/lang-markdown", () => ({
  markdown: () => [],
}));
vi.mock("@codemirror/theme-one-dark", () => ({
  oneDark: [],
}));
vi.mock("@codemirror/state", () => ({
  EditorState: {
    create: () => ({
      doc: { length: 0, toString: () => "" },
    }),
  },
}));
// Captured updateListener callback from the most recent EditorView instance,
// allowing tests to simulate user edits in the CodeMirror editor.
let _cmUpdateListener: ((update: any) => void) | null = null;

/** Simulate a user typing in the CodeMirror editor. Sets body + bodyModified. */
function simulateCmEdit(newText: string) {
  if (!_cmUpdateListener) throw new Error("No CM updateListener captured");
  _cmUpdateListener({
    docChanged: true,
    state: { doc: { toString: () => newText } },
  });
}

vi.mock("@codemirror/view", () => {
  return {
    EditorView: class {
      static lineWrapping = [];
      static updateListener = {
        of: (fn: (update: any) => void) => {
          _cmUpdateListener = fn;
          return [];
        },
      };
      state = { doc: { length: 0, toString: () => "" } };
      destroy = vi.fn();
      dispatch = vi.fn();
      constructor(config: any) {
        if (config.parent) {
          config.parent.innerHTML = "<div data-testid='mock-cm'>CodeMirror</div>";
        }
      }
    },
    keymap: { of: () => [] },
  };
});

// Mock @urql/svelte for subscription support
const { mockSubscriptionStore } = vi.hoisted(() => {
  return { mockSubscriptionStore: vi.fn() };
});
vi.mock("@urql/svelte", async () => {
  const actual = await vi.importActual<typeof import("@urql/svelte")>("@urql/svelte");
  return {
    ...actual,
    getContextClient: vi.fn().mockReturnValue({}),
    subscriptionStore: mockSubscriptionStore,
  };
});

function makeNibData(overrides: Record<string, unknown> = {}) {
  return {
    title: "Existing Nib",
    status: "in-progress",
    type: "task",
    priority: "high",
    estimate: "m",
    tags: ["test"],
    body: "Some body content",
    etag: "abc123",
    ...overrides,
  };
}

describe("EditorModal", () => {
  beforeEach(() => {
    _cmUpdateListener = null;
    mockExecute.mockReset().mockResolvedValue({ ok: true, data: { createNib: { id: "nibs-new1" } } });
    mockToastError.mockReset();
    mockToastSuccess.mockReset();
    mockSubscriptionStore.mockReset().mockReturnValue(readable({ data: undefined }));
  });

  it("auto-focuses the title input when opened in create mode", async () => {
    render(EditorModal, {
      open: true,
      mode: "create",
      onclose: vi.fn(),
    });

    await waitFor(() => {
      expect(screen.getByTestId("editor-title")).toBeInTheDocument();
    });

    await waitFor(() => {
      expect(document.activeElement).toBe(screen.getByTestId("editor-title"));
    });
  });

  it("renders in create mode with default fields", async () => {
    render(EditorModal, {
      open: true,
      mode: "create",
      onclose: vi.fn(),
    });

    await waitFor(() => {
      expect(screen.getByTestId("editor-modal")).toBeInTheDocument();
    });

    // Title input should be empty
    const titleInput = screen.getByTestId("editor-title") as HTMLInputElement;
    expect(titleInput.value).toBe("");

    // Status should default to draft
    expect(screen.getByTestId("editor-status")).toHaveTextContent("draft");

    // Type should default to task
    expect(screen.getByTestId("editor-type")).toHaveTextContent("task");
  });

  it("renders in edit mode with pre-populated fields", async () => {
    render(EditorModal, {
      open: true,
      mode: "edit",
      nibId: "nibs-abc1",
      nibData: makeNibData(),
      onclose: vi.fn(),
    });

    await waitFor(() => {
      expect(screen.getByTestId("editor-modal")).toBeInTheDocument();
    });

    const titleInput = screen.getByTestId("editor-title") as HTMLInputElement;
    expect(titleInput.value).toBe("Existing Nib");

    expect(screen.getByTestId("editor-status")).toHaveTextContent("in-progress");
    expect(screen.getByTestId("editor-type")).toHaveTextContent("task");
    expect(screen.getByTestId("editor-priority")).toHaveTextContent("high");
    expect(screen.getByTestId("editor-estimate")).toHaveTextContent("Medium");
  });

  it("does not render when open is false", () => {
    render(EditorModal, {
      open: false,
      mode: "create",
      onclose: vi.fn(),
    });

    expect(screen.queryByTestId("editor-modal")).not.toBeInTheDocument();
  });

  it("has save and cancel buttons", async () => {
    render(EditorModal, {
      open: true,
      mode: "create",
      onclose: vi.fn(),
    });

    await waitFor(() => {
      expect(screen.getByTestId("editor-save")).toBeInTheDocument();
      expect(screen.getByTestId("editor-cancel")).toBeInTheDocument();
    });

    expect(screen.getByTestId("editor-save")).toHaveTextContent("Create");
  });

  it("shows Save label in edit mode", async () => {
    render(EditorModal, {
      open: true,
      mode: "edit",
      nibId: "nibs-abc1",
      nibData: makeNibData(),
      onclose: vi.fn(),
    });

    await waitFor(() => {
      expect(screen.getByTestId("editor-save")).toHaveTextContent("Save");
    });
  });

  it("calls createNib command on save in create mode", async () => {
    const onsave = vi.fn();
    render(EditorModal, {
      open: true,
      mode: "create",
      onclose: vi.fn(),
      onsave,
    });

    await waitFor(() => {
      expect(screen.getByTestId("editor-title")).toBeInTheDocument();
    });

    const titleInput = screen.getByTestId("editor-title") as HTMLInputElement;
    await user.type(titleInput, "New Nib Title");
    await user.click(screen.getByTestId("editor-save"));

    await waitFor(() => {
      expect(mockExecute).toHaveBeenCalledWith(
        expect.objectContaining({
          kind: "create-nib",
          input: expect.objectContaining({
            title: "New Nib Title",
            type: "task",
            status: "draft",
          }),
        })
      );
    });
  });

  it("shows error toast when title is empty on save", async () => {
    render(EditorModal, {
      open: true,
      mode: "create",
      onclose: vi.fn(),
    });

    await waitFor(() => {
      expect(screen.getByTestId("editor-save")).toBeInTheDocument();
    });

    await user.click(screen.getByTestId("editor-save"));

    expect(mockToastError).toHaveBeenCalledWith("Title is required");
  });

  it("shows unsaved changes dialog on close when there are changes", async () => {
    render(EditorModal, {
      open: true,
      mode: "create",
      onclose: vi.fn(),
    });

    await waitFor(() => {
      expect(screen.getByTestId("editor-title")).toBeInTheDocument();
    });

    // Set title directly via fireEvent to avoid pointer-events flakiness
    const titleInput = screen.getByTestId("editor-title") as HTMLInputElement;
    fireEvent.input(titleInput, { target: { value: "Unsaved title" } });

    // Click close via fireEvent to bypass pointer-events check
    fireEvent.click(screen.getByTestId("editor-close"));

    // Should show confirm dialog
    await waitFor(() => {
      expect(screen.getByTestId("confirm-dialog")).toBeInTheDocument();
      expect(screen.getByTestId("confirm-dialog-title")).toHaveTextContent("Unsaved changes");
    });
  });

  it("does not show unsaved changes dialog on close when empty", async () => {
    const onclose = vi.fn();
    render(EditorModal, {
      open: true,
      mode: "create",
      onclose,
    });

    await waitFor(() => {
      expect(screen.getByTestId("editor-close")).toBeInTheDocument();
    });

    // Click close without any changes
    await user.click(screen.getByTestId("editor-close"));

    // Should close directly — no confirm dialog
    expect(screen.queryByTestId("confirm-dialog")).not.toBeInTheDocument();
    expect(onclose).toHaveBeenCalled();
  });

  it("has write and preview tabs", async () => {
    render(EditorModal, {
      open: true,
      mode: "create",
      onclose: vi.fn(),
    });

    await waitFor(() => {
      expect(screen.getByTestId("editor-tab-write")).toBeInTheDocument();
      expect(screen.getByTestId("editor-tab-preview")).toBeInTheDocument();
    });
  });

  describe("conflict detection", () => {
    function makeExternalChangeSubscription(overrides: Record<string, unknown> = {}) {
      return readable({
        data: {
          nibChanged: {
            type: "updated",
            nibId: "nibs-abc1",
            nib: {
              id: "nibs-abc1",
              title: "Externally Modified Title",
              status: "todo",
              type: "bug",
              priority: "normal",
              estimate: "l",
              tags: ["external"],
              body: "External body",
              etag: "different-etag",
              updatedAt: "2026-03-25T00:00:00Z",
              parentId: null,
              blockingIds: [],
              blockedByIds: [],
              ...overrides,
            },
          },
        },
      });
    }

    it("shows warning banner when external change is detected", async () => {
      mockSubscriptionStore.mockReturnValue(makeExternalChangeSubscription());

      render(EditorModal, {
        open: true,
        mode: "edit",
        nibId: "nibs-abc1",
        nibData: makeNibData(),
        onclose: vi.fn(),
      });

      await waitFor(() => {
        expect(screen.getByTestId("editor-conflict-banner")).toBeInTheDocument();
      });

      expect(screen.getByTestId("editor-conflict-banner")).toHaveTextContent("modified externally");
    });

    it("revert restores original nibData and dismisses banner", async () => {
      mockSubscriptionStore.mockReturnValue(makeExternalChangeSubscription());

      render(EditorModal, {
        open: true,
        mode: "edit",
        nibId: "nibs-abc1",
        nibData: makeNibData(),
        onclose: vi.fn(),
      });

      await waitFor(() => {
        expect(screen.getByTestId("editor-conflict-banner")).toBeInTheDocument();
      });

      // Click Revert
      fireEvent.click(screen.getByTestId("editor-conflict-revert"));

      // Banner should disappear
      await waitFor(() => {
        expect(screen.queryByTestId("editor-conflict-banner")).not.toBeInTheDocument();
      });

      // Fields should be back to original nibData values
      const titleInput = screen.getByTestId("editor-title") as HTMLInputElement;
      expect(titleInput.value).toBe("Existing Nib");
      expect(screen.getByTestId("editor-status")).toHaveTextContent("in-progress");
      expect(screen.getByTestId("editor-type")).toHaveTextContent("task");
      expect(screen.getByTestId("editor-priority")).toHaveTextContent("high");
      expect(screen.getByTestId("editor-estimate")).toHaveTextContent("Medium");
    });

    it("reload loads external server state and dismisses banner", async () => {
      mockSubscriptionStore.mockReturnValue(makeExternalChangeSubscription());

      render(EditorModal, {
        open: true,
        mode: "edit",
        nibId: "nibs-abc1",
        nibData: makeNibData(),
        onclose: vi.fn(),
      });

      await waitFor(() => {
        expect(screen.getByTestId("editor-conflict-banner")).toBeInTheDocument();
      });

      // Click Reload
      fireEvent.click(screen.getByTestId("editor-conflict-reload"));

      // Banner should disappear
      await waitFor(() => {
        expect(screen.queryByTestId("editor-conflict-banner")).not.toBeInTheDocument();
      });

      // Fields should match the external (subscription) values
      const titleInput = screen.getByTestId("editor-title") as HTMLInputElement;
      expect(titleInput.value).toBe("Externally Modified Title");
      expect(screen.getByTestId("editor-status")).toHaveTextContent("todo");
      expect(screen.getByTestId("editor-type")).toHaveTextContent("bug");
      expect(screen.getByTestId("editor-priority")).toHaveTextContent("normal");
      expect(screen.getByTestId("editor-estimate")).toHaveTextContent("Large");
    });

    it("save with external changes shows overwrite confirmation instead of saving", async () => {
      mockSubscriptionStore.mockReturnValue(makeExternalChangeSubscription());

      render(EditorModal, {
        open: true,
        mode: "edit",
        nibId: "nibs-abc1",
        nibData: makeNibData(),
        onclose: vi.fn(),
      });

      // Wait for conflict banner to appear
      await waitFor(() => {
        expect(screen.getByTestId("editor-conflict-banner")).toBeInTheDocument();
      });

      // Click Save
      fireEvent.click(screen.getByTestId("editor-save"));

      // Should show overwrite confirmation dialog instead of saving
      await waitFor(() => {
        expect(screen.getByTestId("editor-overwrite-confirm")).toBeInTheDocument();
      });

      // Save should NOT have been called
      expect(mockExecute).not.toHaveBeenCalled();
    });

    it("overwrite confirmation proceeds with save", async () => {
      mockSubscriptionStore.mockReturnValue(makeExternalChangeSubscription());
      mockExecute.mockResolvedValue({ ok: true, data: { updateNib: { id: "nibs-abc1" } } });

      render(EditorModal, {
        open: true,
        mode: "edit",
        nibId: "nibs-abc1",
        nibData: makeNibData(),
        onclose: vi.fn(),
      });

      // Wait for conflict banner
      await waitFor(() => {
        expect(screen.getByTestId("editor-conflict-banner")).toBeInTheDocument();
      });

      // Click Save to trigger overwrite confirmation
      fireEvent.click(screen.getByTestId("editor-save"));

      await waitFor(() => {
        expect(screen.getByTestId("editor-overwrite-confirm")).toBeInTheDocument();
      });

      // Confirm the overwrite
      fireEvent.click(screen.getByTestId("editor-overwrite-confirm-confirm"));

      // Save should now proceed
      await waitFor(() => {
        expect(mockExecute).toHaveBeenCalledWith(
          expect.objectContaining({
            kind: "update-nib",
          })
        );
      });
    });
  });

  describe("type change in create mode", () => {
    async function openCreateModalAndTypeTitle(titleText: string) {
      render(EditorModal, {
        open: true,
        mode: "create",
        onclose: vi.fn(),
      });

      await waitFor(() => {
        expect(screen.getByTestId("editor-modal")).toBeInTheDocument();
      });

      // Type a title
      const titleInput = screen.getByTestId("editor-title") as HTMLInputElement;
      fireEvent.input(titleInput, { target: { value: titleText } });
      expect(titleInput.value).toBe(titleText);

      return titleInput;
    }

    async function clickTypeOption(newType: string) {
      // Open the type select dropdown and pick an option (must use userEvent for Bits UI Select)
      const typeTrigger = screen.getByTestId("editor-type");
      await user.click(typeTrigger);

      const option = screen.getByRole("option", { name: newType });
      await user.click(option);
    }

    it("preserves title when type is changed", async () => {
      const titleInput = await openCreateModalAndTypeTitle("My Important Feature");

      // Change type from task to bug
      await clickTypeOption("bug");

      // Title should be preserved — not reset to empty
      await waitFor(() => {
        expect(titleInput.value).toBe("My Important Feature");
      });
    });

    it("preserves priority and estimate when type is changed", async () => {
      render(EditorModal, {
        open: true,
        mode: "create",
        onclose: vi.fn(),
      });

      await waitFor(() => {
        expect(screen.getByTestId("editor-modal")).toBeInTheDocument();
      });

      // Set priority to "high"
      const priorityTrigger = screen.getByTestId("editor-priority");
      await user.click(priorityTrigger);
      await user.click(screen.getByRole("option", { name: "high" }));
      await waitFor(() => {
        expect(screen.getByTestId("editor-priority")).toHaveTextContent("high");
      });

      // Set estimate to "Large"
      const estimateTrigger = screen.getByTestId("editor-estimate");
      await user.click(estimateTrigger);
      await user.click(screen.getByRole("option", { name: "Large" }));
      await waitFor(() => {
        expect(screen.getByTestId("editor-estimate")).toHaveTextContent("Large");
      });

      // Change type from task to bug
      await clickTypeOption("bug");

      // Priority and estimate should be preserved
      expect(screen.getByTestId("editor-priority")).toHaveTextContent("high");
      expect(screen.getByTestId("editor-estimate")).toHaveTextContent("Large");
    });

    it("updates body template when type changes and body has not been manually edited", async () => {
      render(EditorModal, {
        open: true,
        mode: "create",
        onclose: vi.fn(),
      });

      await waitFor(() => {
        expect(screen.getByTestId("editor-modal")).toBeInTheDocument();
      });

      // Change type from task to bug (body hasn't been manually edited)
      await clickTypeOption("bug");

      // Now save to inspect which body was sent
      const titleInput = screen.getByTestId("editor-title") as HTMLInputElement;
      fireEvent.input(titleInput, { target: { value: "Bug report" } });
      fireEvent.click(screen.getByTestId("editor-save"));

      await waitFor(() => {
        expect(mockExecute).toHaveBeenCalledWith(
          expect.objectContaining({
            kind: "create-nib",
            input: expect.objectContaining({
              type: "bug",
              body: expect.stringContaining("Steps to Reproduce"),
            }),
          })
        );
      });
    });

    it("does not replace body when it has been manually edited", async () => {
      render(EditorModal, {
        open: true,
        mode: "create",
        onclose: vi.fn(),
      });

      await waitFor(() => {
        expect(screen.getByTestId("editor-modal")).toBeInTheDocument();
      });

      // Wait for the CM mock to initialize (async import in MarkdownEditor $effect)
      await waitFor(() => {
        expect(screen.getByTestId("mock-cm")).toBeInTheDocument();
      });

      // Simulate user manually editing the body in CodeMirror
      simulateCmEdit("My custom body content");

      // Change type from task to bug — body should NOT be replaced
      await clickTypeOption("bug");

      // Save and verify the body is the custom content, not the bug template
      const titleInput = screen.getByTestId("editor-title") as HTMLInputElement;
      fireEvent.input(titleInput, { target: { value: "My nib" } });
      fireEvent.click(screen.getByTestId("editor-save"));

      await waitFor(() => {
        expect(mockExecute).toHaveBeenCalledWith(
          expect.objectContaining({
            kind: "create-nib",
            input: expect.objectContaining({
              body: "My custom body content",
            }),
          })
        );
      });
    });
  });
});
