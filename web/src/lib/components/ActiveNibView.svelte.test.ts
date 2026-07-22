import { render, screen, waitFor } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { flushSync } from "svelte";
import { ACTIVE_VIEW_KEY, CONFIRM_DIALOG_KEY } from "$lib/contexts";
import type { ConfirmDialogState, ConfirmDialogOptions } from "$lib/composables/useConfirmDialog.svelte";
import { Preferences } from "$lib/preferences.svelte";
import { editNibForm } from "$lib/nibForm.svelte";
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
    saveLabel: null as string | null,
    saveAction: null as (() => void) | null,
    lastOpts: null as ConfirmDialogOptions | null,
    showConfirm: vi.fn((opts: ConfirmDialogOptions) => {
      state.open = true;
      state.lastOpts = opts;
      state.action = opts.action;
      state.saveLabel = opts.saveLabel ?? null;
      state.saveAction = opts.saveAction ?? null;
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
  setBody: (value: string, opts?: { reinitEditor?: boolean }) => void;
  addTag: (t: string) => void;
  removeTag: (t: string) => void;
  discard: ReturnType<typeof vi.fn>;
  applyExternal: ReturnType<typeof vi.fn>;
  noteExternalChange: ReturnType<typeof vi.fn>;
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
    setBody(value: string, opts?: { reinitEditor?: boolean }) {
      form.body = value;
      if (opts?.reinitEditor === true) form.bodyVersion++;
    },
    addTag(t: string) {
      if (!form.tags.includes(t)) form.tags = [...form.tags, t];
    },
    removeTag(t: string) {
      form.tags = form.tags.filter((x) => x !== t);
    },
    discard: vi.fn(),
    applyExternal: vi.fn(),
    noteExternalChange: vi.fn(),
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
    setBody(value: string, opts?: { reinitEditor?: boolean }) {
      form.body = value;
      if (opts?.reinitEditor === true) form.bodyVersion++;
    },
    addTag(t: string) {
      if (!form.tags.includes(t)) form.tags = [...form.tags, t];
    },
    removeTag(t: string) {
      form.tags = form.tags.filter((x) => x !== t);
    },
    discard: vi.fn(),
    applyExternal: vi.fn(),
    noteExternalChange: vi.fn(),
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
  externalApplied: number;
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
  noteMissing: ReturnType<typeof vi.fn>;
  dispose: ReturnType<typeof vi.fn>;
}

function makeView(opts: {
  kind?: "viewing" | "gone" | "creating";
  /** Only meaningful for `gone`; defaults to "deleted". */
  reason?: "deleted" | "archived";
  form?: FakeForm;
  detail?: { nib: unknown; fetching: boolean } | null;
  presentation?: "docked" | "expanded";
}): FakeView {
  const kind = opts.kind ?? "viewing";
  const presentation = opts.presentation ?? "docked";
  const stateObj =
    kind === "creating"
      ? { kind, defaults: { type: "task" }, presentation }
      : kind === "gone"
        ? { kind, nibId: "nibs-1t4t", presentation, reason: opts.reason ?? "deleted" }
        : { kind, nibId: "nibs-1t4t", presentation };
  const view = $state({
    state: stateObj,
    form: opts.form ?? makeEditForm(),
    detail: opts.detail ?? { nib: makeDetailNib(), fetching: false },
    isOpen: true,
    presentation: opts.presentation ?? "docked",
    blocksHistoryNav: false,
    externalApplied: 0,
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
    noteMissing: vi.fn(() => "closed"),
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

    it("disables — not merely readonlys — the title while the detail fetch is unseeded", () => {
      // `readonly` earns its place only where there are unsaved edits to select
      // and copy (see the gone tests). An unseeded title is an empty placeholder
      // with nothing to recover, so it keeps plain `disabled` and stays
      // unfocusable: `readonly` would let a click park a caret in it, and because
      // the input is patched in place rather than remounted, the seed landing
      // under that caret would splice the user's keystrokes into the
      // freshly-seeded title.
      renderView(makeView({ form: makeEditForm({ title: "" }), detail: { nib: null, fetching: true } }), confirmDialog);

      const title = screen.getByTestId("anv-title");
      expect(title).toBeDisabled();
      expect(title).not.toHaveAttribute("readonly");
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

  describe("relation badges (header)", () => {
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

    it("shows the blocking pill (default emphasis) when detailNib.blocking is non-empty", () => {
      const detail = {
        nib: makeDetailNib({ blocking: [ref("nibs-c1", "Blocked child", "todo", "task")] }),
        fetching: false,
      };
      renderView(makeView({ detail }), confirmDialog);

      const badge = screen.getByTestId("blocking-badge");
      expect(badge).toBeInTheDocument();
      expect(badge).toHaveTextContent("Blocking");
      expect(badge).toHaveAttribute("title", "Blocking 1 nib(s)");
    });

    it("hides the blocking badge when blocking is empty", () => {
      // The default detail nib has blocking: [].
      renderView(makeView({}), confirmDialog);
      expect(screen.queryByTestId("blocking-badge")).not.toBeInTheDocument();
      expect(screen.queryByTestId("blocking-icon")).not.toBeInTheDocument();
    });

    it("renders the bare link icon (no pill) for blocking when blockedEmphasis is 'subtle'", () => {
      const detail = {
        nib: makeDetailNib({ blocking: [ref("nibs-c1", "Blocked child", "todo", "task")] }),
        fetching: false,
      };
      renderView(makeView({ detail }), confirmDialog, { blockedEmphasis: "subtle" });

      expect(screen.getByTestId("blocking-icon")).toBeInTheDocument();
      expect(screen.queryByTestId("blocking-badge")).not.toBeInTheDocument();
    });

    it("does not show the blocked or blocking badges while creating, even with a populated detail (isCreating gate)", () => {
      // Feed a detail that is BOTH blocked and blocking so both counts are > 0;
      // then only the `!isCreating` gate suppresses the badges. Removing that
      // gate makes this test fail — guarding against a vacuous pass on an empty
      // detail (where blockedByCount/blockingCount are 0 regardless of the gate).
      const form = makeCreateForm({ title: "New nib", dirty: true });
      const detail = {
        nib: makeDetailNib({
          blockedBy: [ref("nibs-b1", "Blocker", "todo", "task")],
          blocking: [ref("nibs-c1", "Blocked child", "todo", "task")],
        }),
        fetching: false,
      };
      renderView(makeView({ kind: "creating", form, detail }), confirmDialog);
      expect(screen.queryByTestId("blocked-badge")).not.toBeInTheDocument();
      expect(screen.queryByTestId("blocked-icon")).not.toBeInTheDocument();
      expect(screen.queryByTestId("blocking-badge")).not.toBeInTheDocument();
      expect(screen.queryByTestId("blocking-icon")).not.toBeInTheDocument();
    });

    it("shows both the blocked and blocking badges for a nib that is simultaneously blocked and blocking", () => {
      const detail = {
        nib: makeDetailNib({
          blockedBy: [ref("nibs-b1", "Blocker", "todo", "task")],
          blocking: [ref("nibs-c1", "Blocked child", "todo", "task")],
        }),
        fetching: false,
      };
      renderView(makeView({ detail }), confirmDialog);
      expect(screen.getByTestId("blocked-badge")).toBeInTheDocument();
      expect(screen.getByTestId("blocking-badge")).toBeInTheDocument();
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

  describe("task-list checkboxes (click to toggle, persist on save)", () => {
    // Uses a REAL EditForm so `dirty`/`discard`/`body` behave authentically —
    // the checkbox flip must mark the buffer dirty exactly like typing does.
    function realEditForm(body: string) {
      return editNibForm(
        { mutations: { execute: mockExecute } } as never,
        {
          id: "nibs-1t4t",
          title: "Task nib",
          status: "todo",
          type: "task",
          priority: "normal",
          estimate: "m",
          tags: [],
          body,
          etag: "e0",
        },
      );
    }

    function renderWithForm(form: unknown) {
      const detail = { nib: makeDetailNib({ body: "" }), fetching: false };
      return renderView(makeView({ form: form as FakeForm, detail }), confirmDialog);
    }

    it("renders enabled checkboxes with data-task-ordinal in the preview prose", () => {
      const form = realEditForm("- [ ] one\n- [x] two");
      renderWithForm(form);

      const prose = screen.getByTestId("anv-body-prose");
      const boxes = prose.querySelectorAll<HTMLInputElement>('input[type="checkbox"][data-task-ordinal]');
      expect(boxes).toHaveLength(2);
      expect(boxes[0].disabled).toBe(false);
      expect(boxes[0].getAttribute("data-task-ordinal")).toBe("0");
      expect(boxes[1].getAttribute("data-task-ordinal")).toBe("1");
    });

    it("clicking a checkbox flips the matching source line and marks the buffer dirty (no save)", async () => {
      const form = realEditForm("- [ ] first\n- [ ] second\n- [ ] third");
      const view = makeView({ form: form as unknown as FakeForm, detail: { nib: makeDetailNib({ body: "" }), fetching: false } });
      renderView(view, confirmDialog);
      expect(form.dirty).toBe(false);

      const prose = screen.getByTestId("anv-body-prose");
      const box = prose.querySelector('input[data-task-ordinal="1"]') as HTMLInputElement;
      await user.click(box);

      expect(form.body).toBe("- [ ] first\n- [x] second\n- [ ] third");
      expect(form.dirty).toBe(true);
      // Buffered edit only — nothing is persisted until Save.
      expect(mockExecute).not.toHaveBeenCalled();
      expect(view.save).not.toHaveBeenCalled();
    });

    it("flipping a checkbox updates body + dirty WITHOUT bumping bodyVersion (no editor remount)", async () => {
      const form = realEditForm("- [ ] a\n- [ ] b");
      const v0 = form.bodyVersion;
      renderWithForm(form);

      const prose = screen.getByTestId("anv-body-prose");
      await user.click(prose.querySelector('input[data-task-ordinal="0"]') as HTMLInputElement);

      expect(form.body).toBe("- [x] a\n- [ ] b");
      expect(form.dirty).toBe(true);
      // No remount: the flip uses setBody's in-place default (no
      // bodyVersion bump), so an open editor pane keeps its undo history / cursor /
      // scroll — MarkdownEditor syncs the change via a minimal-diff transaction.
      expect(form.bodyVersion).toBe(v0);
    });

    it("maps ordinals by position with duplicate lines (no text drift)", async () => {
      const form = realEditForm("- [ ] same\n- [ ] same\n- [ ] same");
      renderWithForm(form);

      const prose = screen.getByTestId("anv-body-prose");
      await user.click(prose.querySelector('input[data-task-ordinal="2"]') as HTMLInputElement);
      expect(form.body).toBe("- [ ] same\n- [ ] same\n- [x] same");
    });

    it("re-renders the preview with the flipped checkbox checked", async () => {
      const form = realEditForm("- [ ] toggle me");
      renderWithForm(form);

      const prose = screen.getByTestId("anv-body-prose");
      const box = () => prose.querySelector('input[data-task-ordinal="0"]') as HTMLInputElement;
      expect(box().checked).toBe(false);

      await user.click(box());
      await waitFor(() => expect(box().checked).toBe(true));
    });

    it("Discard reverts a checkbox flip", async () => {
      const form = realEditForm("- [ ] a\n- [ ] b");
      renderWithForm(form);

      const prose = screen.getByTestId("anv-body-prose");
      await user.click(prose.querySelector('input[data-task-ordinal="0"]') as HTMLInputElement);
      expect(form.body).toBe("- [x] a\n- [ ] b");
      expect(form.dirty).toBe(true);

      await user.click(screen.getByTestId("anv-discard"));
      expect(form.body).toBe("- [ ] a\n- [ ] b");
      expect(form.dirty).toBe(false);
    });

    // The same handleProseClick is wired to two DOM sites: the preview-only prose
    // (no editor mounted) and the side-by-side preview pane (editor mounted beside
    // it). A flip must persist the same bytes from both — an unrelated view toggle
    // must not decide the body's line endings.
    it("persists the SAME body for a flip in preview-only and side-by-side modes (CRLF kept)", async () => {
      const crlfBody = "- [ ] a\r\n- [ ] b";

      // Preview-only: no editor exists, so nothing can rewrite the flipped body.
      const previewForm = realEditForm(crlfBody);
      const { unmount } = renderWithForm(previewForm);
      await user.click(
        screen.getByTestId("anv-body-prose").querySelector('input[data-task-ordinal="0"]') as HTMLInputElement,
      );
      const previewOnlyBody = previewForm.body;
      unmount();

      // Side-by-side: identical click, but a live CodeMirror editor is mounted.
      const sideForm = realEditForm(crlfBody);
      renderWithForm(sideForm);
      await user.click(screen.getByTestId("anv-edit-toggle"));
      // The editor must have really initialized: the sync effect no-ops while
      // `view` is undefined, which would make the assertion below vacuous.
      await waitFor(() =>
        expect(screen.getByTestId("anv-editor-container").querySelector(".cm-content")).not.toBeNull(),
      );
      await user.click(
        screen.getByTestId("anv-preview-pane").querySelector('input[data-task-ordinal="0"]') as HTMLInputElement,
      );

      expect(sideForm.body).toBe(previewOnlyBody);
      expect(sideForm.body).toBe("- [x] a\r\n- [ ] b");
    });

    it("does not toggle when the nib is read-only (gone)", async () => {
      const form = realEditForm("- [ ] a");
      renderView(makeView({ kind: "gone", form: form as unknown as FakeForm }), confirmDialog);

      const prose = screen.getByTestId("anv-body-prose");
      const box = prose.querySelector('input[data-task-ordinal="0"]') as HTMLInputElement;
      await user.click(box);
      // Body unchanged: a deleted nib is read-only, so the flip is ignored.
      expect(form.body).toBe("- [ ] a");
      expect(form.dirty).toBe(false);
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

    it("New child nib calls view.startCreateChild(nibId, parentType, anchor)", async () => {
      const view = makeView({});
      renderView(view, confirmDialog);
      await openMenu();
      await user.click(screen.getByTestId("anv-menu-new-child"));
      // Third arg is the ⋯ trigger's rect (the picker anchors there).
      expect(view.startCreateChild).toHaveBeenCalledWith(
        "nibs-1t4t",
        "feature",
        expect.objectContaining({ x: expect.any(Number), y: expect.any(Number) }),
      );
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

  describe("gone (deleted or archived while viewing)", () => {
    it("shows the deleted notice and fully disables editing", () => {
      // Dirty form so Save/Discard aren't disabled purely by the clean-guard —
      // the `gone` state alone must keep them disabled.
      const form = makeEditForm({ dirty: true });
      renderView(makeView({ kind: "gone", form }), confirmDialog);
      expect(screen.getByTestId("anv-gone-notice")).toHaveTextContent("This nib was deleted");
      // The title refuses edits via `readonly`, NOT `disabled` (see the input's
      // comment). Both halves are load-bearing: dropping the attribute makes the
      // title editable, while swapping it back to `disabled` puts recovery of an
      // unsaved title back at the user agent's discretion. Whether the text is
      // genuinely *selectable* is a rendering behavior jsdom cannot answer —
      // only the attribute is asserted here.
      expect(screen.getByTestId("anv-title")).toHaveAttribute("readonly");
      expect(screen.getByTestId("anv-title")).not.toBeDisabled();
      // No readonly equivalent exists for the metadata selects — bits-ui's
      // Select takes only `disabled` — so these stay disabled.
      expect(screen.getByTestId("anv-status")).toBeDisabled();
      expect(screen.getByTestId("anv-type")).toBeDisabled();
      // Body editing, Save, and Discard must all be disabled even when dirty,
      // so a deleted nib can never fire an update against a nib that is gone.
      expect(screen.getByTestId("anv-edit-toggle")).toBeDisabled();
      expect(screen.getByTestId("anv-save")).toBeDisabled();
      expect(screen.getByTestId("anv-discard")).toBeDisabled();
    });

    it("keeps the title's value intact and uneditable while gone", async () => {
      // `readonly` must not become a license to edit: the preserved buffer is
      // shown so it can be recovered, not revised. Typing into it must not
      // reach the form (which would also mark a gone buffer newly dirty).
      const form = makeEditForm({ title: "Preserved title", dirty: true });
      renderView(makeView({ kind: "gone", form }), confirmDialog);
      const title = screen.getByTestId("anv-title") as HTMLInputElement;
      expect(title.value).toBe("Preserved title");
      await user.type(title, "XYZ");
      expect(title.value).toBe("Preserved title");
      expect(form.title).toBe("Preserved title");
    });

    it("Copy body puts the buffer's RAW markdown on the clipboard, not its rendered form", async () => {
      // The whole point of the action: while gone the body renders as HTML
      // (bodyModeEffective pins preview), so the markdown source of unsaved
      // edits is otherwise unrecoverable. It must copy form.body — the live
      // dirty buffer — verbatim, syntax intact.
      const writeText = vi.fn().mockResolvedValue(undefined);
      Object.defineProperty(navigator, "clipboard", { value: { writeText }, writable: true, configurable: true });
      const raw = "# Heading\n\nEdited [link](http://x) and **bold**.";
      const form = makeEditForm({ body: raw, dirty: true });
      renderView(makeView({ kind: "gone", form }), confirmDialog);

      await user.click(screen.getByTestId("anv-gone-copy-body"));

      // Pins the source against the rendered form: bodyHtml would arrive as
      // "<h1>Heading</h1>...", so an exact match on `raw` is what bites.
      expect(writeText).toHaveBeenCalledWith(raw);
    });

    it("Copy body names the body rather than quoting it into the toast", async () => {
      // A nib body is far too long to sit in a toast, so the id path's
      // Copied "<text>" phrasing must not be reused for it.
      const writeText = vi.fn().mockResolvedValue(undefined);
      Object.defineProperty(navigator, "clipboard", { value: { writeText }, writable: true, configurable: true });
      const raw = "x".repeat(400);
      renderView(makeView({ kind: "gone", form: makeEditForm({ body: raw }) }), confirmDialog);

      await user.click(screen.getByTestId("anv-gone-copy-body"));

      // The exact match is what bites: the id path's `Copied "<text>"` phrasing
      // would inline all 400 characters here.
      await waitFor(() => expect(mockToastSuccess).toHaveBeenCalledWith("Copied body to clipboard"));
    });

    it("offers no Copy body action when there is no body to copy", () => {
      // An empty buffer has nothing to recover; offering the action would
      // promise a copy and deliver an empty clipboard.
      renderView(makeView({ kind: "gone", form: makeEditForm({ body: "" }) }), confirmDialog);
      expect(screen.getByTestId("anv-gone-notice")).toBeInTheDocument();
      expect(screen.queryByTestId("anv-gone-copy-body")).not.toBeInTheDocument();
    });

    it("does not offer Copy body outside the gone state", () => {
      // It is the gone notice's escape hatch. A live nib's body is already
      // retrievable through the editor, so the action would be noise.
      renderView(makeView({ form: makeEditForm({ body: "some body" }) }), confirmDialog);
      expect(screen.queryByTestId("anv-gone-copy-body")).not.toBeInTheDocument();
    });

    it("does not offer Copy body for an archived nib, whose buffer still saves", () => {
      // Copy body is the escape hatch for edits with no write path. An archived
      // nib keeps a working Save in the close guard's prompt (canSaveState is
      // true for it), so offering to copy the edits out would steer the user to
      // Discard and destroy the title edits, which have no copy action.
      renderView(
        makeView({ kind: "gone", reason: "archived", form: makeEditForm({ body: "some body", dirty: true }) }),
        confirmDialog,
      );
      expect(screen.getByTestId("anv-gone-notice")).toHaveTextContent("This nib was archived");
      expect(screen.queryByTestId("anv-gone-copy-body")).not.toBeInTheDocument();
    });

    it("paints Copy body against the notice band rather than the app background", () => {
      // `outline` sets `bg-background` with no resting text color, so it would
      // inherit the notice's --destructive-foreground and render near-white on
      // near-white in Daylight (measured 1.01:1 — the control would be invisible
      // at rest). Each `dark:`/`hover:` variant needs its own override because
      // tailwind-merge keys on the modifier.
      renderView(makeView({ kind: "gone", form: makeEditForm({ body: "some body" }) }), confirmDialog);

      const classes = screen.getByTestId("anv-gone-copy-body").className.split(/\s+/);
      expect(classes).not.toContain("bg-background");
      expect(classes).not.toContain("dark:bg-input/30");
      expect(classes).not.toContain("hover:text-foreground");
      expect(classes).toContain("bg-transparent");
    });

    it("forces an OPEN editor back to preview when the nib goes away mid-edit", async () => {
      // The editor's `onsave` is the other way into handleSave, so `gone` must
      // unmount it. Reaching that needs `bodyMode` to actually be "edit" first —
      // a buffer that never left preview would assert nothing, since
      // bodyModeEffective returns "preview" either way.
      const form = makeEditForm({ body: "body text" });
      const view = makeView({ form });
      renderView(view, confirmDialog);

      await user.click(screen.getByTestId("anv-edit-toggle"));
      // The editor really mounted, so `bodyMode` is committed to "edit" and the
      // override below is what does the work.
      await waitFor(() =>
        expect(screen.getByTestId("anv-editor-container")).toBeInTheDocument(),
      );

      // The nib is deleted out from under the open editor.
      view.state = {
        kind: "gone",
        nibId: "nibs-1t4t",
        presentation: "docked",
        reason: "deleted",
      };

      // `disabled` forces bodyModeEffective to "preview" regardless of bodyMode,
      // so the editor unmounts and takes its onsave with it.
      await waitFor(() =>
        expect(screen.queryByTestId("anv-editor-container")).not.toBeInTheDocument(),
      );
      expect(screen.getByTestId("anv-gone-notice")).toBeInTheDocument();
    });

    it("hides the conflict resolver even with a pending external change (MEDIUM #3)", () => {
      // A nib deleted while the resolver was up: the deleted notice and the
      // "keep your edits or load the new version" resolver must not render
      // simultaneously (contradictory), and Overwrite must not be able to save
      // against a deleted nib. Gone → show only the deleted notice.
      const remote = { id: "nibs-1t4t", etag: "e9" };
      const form = makeEditForm({ dirty: true, externalChange: remote });
      renderView(makeView({ kind: "gone", form }), confirmDialog);

      expect(screen.getByTestId("anv-gone-notice")).toBeInTheDocument();
      expect(screen.queryByTestId("anv-conflict-banner")).not.toBeInTheDocument();
      expect(screen.queryByTestId("anv-conflict-overwrite")).not.toBeInTheDocument();
      expect(screen.queryByTestId("anv-conflict-load-theirs")).not.toBeInTheDocument();
    });

    it("says the nib was ARCHIVED — not deleted — when that is why it went away", () => {
      // The notice is the user's only explanation of why the panel went
      // read-only. An archived nib still exists in the archive, so calling it
      // deleted is simply false.
      const form = makeEditForm({ dirty: true });
      renderView(makeView({ kind: "gone", reason: "archived", form }), confirmDialog);
      expect(screen.getByTestId("anv-gone-notice")).toHaveTextContent("This nib was archived");
    });

    it("paints the archived notice as a warning and the deleted notice as an error", () => {
      // The notice's color is signal, not decoration: it sits next to copy that
      // distinguishes the two causes, so painting an archived nib — reversible,
      // still savable — in the deletion's error color contradicts the text above
      // it. The modifier carries the warning token; its absence leaves the base
      // destructive rule.
      const archived = renderView(
        makeView({ kind: "gone", reason: "archived", form: makeEditForm() }),
        confirmDialog,
      );
      expect(screen.getByTestId("anv-gone-notice")).toHaveClass("anv-gone-notice--archived");
      archived.unmount();

      renderView(makeView({ kind: "gone", reason: "deleted", form: makeEditForm() }), confirmDialog);
      expect(screen.getByTestId("anv-gone-notice")).not.toHaveClass("anv-gone-notice--archived");
    });
  });

  describe("creating", () => {
    it("shows a Create primary button, no overflow menu, and no relationships", () => {
      const form = makeCreateForm({ title: "New nib", dirty: true });
      renderView(makeView({ kind: "creating", form, detail: null }), confirmDialog);

      expect(screen.getByTestId("anv-save")).toHaveTextContent("Create");
      expect(screen.queryByTestId("anv-overflow")).not.toBeInTheDocument();
      expect(screen.queryByTestId("anv-related-section")).not.toBeInTheDocument();
      expect(screen.queryByTestId("anv-gone-notice")).not.toBeInTheDocument();
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

    it("auto-focuses the empty title input on entering the create form", async () => {
      // Both entry points (toolbar New menu and a row's add-child [+]) land the
      // view in `creating` with a fresh create form, so one assertion covers both.
      const form = makeCreateForm();
      renderView(makeView({ kind: "creating", form, detail: null }), confirmDialog);

      const title = screen.getByTestId("anv-title");
      await waitFor(() => expect(title).toHaveFocus());
    });

    it("focuses the title only once per entry — an unrelated update must not re-steal focus", async () => {
      const form = makeCreateForm();
      renderView(makeView({ kind: "creating", form, detail: null }), confirmDialog);

      const title = screen.getByTestId("anv-title") as HTMLInputElement;
      await waitFor(() => expect(title).toHaveFocus());

      // The user moves focus out of the title; then an unrelated reactive update
      // fires on the SAME create buffer (no form swap — dirty flips). Focus must
      // stay where the user put it, not snap back to the title.
      title.blur();
      expect(title).not.toHaveFocus();

      form.dirty = true;
      flushSync();
      await Promise.resolve(); // drain any queued focus microtask

      expect(title).not.toHaveFocus();
    });
  });

  describe("external-change resolver (persistent, non-modal)", () => {
    it("shows the persistent warning region with Load theirs / Overwrite when externalChange is set", () => {
      const remote = { id: "nibs-1t4t", etag: "e9" };
      const form = makeEditForm({ dirty: true, externalChange: remote });
      renderView(makeView({ form }), confirmDialog);

      const banner = screen.getByTestId("anv-conflict-banner");
      expect(banner).toBeInTheDocument();
      // It is a persistent, NON-MODAL surface (SettingsSheet idiom, F6):
      // role="dialog" + aria-modal="false", and it never routes through the
      // confirm-dialog modal — the actual proof of non-modality.
      expect(banner).toHaveAttribute("role", "dialog");
      expect(banner).toHaveAttribute("aria-modal", "false");
      expect(banner).toHaveAttribute("aria-label");
      expect(confirmDialog.showConfirm).not.toHaveBeenCalled();
      expect(screen.getByTestId("anv-conflict-load-theirs")).toBeInTheDocument();
      expect(screen.getByTestId("anv-conflict-overwrite")).toBeInTheDocument();
    });

    it("hides the warning region for a NOT-dirty buffer even if externalChange is set (F1 gate)", () => {
      // Defensive: a non-dirty buffer must never expose Overwrite (it would force
      // a stale write over the remote's newer change). The presenter also adopts
      // the remote via the clean path, but the visibility gate is belt-and-braces.
      const remote = { id: "nibs-1t4t", etag: "e9" };
      const form = makeEditForm({ dirty: false, externalChange: remote });
      renderView(makeView({ form }), confirmDialog);
      expect(screen.queryByTestId("anv-conflict-banner")).not.toBeInTheDocument();
    });

    it("disables both conflict buttons while a save is in flight, and Load theirs is a no-op — F5", async () => {
      const remote = { id: "nibs-1t4t", etag: "e9" };
      const form = makeEditForm({ dirty: true, externalChange: remote, saving: true });
      renderView(makeView({ form }), confirmDialog);

      const loadBtn = screen.getByTestId("anv-conflict-load-theirs");
      const overwriteBtn = screen.getByTestId("anv-conflict-overwrite");
      expect(loadBtn).toBeDisabled();
      expect(overwriteBtn).toBeDisabled();

      // Clicking the disabled Load-theirs must not diverge the buffer from disk.
      await user.click(loadBtn);
      expect(form.applyExternal).not.toHaveBeenCalled();
    });

    it("Overwrite failure surfaces a toast.error (handleOverwrite error path)", async () => {
      const remote = { id: "nibs-1t4t", etag: "e9" };
      const save = vi.fn().mockResolvedValue({ kind: "error", message: "disk full" });
      const form = makeEditForm({ dirty: true, externalChange: remote, save });
      renderView(makeView({ form }), confirmDialog);

      await user.click(screen.getByTestId("anv-conflict-overwrite"));
      expect(save).toHaveBeenCalledWith({ overwrite: true });
      await waitFor(() => expect(mockToastError).toHaveBeenCalledWith("disk full"));
    });

    it("Load theirs discards local edits and loads the remote snapshot", async () => {
      const remote = { id: "nibs-1t4t", etag: "e9" };
      const form = makeEditForm({ dirty: true, externalChange: remote });
      renderView(makeView({ form }), confirmDialog);

      await user.click(screen.getByTestId("anv-conflict-load-theirs"));
      expect(form.applyExternal).toHaveBeenCalledWith(remote);
    });

    it("Overwrite force-saves the local buffer over the remote (overwrite:true)", async () => {
      const remote = { id: "nibs-1t4t", etag: "e9" };
      const save = vi.fn().mockResolvedValue({ kind: "saved", snapshot: {} });
      const form = makeEditForm({ dirty: true, externalChange: remote, save });
      renderView(makeView({ form }), confirmDialog);

      await user.click(screen.getByTestId("anv-conflict-overwrite"));
      expect(save).toHaveBeenCalledWith({ overwrite: true });
      // Never a modal for this path.
      expect(confirmDialog.showConfirm).not.toHaveBeenCalled();
    });

    it("Save on a stale base routes into the SAME inline resolver, not a modal", async () => {
      const remote = { id: "nibs-1t4t", etag: "e9" };
      const form = makeEditForm({ dirty: true });
      const view = makeView({ form });
      // The save comes back a conflict (stale if-match) carrying the remote.
      view.save = vi.fn(async () => ({ kind: "conflict", remote }));
      renderView(view, confirmDialog);

      await user.click(screen.getByTestId("anv-save"));

      // No overwrite modal — the conflict is surfaced in the persistent region.
      await waitFor(() => expect(form.noteExternalChange).toHaveBeenCalledWith(remote));
      expect(confirmDialog.showConfirm).not.toHaveBeenCalledWith(
        expect.objectContaining({ title: "Overwrite external changes?" }),
      );
    });

    it("does not show the warning region for a clean buffer (no false prompt)", () => {
      const form = makeEditForm({ dirty: false, externalChange: null });
      renderView(makeView({ form }), confirmDialog);
      expect(screen.queryByTestId("anv-conflict-banner")).not.toBeInTheDocument();
    });
  });

  describe("mid-flight buffer swap (cross-nib safety)", () => {
    it("a conflict outcome that resolves AFTER the buffer swaps does not contaminate the new nib", async () => {
      // handleSave captures the form before the await; if the buffer swaps to
      // another nib during the in-flight save (popstate / syncTo guard-bypass),
      // nib A's conflict snapshot must not be attached to nib B's form (which
      // would fabricate a false resolver on B and lose B's work on Load-theirs).
      const remote = { id: "nibs-A", etag: "e9" };
      const formA = makeEditForm({ id: "nibs-A", dirty: true });
      const formB = makeEditForm({ id: "nibs-B", dirty: true });
      const view = makeView({ form: formA });
      view.save = vi.fn(async () => {
        view.form = formB; // buffer swaps to nib B before the save resolves
        return { kind: "conflict", remote };
      });
      renderView(view, confirmDialog);

      await user.click(screen.getByTestId("anv-save"));

      // Neither nib's form receives nib A's snapshot once the buffer has swapped.
      expect(formB.noteExternalChange).not.toHaveBeenCalled();
      expect(formA.noteExternalChange).not.toHaveBeenCalled();
    });

    it("a saved outcome that resolves AFTER the buffer swaps does not misname the toast", async () => {
      const formA = makeEditForm({ id: "nibs-A", dirty: true });
      const formB = makeEditForm({ id: "nibs-B", dirty: true });
      const view = makeView({ form: formA });
      view.save = vi.fn(async () => {
        view.form = formB;
        return { kind: "saved", snapshot: {} };
      });
      renderView(view, confirmDialog);

      await user.click(screen.getByTestId("anv-save"));

      // The success toast must never announce nib B's id for a save that
      // targeted nib A — the toast must use the nibId captured BEFORE the await,
      // not the live derived value, which a mid-flight swap retargets.
      expect(mockToastSuccess).not.toHaveBeenCalledWith("Updated nibs-B");
    });

    it("a `missing` overwrite that resolves AFTER a reopen does not close the freshly-reopened buffer", async () => {
      // handleOverwrite's `missing` branch must be `form === f`-guarded (mirroring
      // its own `saved` branch and useActiveView.save()'s routing). Reopen-race:
      // an in-flight Overwrite on nib A, the user Closes→Discards and reopens A as
      // a fresh PRISTINE buffer, then the stale overwrite resolves NOT_FOUND. Since
      // the reopened id equals the original, noteMissing's internal nibId guard
      // does NOT catch it — only `form === f` does. Without the guard, the stale
      // report silently closes the innocent reopened buffer and desyncs the URL.
      const remote = { id: "nibs-A", etag: "e9" };
      const formB = makeEditForm({ id: "nibs-A", dirty: true });
      const view = makeView({
        form: makeEditForm({
          id: "nibs-A",
          dirty: true,
          externalChange: remote,
          save: vi.fn(async () => {
            view.form = formB; // buffer swaps to the reopened (same-id) pristine form
            return { kind: "missing" };
          }),
        }),
      });
      renderView(view, confirmDialog);

      await user.click(screen.getByTestId("anv-conflict-overwrite"));

      // The `form === f` guard suppresses the stale report — the reopened buffer
      // is never routed through noteMissing.
      expect(view.noteMissing).not.toHaveBeenCalled();
    });
  });

  describe("clean auto-apply toast", () => {
    it("fires a minor 'Nib updated' toast when the presenter rebaselines a clean buffer", async () => {
      const view = makeView({});
      renderView(view, confirmDialog);

      expect(mockToastSuccess).not.toHaveBeenCalled();
      // The presenter silently applied an incoming change and advanced the counter.
      view.externalApplied = 1;
      flushSync();

      await waitFor(() => expect(mockToastSuccess).toHaveBeenCalledWith("Nib updated"));
    });
  });
});
