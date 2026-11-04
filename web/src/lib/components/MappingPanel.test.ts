import { render, screen } from "@testing-library/svelte";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { readable, writable } from "svelte/store";
import { tick } from "svelte";
import MappingPanel from "./MappingPanel.svelte";
import { SelectionState } from "../selection.svelte";
import { DragState } from "../drag.svelte";
import { MappingModeState } from "../mappingMode.svelte";
import { makeTestContext } from "../contexts";
import type { MembershipNib } from "../membership";

vi.mock("@urql/svelte", async () => {
  const actual = await vi.importActual<typeof import("@urql/svelte")>("@urql/svelte");
  return { ...actual, getContextClient: vi.fn(), queryStore: vi.fn(), subscriptionStore: vi.fn() };
});

import { queryStore, subscriptionStore } from "@urql/svelte";
const mockQueryStore = vi.mocked(queryStore);
const mockSubscriptionStore = vi.mocked(subscriptionStore);

type PlanValue = { fetching: boolean; data?: { nibs: unknown[] }; error?: Error };

const MILESTONES = [{ id: "m1", title: "v1.0", status: "in-progress" }];
const NIBS = [
  { id: "m1", title: "v1.0", status: "in-progress", type: "milestone", milestone: "", parentId: null },
  { id: "t1", title: "Task one", status: "todo", type: "task", milestone: "m1", parentId: null },
  { id: "t2", title: "Task two", status: "todo", type: "task", milestone: "", parentId: null },
];

function setup(initial: PlanValue) {
  const plan = writable<PlanValue>(initial);
  mockQueryStore.mockReturnValue(Object.assign(plan, { reexecute: vi.fn() }) as any);
  mockSubscriptionStore.mockReturnValue(readable({ fetching: false, data: undefined }) as any);
  const mapping = new MappingModeState();
  const view = render(MappingPanel, {
    props: { onclose: vi.fn() },
    context: makeTestContext(new SelectionState(), new DragState(), { milestones: MILESTONES, mappingMode: mapping }),
  });
  const nibOf = (id: string) => NIBS.find((n) => n.id === id) as MembershipNib | undefined;
  const planT2 = () => mapping.planner!("m1", ["t2"], nibOf);
  return { plan, mapping, view, planT2 };
}

describe("MappingPanel", () => {
  beforeEach(() => {
    mockQueryStore.mockReset();
    mockSubscriptionStore.mockReset();
  });

  it("shows no counts while its query has not answered, and a card drop refuses", () => {
    const { planT2 } = setup({ fetching: true });
    expect(screen.queryByTestId("mapping-open-count")).toBeNull();
    expect(screen.getByTestId("mapping-counts-loading")).toBeInTheDocument();
    expect(planT2()).toEqual({ ok: false, refusal: { reason: "panel-unavailable", message: "Milestone counts are still loading." } });
  });

  it("says so when its query fails, and a card drop refuses", () => {
    const { planT2 } = setup({ fetching: false, error: new Error("boom") });
    expect(screen.queryByTestId("mapping-open-count")).toBeNull();
    expect(screen.getByTestId("mapping-counts-error")).toBeInTheDocument();
    const plan = planT2();
    expect(!plan.ok && plan.refusal.reason).toBe("panel-unavailable");
    expect(!plan.ok && plan.refusal.message).toContain("could not be loaded");
  });

  it("shows the counts and plans drops once its query answers", async () => {
    const { plan, planT2 } = setup({ fetching: true });
    plan.set({ fetching: false, data: { nibs: NIBS } });
    await tick();
    expect(screen.getByTestId("mapping-open-count")).toHaveTextContent("1");
    expect(screen.queryByTestId("mapping-counts-loading")).toBeNull();
    expect(planT2()).toMatchObject({ ok: true, label: "Assign to v1.0" });
  });

  it("keeps the counts and the planner through a failed refetch, and says they may be stale", async () => {
    const { plan, planT2 } = setup({ fetching: false, data: { nibs: NIBS } });
    plan.set({ fetching: false, error: new Error("refetch failed") });
    await tick();
    expect(screen.getByTestId("mapping-open-count")).toHaveTextContent("1");
    expect(screen.queryByTestId("mapping-counts-error")).toBeNull();
    expect(screen.getByTestId("mapping-counts-stale")).toBeInTheDocument();
    expect(planT2()).toMatchObject({ ok: true });

    plan.set({ fetching: false, data: { nibs: NIBS } });
    await tick();
    expect(screen.queryByTestId("mapping-counts-stale")).toBeNull();
  });

  it("refuses a drop that clears before it assigns while the counts are stale", async () => {
    const nibs = [
      ...NIBS,
      { id: "e1", title: "Epic one", status: "todo", type: "epic", milestone: "", parentId: null },
      { id: "d1", title: "Direct one", status: "todo", type: "task", milestone: "m1", parentId: "e1" },
    ];
    const { plan, mapping } = setup({ fetching: false, data: { nibs } });
    const nibOf = (id: string) => nibs.find((n) => n.id === id) as MembershipNib | undefined;
    expect(mapping.planner!("m1", ["e1", "d1"], nibOf).ok).toBe(true);

    plan.set({ fetching: false, error: new Error("refetch failed") });
    await tick();
    const refused = mapping.planner!("m1", ["e1", "d1"], nibOf);
    expect(!refused.ok && refused.refusal.reason).toBe("panel-unavailable");
  });

  it("unregisters its planner when it unmounts", () => {
    const { mapping, view } = setup({ fetching: false, data: { nibs: NIBS } });
    expect(mapping.planner).not.toBeNull();
    view.unmount();
    expect(mapping.planner).toBeNull();
  });
});
