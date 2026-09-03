import { render, waitFor } from "@testing-library/svelte";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { readable } from "svelte/store";
import TreeTable from "./TreeTable.svelte";
import type { ViewLevel } from "../types";
import { SelectionState } from "../selection.svelte";
import { DragState } from "../drag.svelte";
import { makeTestContext, VIEW_SPINE_KEY } from "../contexts";
import { makeViewSpine, LOADING_SPINE, UNAVAILABLE_SPINE, EMPTY_SPINE } from "../viewSpine";
import type { ViewSpine } from "../viewSpine";
import { createAreaVocabulary } from "../areas";

// What the table actually SENDS for an `area` value, which the pure
// prepareFilter tests cannot answer: whether TreeTable asks the spine at all,
// and whether it asks it again when the vocabulary lands. A call site that
// resolved the vocabulary once, outside the `$derived`, passes every test in
// filter.test.ts and fails the transition test here.
//
// The filter arrives as a PROP. Nothing in the app can put `area` into a filter
// yet — the query box's QueryFilter omits it — so this is the only way to drive
// the path, and it is the shape a restored or shared value will take once
// nibs-gdvz gives `area:` a token.

vi.mock("@urql/svelte", async () => {
  const actual = await vi.importActual<typeof import("@urql/svelte")>("@urql/svelte");
  return {
    ...actual,
    getContextClient: vi.fn(),
    queryStore: vi.fn(),
    subscriptionStore: vi.fn(),
  };
});

import { queryStore, subscriptionStore } from "@urql/svelte";
const mockQueryStore = vi.mocked(queryStore);
const mockSubscriptionStore = vi.mocked(subscriptionStore);

const areaNodes = [
  { path: "web", name: "web", description: "", color: "", depth: 0 },
  { path: "web/dashboard", name: "dashboard", description: "", color: "", depth: 1 },
];
const readySpine = makeViewSpine(createAreaVocabulary(areaNodes));

/** The filter variable of the most recent list query. */
function sentFilter(): Record<string, unknown> {
  const last = mockQueryStore.mock.calls.at(-1)?.[0] as
    | { variables?: { filter?: Record<string, unknown> } }
    | undefined;
  if (!last?.variables) throw new Error("TreeTable ran no list query");
  return last.variables.filter ?? {};
}

describe("TreeTable area filter", () => {
  beforeEach(() => {
    mockQueryStore.mockReset();
    mockQueryStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: { nibs: [] }, stale: false }) as any,
    );
    mockSubscriptionStore.mockReset();
    mockSubscriptionStore.mockReturnValue(
      readable({ fetching: false, error: undefined, data: undefined, stale: false }) as any,
    );
  });

  function renderWith(spine: ViewSpine, area: string) {
    return render(TreeTable, {
      props: { filter: { area }, viewLevel: "none" as ViewLevel } as any,
      context: makeTestContext(new SelectionState(), new DragState(), { viewSpine: spine }),
    });
  }

  it("sends an area the vocabulary declares", () => {
    renderWith(readySpine, "web");
    expect(sentFilter().area).toBe("web");
  });

  // The bug this guards: the server refuses an undeclared `area:` outright, so a
  // value sent here fails the WHOLE query and the table blanks with a GraphQL
  // error instead of rendering.
  it.each([
    ["pre-load", LOADING_SPINE],
    ["failed config query", UNAVAILABLE_SPINE],
    ["project declaring no areas", EMPTY_SPINE],
  ])("withholds an area the %s spine cannot declare", (_label, spine) => {
    renderWith(spine, "web");
    expect(sentFilter()).not.toHaveProperty("area");
  });

  it("withholds a retired area even from a loaded vocabulary", () => {
    renderWith(readySpine, "retired");
    expect(sentFilter()).not.toHaveProperty("area");
  });

  it("re-applies the withheld area when the vocabulary lands", async () => {
    // App provides the spine as a getter over a `$derived`, so the identity it
    // returns changes once — when the config query answers. Mirrored here with a
    // reactive holder, since a plain closure would let a call site that read the
    // vocabulary once still pass.
    const holder = $state({ spine: LOADING_SPINE as ViewSpine });
    const context = makeTestContext(new SelectionState(), new DragState());
    context.set(VIEW_SPINE_KEY, () => holder.spine);

    render(TreeTable, {
      props: { filter: { area: "web" }, viewLevel: "none" as ViewLevel } as any,
      context,
    });
    expect(sentFilter()).not.toHaveProperty("area");

    holder.spine = readySpine;

    // The list query is debounced (LIST_REFETCH_DEBOUNCE_MS), so the re-key
    // arrives a beat after the vocabulary does.
    await waitFor(() => expect(sentFilter().area).toBe("web"));
  });
});
