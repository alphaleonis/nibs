import { render, screen, waitFor } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { flushSync } from "svelte";
import { ACTIVE_VIEW_KEY, CONFIRM_DIALOG_KEY } from "$lib/contexts";
import type { ConfirmDialogState, ConfirmDialogOptions } from "$lib/composables/useConfirmDialog.svelte";
import { Preferences } from "$lib/preferences.svelte";
import ActiveNibView from "./ActiveNibView.svelte";

// bits-ui scroll lock sets pointer-events: none on <body>, so disable the check.
const user = userEvent.setup({ pointerEventsCheck: 0 });

// --- module mocks -------------------------------------------------------
const { mockExecute, mockIsMutating } = vi.hoisted(() => ({
  mockExecute: vi.fn().mockResolvedValue({ ok: true, data: {} }),
  mockIsMutating: vi.fn().mockReturnValue(false),
}));
vi.mock("$lib/mutations", () => ({
  getMutationStore: () => ({
    execute: mockExecute,
    isMutating: mockIsMutating,
    get pending() {
      return false;
    },
  }),
}));

const { mockToastError, mockToastSuccess } = vi.hoisted(() => ({
  mockToastError: vi.fn(),
  mockToastSuccess: vi.fn(),
}));
vi.mock("svelte-sonner", async () => {
  const actual = await vi.importActual<typeof import("svelte-sonner")>("svelte-sonner");
  return { ...actual, toast: { ...actual.toast, error: mockToastError, success: mockToastSuccess } };
});

// --- fakes --------------------------------------------------------------
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
      state.lastOpts = opts;
      state.action = opts.action;
    }),
    close: vi.fn(() => {
      state.open = false;
      state.action = null;
    }),
  };
  return state;
}

interface FakeForm {
  mode: "edit" | "create";
  id?: string;
  title: string;
  status: string;
  type: string;
  priority: string;
  estimate: string;
  tags: string[];
  body: string;
  bodyVersion: number;
  dirty: boolean;
  saving: boolean;
  externalChange: unknown;
  addTag: (t: string) => void;
  removeTag: (t: string) => void;
  discard: ReturnType<typeof vi.fn>;
  applyExternal: ReturnType<typeof vi.fn>;
  save?: ReturnType<typeof vi.fn>;
}

function makeEditForm(overrides: Partial<FakeForm> = {}): FakeForm {
  const form = $state({
    mode: "edit" as const,
    id: "nibs-1t4t",
    title: "Rework detail-panel layout: action toolbar + inline tag input",
    status: "todo",
    type: "feature",
    priority: "normal",
    estimate: "m",
    tags: ["web-ui", "detail-panel"],
    body: "## Heading\n\nSome **bold** text and see #gx0f for details.",
    bodyVersion: 0,
    dirty: false,
    saving: false,
    externalChange: null as unknown,
    addTag(t: string) {
      if (!form.tags.includes(t)) form.tags = [...form.tags, t];
    },
    removeTag(t: string) {
      form.tags = form.tags.filter((x) => x !== t);
    },
    discard: vi.fn(),
    applyExternal: vi.fn(),
    ...overrides,
  });
  return form as FakeForm;
}

function makeCreateForm(overrides: Partial<FakeForm> = {}): FakeForm {
  const form = $state({
    mode: "create" as const,
    title: "",
    status: "draft",
    type: "task",
    priority: "",
    estimate: "",
    tags: [] as string[],
    body: "Template body",
    bodyVersion: 0,
    dirty: false,
    saving: false,
    externalChange: null as unknown,
    addTag(t: string) {
      if (!form.tags.includes(t)) form.tags = [...form.tags, t];
    },
    removeTag(t: string) {
      form.tags = form.tags.filter((x) => x !== t);
    },
    discard: vi.fn(),
    applyExternal: vi.fn(),
    ...overrides,
  });
  return form as FakeForm;
}

function ref(id: string, title: string, status = "todo", type = "task") {
  return { id, title, status, type };
}

function makeDetailNib(overrides: Record<string, unknown> = {}) {
  return {
    id: "nibs-1t4t",
    title: "Rework detail-panel layout",
    status: "todo",
    type: "feature",
    priority: "normal",
    estimate: "m",
    tags: ["web-ui", "detail-panel"],
    body: "",
    documents: [],
    etag: "e0",
    parent: ref("nibs-9kvw", "Web UI Polish", "in-progress", "epic"),
    children: [],
    blocking: [],
    blockedBy: [],
    mentions: [ref("nibs-gx0f", "Mentioned Nib", "todo", "feature")],
    mentionedBy: [],
    ...overrides,
  };
}

interface FakeView {
  state: unknown;
  form: FakeForm | null;
  detail: { nib: unknown; fetching: boolean } | null;
  isOpen: boolean;
  presentation: "docked" | "expanded";
  blocksHistoryNav: boolean;
  open: ReturnType<typeof vi.fn>;
  expand: ReturnType<typeof vi.fn>;
  collapse: ReturnType<typeof vi.fn>;
  startCreate: ReturnType<typeof vi.fn>;
  startCreateChild: ReturnType<typeof vi.fn>;
  chooseType: ReturnType<typeof vi.fn>;
  cancelType: ReturnType<typeof vi.fn>;
  save: ReturnType<typeof vi.fn>;
  requestClose: ReturnType<typeof vi.fn>;
  syncTo: ReturnType<typeof vi.fn>;
  dispose: ReturnType<typeof vi.fn>;
}

function makeView(opts: {
  kind?: "viewing" | "gone" | "creating";
  form?: FakeForm;
  detail?: { nib: unknown; fetching: boolean } | null;
  presentation?: "docked" | "expanded";
}): FakeView {
  const kind = opts.kind ?? "viewing";
  const stateObj =
    kind === "creating"
      ? { kind, defaults: { type: "task" }, presentation: opts.presentation ?? "docked" }
      : { kind, nibId: "nibs-1t4t", presentation: opts.presentation ?? "docked" };
  const view = $state({
    state: stateObj,
    form: opts.form ?? makeEditForm(),
    detail: opts.detail ?? { nib: makeDetailNib(), fetching: false },
    isOpen: true,
    presentation: opts.presentation ?? "docked",
    blocksHistoryNav: false,
    open: vi.fn(),
    expand: vi.fn(),
    collapse: vi.fn(),
    startCreate: vi.fn(),
    startCreateChild: vi.fn(),
    chooseType: vi.fn(),
    cancelType: vi.fn(),
    save: vi.fn(async () => ({ kind: "saved", snapshot: {} })),
    requestClose: vi.fn(),
    syncTo: vi.fn(),
    dispose: vi.fn(),
  });
  return view as unknown as FakeView;
}

function renderView(
  view: FakeView,
  confirmDialog: ConfirmDialogState,
  props: Record<string, unknown> = {},
) {
  const ctx = new Map<string, unknown>();
  ctx.set(ACTIVE_VIEW_KEY, view);
  ctx.set(CONFIRM_DIALOG_KEY, confirmDialog);
  return render(ActiveNibView, { context: ctx, props });
}

describe("ActiveNibView", () => {
  let confirmDialog: ReturnType<typeof makeMockConfirmDialog>;

  beforeEach(() => {
    mockExecute.mockReset().mockResolvedValue({ ok: true, data: {} });
    mockIsMutating.mockReset().mockReturnValue(false);
    mockToastError.mockReset();
    mockToastSuccess.mockReset();
    confirmDialog = makeMockConfirmDialog();
  });

  describe("header", () => {
    it("renders the nib id and the single-line title with a full-text tooltip", () => {
      const form = makeEditForm();
      renderView(makeView({ form }), confirmDialog);

      expect(screen.getByTestId("anv-id")).toHaveTextContent("nibs-1t4t");

      const title = screen.getByTestId("anv-title") as HTMLInputElement;
      expect(title.value).toBe(form.title);
      // ellipsis + tooltip: full text carried in the title attribute, single-line class.
      expect(title).toHaveAttribute("title", form.title);
      expect(title).toHaveClass("anv-title");
    });

    it("renders the current tags", () => {
      renderView(makeView({}), confirmDialog);
      const tags = screen.getAllByTestId("anv-tag");
      expect(tags).toHaveLength(2);
      expect(tags[0]).toHaveTextContent("web-ui");
      expect(tags[1]).toHaveTextContent("detail-panel");
    });

    it("disables Save and Discard while the form is clean, enables them once dirty", async () => {
      const form = makeEditForm({ dirty: false });
      renderView(makeView({ form }), confirmDialog);

      expect(screen.getByTestId("anv-save")).toBeDisabled();
      expect(screen.getByTestId("anv-discard")).toBeDisabled();

      form.dirty = true;
      flushSync();

      await waitFor(() => {
        expect(screen.getByTestId("anv-save")).toBeEnabled();
        expect(screen.getByTestId("anv-discard")).toBeEnabled();
      });
    });

    it("shows the unsaved dot only when dirty", async () => {
      const form = makeEditForm({ dirty: false });
      renderView(makeView({ form }), confirmDialog);

      expect(screen.queryByTestId("anv-unsaved-dot")).not.toBeInTheDocument();

      form.dirty = true;
      flushSync();
      await waitFor(() => expect(screen.getByTestId("anv-unsaved-dot")).toBeInTheDocument());
    });

    it("Save calls view.save() and Discard calls form.discard()", async () => {
      const form = makeEditForm({ dirty: true });
      const view = makeView({ form });
      renderView(view, confirmDialog);

      await user.click(screen.getByTestId("anv-save"));
      expect(view.save).toHaveBeenCalledTimes(1);

      await user.click(screen.getByTestId("anv-discard"));
      expect(form.discard).toHaveBeenCalledTimes(1);
    });

    it("expand button calls view.expand() when docked; collapse when expanded", async () => {
      const dockedView = makeView({ presentation: "docked" });
      const { unmount } = renderView(dockedView, confirmDialog);
      await user.click(screen.getByTestId("anv-expand"));
      expect(dockedView.expand).toHaveBeenCalledTimes(1);
      expect(screen.queryByTestId("anv-collapse")).not.toBeInTheDocument();
      unmount();

      const expandedView = makeView({ presentation: "expanded" });
      renderView(expandedView, confirmDialog);
      await user.click(screen.getByTestId("anv-collapse"));
      expect(expandedView.collapse).toHaveBeenCalledTimes(1);
    });

    it("close button calls view.requestClose()", async () => {
      const view = makeView({});
      renderView(view, confirmDialog);
      await user.click(screen.getByTestId("anv-close"));
      expect(view.requestClose).toHaveBeenCalledTimes(1);
    });
  });

  describe("blocked badge (header)", () => {
    it("shows the blocked pill (default emphasis) when detailNib.blockedBy is non-empty", () => {
      const detail = {
        nib: makeDetailNib({ blockedBy: [ref("nibs-b1", "Blocker", "todo", "task")] }),
        fetching: false,
      };
      renderView(makeView({ detail }), confirmDialog);

      const badge = screen.getByTestId("blocked-badge");
      expect(badge).toBeInTheDocument();
      expect(badge).toHaveTextContent("Blocked");
      expect(badge).toHaveAttribute("title", "Blocked by 1 nib(s)");
    });

    it("hides the blocked badge when blockedBy is empty", () => {
      // The default detail nib has blockedBy: [].
      renderView(makeView({}), confirmDialog);
      expect(screen.queryByTestId("blocked-badge")).not.toBeInTheDocument();
      expect(screen.queryByTestId("blocked-icon")).not.toBeInTheDocument();
    });

    it("renders the bare lock icon (no pill) when blockedEmphasis is 'subtle'", () => {
      const detail = {
        nib: makeDetailNib({ blockedBy: [ref("nibs-b1", "Blocker", "todo", "task")] }),
        fetching: false,
      };
      renderView(makeView({ detail }), confirmDialog, { blockedEmphasis: "subtle" });

      expect(screen.getByTestId("blocked-icon")).toBeInTheDocument();
      expect(screen.queryByTestId("blocked-badge")).not.toBeInTheDocument();
    });

    it("does not show the badge while creating (no blockers yet)", () => {
      const form = makeCreateForm({ title: "New nib", dirty: true });
      renderView(makeView({ kind: "creating", form, detail: null }), confirmDialog);
      expect(screen.queryByTestId("blocked-badge")).not.toBeInTheDocument();
      expect(screen.queryByTestId("blocked-icon")).not.toBeInTheDocument();
    });
  });

  describe("metadata band", () => {
    it("renders Status/Type/Priority/Estimate selects reflecting the form", () => {
      renderView(makeView({}), confirmDialog);
      expect(screen.getByTestId("anv-metaband")).toBeInTheDocument();
      expect(screen.getByTestId("anv-status")).toHaveTextContent("todo");
      expect(screen.getByTestId("anv-type")).toHaveTextContent("feature");
      expect(screen.getByTestId("anv-priority")).toHaveTextContent("normal");
      expect(screen.getByTestId("anv-estimate")).toHaveTextContent("Medium");
    });
  });

  describe("body", () => {
    it("renders the markdown preview by default", () => {
      renderView(makeView({}), confirmDialog);
      const prose = screen.getByTestId("anv-body-prose");
      expect(prose.innerHTML).toContain("<h2");
      expect(prose.innerHTML).toContain("<strong>bold</strong>");
      expect(screen.queryByTestId("anv-editor-container")).not.toBeInTheDocument();
    });

    it("toggles between preview and editor", async () => {
      renderView(makeView({}), confirmDialog);

      expect(screen.getByTestId("anv-body-prose")).toBeInTheDocument();
      expect(screen.queryByTestId("anv-editor-container")).not.toBeInTheDocument();

      await user.click(screen.getByTestId("anv-edit-toggle"));
      expect(screen.getByTestId("anv-editor-container")).toBeInTheDocument();

      await user.click(screen.getByTestId("anv-edit-toggle"));
      expect(screen.queryByTestId("anv-editor-container")).not.toBeInTheDocument();
      expect(screen.getByTestId("anv-body-prose")).toBeInTheDocument();
    });

    it("clicking a #mention in the preview calls view.open(fullId)", async () => {
      const view = makeView({});
      renderView(view, confirmDialog);

      const prose = screen.getByTestId("anv-body-prose");
      const anchor = prose.querySelector('a[data-nib-id="nibs-gx0f"]') as HTMLAnchorElement;
      expect(anchor).toBeInTheDocument();
      expect(anchor).toHaveTextContent("#gx0f");

      await user.click(anchor);
      expect(view.open).toHaveBeenCalledWith("nibs-gx0f");
    });
  });

  describe("preview toggle (persisted)", () => {
    it("shows the preview pane while editing when prefs.previewOpen is true (default)", async () => {
      const prefs = new Preferences();
      expect(prefs.previewOpen).toBe(true);
      renderView(makeView({}), confirmDialog, { prefs });

      await user.click(screen.getByTestId("anv-edit-toggle"));
      expect(screen.getByTestId("anv-editor-container")).toBeInTheDocument();
      expect(screen.getByTestId("anv-preview-pane")).toBeInTheDocument();
      expect(screen.getByTestId("anv-preview-toggle")).toHaveAttribute("aria-checked", "true");
    });

    it("starts with the preview pane hidden when prefs.previewOpen is false (persistence round-trip)", async () => {
      const prefs = new Preferences();
      prefs.previewOpen = false;
      renderView(makeView({}), confirmDialog, { prefs });

      await user.click(screen.getByTestId("anv-edit-toggle"));
      expect(screen.getByTestId("anv-editor-container")).toBeInTheDocument();
      expect(screen.queryByTestId("anv-preview-pane")).not.toBeInTheDocument();
      expect(screen.getByTestId("anv-preview-toggle")).toHaveAttribute("aria-checked", "false");
    });

    it("toggling the Preview switch writes through to prefs.previewOpen and updates the view", async () => {
      const prefs = new Preferences();
      renderView(makeView({}), confirmDialog, { prefs });

      await user.click(screen.getByTestId("anv-edit-toggle"));
      const toggle = screen.getByTestId("anv-preview-toggle");
      expect(toggle).toHaveAttribute("aria-checked", "true");

      await user.click(toggle);
      expect(prefs.previewOpen).toBe(false);
      expect(screen.getByTestId("anv-preview-toggle")).toHaveAttribute("aria-checked", "false");
      expect(screen.queryByTestId("anv-preview-pane")).not.toBeInTheDocument();

      await user.click(screen.getByTestId("anv-preview-toggle"));
      expect(prefs.previewOpen).toBe(true);
      expect(screen.getByTestId("anv-preview-pane")).toBeInTheDocument();
    });
  });

  describe("overflow menu", () => {
    async function openMenu() {
      await user.click(screen.getByTestId("anv-overflow"));
      await waitFor(() => expect(screen.getByTestId("anv-menu-copy-id")).toBeInTheDocument());
    }

    it("exposes New child / Copy ID / Archive / Delete", async () => {
      renderView(makeView({}), confirmDialog);
      await openMenu();
      expect(screen.getByTestId("anv-menu-new-child")).toBeInTheDocument();
      expect(screen.getByTestId("anv-menu-copy-id")).toBeInTheDocument();
      expect(screen.getByTestId("anv-menu-archive")).toBeInTheDocument();
      expect(screen.getByTestId("anv-menu-delete")).toBeInTheDocument();
    });

    it("New child nib calls view.startCreateChild(nibId, parentType)", async () => {
      const view = makeView({});
      renderView(view, confirmDialog);
      await openMenu();
      await user.click(screen.getByTestId("anv-menu-new-child"));
      expect(view.startCreateChild).toHaveBeenCalledWith("nibs-1t4t", "feature");
    });

    it("Copy ID writes the nib id to the clipboard", async () => {
      const writeText = vi.fn().mockResolvedValue(undefined);
      Object.defineProperty(navigator, "clipboard", { value: { writeText }, writable: true, configurable: true });
      renderView(makeView({}), confirmDialog);
      await openMenu();
      await user.click(screen.getByTestId("anv-menu-copy-id"));
      expect(writeText).toHaveBeenCalledWith("nibs-1t4t");
    });

    it("Archive opens a warning confirm dialog referencing the nib", async () => {
      renderView(makeView({}), confirmDialog);
      await openMenu();
      await user.click(screen.getByTestId("anv-menu-archive"));
      expect(confirmDialog.showConfirm).toHaveBeenCalledWith(
        expect.objectContaining({ title: "Archive nib?", variant: "warning" }),
      );
      expect(confirmDialog.lastOpts?.message).toContain("nibs-1t4t");
    });

    it("Delete opens a danger confirm dialog referencing the nib", async () => {
      renderView(makeView({}), confirmDialog);
      await openMenu();
      await user.click(screen.getByTestId("anv-menu-delete"));
      expect(confirmDialog.showConfirm).toHaveBeenCalledWith(
        expect.objectContaining({ title: "Delete nib?", variant: "danger" }),
      );
      expect(confirmDialog.lastOpts?.message).toContain("nibs-1t4t");
    });

    it("Delete confirm action runs the delete mutation and requests close", async () => {
      mockExecute.mockResolvedValue({ ok: true, data: { deleteNib: true } });
      const view = makeView({});
      renderView(view, confirmDialog);
      await openMenu();
      await user.click(screen.getByTestId("anv-menu-delete"));
      await confirmDialog.lastOpts!.action();
      await waitFor(() => {
        expect(mockExecute).toHaveBeenCalledWith(expect.objectContaining({ kind: "delete-nib", id: "nibs-1t4t" }));
        expect(view.requestClose).toHaveBeenCalled();
      });
    });
  });

  describe("relationships", () => {
    it("renders the parent relationship group with a clickable link", async () => {
      const view = makeView({});
      renderView(view, confirmDialog);
      const parentGroup = screen.getByTestId("anv-related-parent");
      const link = parentGroup.querySelector('[data-testid="detail-related-link"]') as HTMLElement;
      expect(link).toHaveTextContent("Web UI Polish");
      await user.click(link);
      expect(view.open).toHaveBeenCalledWith("nibs-9kvw");
    });

    it("renders the documents list when documents are present", () => {
      const detail = { nib: makeDetailNib({ documents: ["src/main.ts", "docs/README.md"] }), fetching: false };
      renderView(makeView({ detail }), confirmDialog);
      const docs = screen.getAllByTestId("anv-document");
      expect(docs).toHaveLength(2);
      expect(docs[0]).toHaveTextContent("src/main.ts");
    });
  });

  describe("gone (deleted while viewing)", () => {
    it("shows the deleted notice and fully disables editing", () => {
      // Dirty form so Save/Discard aren't disabled purely by the clean-guard —
      // the `gone` state alone must keep them disabled.
      const form = makeEditForm({ dirty: true });
      renderView(makeView({ kind: "gone", form }), confirmDialog);
      expect(screen.getByTestId("anv-deleted-notice")).toHaveTextContent("This nib was deleted");
      expect(screen.getByTestId("anv-title")).toBeDisabled();
      expect(screen.getByTestId("anv-status")).toBeDisabled();
      expect(screen.getByTestId("anv-type")).toBeDisabled();
      // Body editing, Save, and Discard must all be disabled even when dirty,
      // so a deleted nib can never fire an update against a nib that is gone.
      expect(screen.getByTestId("anv-edit-toggle")).toBeDisabled();
      expect(screen.getByTestId("anv-save")).toBeDisabled();
      expect(screen.getByTestId("anv-discard")).toBeDisabled();
    });
  });

  describe("creating", () => {
    it("shows a Create primary button, no overflow menu, and no relationships", () => {
      const form = makeCreateForm({ title: "New nib", dirty: true });
      renderView(makeView({ kind: "creating", form, detail: null }), confirmDialog);

      expect(screen.getByTestId("anv-save")).toHaveTextContent("Create");
      expect(screen.queryByTestId("anv-overflow")).not.toBeInTheDocument();
      expect(screen.queryByTestId("anv-related-section")).not.toBeInTheDocument();
      expect(screen.queryByTestId("anv-deleted-notice")).not.toBeInTheDocument();
    });

    it("opens the body in edit mode by default (editor visible, not the preview-only prose)", () => {
      const form = makeCreateForm({ title: "New nib", dirty: true });
      renderView(makeView({ kind: "creating", form, detail: null }), confirmDialog);

      // The markdown editor is shown immediately so the user can type the body.
      expect(screen.getByTestId("anv-editor-container")).toBeInTheDocument();
      expect(screen.getByTestId("anv-edit-toggle")).toHaveAttribute("aria-pressed", "true");
      // The preview-only prose block (edit mode off) is not the body content.
      expect(screen.queryByTestId("anv-body-prose")).not.toBeInTheDocument();
    });

    it("still lets the user toggle a new nib's body back to preview", async () => {
      const form = makeCreateForm({ title: "New nib", dirty: true });
      renderView(makeView({ kind: "creating", form, detail: null }), confirmDialog);

      expect(screen.getByTestId("anv-editor-container")).toBeInTheDocument();

      await user.click(screen.getByTestId("anv-edit-toggle"));
      expect(screen.queryByTestId("anv-editor-container")).not.toBeInTheDocument();
      expect(screen.getByTestId("anv-body-prose")).toBeInTheDocument();
    });
  });

  describe("conflict banner", () => {
    it("shows the external-change banner and Reload applies the remote snapshot", async () => {
      const remote = { id: "nibs-1t4t", etag: "e9" };
      const form = makeEditForm({ externalChange: remote });
      renderView(makeView({ form }), confirmDialog);

      expect(screen.getByTestId("anv-conflict-banner")).toBeInTheDocument();
      await user.click(screen.getByTestId("anv-conflict-reload"));
      expect(form.applyExternal).toHaveBeenCalledWith(remote);
    });

    it("Save on a conflict prompts to overwrite and re-saves with overwrite on confirm", async () => {
      const remote = { id: "nibs-1t4t", etag: "e9" };
      const save = vi.fn().mockResolvedValue(undefined);
      const form = makeEditForm({ dirty: true, save });
      const view = makeView({ form });
      // First save reports a conflict against the remote snapshot.
      view.save = vi.fn(async () => ({ kind: "conflict", remote }));
      renderView(view, confirmDialog);

      await user.click(screen.getByTestId("anv-save"));

      await waitFor(() =>
        expect(confirmDialog.showConfirm).toHaveBeenCalledWith(
          expect.objectContaining({ title: "Overwrite external changes?", variant: "warning" }),
        ),
      );

      // Confirming the overwrite re-runs EditForm.save with { overwrite: true }.
      await confirmDialog.lastOpts!.action();
      expect(save).toHaveBeenCalledWith({ overwrite: true });
    });
  });
});
