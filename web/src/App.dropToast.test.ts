import { render, waitFor } from "@testing-library/svelte";
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import App from "./App.svelte";
import { CONFIG_QUERY, NIB_DETAIL_QUERY } from "./lib/queries";
import { STORAGE_KEY } from "./lib/storage";
import {
  BACKLOG_EPIC_ID,
  BACKLOG_TASK_ID,
  dragOnto,
  MILESTONE_ID,
  MILESTONE_TITLE,
  QUEUED_TASK_ID,
} from "./lib/testing/dropFixtures";

// Every drop refusal is raised on ONE toast id, and svelte-sonner MERGES a
// repeat raise into the live toast (`toast-state.svelte.js`'s `updateToast`
// spreads `...toastToUpdate, ...data`) rather than replacing it. So an omitted
// `action` key cannot take the previous refusal's button away — the option has
// to be passed as an explicit `undefined` for the merge to clear it.
//
// This file exists because `App.drop.test.ts` mocks `toast.error` and asserts on
// the CALL's options, which look right either way: what is wrong is only what
// sonner renders afterwards. Nothing here is mocked at that seam, so the real
// `Toaster` App mounts is what answers.

vi.mock("./lib/graphql", async () => {
  const actual = await vi.importActual<typeof import("./lib/graphql")>("./lib/graphql");
  return {
    ...actual,
    createClient: () => ({
      client: {
        mutation: (_doc: unknown, vars: Record<string, unknown>) => ({
          toPromise: () => Promise.resolve({ data: { reorderNib: { id: vars.id } }, error: undefined }),
        }),
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

/** The remedy button sonner draws, if the live toast currently has one. `data-button`
 *  alone would also match a cancel button, which no toast here asks for. */
function remedyButton(): HTMLElement | null {
  return document.querySelector("[data-sonner-toast] [data-button]:not([data-cancel])");
}

describe("App drop refusal toast", () => {
  beforeEach(() => {
    window.history.replaceState(null, "", "/");
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ filter: {}, viewLevel: "milestones" }));
  });

  afterEach(() => localStorage.removeItem(STORAGE_KEY));

  it("takes the previous remedy button away when the next refusal offers none", async () => {
    const { container } = render(App);
    await waitFor(() =>
      expect(container.querySelector(`tr[data-nib-id="${BACKLOG_TASK_ID}"]`)).not.toBeNull(),
    );

    // A position beside a queue member: refused, and the refusal names the
    // assignment that would express it — so this raise puts a button on the
    // shared toast. (Aiming at the header itself is the same assignment and is
    // accepted, so it raises no toast at all.)
    dragOnto(container, BACKLOG_TASK_ID, QUEUED_TASK_ID);
    await waitFor(() => expect(remedyButton()).not.toBeNull());
    expect(remedyButton()?.textContent).toContain(`Assign to ${MILESTONE_TITLE}`);

    // Out of it: refused with no remedy, onto the SAME toast. Clicking a button
    // still drawn here would run the first drag's writes against the first
    // drag's rows, which is why this is a data-mutation bug and not a cosmetic one.
    dragOnto(container, QUEUED_TASK_ID, BACKLOG_EPIC_ID);
    await waitFor(() =>
      expect(document.querySelector("[data-sonner-toast]")?.textContent).toContain(
        "clear the milestone assignment",
      ),
    );
    expect(remedyButton()).toBeNull();
  });
});
