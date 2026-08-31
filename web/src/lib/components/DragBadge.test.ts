import { render, screen } from "@testing-library/svelte";
import { describe, it, expect } from "vitest";
import DragBadge from "./DragBadge.svelte";
import { SelectionState } from "../selection.svelte";
import { DragState } from "../drag.svelte";
import { makeTestContext } from "../contexts";
import type { Region } from "../ordering/region";

const QUEUE: Region = { axis: "milestone", milestoneId: "tnib-m001" };
const TOP_LEVEL: Region = { axis: "parent", parentId: null };

function renderBadge(drag: DragState) {
  return render(DragBadge, { props: {}, context: makeTestContext(new SelectionState(), drag) });
}

/** A drag in flight over a target whose plan was accepted. */
function dragging(ids: string[], accepted?: { label: string; region: Region }): DragState {
  const drag = new DragState();
  drag.startDrag(ids);
  if (accepted) drag.setDropTarget("target", "before", true, accepted);
  return drag;
}

describe("DragBadge", () => {
  it("renders nothing when no drag is in flight", () => {
    renderBadge(new DragState());
    expect(screen.queryByTestId("drag-badge")).toBeNull();
  });

  it("names the destination the release would write to, with no count beside it", () => {
    renderBadge(dragging(["a"], { label: "Reorder in the top level", region: TOP_LEVEL }));
    expect(screen.getByTestId("drag-badge-label")).toHaveTextContent("Reorder in the top level");
    // "3 items" is a plural the count element cannot say correctly for one row,
    // so its absence is part of what this case asserts, not an omission.
    expect(screen.queryByTestId("drag-badge-count")).toBeNull();
  });

  // The queue label the RFC asks for, on a region no shipped lens can mint yet
  // (nibs-iaqd's membership lens is what will) — so it is built here directly.
  it("carries a queue destination and marks the badge with the queue's own color", () => {
    renderBadge(dragging(["a"], { label: "Reorder in the Q3 Launch queue", region: QUEUE }));
    expect(screen.getByTestId("drag-badge-label")).toHaveTextContent("Reorder in the Q3 Launch queue");
    // `border-region-queue` resolves to a color only because `--region-queue` is
    // registered in app.css's `@theme inline` block as well as in `:root` — the
    // Tailwind v4 two-layer rule, pinned by regionQueueToken.test.ts.
    expect(screen.getByTestId("drag-badge")).toHaveClass("border-region-queue");
  });

  it("leaves the queue color off a parent-axis destination", () => {
    renderBadge(dragging(["a"], { label: "Reorder in the top level", region: TOP_LEVEL }));
    const badge = screen.getByTestId("drag-badge");
    expect(badge).toHaveClass("border-border");
    expect(badge).not.toHaveClass("border-region-queue");
  });

  it("keeps the multi-select count, beside the destination", () => {
    renderBadge(dragging(["a", "b", "c"], { label: "Reorder in the top level", region: TOP_LEVEL }));
    expect(screen.getByTestId("drag-badge-count")).toHaveTextContent("3 items");
    expect(screen.getByTestId("drag-badge-label")).toHaveTextContent("Reorder in the top level");
  });

  it("shows the count alone while the cursor is over no accepted target", () => {
    renderBadge(dragging(["a", "b"]));
    expect(screen.getByTestId("drag-badge-count")).toHaveTextContent("2 items");
    expect(screen.queryByTestId("drag-badge-label")).toBeNull();
  });

  // The affordance for a target nothing can happen on: the badge names no
  // destination. On release App.svelte's handleDrop raises the refusal's own
  // sentence as a toast — for every reason but `drop-on-self`, the cancel
  // gesture, which is deliberately silent. `.drop-invalid` is styleless by
  // design, so on a single-row cancel this absence is the whole signal there is.
  it("disappears entirely on a single-row drag the plan refuses", () => {
    const drag = new DragState();
    drag.startDrag(["a"]);
    drag.setDropTarget("target", "before", false);
    renderBadge(drag);
    expect(screen.queryByTestId("drag-badge")).toBeNull();
  });

  // A fixed box is laid out against the VIEWPORT, so a position past its edge is
  // not merely off-screen: the box shrinks to the space that is left and the
  // label wraps into a column, with no scrollable overflow to reveal it. jsdom
  // reports every element as 0x0, so what is checked here is the clamp's own
  // arithmetic against `window.innerWidth`/`innerHeight`; the label not wrapping
  // is `whitespace-nowrap`, which only a real engine applies.
  describe("stays inside the viewport", () => {
    const styleOf = () => screen.getByTestId("drag-badge").getAttribute("style") ?? "";

    it("pulls back from the right and bottom edges", () => {
      const drag = dragging(["a"], { label: "Reorder in the top level", region: TOP_LEVEL });
      drag.cursorX = window.innerWidth + 500;
      drag.cursorY = window.innerHeight + 500;
      renderBadge(drag);
      expect(styleOf()).toContain(`left: ${window.innerWidth - 8}px`);
      expect(styleOf()).toContain(`top: ${window.innerHeight - 8}px`);
    });

    it("keeps a margin at the left and top edges", () => {
      // `cursorY - 12` is what puts the badge above the cursor, and it is
      // negative for any cursor within 12px of the top.
      const drag = dragging(["a"], { label: "Reorder in the top level", region: TOP_LEVEL });
      drag.cursorX = 0;
      drag.cursorY = 0;
      renderBadge(drag);
      expect(styleOf()).toContain("left: 12px");
      expect(styleOf()).toContain("top: 8px");
    });
  });
});
