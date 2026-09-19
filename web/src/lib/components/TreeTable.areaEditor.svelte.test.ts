import { fireEvent, render, screen, waitFor, within } from "@testing-library/svelte";
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

function renderAreas(areas: AreaVocabulary, nibs: TreeTableNib[] = []) {
  mockQueryStore.mockReturnValue(
    readable({ fetching: false, error: undefined, data: { nibs }, stale: false }) as any,
  );
  return render(TreeTable, {
    props: { filter: {}, viewLevel: "areas" as ViewLevel } as any,
    context: makeTestContext(new SelectionState(), new DragState(), {
      viewSpine: makeViewSpine(areas),
    }),
  });
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

    await user.click(screen.getByRole("button", { name: /declare an area/i }));
    await user.type(screen.getByTestId("area-path-input"), "web");
    await user.click(screen.getByRole("button", { name: /^declare$/i }));

    expect(dispatchedCommand()).toEqual(expect.objectContaining({ kind: "add-area", path: "web" }));
  });
});

describe("declaring a child area from a section row", () => {
  beforeEach(() => {
    mockExecute.mockReset().mockResolvedValue({ ok: true, data: {} });
    mockQueryStore.mockReset();
    mockSubscriptionStore.mockReset();
    mockSubscriptionStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any,
    );
  });

  // The schema takes the FULL path and refuses an undeclared parent, so the row
  // the user opened the panel on supplies everything before the last segment.
  // Typing a bare name and sending it as the path would declare a second ROOT
  // area that merely looks nested.
  it("assembles the full path under the parent area", async () => {
    const user = userEvent.setup();
    renderAreas(webAreas, [makeTreeTableNib({ id: "nibs-001", area: "web" })]);

    await user.click(within(sectionRow("web")).getByTestId("row-area-actions"));

    // Scoped to the panel: every nib row that can hold children carries its own
    // [+] button titled "Add child", so an unscoped name query matches those too.
    const panel = () => within(screen.getByTestId("area-row-panel"));
    await user.click(panel().getByRole("button", { name: /add child/i }));
    await user.type(panel().getByTestId("area-path-input"), "dashboard");
    await user.click(panel().getByRole("button", { name: /^declare$/i }));

    expect(dispatchedCommand()).toEqual(
      expect.objectContaining({ kind: "add-area", path: "web/dashboard" }),
    );
  });
});

describe("renaming an area", () => {
  beforeEach(() => {
    mockExecute.mockReset().mockResolvedValue({ ok: true, data: {} });
    mockQueryStore.mockReset();
    mockSubscriptionStore.mockReset();
    mockSubscriptionStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any,
    );
  });

  // `newName` is the node's own SEGMENT, and it is the only field a rename sets.
  // Sending description or color alongside it — even as "" — would CLEAR what the
  // store declares, so a rename would quietly erase a description nobody touched.
  it("sends newName alone, leaving description and color as the store declares them", async () => {
    const user = userEvent.setup();
    renderAreas(webAreas, [makeTreeTableNib({ id: "nibs-001", area: "web" })]);

    await user.click(within(sectionRow("web")).getByTestId("row-area-actions"));
    const panel = () => within(screen.getByTestId("area-row-panel"));
    await user.click(panel().getByRole("button", { name: /rename/i }));
    await user.clear(panel().getByTestId("area-name-input"));
    await user.type(panel().getByTestId("area-name-input"), "platform");
    await user.click(panel().getByRole("button", { name: /^rename$/i }));

    const cmd = dispatchedCommand() as Record<string, unknown>;
    expect(cmd).toEqual({ kind: "update-area", path: "web", newName: "platform" });
    // toEqual ignores an undefined-valued key, so absence is asserted outright.
    expect(cmd).not.toHaveProperty("description");
    expect(cmd).not.toHaveProperty("color");
  });
});

describe("retiring an area with no members", () => {
  beforeEach(() => {
    mockExecute.mockReset().mockResolvedValue({ ok: true, data: {} });
    mockQueryStore.mockReset();
    mockSubscriptionStore.mockReset();
    mockSubscriptionStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any,
    );
  });

  // A disposition says what to do with the members instead of leaving them on a
  // path nothing declares. With nothing assigned there is nothing to dispose of,
  // and the server REFUSES a disposition naming an area nothing is assigned to —
  // so sending one here would turn an ordinary retirement into a refusal.
  it("sends no disposition, because there is nothing to dispose of", async () => {
    const user = userEvent.setup();
    renderAreas(webAreas, []);

    await user.click(within(sectionRow("web")).getByTestId("row-area-actions"));
    const panel = () => within(screen.getByTestId("area-row-panel"));
    await user.click(panel().getByRole("button", { name: /remove/i }));
    await user.click(panel().getByRole("button", { name: /^retire$/i }));

    const cmd = dispatchedCommand() as Record<string, unknown>;
    expect(cmd).toEqual({ kind: "remove-area", path: "web" });
    expect(cmd).not.toHaveProperty("moveTo");
    expect(cmd).not.toHaveProperty("unassign");
  });
});

describe("retiring an area that has members", () => {
  beforeEach(() => {
    mockExecute.mockReset().mockResolvedValue({ ok: true, data: {} });
    mockQueryStore.mockReset();
    mockSubscriptionStore.mockReset();
    mockSubscriptionStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any,
    );
  });

  /**
   * Answer each query by its OPERATION NAME, so the remove form's member count
   * can differ from the table's own row list. Routing on the document's name
   * rather than on an imported constant keeps this independent of which module
   * declares it.
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

  // Two of the three sit BELOW web rather than on it, so a count that walked
  // only the direct members would say 1 where the retirement strands 3.
  const members = [
    makeTreeTableNib({ id: "nibs-001", area: "web" }),
    makeTreeTableNib({ id: "nibs-002", area: "web/ui" }),
    makeTreeTableNib({ id: "nibs-003", area: "web/ui" }),
  ];

  async function openRemove(user: ReturnType<typeof userEvent.setup>) {
    routeQueries({ AreaMembers: members }, members);
    render(TreeTable, {
      props: { filter: {}, viewLevel: "areas" as ViewLevel } as any,
      context: makeTestContext(new SelectionState(), new DragState(), {
        viewSpine: makeViewSpine(treeAreas),
      }),
    });
    await user.click(within(sectionRow("web")).getByTestId("row-area-actions"));
    const panel = () => within(screen.getByTestId("area-row-panel"));
    await user.click(panel().getByRole("button", { name: /remove/i }));
    await waitFor(() => expect(panel().getByTestId("area-remove-count")).toBeInTheDocument());
    return panel;
  }

  // The count is the whole point of asking first: the server refuses the
  // retirement while anything is assigned at or below the node, and the user
  // cannot choose a disposition sensibly without knowing how much it moves.
  it("shows how much work the retirement would strand, counting the whole subtree", async () => {
    const user = userEvent.setup();
    const panel = await openRemove(user);

    expect(panel().getByTestId("area-remove-count")).toHaveTextContent("3");
  });

  it("refuses to submit until a disposition is chosen", async () => {
    const user = userEvent.setup();
    const panel = await openRemove(user);

    await user.click(panel().getByRole("button", { name: /^retire$/i }));

    expect(mockExecute).not.toHaveBeenCalled();
  });

  it("dispatches the disposition the user chose", async () => {
    const user = userEvent.setup();
    const panel = await openRemove(user);

    await user.click(panel().getByRole("radio", { name: /clear their area/i }));
    await user.click(panel().getByRole("button", { name: /^retire$/i }));

    expect(dispatchedCommand()).toEqual({
      kind: "remove-area",
      path: "web",
      unassign: true,
    });
  });

  // The server refuses a moveTo declared at or below the node being retired —
  // that target is about to stop existing, so offering it would produce exactly
  // the stranded state the member refusal exists to prevent.
  it("does not offer the retiring area or its subtree as a destination", async () => {
    const user = userEvent.setup();
    const panel = await openRemove(user);

    expect(panel().getByRole("radio", { name: /docs/i })).toBeInTheDocument();
    expect(panel().queryByRole("radio", { name: /web\/ui/i })).not.toBeInTheDocument();
    expect(panel().queryByRole("radio", { name: /move them to web$/i })).not.toBeInTheDocument();
  });
});

describe("retiring an area whose member count cannot be read", () => {
  beforeEach(() => {
    mockExecute.mockReset().mockResolvedValue({ ok: true, data: {} });
    mockQueryStore.mockReset();
    mockSubscriptionStore.mockReset();
    mockSubscriptionStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any,
    );
  });

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

  async function openRemove(user: ReturnType<typeof userEvent.setup>, state: Record<string, unknown>) {
    routeMembers(state);
    render(TreeTable, {
      props: { filter: {}, viewLevel: "areas" as ViewLevel } as any,
      context: makeTestContext(new SelectionState(), new DragState(), {
        viewSpine: makeViewSpine(treeAreas),
      }),
    });
    await user.click(within(sectionRow("web")).getByTestId("row-area-actions"));
    const panel = () => within(screen.getByTestId("area-row-panel"));
    await user.click(panel().getByRole("button", { name: /remove/i }));
    return panel;
  }

  /** A urql CombinedError as the read path delivers it. */
  const boom = {
    message: "[GraphQL] the store could not be read",
    graphQLErrors: [{ message: "the store could not be read" }],
  };

  // `?? 0` turned "the query failed" into "nothing is assigned", which is the
  // one sentence this form exists to avoid saying without knowing.
  it("says the count is unknown rather than presenting the area as empty", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const panel = await openRemove(user, {
      fetching: false,
      error: boom,
      data: undefined,
      stale: false,
    });

    await waitFor(() => expect(panel().getByTestId("area-remove-unknown")).toBeInTheDocument());
    expect(screen.queryByText(/and every area declared beneath it/i)).not.toBeInTheDocument();
    expect(panel().getByTestId("area-edit-error")).toHaveTextContent("the store could not be read");
    // The transport prefix is urql's, and must not reach the user.
    expect(screen.queryByText(/\[GraphQL\]/)).not.toBeInTheDocument();
  });

  it("leaves Retire disabled, dispatching nothing when it is clicked", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const panel = await openRemove(user, {
      fetching: false,
      error: boom,
      data: undefined,
      stale: false,
    });

    await waitFor(() => expect(panel().getByTestId("area-remove-unknown")).toBeInTheDocument());
    const retire = panel().getByRole("button", { name: /^retire$/i });
    expect(retire).toBeDisabled();
    await user.click(retire);
    expect(mockExecute).not.toHaveBeenCalled();
  });

  // A partial result carries data AND an error: the list may be short, and a
  // count that is short understates exactly the work the retirement strands.
  it("treats a partial result as unknown, not as the count it happens to carry", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const panel = await openRemove(user, {
      fetching: false,
      error: boom,
      data: { nibs: [makeTreeTableNib({ id: "nibs-001", area: "web" })] },
      stale: false,
    });

    await waitFor(() => expect(panel().getByTestId("area-remove-unknown")).toBeInTheDocument());
    expect(panel().queryByTestId("area-remove-count")).not.toBeInTheDocument();
    expect(panel().getByRole("button", { name: /^retire$/i })).toBeDisabled();
  });

  // The same absence of knowledge with no error to explain it: a settled query
  // that carried no data at all must not read as an empty area either.
  it("treats a settled query that carried no data as unknown", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const panel = await openRemove(user, {
      fetching: false,
      error: undefined,
      data: undefined,
      stale: false,
    });

    await waitFor(() => expect(panel().getByTestId("area-remove-unknown")).toBeInTheDocument());
    expect(panel().getByRole("button", { name: /^retire$/i })).toBeDisabled();
    expect(panel().queryByTestId("area-edit-error")).not.toBeInTheDocument();
  });

  // Asked network-only because urql's document cache never invalidates a result
  // that came back EMPTY: an empty list carries no Nib typename for a later nib
  // mutation to match, so a cached zero would arm the no-disposition retirement
  // on an area that has gained members since.
  it("asks the server for the count rather than accepting a cached one", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    await openRemove(user, { fetching: false, error: undefined, data: { nibs: [] }, stale: false });

    const call = mockQueryStore.mock.calls.find(
      (args: any) => args?.[0]?.query?.definitions?.[0]?.name?.value === "AreaMembers",
    );
    expect(call?.[0]).toEqual(expect.objectContaining({ requestPolicy: "network-only" }));
  });

  // A failed query being retried is still in flight, so it reads as checking
  // rather than as a settled refusal.
  it("still reads as checking while a failed query refetches", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const panel = await openRemove(user, {
      fetching: true,
      error: boom,
      data: undefined,
      stale: false,
    });

    expect(panel().queryByTestId("area-remove-unknown")).not.toBeInTheDocument();
    expect(panel().queryByRole("button", { name: /^retire$/i })).not.toBeInTheDocument();
    expect(screen.getByText(/checking what is assigned/i)).toBeInTheDocument();
  });
});

describe("an edit the server refuses", () => {
  beforeEach(() => {
    mockExecute.mockReset();
    mockQueryStore.mockReset();
    mockSubscriptionStore.mockReset();
    mockSubscriptionStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any,
    );
  });

  /** A rejected ARGUMENT: no extensions code, repaired by changing the input. */
  const refusal = {
    ok: false,
    error: '[GraphQL] area "platform" already exists',
  };

  async function attemptRename(user: ReturnType<typeof userEvent.setup>) {
    renderAreas(webAreas, [makeTreeTableNib({ id: "nibs-001", area: "web" })]);
    await user.click(within(sectionRow("web")).getByTestId("row-area-actions"));
    const panel = () => within(screen.getByTestId("area-row-panel"));
    await user.click(panel().getByRole("button", { name: /rename/i }));
    await user.clear(panel().getByTestId("area-name-input"));
    await user.type(panel().getByTestId("area-name-input"), "platform");
    await user.click(panel().getByRole("button", { name: /^rename$/i }));
    return panel;
  }

  // The message is the part that says what is on disk and what to do about it,
  // so it has to reach the user — but urql's "[GraphQL] " transport prefix must
  // not, and this surface is where the user is most likely to read closely.
  it("shows the server's sentence inline, without the transport prefix", async () => {
    const user = userEvent.setup();
    mockExecute.mockResolvedValue(refusal);
    const panel = await attemptRename(user);

    await waitFor(() =>
      expect(panel().getByTestId("area-edit-error")).toHaveTextContent(
        'area "platform" already exists',
      ),
    );
    expect(screen.queryByText(/\[GraphQL\]/)).not.toBeInTheDocument();
  });

  // A rejected argument is repaired by CHANGING the argument, so the form stays
  // open with what was typed still in it rather than closing over the failure.
  it("keeps the form open and the declared vocabulary unchanged", async () => {
    const user = userEvent.setup();
    mockExecute.mockResolvedValue(refusal);
    const panel = await attemptRename(user);

    await waitFor(() => expect(panel().getByTestId("area-edit-error")).toBeInTheDocument());
    expect(panel().getByTestId("area-name-input")).toHaveValue("platform");
    // The row still reads "web": nothing was spliced into the local vocabulary.
    expect(within(sectionRow("web")).getByTestId("title-text")).toHaveTextContent("web");
  });

  // Without suppression the dispatcher raises its own toast, so the user reads
  // the same refusal twice — once transient and once inline.
  it("suppresses the dispatcher's toast, since this surface owns the message", async () => {
    const user = userEvent.setup();
    mockExecute.mockResolvedValue(refusal);
    await attemptRename(user);

    expect(mockExecute.mock.calls[0][1]).toEqual({ suppressToast: true });
  });
});

describe("a successful edit that still owes the user a message", () => {
  beforeEach(() => {
    mockExecute.mockReset();
    mockToastWarning.mockReset();
    mockQueryStore.mockReset();
    mockSubscriptionStore.mockReset();
    mockSubscriptionStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any,
    );
  });

  // The one note the server sends today: the store's areas.yml was a symlink and
  // the edit replaced it with a regular file.
  const note =
    "areas.yml was a symbolic link and has been replaced with a regular file";

  async function renameSucceedingWith(
    user: ReturnType<typeof userEvent.setup>,
    notes: string[],
  ) {
    mockExecute.mockResolvedValue({
      ok: true,
      data: { updateArea: { config: {}, notes } },
    });
    renderAreas(webAreas, [makeTreeTableNib({ id: "nibs-001", area: "web" })]);
    await user.click(within(sectionRow("web")).getByTestId("row-area-actions"));
    const panel = () => within(screen.getByTestId("area-row-panel"));
    await user.click(panel().getByRole("button", { name: /rename/i }));
    await user.clear(panel().getByTestId("area-name-input"));
    await user.type(panel().getByTestId("area-name-input"), "platform");
    await user.click(panel().getByRole("button", { name: /^rename$/i }));
  }

  // The edit SUCCEEDED, so there is no error to show and the form closes — but
  // every note is something the caller has to act on, so it has to outlive the
  // form. It must also not time out on its own: a notice that disappears is one
  // the user can miss entirely.
  it("surfaces a note that arrives with a successful edit, and does not auto-dismiss it", async () => {
    const user = userEvent.setup();
    await renameSucceedingWith(user, [note]);

    await waitFor(() =>
      expect(mockToastWarning).toHaveBeenCalledWith(
        note,
        expect.objectContaining({ duration: Infinity }),
      ),
    );
  });

  it("surfaces every note, not just the first", async () => {
    const user = userEvent.setup();
    await renameSucceedingWith(user, [note, "a second thing to act on"]);

    await waitFor(() => expect(mockToastWarning).toHaveBeenCalledTimes(2));
  });

  // Notes are empty for an ordinary edit, which must stay silent — otherwise the
  // channel reserved for "you must act on this" becomes routine noise.
  it("says nothing when the edit carried no notes", async () => {
    const user = userEvent.setup();
    await renameSucceedingWith(user, []);

    await waitFor(() => expect(screen.queryByTestId("area-row-panel")).not.toBeInTheDocument());
    expect(mockToastWarning).not.toHaveBeenCalled();
  });
});

describe("a change another process made to the vocabulary", () => {
  beforeEach(() => {
    mockExecute.mockReset().mockResolvedValue({ ok: true, data: {} });
    mockToastWarning.mockReset();
    mockQueryStore.mockReset();
    mockSubscriptionStore.mockReset();
    mockSubscriptionStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any,
    );
  });

  const webOnly = createAreaVocabulary([
    { path: "web", name: "web", description: "", color: "", depth: 0 },
  ]);
  const webAndDocs = createAreaVocabulary([
    { path: "docs", name: "docs", description: "", color: "", depth: 0 },
    { path: "web", name: "web", description: "", color: "", depth: 0 },
  ]);

  /**
   * App hands the spine down as a GETTER over a `$derived`, so its identity
   * changes when `configChanged` delivers a new config. Mirrored here with a
   * reactive holder: a component that read the vocabulary once, at mount, still
   * satisfies a plain closure and would pass without this.
   */
  it("reaches an already-open editor, with no refresh path of its own", async () => {
    const user = userEvent.setup();
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

    await user.click(within(sectionRow("web")).getByTestId("row-area-actions"));
    const panel = () => within(screen.getByTestId("area-row-panel"));
    await user.click(panel().getByRole("button", { name: /remove/i }));
    await waitFor(() => expect(panel().getByTestId("area-remove-count")).toBeInTheDocument());

    // Nothing to move the members to yet: web is the only declared area, and it
    // is the one being retired.
    expect(panel().queryByRole("radio", { name: /move them to docs/i })).not.toBeInTheDocument();

    // Another process declares `docs`; the config push replaces the vocabulary.
    // The panel is NOT remounted by this — it is keyed by path, and the path is
    // unchanged — so the open form has to be reading the vocabulary live.
    holder.spine = makeViewSpine(webAndDocs);

    await waitFor(() =>
      expect(panel().getByRole("radio", { name: /move them to docs/i })).toBeInTheDocument(),
    );
  });

  // The same push, seen by the table behind the panel: a newly declared area
  // gets its own section row without anything asking for a reload.
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

describe("an area whose panel is open going away underneath it", () => {
  beforeEach(() => {
    mockExecute.mockReset().mockResolvedValue({ ok: true, data: {} });
    mockQueryStore.mockReset();
    mockSubscriptionStore.mockReset();
    mockSubscriptionStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any,
    );
  });

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

  /**
   * Open the panel on `web`, over a spine the caller then replaces — the shape a
   * config push takes, since App hands the spine down as a getter.
   */
  async function openPanelOverSpine(
    user: ReturnType<typeof userEvent.setup>,
    initial: AreaVocabulary,
  ) {
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

    await user.click(within(sectionRow("web")).getByTestId("row-area-actions"));
    await waitFor(() => expect(screen.getByTestId("area-row-panel")).toBeInTheDocument());
    return holder;
  }

  // The one dismissal the Popover cannot answer: it involves neither a pointer
  // nor a keystroke. Retiring the area destroys the row and the [⋯] it is
  // anchored to, and bits-ui answers a vanished anchor by HIDING its wrapper —
  // so without this the panel stays mounted and invisible, holding whatever
  // draft or disposition the user had chosen.
  it("closes the panel when the push retires the area it was opened on", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const holder = await openPanelOverSpine(user, webAndDocs);

    holder.spine = makeViewSpine(docsOnly);

    await waitFor(() => expect(screen.queryByTestId("area-row-panel")).not.toBeInTheDocument());
  });

  // A rename is the same loss seen from the other side: the path the panel acts
  // on is gone, and every form in it sends that path.
  it("closes the panel when the push renames the area out from under it", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const holder = await openPanelOverSpine(user, webAndDocs);

    holder.spine = makeViewSpine(platformAndDocs);

    await waitFor(() => expect(screen.queryByTestId("area-row-panel")).not.toBeInTheDocument());
  });

  // The other direction, and the reason the check reads the vocabulary rather
  // than the anchor node: a push replaces the spine wholesale, and an open panel
  // on an area the push still declares must survive it.
  it("leaves the panel open when the push declares another area", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const holder = await openPanelOverSpine(user, webOnly);

    holder.spine = makeViewSpine(webAndDocs);

    await waitFor(() => expect(sectionRow("docs")).toBeInTheDocument());
    expect(screen.getByTestId("area-row-panel")).toBeInTheDocument();
  });

  // The nearest miss to a spurious close: a push that retires a DIFFERENT area
  // destroys that row and rebuilds the table, leaving the open panel's own area
  // declared.
  it("leaves the panel open when the push retires a different area", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const holder = await openPanelOverSpine(user, webAndDocs);

    holder.spine = makeViewSpine(webOnly);

    await waitFor(() => expect(() => sectionRow("docs")).toThrow());
    expect(screen.getByTestId("area-row-panel")).toBeInTheDocument();
  });
});

describe("backing out of the area row panel", () => {
  beforeEach(() => {
    mockExecute.mockReset().mockResolvedValue({ ok: true, data: {} });
    mockQueryStore.mockReset();
    mockSubscriptionStore.mockReset();
    mockSubscriptionStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any,
    );
  });

  /** Open the panel on the "web" section row and hand back its trigger. */
  async function openPanel(user: ReturnType<typeof userEvent.setup>) {
    renderAreas(webAreas, [makeTreeTableNib({ id: "nibs-001", area: "web" })]);
    const trigger = within(sectionRow("web")).getByTestId("row-area-actions");
    await user.click(trigger);
    await waitFor(() => expect(screen.getByTestId("area-row-panel")).toBeInTheDocument());
    return trigger;
  }

  // A user who opens the menu and changes their mind must be able to leave. With
  // no dismissal the panel stays pinned over the table for the rest of the page's
  // life, because every form closes it only after a mutation LANDS.
  it("closes on Escape, and the [⋯] opens it again", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const trigger = await openPanel(user);

    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByTestId("area-row-panel")).not.toBeInTheDocument());

    // Re-opening is what proves the TABLE forgot the panel. bits-ui unmounts its
    // own content either way, so a dismissal that left `areaPanel` set would look
    // identical at this point — until the next [⋯] click toggled that stale
    // entry closed and opened nothing.
    await user.click(trigger);
    await waitFor(() => expect(screen.getByTestId("area-row-panel")).toBeInTheDocument());
  });

  // The pointerdown carries coordinates on purpose. bits-ui decides "outside"
  // with rect math — `isClickTrulyOutside` compares clientX/clientY against the
  // layer's getBoundingClientRect() — and jsdom reports every rect as 0x0 at the
  // origin, so the (0,0) a synthesized click carries reads as INSIDE the panel
  // and dismisses nothing. The gesture is real in a browser; only the geometry
  // has to be supplied here.
  it("closes when a pointer interaction lands outside it, and the [⋯] opens it again", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const trigger = await openPanel(user);

    await fireEvent.pointerDown(document.body, { clientX: 500, clientY: 500 });
    await waitFor(() => expect(screen.queryByTestId("area-row-panel")).not.toBeInTheDocument());

    // As above: the reopen is the half that reaches the table's own state.
    await user.click(trigger);
    await waitFor(() => expect(screen.getByTestId("area-row-panel")).toBeInTheDocument());
  });

  // The [⋯] is the one gesture a user reaches for to get the menu back, so it has
  // to answer rather than look like it did nothing.
  it("closes when the same row's [⋯] is clicked again", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const trigger = await openPanel(user);

    await user.click(trigger);

    await waitFor(() => expect(screen.queryByTestId("area-row-panel")).not.toBeInTheDocument());
  });

  // The [⋯] of ANOTHER row is both an outside interaction and an open request.
  // It has to re-point the panel rather than dismiss it, which is what makes the
  // gesture reach the second row's menu in one click.
  it("moves to another row's menu when that row's [⋯] is clicked", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderAreas(
      createAreaVocabulary([
        { path: "docs", name: "docs", description: "", color: "", depth: 0 },
        { path: "web", name: "web", description: "", color: "", depth: 0 },
      ]),
      [makeTreeTableNib({ id: "nibs-001", area: "web" })],
    );

    await user.click(within(sectionRow("web")).getByTestId("row-area-actions"));
    await waitFor(() => expect(screen.getByTestId("area-row-panel")).toBeInTheDocument());

    await user.click(within(sectionRow("docs")).getByTestId("row-area-actions"));

    await waitFor(() =>
      expect(within(screen.getByTestId("area-row-panel")).getByText("docs")).toBeInTheDocument(),
    );
  });

  // Cancel inside a sub-form is a step BACK, not a dismissal: the menu is where
  // the user came from, and destroying the panel instead costs them another [⋯].
  it("returns to the menu when a sub-form is canceled, leaving the panel open", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    await openPanel(user);
    const panel = () => within(screen.getByTestId("area-row-panel"));

    await user.click(panel().getByRole("button", { name: /add child/i }));
    await waitFor(() => expect(panel().getByTestId("area-declare-form")).toBeInTheDocument());
    await user.click(panel().getByRole("button", { name: /^cancel$/i }));

    await waitFor(() => expect(screen.queryByTestId("area-declare-form")).not.toBeInTheDocument());
    expect(screen.getByTestId("area-row-panel")).toBeInTheDocument();
    expect(panel().getByRole("button", { name: /add child/i })).toBeInTheDocument();
    expect(mockExecute).not.toHaveBeenCalled();
  });

  // Re-opening must present the MENU, not the sub-form the panel was left in with
  // the previous draft still in its input.
  it("re-opens on the menu rather than the sub-form it was left in", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    const trigger = await openPanel(user);

    await user.click(screen.getByTestId("area-row-panel").querySelector("button")!);
    await user.click(within(screen.getByTestId("area-row-panel")).getByRole("button", { name: /^rename$/i }));
    await waitFor(() =>
      expect(within(screen.getByTestId("area-row-panel")).getByTestId("area-name-input")).toBeInTheDocument(),
    );

    await user.click(trigger);
    await waitFor(() => expect(screen.queryByTestId("area-row-panel")).not.toBeInTheDocument());
    await user.click(trigger);
    await waitFor(() => expect(screen.getByTestId("area-row-panel")).toBeInTheDocument());

    const panel = within(screen.getByTestId("area-row-panel"));
    expect(panel.queryByTestId("area-name-input")).not.toBeInTheDocument();
    expect(panel.getByRole("button", { name: /add child/i })).toBeInTheDocument();
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

    await user.click(screen.getByRole("button", { name: /declare an area/i }));

    expect(screen.getByTestId("area-declare-form")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /switch to tree/i })).toBeInTheDocument();
  });

  // `onclose` fires only on a successful declaration, so without a cancel the
  // form cannot be closed by a user who changed their mind.
  it("returns to the empty state when the form is canceled", async () => {
    const user = userEvent.setup({ pointerEventsCheck: 0 });
    renderAreas(EMPTY_AREAS);

    await user.click(screen.getByRole("button", { name: /declare an area/i }));
    await user.click(screen.getByRole("button", { name: /^cancel$/i }));

    await waitFor(() => expect(screen.queryByTestId("area-declare-form")).not.toBeInTheDocument());
    expect(screen.getByRole("button", { name: /declare an area/i })).toBeInTheDocument();
    expect(mockExecute).not.toHaveBeenCalled();
  });
});

describe("editing an area's description and color", () => {
  beforeEach(() => {
    mockExecute.mockReset().mockResolvedValue({ ok: true, data: {} });
    mockQueryStore.mockReset();
    mockSubscriptionStore.mockReset();
    mockSubscriptionStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any,
    );
  });

  /** Open the details form on the "web" section row. */
  async function openDetails(user: ReturnType<typeof userEvent.setup>) {
    renderAreas(describedAreas, [makeTreeTableNib({ id: "nibs-001", area: "web" })]);
    await user.click(within(sectionRow("web")).getByTestId("row-area-actions"));
    const panel = () => within(screen.getByTestId("area-row-panel"));
    await user.click(panel().getByRole("button", { name: /description/i }));
    return panel;
  }

  // An untouched field must be left OUT rather than echoed back: echoing is
  // indistinguishable from an edit on the wire, and it would overwrite a change
  // another process made to the field this user never looked at.
  it("sends only the field the edit changed", async () => {
    const user = userEvent.setup();
    const panel = await openDetails(user);

    await user.clear(panel().getByTestId("area-description-input"));
    await user.type(panel().getByTestId("area-description-input"), "Everything in the browser");
    await user.click(panel().getByRole("button", { name: /^save$/i }));

    const cmd = dispatchedCommand() as Record<string, unknown>;
    expect(cmd).toEqual({
      kind: "update-area",
      path: "web",
      description: "Everything in the browser",
    });
    expect(cmd).not.toHaveProperty("color");
    expect(cmd).not.toHaveProperty("newName");
  });

  // The other half of "omitted is not empty": emptying the box is a real edit
  // that CLEARS the key, so "" has to be sent rather than treated as untouched.
  it("clears a field by sending it empty", async () => {
    const user = userEvent.setup();
    const panel = await openDetails(user);

    await user.clear(panel().getByTestId("area-description-input"));
    await user.click(panel().getByRole("button", { name: /^save$/i }));

    const cmd = dispatchedCommand() as Record<string, unknown>;
    expect(cmd).toEqual({ kind: "update-area", path: "web", description: "" });
  });

  // The server refuses an input that sets none of the three, so a save with
  // nothing edited must not become a refusal the user has to read.
  it("never reaches the wire when nothing was edited", async () => {
    const user = userEvent.setup();
    const panel = await openDetails(user);

    await user.click(panel().getByRole("button", { name: /^save$/i }));

    expect(mockExecute).not.toHaveBeenCalled();
  });
});
