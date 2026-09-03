import { render, waitFor } from "@testing-library/svelte";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import App from "./App.svelte";
import { CONFIG_QUERY, NIB_DETAIL_QUERY, REORDER_NIB_MUTATION, UPDATE_NIB_MUTATION } from "./lib/queries";
import { STORAGE_KEY } from "./lib/storage";
import {
  BACKLOG_EPIC_ID,
  BACKLOG_TASK_ID,
  dragOnto,
  MILESTONE_ID,
  QUEUED_TASK_ID,
} from "./lib/testing/dropFixtures";

// The end of the refusal seam that lives in App: a refusal carrying a remedy is
// raised as a toast WITH an action, and taking that action dispatches the write
// the plan built. `dropPlan.test.ts` owns which write that is; this owns whether
// anything renders it and runs it.
//
// `toast.error` is mocked here, so what these tests read is the CALL's options.
// What sonner does with them across a RUN of refusals — where the shared id makes
// a repeat raise a merge — is out of reach by construction, and is owned by
// `App.dropToast.test.ts` against the real `Toaster`.
//
// Guard proof (nibs-rmq6): replacing handleDrop's `action:` with a bare
// `undefined` in App.svelte fails "offers the refusal's remedy as a toast
// action" with `undefined` for the label.

// Hoisted with the mocks that read them: `vi.mock` factories run before any
// top-level binding in this file is initialized.
const { mockToastError, mutationCalls } = vi.hoisted(() => ({
  mockToastError: vi.fn(),
  mutationCalls: [] as { doc: unknown; vars: Record<string, unknown> }[],
}));

vi.mock("svelte-sonner", async () => {
  const actual = await vi.importActual<typeof import("svelte-sonner")>("svelte-sonner");
  return { ...actual, toast: { ...actual.toast, error: mockToastError } };
});

// App builds its own urql client, so the fake has to be handed over at that seam.
// Recording `mutation` is what makes the dispatched command readable here.
vi.mock("./lib/graphql", async () => {
  const actual = await vi.importActual<typeof import("./lib/graphql")>("./lib/graphql");
  return {
    ...actual,
    createClient: () => ({
      client: {
        mutation: (doc: unknown, vars: Record<string, unknown>) => {
          mutationCalls.push({ doc, vars });
          return {
            toPromise: () =>
              Promise.resolve({ data: { reorderNib: { id: vars.id } }, error: undefined }),
          };
        },
      },
      reconnect: vi.fn(),
    }),
  };
});

vi.mock("@urql/svelte", async () => {
  const { readable } = await import("svelte/store");
  const actual = await vi.importActual<typeof import("@urql/svelte")>("@urql/svelte");
  const { DROP_TEST_NIBS: nibs } = await import("./lib/testing/dropFixtures");

  const configData = readable({
    fetching: false,
    error: undefined,
    data: { config: { projectName: "test-project" } },
    stale: false,
  });
  const nibsData = Object.assign(
    readable({ fetching: false, error: undefined, data: { nibs }, stale: false }),
    { reexecute: vi.fn() },
  );
  const detailData = readable({ fetching: false, error: undefined, data: { nib: null }, stale: false });

  return {
    ...actual,
    getContextClient: vi.fn(),
    setContextClient: vi.fn(),
    queryStore: vi.fn().mockImplementation((opts: { query: unknown }) => {
      if (opts.query === CONFIG_QUERY) return configData;
      if (opts.query === NIB_DETAIL_QUERY) return detailData;
      return nibsData;
    }),
    subscriptionStore: vi
      .fn()
      .mockReturnValue(readable({ fetching: false, error: undefined, data: undefined, stale: false })),
  };
});

describe("App drop refusals", () => {
  beforeEach(() => {
    mockToastError.mockReset();
    mutationCalls.length = 0;
    window.history.replaceState(null, "", "/");
    // The membership lens is what mints a milestone queue for a header row to
    // declare, so the refusal is only reachable in this view.
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ filter: {}, viewLevel: "milestones" }));
  });

  afterEach(() => localStorage.removeItem(STORAGE_KEY));

  it("takes a drop onto the queue's header, with no refusal in the way", async () => {
    const { container } = render(App);
    await waitFor(() =>
      expect(container.querySelector(`tr[data-nib-id="${BACKLOG_TASK_ID}"]`)).not.toBeNull(),
    );

    // Aiming AT the queue names it, so the assignment is what the gesture asked
    // for and the drop performs it — no toast, no second click.
    dragOnto(container, BACKLOG_TASK_ID, MILESTONE_ID);
    await waitFor(() => expect(mutationCalls).toHaveLength(2));

    expect(mockToastError).not.toHaveBeenCalled();
    expect(mutationCalls[0]).toEqual({
      doc: UPDATE_NIB_MUTATION,
      vars: { id: "nibs-b1", input: { milestone: "nibs-m1" } },
    });
    // The front of the queue — where the remedy button used to put it, since an
    // entry indicator names no neighbour.
    expect(mutationCalls[1]).toEqual({
      doc: REORDER_NIB_MUTATION,
      vars: { id: "nibs-b1", first: true, scope: "MILESTONE" },
    });
  });

  it("offers the refusal's remedy as a toast action", async () => {
    const { container } = render(App);
    await waitFor(() =>
      expect(container.querySelector(`tr[data-nib-id="${QUEUED_TASK_ID}"]`)).not.toBeNull(),
    );

    // A position BESIDE a queue member: the gesture named a neighbour, not the
    // queue, so joining one is still an assignment rather than a move.
    dragOnto(container, BACKLOG_TASK_ID, QUEUED_TASK_ID);

    expect(mockToastError).toHaveBeenCalledTimes(1);
    const [message, options] = mockToastError.mock.calls[0];
    expect(message).toContain("the v1.0 Launch queue");
    // The label the plan wrote, spelled with the milestone's title.
    expect(options?.action?.label).toBe("Assign to v1.0 Launch");
    // One id across every refused release, so a run of them replaces rather than
    // stacks — the discipline the drag-block toast already keeps.
    expect(options?.id).toBe("drop-refusal");
  });

  it("dispatches the assignment when the action is taken", async () => {
    const { container } = render(App);
    await waitFor(() =>
      expect(container.querySelector(`tr[data-nib-id="${QUEUED_TASK_ID}"]`)).not.toBeNull(),
    );

    dragOnto(container, BACKLOG_TASK_ID, QUEUED_TASK_ID);
    const options = mockToastError.mock.calls[0][1];

    options.action.onClick(new MouseEvent("click"));
    await waitFor(() => expect(mutationCalls).toHaveLength(2));

    // The scheduling axis, not a parent link: a milestone accepts no children, so
    // an assignment is the only write that can express this drop.
    expect(mutationCalls[0]).toEqual({
      doc: UPDATE_NIB_MUTATION,
      vars: { id: "nibs-b1", input: { milestone: "nibs-m1" } },
    });
    // And the position the drop pointed at, which the assignment alone does not
    // give: the server enters a newly assigned nib last. Here that position is
    // the neighbour the gesture named, not the front.
    expect(mutationCalls[1]).toEqual({
      doc: REORDER_NIB_MUTATION,
      vars: { id: "nibs-b1", afterId: "nibs-q1", scope: "MILESTONE" },
    });
  });

  it("raises a refusal with no remedy without an action", async () => {
    const { container } = render(App);
    await waitFor(() =>
      expect(container.querySelector(`tr[data-nib-id="${QUEUED_TASK_ID}"]`)).not.toBeNull(),
    );

    // Out of the queue rather than into it. Clearing an assignment is the write
    // that would express this, and it is deliberately not offered as one click.
    dragOnto(container, QUEUED_TASK_ID, BACKLOG_EPIC_ID);

    expect(mockToastError).toHaveBeenCalledTimes(1);
    const [message, options] = mockToastError.mock.calls[0];
    // The refusal this drag reaches, named so a later hierarchy change cannot
    // quietly turn it into a different one that also carries no action.
    expect(message).toContain("clear the milestone assignment");
    expect(options?.action).toBeUndefined();
    expect(mutationCalls).toHaveLength(0);
  });
});
