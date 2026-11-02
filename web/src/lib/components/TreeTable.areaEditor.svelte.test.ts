import { render, screen, waitFor, within } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { readable } from "svelte/store";
import TreeTable from "./TreeTable.svelte";
import { createAreaVocabulary } from "../areas";
import type { TreeTableNib, ViewLevel } from "../types";
import { SelectionState } from "../selection.svelte";
import { DragState } from "../drag.svelte";
import { makeTestContext, VIEW_SPINE_KEY } from "../contexts";
import { makeViewSpine } from "../viewSpine";
import { EMPTY_AREAS } from "../areas";
import type { AreaVocabulary } from "../areas";

// The vocabulary editor's surfaces, exercised through the table that hosts them.
// The mutation STORE is mocked rather than the urql client: what these cases are
// about is which command each affordance builds, and the command is the contract
// between this layer and the dispatcher.

vi.mock("@urql/svelte", async () => {
  const actual = await vi.importActual<typeof import("@urql/svelte")>("@urql/svelte");
  return {
    ...actual,
    getContextClient: vi.fn(),
    queryStore: vi.fn(),
    subscriptionStore: vi.fn(),
  };
});

// Spread the real module: TreeTable's import graph pulls runtime values out of
// it (the ordering layer's command factories), so a bare replacement would
// leave those undefined and fail for a reason unrelated to the case.
const { mockExecute } = vi.hoisted(() => ({
  mockExecute: vi.fn().mockResolvedValue({ ok: true, data: {} }),
}));
vi.mock("$lib/mutations", async () => {
  const actual = await vi.importActual<typeof import("$lib/mutations")>("$lib/mutations");
  return {
    ...actual,
    getMutationStore: () => ({
      execute: mockExecute,
      isMutating: () => false,
      get pending() {
        return false;
      },
    }),
  };
});

// Spreads the real toast object: `warning` genuinely exists on svelte-sonner
// 1.1.1, so overriding it here replaces a real method rather than inventing one
// the app would fail on outside the test.
const { mockToastWarning } = vi.hoisted(() => ({ mockToastWarning: vi.fn() }));
vi.mock("svelte-sonner", async () => {
  const actual = await vi.importActual<typeof import("svelte-sonner")>("svelte-sonner");
  return { ...actual, toast: { ...actual.toast, warning: mockToastWarning } };
});

// jsdom has no elementFromPoint, which the drag move handler calls.
if (typeof document.elementFromPoint !== "function") {
  document.elementFromPoint = () => null;
}

import { queryStore, subscriptionStore } from "@urql/svelte";
const mockQueryStore = vi.mocked(queryStore);
const mockSubscriptionStore = vi.mocked(subscriptionStore);

/** The command passed to the first `execute` call. */
function dispatchedCommand(): unknown {
  const call = mockExecute.mock.calls[0];
  if (!call) throw new Error("no mutation was dispatched");
  return call[0];
}

function makeTreeTableNib(overrides: Partial<TreeTableNib> = {}): TreeTableNib {
  return {
    id: "nibs-abc1",
    title: "Fix login bug",
    status: "in-progress",
    type: "bug",
    priority: "high",
    estimate: "m",
    tags: [],
    createdAt: "2026-03-15T10:00:00Z",
    updatedAt: "2026-03-20T10:00:00Z",
    parentId: null,
    milestone: "",
    milestoneOrder: "",
    area: "",
    blockingIds: [],
    blockedByIds: [],
    etag: "etag-test",
    ...overrides,
  };
}

function renderAreas(
  areas: AreaVocabulary,
  nibs: TreeTableNib[] = [],
  opts: { onrowcontextmenu?: ReturnType<typeof vi.fn>; drag?: DragState } = {},
) {
  mockQueryStore.mockReturnValue(
    readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any,
  );
  return render(TreeTable, {
    props: { filter: {}, viewLevel: "areas" as ViewLevel, onrowcontextmenu: opts.onrowcontextmenu } as any,
    context: makeTestContext(new SelectionState(), opts.drag ?? new DragState(), {
      viewSpine: makeViewSpine(areas),
    }),
  });
}

/** Reset every mock to the answers most cases want. */
function resetMocks() {
  mockExecute.mockReset().mockResolvedValue({ ok: true, data: {} });
  mockToastWarning.mockReset();
  mockQueryStore.mockReset();
  mockSubscriptionStore.mockReset();
  mockSubscriptionStore.mockReturnValue(
    readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any,
  );
}

type User = ReturnType<typeof userEvent.setup>;

/** The area menu, which bits-ui portals to the document body. */
const areaMenu = () => screen.queryByTestId("area-row-menu");
/** The open form dialog, scoped so the table's own buttons cannot match. */
const dialog = () => within(screen.getByTestId("area-form-dialog"));

/** Open the area menu from the [⋯] on the section row labeled `label`. */
async function openAreaMenu(user: User, label: string): Promise<HTMLElement> {
  await user.click(within(sectionRow(label)).getByTestId("row-area-actions"));
  return screen.findByTestId("area-row-menu");
}

/** Pick `item` from the area menu of `label` and wait for the dialog it opens. */
async function openAreaDialog(user: User, label: string, item: RegExp) {
  const menu = await openAreaMenu(user, label);
  await user.click(within(menu).getByRole("menuitem", { name: item }));
  const el = await screen.findByTestId("area-form-dialog");
  // bits-ui moves focus into the dialog after it mounts; typing before that
  // lands in whichever field the deferred focus then steals.
  await waitFor(() => expect(el.contains(document.activeElement)).toBe(true));
  return dialog;
}

/** The commands of every `execute` call, in order. */
function dispatchedCommands(): Array<Record<string, unknown>> {
  return mockExecute.mock.calls.map((call) => call[0] as Record<string, unknown>);
}

/**
 * The fabricated row whose section label is `label`.
 *
 * Restricted to section rows before matching the label, because a member's Area
 * cell renders the same path as text — `getByText("web")` matches both. The
 * synthetic id is used only to tell a fabricated row from a real one; the area
 * path is never read back out of it.
 */
function sectionRow(label: string): HTMLElement {
  const rows = Array.from(document.querySelectorAll<HTMLElement>('tr[data-nib-id^="/section:"]'));
  const row = rows.find(
    (r) => within(r).queryByTestId("title-text")?.textContent?.trim() === label,
  );
  if (!row) throw new Error(`no fabricated row draws a section labeled ${label}`);
  return row;
}

const webAreas = createAreaVocabulary([
  { path: "web", name: "web", description: "", color: "", depth: 0 },
]);

/** A vocabulary whose one area already declares a description and a color. */
const describedAreas = createAreaVocabulary([
  { path: "web", name: "web", description: "The browser surface", color: "blue", depth: 0 },
]);

describe("declaring an area from the empty-areas state", () => {
  beforeEach(() => {
    mockExecute.mockReset().mockResolvedValue({ ok: true, data: {} });
    mockQueryStore.mockReset();
    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs: [] }, stale: false }) as any,
    );
    mockSubscriptionStore.mockReset();
    mockSubscriptionStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any,
    );
  });

  // The tracer: a store that declares no areas is the ordinary first call to
  // addArea, not a refusal — so this state is where the vocabulary begins, and
  // until now it only told the user to go and edit areas.yml by hand.
  it("dispatches add-area with the typed path", async () => {
    const user = userEvent.setup();
    renderAreas(EMPTY_AREAS);

    await user.click(screen.getByRole("button", { name: /create an area/i }));
    await user.type(screen.getByTestId("area-path-input"), "web");
    await user.click(screen.getByRole("button", { name: /^create$/i }));

    expect(dispatchedCommand()).toEqual(expect.objectContaining({ kind: "add-area", path: "web" }));
  });
});

describe("the area menu", () => {
  beforeEach(resetMocks);

  // `-t "[⋯] opens the area menu"` selects this: -t is a regex, and `[⋯]` matches a bare ⋯.
  it("the row's ⋯ opens the area menu", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderAreas(webAreas, [makeTreeTableNib({ id: "nibs-001", area: "web" })]);

    const menu = await openAreaMenu(user, "web");

    expect(menu).toHaveAttribute("role", "menu");
    const items = within(menu).getAllByRole("menuitem").map((item) => item.textContent?.trim());
    expect(items).toEqual(["Edit…", "Add child…", "Remove…"]);
    expect(within(menu).queryByRole("menuitem", { name: /rename|description|color/i })).toBeNull();
  });

  // Remove is the one entry that destroys something, styled as Delete is in the row menu.
  it("marks Remove… as destructive", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderAreas(webAreas, [makeTreeTableNib({ id: "nibs-001", area: "web" })]);

    const menu = await openAreaMenu(user, "web");

    expect(within(menu).getByRole("menuitem", { name: "Remove…" })).toHaveAttribute(
      "data-variant",
      "destructive",
    );
  });

  it("right-click on a declared area row opens the area menu", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const onrowcontextmenu = vi.fn();
    renderAreas(webAreas, [makeTreeTableNib({ id: "nibs-001", area: "web" })], { onrowcontextmenu });

    await user.pointer({ target: sectionRow("web"), keys: "[MouseRight]" });

    const menu = await screen.findByTestId("area-row-menu");
    const items = within(menu).getAllByRole("menuitem").map((item) => item.textContent?.trim());
    expect(items).toEqual(["Edit…", "Add child…", "Remove…"]);
    expect(onrowcontextmenu).not.toHaveBeenCalled();
  });

  // The leftover row has no declaration behind it to edit, so it keeps swallowing
  // the gesture: no area menu, no row menu, and no browser menu either.
  it("right-click on the No area row opens nothing", async () => {
    const onrowcontextmenu = vi.fn();
    const { container } = renderAreas(
      webAreas,
      [
        makeTreeTableNib({ id: "nibs-001", area: "web" }),
        makeTreeTableNib({ id: "nibs-002", area: "" }),
      ],
      { onrowcontextmenu },
    );
    const row = container.querySelector<HTMLElement>('tr[data-nib-id="/__no_area__"]');
    expect(row).toBeInTheDocument();

    const event = new MouseEvent("contextmenu", { bubbles: true, cancelable: true });
    row!.dispatchEvent(event);
    // Long enough for a menu that did open to reach the portal.
    await new Promise((resolve) => setTimeout(resolve, 50));

    expect(event.defaultPrevented).toBe(true);
    expect(areaMenu()).toBeNull();
    expect(onrowcontextmenu).not.toHaveBeenCalled();
  });

  it("right-click on a nib row in the Areas view still reaches the row menu", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const onrowcontextmenu = vi.fn();
    const { container } = renderAreas(
      webAreas,
      [makeTreeTableNib({ id: "nibs-001", area: "web" })],
      { onrowcontextmenu },
    );

    await user.pointer({
      target: container.querySelector<HTMLElement>('tr[data-nib-id="nibs-001"]')!,
      keys: "[MouseRight]",
    });

    expect(onrowcontextmenu).toHaveBeenCalledWith(
      "nibs-001",
      expect.any(MouseEvent),
      expect.objectContaining({ id: "nibs-001" }),
      expect.any(Object),
      expect.any(Function),
    );
    expect(areaMenu()).toBeNull();
  });

  it("right-click on a declared area row opens nothing while a drag is in flight", async () => {
    const drag = new DragState();
    const { container } = renderAreas(
      webAreas,
      [makeTreeTableNib({ id: "nibs-001", area: "web" })],
      { drag },
    );
    const nibRow = container.querySelector<HTMLElement>('tr[data-nib-id="nibs-001"]')!;
    nibRow.dispatchEvent(new PointerEvent("pointerdown", { clientX: 100, clientY: 100, bubbles: true, button: 0 }));
    window.dispatchEvent(new PointerEvent("pointermove", { clientX: 130, clientY: 100, bubbles: true }));
    // A gesture that never started would let this pass for an unrelated reason.
    expect(drag.isDragging).toBe(true);

    try {
      sectionRow("web").dispatchEvent(new MouseEvent("contextmenu", { bubbles: true, cancelable: true }));
      await new Promise((resolve) => setTimeout(resolve, 50));
      expect(areaMenu()).toBeNull();
    } finally {
      window.dispatchEvent(new PointerEvent("pointerup", { bubbles: true }));
    }
  });

  it("closes on Escape, and the [⋯] opens it again", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderAreas(webAreas, [makeTreeTableNib({ id: "nibs-001", area: "web" })]);

    await openAreaMenu(user, "web");
    await user.keyboard("{Escape}");
    await waitFor(() => expect(areaMenu()).toBeNull());

    // The reopen proves the table forgot the menu, not only bits-ui.
    await openAreaMenu(user, "web");
  });
});

describe("leaving an area dialog", () => {
  beforeEach(resetMocks);

  for (const [item, testId] of [
    [/edit/i, "area-edit-form"],
    [/add child/i, "area-declare-form"],
    [/remove/i, "area-remove-form"],
  ] as const) {
    it(`Cancel closes the ${testId} dialog without reopening the menu`, async () => {
      const user = userEvent.setup({ pointerEventsCheck: 0 });
      renderAreas(webAreas, [makeTreeTableNib({ id: "nibs-001", area: "web" })]);

      const d = await openAreaDialog(user, "web", item);
      expect(d().getByTestId(testId)).toBeInTheDocument();
      await user.click(d().getByRole("button", { name: /^cancel$/i }));

      await waitFor(() => expect(screen.queryByTestId("area-form-dialog")).toBeNull());
      expect(areaMenu()).toBeNull();
      expect(mockExecute).not.toHaveBeenCalled();
    });
  }

  it("Escape closes the dialog without reopening the menu", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderAreas(webAreas, [makeTreeTableNib({ id: "nibs-001", area: "web" })]);

    await openAreaDialog(user, "web", /edit/i);
    await user.keyboard("{Escape}");

    await waitFor(() => expect(screen.queryByTestId("area-form-dialog")).toBeNull());
    expect(areaMenu()).toBeNull();
  });

  // A dialog opened again starts from the store's values, not from the draft
  // the previous one was left with.
  it("re-opens Edit with the declared values, not the abandoned draft", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderAreas(webAreas, [makeTreeTableNib({ id: "nibs-001", area: "web" })]);

    let d = await openAreaDialog(user, "web", /edit/i);
    await user.type(d().getByTestId("area-name-input"), "-draft");
    await user.click(d().getByRole("button", { name: /^cancel$/i }));
    await waitFor(() => expect(screen.queryByTestId("area-form-dialog")).toBeNull());

    d = await openAreaDialog(user, "web", /edit/i);
    expect(d().getByTestId("area-name-input")).toHaveValue("web");
  });
});

describe("declaring a child area from a section row", () => {
  beforeEach(resetMocks);

  // The schema takes the FULL path and refuses an undeclared parent, so the row
  // the user opened the dialog on supplies everything before the last segment.
  // Sending a bare name would declare a second ROOT area that only looks nested.
  it("Add child declares under the area", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderAreas(webAreas, [makeTreeTableNib({ id: "nibs-001", area: "web" })]);

    const d = await openAreaDialog(user, "web", /add child/i);
    await user.type(d().getByTestId("area-path-input"), "dashboard");
    await user.click(d().getByRole("button", { name: /^create$/i }));

    expect(dispatchedCommands()).toEqual([
      expect.objectContaining({ kind: "add-area", path: "web/dashboard" }),
    ]);
    await waitFor(() => expect(screen.queryByTestId("area-form-dialog")).toBeNull());
  });
});

describe("editing an area", () => {
  beforeEach(resetMocks);

  /** Open the Edit dialog on "web", which declares a description and a color. */
  async function openEdit(user: User) {
    renderAreas(describedAreas, [makeTreeTableNib({ id: "nibs-001", area: "web" })]);
    return openAreaDialog(user, "web", /edit/i);
  }

  async function replace(user: User, input: HTMLElement, value: string) {
    await user.clear(input);
    if (value !== "") await user.type(input, value);
  }

  it("seeds each field with what the store declares", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openEdit(user);

    expect(d().getByTestId("area-name-input")).toHaveValue("web");
    expect(d().getByTestId("area-description-input")).toHaveValue("The browser surface");
    expect(d().getByRole("button", { name: "Blue" })).toHaveAttribute("aria-pressed", "true");
  });

  // The input carries all three, and the resolver applies them as one node update.
  it("Edit saves name, description and color in one updateArea call", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openEdit(user);

    await replace(user, d().getByTestId("area-name-input"), "platform");
    await replace(user, d().getByTestId("area-description-input"), "Everything in the browser");
    await user.click(d().getByRole("button", { name: "Teal" }));
    await user.click(d().getByRole("button", { name: /^save$/i }));

    expect(dispatchedCommands()).toEqual([
      {
        kind: "update-area",
        path: "web",
        newName: "platform",
        description: "Everything in the browser",
        color: "teal",
      },
    ]);
    await waitFor(() => expect(screen.queryByTestId("area-form-dialog")).toBeNull());
  });

  // An echo is indistinguishable from an edit on the wire, so it would overwrite
  // a change another process made to a field this user never touched.
  it("Edit sends only the fields that changed", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openEdit(user);

    await user.click(d().getByRole("button", { name: "Red" }));
    await user.click(d().getByRole("button", { name: /^save$/i }));

    const cmd = dispatchedCommand() as Record<string, unknown>;
    expect(cmd).toEqual({ kind: "update-area", path: "web", color: "red" });
    // toEqual ignores an undefined-valued key, so absence is asserted outright.
    expect(cmd).not.toHaveProperty("newName");
    expect(cmd).not.toHaveProperty("description");
  });

  it("sends a rename alone, leaving description and color as the store declares them", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openEdit(user);

    await replace(user, d().getByTestId("area-name-input"), "platform");
    await user.click(d().getByRole("button", { name: /^save$/i }));

    const cmd = dispatchedCommand() as Record<string, unknown>;
    expect(cmd).toEqual({ kind: "update-area", path: "web", newName: "platform" });
    expect(cmd).not.toHaveProperty("description");
    expect(cmd).not.toHaveProperty("color");
  });

  // Emptying a box is a real edit that clears the key, so "" is sent.
  it("clears a field by sending it empty", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openEdit(user);

    await replace(user, d().getByTestId("area-description-input"), "");
    await user.click(d().getByRole("button", { name: /^save$/i }));

    expect(dispatchedCommand()).toEqual({ kind: "update-area", path: "web", description: "" });
  });

  // The server refuses an input that sets nothing.
  it("Edit with nothing changed closes without a request", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openEdit(user);

    await user.click(d().getByRole("button", { name: /^save$/i }));

    await waitFor(() => expect(screen.queryByTestId("area-form-dialog")).toBeNull());
    expect(mockExecute).not.toHaveBeenCalled();
  });

  // The server refuses a rename to the current name, so padding is not a change.
  it("treats a name that only gained whitespace as unchanged", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openEdit(user);

    await replace(user, d().getByTestId("area-name-input"), "  web  ");
    await user.click(d().getByRole("button", { name: "Red" }));
    await user.click(d().getByRole("button", { name: /^save$/i }));

    expect(dispatchedCommand()).toEqual({ kind: "update-area", path: "web", color: "red" });
  });

  it("sends a changed name trimmed", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openEdit(user);

    await replace(user, d().getByTestId("area-name-input"), " platform ");
    await user.click(d().getByRole("button", { name: /^save$/i }));

    expect(dispatchedCommand()).toEqual({ kind: "update-area", path: "web", newName: "platform" });
  });

  it("refuses an empty name inline and sends nothing", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openEdit(user);

    await replace(user, d().getByTestId("area-name-input"), "   ");
    await user.click(d().getByRole("button", { name: "Red" }));
    await user.click(d().getByRole("button", { name: /^save$/i }));

    expect(d().getByTestId("area-edit-error")).toHaveTextContent(/name/i);
    expect(mockExecute).not.toHaveBeenCalled();
    expect(screen.getByTestId("area-form-dialog")).toBeInTheDocument();
  });
});

describe("retiring an area with no members", () => {
  beforeEach(resetMocks);

  // The server refuses a disposition on an area nothing is assigned to, so
  // sending one would turn an ordinary retirement into a refusal.
  it("sends no disposition, because there is nothing to dispose of", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderAreas(webAreas, []);

    const d = await openAreaDialog(user, "web", /remove/i);
    await user.click(d().getByRole("button", { name: /^retire$/i }));

    const cmd = dispatchedCommand() as Record<string, unknown>;
    expect(cmd).toEqual({ kind: "remove-area", path: "web" });
    expect(cmd).not.toHaveProperty("moveTo");
    expect(cmd).not.toHaveProperty("unassign");
    await waitFor(() => expect(screen.queryByTestId("area-form-dialog")).toBeNull());
  });
});

describe("retiring an area that has members", () => {
  beforeEach(resetMocks);

  /**
   * Answer each query by its OPERATION NAME, so the remove form's member count
   * can differ from the table's own row list.
   */
  function routeQueries(byOperation: Record<string, TreeTableNib[]>, fallback: TreeTableNib[]) {
    mockQueryStore.mockImplementation((args: any) => {
      const name = args?.query?.definitions?.[0]?.name?.value ?? "";
      const nibs = byOperation[name] ?? fallback;
      return readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any;
    });
  }

  // Server order: siblings by name, each parent immediately before its subtree.
  const treeAreas = createAreaVocabulary([
    { path: "docs", name: "docs", description: "", color: "", depth: 0 },
    { path: "web", name: "web", description: "", color: "", depth: 0 },
    { path: "web/ui", name: "ui", description: "", color: "", depth: 1 },
  ]);

  // Two of the three sit BELOW web, so a count of direct members alone says 1
  // where the retirement strands 3.
  const members = [
    makeTreeTableNib({ id: "nibs-001", area: "web" }),
    makeTreeTableNib({ id: "nibs-002", area: "web/ui" }),
    makeTreeTableNib({ id: "nibs-003", area: "web/ui" }),
  ];

  async function openRemove(user: User) {
    routeQueries({ AreaMembers: members }, members);
    render(TreeTable, {
      props: { filter: {}, viewLevel: "areas" as ViewLevel } as any,
      context: makeTestContext(new SelectionState(), new DragState(), {
        viewSpine: makeViewSpine(treeAreas),
      }),
    });
    const d = await openAreaDialog(user, "web", /remove/i);
    await waitFor(() => expect(d().getByTestId("area-remove-count")).toBeInTheDocument());
    return d;
  }

  // The server refuses the retirement while anything is assigned at or below the
  // node, and a disposition cannot be chosen sensibly without the count.
  it("shows how much work the retirement would strand, counting the whole subtree", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openRemove(user);

    expect(d().getByTestId("area-remove-count")).toHaveTextContent("3");
  });

  it("puts Cancel and Retire on one action row", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openRemove(user);

    const cancels = d().getAllByRole("button", { name: /^cancel$/i });
    expect(cancels).toHaveLength(1);
    expect(d().getByRole("button", { name: /^retire$/i }).parentElement).toBe(cancels[0].parentElement);
  });

  it("refuses to submit until a disposition is chosen", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openRemove(user);

    await user.click(d().getByRole("button", { name: /^retire$/i }));

    expect(mockExecute).not.toHaveBeenCalled();
  });

  // `disabled` is only the visible half: submit() holds the same rule.
  it("Remove refuses Retire when members exist and no disposition is picked", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openRemove(user);

    const retire = d().getByRole("button", { name: /^retire$/i });
    expect(retire).toBeDisabled();
    retire.removeAttribute("disabled");
    await user.click(retire);

    expect(dispatchedCommands().filter((cmd) => cmd.kind === "remove-area")).toEqual([]);
  });

  it("dispatches the disposition the user chose", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openRemove(user);

    await user.click(d().getByRole("radio", { name: /clear their area/i }));
    await user.click(d().getByRole("button", { name: /^retire$/i }));

    expect(dispatchedCommand()).toEqual({
      kind: "remove-area",
      path: "web",
      unassign: true,
    });
  });

  // A moveTo at or below the retiring node names a target that is about to stop
  // existing, and the server refuses it.
  it("does not offer the retiring area or its subtree as a destination", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openRemove(user);

    expect(d().getByRole("radio", { name: /docs/i })).toBeInTheDocument();
    expect(d().queryByRole("radio", { name: /web\/ui/i })).not.toBeInTheDocument();
    expect(d().queryByRole("radio", { name: /move them to web$/i })).not.toBeInTheDocument();
  });
});

describe("retiring an area whose member count cannot be read", () => {
  beforeEach(resetMocks);

  const treeAreas = createAreaVocabulary([
    { path: "docs", name: "docs", description: "", color: "", depth: 0 },
    { path: "web", name: "web", description: "", color: "", depth: 0 },
  ]);

  /** The member query answers with `state`; the table's own query answers normally. */
  function routeMembers(state: Record<string, unknown>) {
    mockQueryStore.mockImplementation((args: any) => {
      const name = args?.query?.definitions?.[0]?.name?.value ?? "";
      if (name === "AreaMembers") return readable(state) as any;
      return readable({ fetching: false, error: undefined, data: { nibs: [] }, stale: false }) as any;
    });
  }

  async function openRemove(user: User, state: Record<string, unknown>) {
    routeMembers(state);
    render(TreeTable, {
      props: { filter: {}, viewLevel: "areas" as ViewLevel } as any,
      context: makeTestContext(new SelectionState(), new DragState(), {
        viewSpine: makeViewSpine(treeAreas),
      }),
    });
    return openAreaDialog(user, "web", /remove/i);
  }

  /** A urql CombinedError as the read path delivers it. */
  const boom = {
    message: "[GraphQL] the store could not be read",
    graphQLErrors: [{ message: "the store could not be read" }],
  };

  // `?? 0` would turn "the query failed" into "nothing is assigned".
  it("says the count is unknown rather than presenting the area as empty", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openRemove(user, {
      fetching: false,
      error: boom,
      data: undefined,
      stale: false,
    });

    await waitFor(() => expect(d().getByTestId("area-remove-unknown")).toBeInTheDocument());
    expect(screen.queryByText(/and every area beneath it/i)).not.toBeInTheDocument();
    expect(d().getByTestId("area-edit-error")).toHaveTextContent("the store could not be read");
    // The transport prefix is urql's, and must not reach the user.
    expect(screen.queryByText(/\[GraphQL\]/)).not.toBeInTheDocument();
  });

  it("leaves Retire disabled, dispatching nothing when it is clicked", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openRemove(user, {
      fetching: false,
      error: boom,
      data: undefined,
      stale: false,
    });

    await waitFor(() => expect(d().getByTestId("area-remove-unknown")).toBeInTheDocument());
    const retire = d().getByRole("button", { name: /^retire$/i });
    expect(retire).toBeDisabled();
    await user.click(retire);
    expect(mockExecute).not.toHaveBeenCalled();
  });

  // A partial result's list may be short, and a short count understates the
  // work the retirement strands.
  it("treats a partial result as unknown, not as the count it happens to carry", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openRemove(user, {
      fetching: false,
      error: boom,
      data: { nibs: [makeTreeTableNib({ id: "nibs-001", area: "web" })] },
      stale: false,
    });

    await waitFor(() => expect(d().getByTestId("area-remove-unknown")).toBeInTheDocument());
    expect(d().queryByTestId("area-remove-count")).not.toBeInTheDocument();
    expect(d().getByRole("button", { name: /^retire$/i })).toBeDisabled();
  });

  it("treats a settled query that carried no data as unknown", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openRemove(user, {
      fetching: false,
      error: undefined,
      data: undefined,
      stale: false,
    });

    await waitFor(() => expect(d().getByTestId("area-remove-unknown")).toBeInTheDocument());
    expect(d().getByRole("button", { name: /^retire$/i })).toBeDisabled();
    expect(d().queryByTestId("area-edit-error")).not.toBeInTheDocument();
  });

  // urql's document cache never invalidates an EMPTY result, so a cached zero
  // would arm the no-disposition retirement on an area that has gained members.
  it("asks the server for the count rather than accepting a cached one", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    await openRemove(user, { fetching: false, error: undefined, data: { nibs: [] }, stale: false });

    const call = mockQueryStore.mock.calls.find(
      (args: any) => args?.[0]?.query?.definitions?.[0]?.name?.value === "AreaMembers",
    );
    expect(call?.[0]).toEqual(expect.objectContaining({ requestPolicy: "network-only" }));
  });

  it("still reads as checking while a failed query refetches", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const d = await openRemove(user, {
      fetching: true,
      error: boom,
      data: undefined,
      stale: false,
    });

    expect(d().queryByTestId("area-remove-unknown")).not.toBeInTheDocument();
    expect(d().queryByRole("button", { name: /^retire$/i })).not.toBeInTheDocument();
    expect(screen.getByText(/checking what is assigned/i)).toBeInTheDocument();
  });
});

describe("an edit the server refuses", () => {
  beforeEach(() => {
    resetMocks();
    mockExecute.mockReset();
  });

  /** A rejected ARGUMENT: no extensions code, repaired by changing the input. */
  const refusal = {
    ok: false,
    error: '[GraphQL] area "platform" already exists',
  };

  async function attemptRename(user: User) {
    renderAreas(webAreas, [makeTreeTableNib({ id: "nibs-001", area: "web" })]);
    const d = await openAreaDialog(user, "web", /edit/i);
    await user.clear(d().getByTestId("area-name-input"));
    await user.type(d().getByTestId("area-name-input"), "platform");
    await user.click(d().getByRole("button", { name: /^save$/i }));
    return d;
  }

  // The message says what is on disk and what to do about it; urql's
  // "[GraphQL] " transport prefix must not reach the user.
  it("shows the server's sentence inline, without the transport prefix", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    mockExecute.mockResolvedValue(refusal);
    const d = await attemptRename(user);

    await waitFor(() =>
      expect(d().getByTestId("area-edit-error")).toHaveTextContent('area "platform" already exists'),
    );
    expect(screen.queryByText(/\[GraphQL\]/)).not.toBeInTheDocument();
  });

  // A rejected argument is repaired by changing it, so the dialog stays open
  // with what was typed still in it.
  it("keeps the dialog open and the declared vocabulary unchanged", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    mockExecute.mockResolvedValue(refusal);
    const d = await attemptRename(user);

    await waitFor(() => expect(d().getByTestId("area-edit-error")).toBeInTheDocument());
    expect(d().getByTestId("area-name-input")).toHaveValue("platform");
    expect(within(sectionRow("web")).getByTestId("title-text")).toHaveTextContent("web");
  });

  // Without suppression the dispatcher toasts the refusal the form already shows.
  it("suppresses the dispatcher's toast, since this surface owns the message", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    mockExecute.mockResolvedValue(refusal);
    await attemptRename(user);

    expect(mockExecute.mock.calls[0][1]).toEqual({ suppressToast: true });
  });
});

describe("a successful edit that still owes the user a message", () => {
  beforeEach(() => {
    resetMocks();
    mockExecute.mockReset();
  });

  // The one note the server sends today.
  const note = "areas.yml was a symbolic link and has been replaced with a regular file";

  async function renameSucceedingWith(user: User, notes: string[]) {
    mockExecute.mockResolvedValue({
      ok: true,
      data: { updateArea: { config: {}, notes } },
    });
    renderAreas(webAreas, [makeTreeTableNib({ id: "nibs-001", area: "web" })]);
    const d = await openAreaDialog(user, "web", /edit/i);
    await user.clear(d().getByTestId("area-name-input"));
    await user.type(d().getByTestId("area-name-input"), "platform");
    await user.click(d().getByRole("button", { name: /^save$/i }));
  }

  // The dialog closes on success, so each note has to outlive it, and a notice
  // that times out is one the user can miss.
  it("surfaces a note that arrives with a successful edit, and does not auto-dismiss it", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    await renameSucceedingWith(user, [note]);

    await waitFor(() =>
      expect(mockToastWarning).toHaveBeenCalledWith(note, expect.objectContaining({ duration: Infinity })),
    );
  });

  it("surfaces every note, not just the first", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    await renameSucceedingWith(user, [note, "a second thing to act on"]);

    await waitFor(() => expect(mockToastWarning).toHaveBeenCalledTimes(2));
  });

  it("says nothing when the edit carried no notes", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    await renameSucceedingWith(user, []);

    await waitFor(() => expect(screen.queryByTestId("area-form-dialog")).not.toBeInTheDocument());
    expect(mockToastWarning).not.toHaveBeenCalled();
  });
});

describe("a change another process made to the vocabulary", () => {
  beforeEach(resetMocks);

  const webOnly = createAreaVocabulary([
    { path: "web", name: "web", description: "", color: "", depth: 0 },
  ]);
  const webAndDocs = createAreaVocabulary([
    { path: "docs", name: "docs", description: "", color: "", depth: 0 },
    { path: "web", name: "web", description: "", color: "", depth: 0 },
  ]);

  /**
   * App hands the spine down as a GETTER over a `$derived`, so its identity
   * changes when `configChanged` delivers a new config. A reactive holder
   * mirrors that here.
   */
  it("reaches an already-open dialog, with no refresh path of its own", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const holder = $state({ spine: makeViewSpine(webOnly) });
    const context = makeTestContext(new SelectionState(), new DragState());
    context.set(VIEW_SPINE_KEY, () => holder.spine);

    mockQueryStore.mockReturnValue(
      readable({
        fetching: false,
        error: undefined,
        data: { nibs: [makeTreeTableNib({ id: "nibs-001", area: "web" })] },
        stale: false,
      }) as any,
    );

    render(TreeTable, {
      props: { filter: {}, viewLevel: "areas" as ViewLevel } as any,
      context,
    });

    const d = await openAreaDialog(user, "web", /remove/i);
    await waitFor(() => expect(d().getByTestId("area-remove-count")).toBeInTheDocument());

    // web is the only declared area, and it is the one being retired.
    expect(d().queryByRole("radio", { name: /move them to docs/i })).not.toBeInTheDocument();

    // The push does not remount the open dialog, so the form has to read the vocabulary live.
    holder.spine = makeViewSpine(webAndDocs);

    await waitFor(() =>
      expect(d().getByRole("radio", { name: /move them to docs/i })).toBeInTheDocument(),
    );
  });

  it("adds the new area's section row to the table underneath", async () => {
    const holder = $state({ spine: makeViewSpine(webOnly) });
    const context = makeTestContext(new SelectionState(), new DragState());
    context.set(VIEW_SPINE_KEY, () => holder.spine);

    mockQueryStore.mockReturnValue(
      readable({
        fetching: false,
        error: undefined,
        data: { nibs: [makeTreeTableNib({ id: "nibs-001", area: "web" })] },
        stale: false,
      }) as any,
    );

    render(TreeTable, {
      props: { filter: {}, viewLevel: "areas" as ViewLevel } as any,
      context,
    });

    expect(() => sectionRow("docs")).toThrow();

    holder.spine = makeViewSpine(webAndDocs);

    await waitFor(() => expect(sectionRow("docs")).toBeInTheDocument());
  });
});

describe("an area going away while its menu or dialog is open", () => {
  beforeEach(resetMocks);

  const webAndDocs = createAreaVocabulary([
    { path: "docs", name: "docs", description: "", color: "", depth: 0 },
    { path: "web", name: "web", description: "", color: "", depth: 0 },
  ]);
  const docsOnly = createAreaVocabulary([
    { path: "docs", name: "docs", description: "", color: "", depth: 0 },
  ]);
  const webOnly = createAreaVocabulary([
    { path: "web", name: "web", description: "", color: "", depth: 0 },
  ]);
  const platformAndDocs = createAreaVocabulary([
    { path: "docs", name: "docs", description: "", color: "", depth: 0 },
    { path: "platform", name: "platform", description: "", color: "", depth: 0 },
  ]);

  /** Render over a spine the caller then replaces, as a config push does. */
  function renderOverSpine(initial: AreaVocabulary) {
    const holder = $state({ spine: makeViewSpine(initial) });
    const context = makeTestContext(new SelectionState(), new DragState());
    context.set(VIEW_SPINE_KEY, () => holder.spine);

    mockQueryStore.mockReturnValue(
      readable({
        fetching: false,
        error: undefined,
        data: { nibs: [makeTreeTableNib({ id: "nibs-001", area: "web" })] },
        stale: false,
      }) as any,
    );

    render(TreeTable, {
      props: { filter: {}, viewLevel: "areas" as ViewLevel } as any,
      context,
    });
    return holder;
  }

  // Every form in the dialog sends the path, and the push has retired it.
  it("a push retiring the area closes its open dialog", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const holder = renderOverSpine(webAndDocs);
    await openAreaDialog(user, "web", /edit/i);

    holder.spine = makeViewSpine(docsOnly);

    await waitFor(() => expect(screen.queryByTestId("area-form-dialog")).not.toBeInTheDocument());
  });

  it("a push renaming the area closes its open dialog", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const holder = renderOverSpine(webAndDocs);
    await openAreaDialog(user, "web", /edit/i);

    holder.spine = makeViewSpine(platformAndDocs);

    await waitFor(() => expect(screen.queryByTestId("area-form-dialog")).not.toBeInTheDocument());
  });

  // The menu hangs off the row the push destroys.
  it("a push retiring the area closes its open menu", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const holder = renderOverSpine(webAndDocs);
    await openAreaMenu(user, "web");

    holder.spine = makeViewSpine(docsOnly);

    await waitFor(() => expect(areaMenu()).toBeNull());
  });

  // A push replaces the spine wholesale, so a dialog on an area it still
  // declares must survive it.
  it("leaves the dialog open when the push declares another area", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const holder = renderOverSpine(webOnly);
    await openAreaDialog(user, "web", /edit/i);

    holder.spine = makeViewSpine(webAndDocs);

    await waitFor(() => expect(sectionRow("docs")).toBeInTheDocument());
    expect(screen.getByTestId("area-form-dialog")).toBeInTheDocument();
  });

  it("leaves the dialog open when the push retires a different area", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const holder = renderOverSpine(webAndDocs);
    await openAreaDialog(user, "web", /edit/i);

    holder.spine = makeViewSpine(webOnly);

    await waitFor(() => expect(() => sectionRow("docs")).toThrow());
    expect(screen.getByTestId("area-form-dialog")).toBeInTheDocument();
  });
});

describe("backing out of the empty-areas declare form", () => {
  beforeEach(() => {
    mockExecute.mockReset().mockResolvedValue({ ok: true, data: {} });
    mockQueryStore.mockReset();
    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs: [] }, stale: false }) as any,
    );
    mockSubscriptionStore.mockReset();
    mockSubscriptionStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any,
    );
  });

  // Opening the form must not take away the escape the user arrived with: this
  // panel is the whole view when no areas are declared.
  it("keeps Switch to Tree reachable while the form is open", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderAreas(EMPTY_AREAS);

    await user.click(screen.getByRole("button", { name: /create an area/i }));

    expect(screen.getByTestId("area-declare-form")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /switch to tree/i })).toBeInTheDocument();
  });

  // `onclose` fires only on a successful declaration, so without a cancel the
  // form cannot be closed by a user who changed their mind.
  it("returns to the empty state when the form is canceled", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderAreas(EMPTY_AREAS);

    await user.click(screen.getByRole("button", { name: /create an area/i }));
    await user.click(screen.getByRole("button", { name: /^cancel$/i }));

    await waitFor(() => expect(screen.queryByTestId("area-declare-form")).not.toBeInTheDocument());
    expect(screen.getByRole("button", { name: /create an area/i })).toBeInTheDocument();
    expect(mockExecute).not.toHaveBeenCalled();
  });
});
