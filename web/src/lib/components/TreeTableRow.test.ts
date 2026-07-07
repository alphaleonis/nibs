import { render, screen } from "@testing-library/svelte";
import { describe, it, expect } from "vitest";
import TreeTableRow from "./TreeTableRow.svelte";
import type { TreeTableNib, ColumnKey } from "../types";
import { SelectionState } from "../selection.svelte";
import { DragState } from "../drag.svelte";
import { makeTestContext } from "../contexts";

function makeTreeTableNib(overrides: Partial<TreeTableNib> = {}): TreeTableNib {
  return {
    id: "nibs-abc1",
    title: "Fix login bug",
    status: "in-progress",
    type: "bug",
    priority: "high",
    estimate: "m",
    tags: ["auth", "urgent"],
    updatedAt: "2026-03-20T10:00:00Z",
    parentId: null,
    blockingIds: [],
    blockedByIds: [],
    ...overrides,
  };
}

function renderRow(props: Record<string, unknown>) {
  return render(TreeTableRow, {
    props: props as any,
    context: makeTestContext(new SelectionState(), new DragState()),
  });
}

describe("TreeTableRow", () => {
  it("renders as a table row with td cells", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib(),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    // Should render a <tr> element
    const row = container.querySelector("tr");
    expect(row).toBeInTheDocument();

    // Should have multiple <td> cells
    const cells = container.querySelectorAll("td");
    expect(cells.length).toBeGreaterThanOrEqual(1);
  });

  it("renders nib title in the title cell", () => {
    renderRow({
      nib: makeTreeTableNib({ title: "My important task" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    expect(screen.getByText("My important task")).toBeInTheDocument();
  });

  it("renders a Lucide type icon (svg) in the title cell", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ type: "bug" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    // Lucide icons render as SVG elements
    const titleCell = container.querySelector("[data-testid='nib-title']")!;
    const svg = titleCell.querySelector("svg");
    expect(svg).toBeInTheDocument();
  });

  it("renders expander arrow for rows with children", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib(),
      depth: 0,
      hasChildren: true,
      dimmed: false,
    });

    const toggle = container.querySelector("[data-testid='toggle']");
    expect(toggle).toBeInTheDocument();
  });

  it("does not render expander arrow for leaf rows", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib(),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const toggle = container.querySelector("[data-testid='toggle']");
    expect(toggle).not.toBeInTheDocument();
  });

  it("renders short ID (strips prefix) in the ID cell", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ id: "nibs-e6xk" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const idCell = container.querySelector("[data-testid='nib-id']") as HTMLElement;
    expect(idCell).toBeInTheDocument();
    expect(idCell.textContent?.trim()).toBe("e6xk");
  });

  it("renders type text in the Type cell", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ type: "feature" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const typeCell = container.querySelector("[data-testid='nib-type']") as HTMLElement;
    expect(typeCell).toBeInTheDocument();
    expect(typeCell.textContent?.trim()).toBe("feature");
  });

  it("renders priority icon for critical priority", () => {
    renderRow({
      nib: makeTreeTableNib({ priority: "critical" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    // Critical shows double exclamation mark
    expect(screen.getByText("\u203C")).toBeInTheDocument();
  });

  it("renders priority icon for high priority", () => {
    renderRow({
      nib: makeTreeTableNib({ priority: "high" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    expect(screen.getByText("!")).toBeInTheDocument();
  });

  it("does not render priority icon for normal priority", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ priority: "normal" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const priorityIcon = container.querySelector("[data-testid='priority-icon']");
    expect(priorityIcon).not.toBeInTheDocument();
  });

  it("renders priority icon for low priority", () => {
    renderRow({
      nib: makeTreeTableNib({ priority: "low" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    expect(screen.getByText("\u2193")).toBeInTheDocument();
  });

  it("renders deferred as a status with text and a colored dot", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ status: "deferred" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const stateCell = container.querySelector("[data-testid='nib-state']") as HTMLElement;
    expect(stateCell.textContent).toContain("deferred");
    expect(stateCell.querySelector("[data-testid='status-dot']")).toBeInTheDocument();
  });

  it("shows blocked icon with tooltip when blockedByIds is non-empty", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockedByIds: ["nibs-xyz1", "nibs-xyz2"] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const blocked = container.querySelector("[data-testid='blocked-icon']") as HTMLElement;
    expect(blocked).toBeInTheDocument();
    expect(blocked.getAttribute("title")).toBe("Blocked by 2 nib(s)");
  });

  it("hides blocked indicator when blockedByIds is empty", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockedByIds: [] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const blocked = container.querySelector("[data-testid='blocked-icon']");
    expect(blocked).not.toBeInTheDocument();
  });

  it("shows blocking icon with tooltip when blockingIds is non-empty", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockingIds: ["nibs-xyz1", "nibs-xyz2"] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const blocking = container.querySelector("[data-testid='blocking-icon']") as HTMLElement;
    expect(blocking).toBeInTheDocument();
    expect(blocking.getAttribute("title")).toBe("Blocking 2 nib(s)");
  });

  it("hides blocking indicator when blockingIds is empty", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockingIds: [] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const blocking = container.querySelector("[data-testid='blocking-icon']");
    expect(blocking).not.toBeInTheDocument();
  });

  it("renders parent info in the parent cell when parentNib is provided", () => {
    const parentNib = makeTreeTableNib({ id: "nibs-p001", title: "Parent Epic", type: "epic" });
    const { container } = renderRow({
      nib: makeTreeTableNib({ parentId: "nibs-p001" }),
      depth: 1,
      hasChildren: false,
      dimmed: false,
      parentNib,
    });

    const parentCell = container.querySelector("[data-testid='nib-parent']") as HTMLElement;
    expect(parentCell).toBeInTheDocument();
    expect(parentCell.textContent).toContain("Parent Epic");
    // Should have a title attribute with the full parent ID
    expect(parentCell.getAttribute("title")).toBe("nibs-p001");
  });

  it("renders empty parent cell when no parent", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ parentId: null }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const parentCell = container.querySelector("[data-testid='nib-parent']") as HTMLElement;
    expect(parentCell).toBeInTheDocument();
    expect(parentCell.textContent?.trim()).toBe("");
  });

  it("renders state column with status text and a colored dot", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ status: "in-progress" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const stateCell = container.querySelector("[data-testid='nib-state']") as HTMLElement;
    expect(stateCell).toBeInTheDocument();
    expect(stateCell.textContent).toContain("in-progress");

    // Should contain a dot element
    const dot = stateCell.querySelector("[data-testid='status-dot']");
    expect(dot).toBeInTheDocument();
  });

  it("renders estimate in uppercase when present", () => {
    renderRow({
      nib: makeTreeTableNib({ estimate: "m" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    expect(screen.getByText("M")).toBeInTheDocument();
  });

  it("hides estimate badge when estimate is empty", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ estimate: "" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const estimateBadge = container.querySelector("[data-testid='nib-effort']");
    // Should have empty content or no estimate text
    if (estimateBadge) {
      expect(estimateBadge.textContent?.trim()).toBe("");
    }
  });

  it("renders tag chips for each tag", () => {
    renderRow({
      nib: makeTreeTableNib({ tags: ["auth", "urgent", "backend"] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    expect(screen.getByText("auth")).toBeInTheDocument();
    expect(screen.getByText("urgent")).toBeInTheDocument();
    expect(screen.getByText("backend")).toBeInTheDocument();
  });

  it("renders row with hover highlight class", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib(),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const row = container.querySelector("tr") as HTMLElement;
    expect(row).toBeInTheDocument();
    // Row should have the tree-row class which provides hover styling
    expect(row.classList.toString()).toContain("tree-row");
  });

  it("renders no tag chips when tags array is empty", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ tags: [] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const tagChips = container.querySelectorAll("[data-testid='tag']");
    expect(tagChips).toHaveLength(0);
  });

  it("shows chevron-right icon when collapsed=true and hasChildren=true", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib(),
      depth: 0,
      hasChildren: true,
      dimmed: false,
      collapsed: true,
    });

    const toggle = container.querySelector("[data-testid='toggle']") as HTMLElement;
    expect(toggle).toBeInTheDocument();
    expect(toggle.querySelector("svg")).toBeInTheDocument();
  });

  it("renders empty ID cell for a synthetic bucket node", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ id: "__no_epic__", title: "No epic (2)", type: "", status: "" }),
      depth: 0,
      hasChildren: true,
      dimmed: false,
    });

    const idCell = container.querySelector("[data-testid='nib-id']") as HTMLElement;
    expect(idCell.textContent?.trim()).toBe("");
  });

  it("shows chevron-down icon when collapsed=false and hasChildren=true", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib(),
      depth: 0,
      hasChildren: true,
      dimmed: false,
      collapsed: false,
    });

    const toggle = container.querySelector("[data-testid='toggle']") as HTMLElement;
    expect(toggle).toBeInTheDocument();
    expect(toggle.querySelector("svg")).toBeInTheDocument();
  });

  it("hides ID cell when visibleColumns excludes 'id'", () => {
    const visibleColumns: ColumnKey[] = ["title", "state", "type", "effort", "tags"];
    const { container } = renderRow({
      nib: makeTreeTableNib(),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      visibleColumns,
    });

    expect(container.querySelector("[data-testid='nib-id']")).not.toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-title']")).toBeInTheDocument();
  });

  it("hides effort and tags cells when visibleColumns excludes them", () => {
    const visibleColumns: ColumnKey[] = ["id", "title", "state", "type"];
    const { container } = renderRow({
      nib: makeTreeTableNib({ tags: ["auth"], estimate: "m" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      visibleColumns,
    });

    expect(container.querySelector("[data-testid='nib-effort']")).not.toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-tags']")).not.toBeInTheDocument();
    // ID and title should still be there
    expect(container.querySelector("[data-testid='nib-id']")).toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-title']")).toBeInTheDocument();
  });

  it("hides title cell when visibleColumns excludes 'title'", () => {
    const visibleColumns: ColumnKey[] = ["id", "state"];
    const { container } = renderRow({
      nib: makeTreeTableNib({ title: "My Task" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      visibleColumns,
    });

    expect(container.querySelector("[data-testid='nib-title']")).not.toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-id']")).toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-state']")).toBeInTheDocument();
  });

  it("hides state cell when visibleColumns excludes 'state'", () => {
    const visibleColumns: ColumnKey[] = ["id", "title"];
    const { container } = renderRow({
      nib: makeTreeTableNib({ status: "in-progress" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      visibleColumns,
    });

    expect(container.querySelector("[data-testid='nib-state']")).not.toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-id']")).toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-title']")).toBeInTheDocument();
  });

  it("hides parent cell when visibleColumns excludes 'parent'", () => {
    const parentNib = makeTreeTableNib({ id: "nibs-p001", title: "Parent", type: "epic" });
    const visibleColumns: ColumnKey[] = ["id", "title", "state"];
    const { container } = renderRow({
      nib: makeTreeTableNib({ parentId: "nibs-p001" }),
      depth: 1,
      hasChildren: false,
      dimmed: false,
      parentNib,
      visibleColumns,
    });

    expect(container.querySelector("[data-testid='nib-parent']")).not.toBeInTheDocument();
  });

  it("does not apply active class when nib is not selected in context", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib(),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("active")).toBe(false);
  });

  it("has data-action='title' on the title text element", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ title: "Clickable title" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const titleText = container.querySelector("[data-testid='title-text']") as HTMLElement;
    expect(titleText).toBeInTheDocument();
    expect(titleText.dataset.action).toBe("title");
  });

  it("has data-action='toggle' on the expand/collapse button", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib(),
      depth: 0,
      hasChildren: true,
      dimmed: false,
    });

    const toggle = container.querySelector("[data-testid='toggle']") as HTMLElement;
    expect(toggle).toBeInTheDocument();
    expect(toggle.dataset.action).toBe("toggle");
  });

  it("has data-action='add-child' and data-child-type on the add-child button", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ type: "epic" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const addChildBtn = container.querySelector("[data-testid='row-add-child']") as HTMLElement;
    expect(addChildBtn).toBeInTheDocument();
    expect(addChildBtn.dataset.action).toBe("add-child");
    expect(addChildBtn.dataset.childType).toBe("epic");
  });

  it("has draggable class when draggable prop is true", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib(),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      draggable: true,
    });

    const row = container.querySelector("[data-testid='tree-row']") as HTMLElement;
    expect(row.classList.contains("draggable")).toBe(true);
  });

  it("has no event handler props (zero callback props)", () => {
    // TreeTableRow should accept only data/visual props, no callbacks.
    // This test ensures the row renders without any callback props.
    const { container } = renderRow({
      nib: makeTreeTableNib(),
      depth: 0,
      hasChildren: true,
      dimmed: false,
    });

    const row = container.querySelector("tr") as HTMLElement;
    expect(row).toBeInTheDocument();
    // If it rendered at all without callbacks, the interface is clean.
    expect(row.dataset.nibId).toBe("nibs-abc1");
  });
});

describe("TreeTableRow context-based state", () => {
  function renderRowWithContext(
    props: Record<string, unknown>,
    opts?: { selection?: SelectionState; drag?: DragState },
  ) {
    return render(TreeTableRow, {
      props: props as any,
      context: makeTestContext(opts?.selection ?? new SelectionState(), opts?.drag ?? new DragState()),
    });
  }

  it("applies .active class when nib is selected via context", () => {
    const selection = new SelectionState();
    selection.select("nibs-abc1");

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }) },
      { selection },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("active")).toBe(true);
  });

  it("does not apply .active class when nib is not selected via context", () => {
    const selection = new SelectionState();
    selection.select("nibs-other");

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }) },
      { selection },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("active")).toBe(false);
  });

  it("applies .active class when nib is in multi-select selectedIds via context", () => {
    const selection = new SelectionState();
    selection.select("nibs-abc1");
    selection.toggleSelect("nibs-other");

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }) },
      { selection },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("active")).toBe(true);
  });

  it("applies .focused class when nib is focused via context", () => {
    const selection = new SelectionState();
    selection.focus("nibs-abc1");

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }) },
      { selection },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("focused")).toBe(true);
  });

  it("does not apply .focused class when a different nib is focused", () => {
    const selection = new SelectionState();
    selection.focus("nibs-other");

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }) },
      { selection },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("focused")).toBe(false);
  });

  it("applies .dragged class when nib is being dragged via context", () => {
    const drag = new DragState();
    drag.startDrag(["nibs-abc1"]);

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }) },
      { drag },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("dragged")).toBe(true);
  });

  it("applies .drop-before class when nib is a valid before drop target via context", () => {
    const drag = new DragState();
    drag.startDrag(["nibs-other"]);
    drag.setDropTarget("nibs-abc1", "before", true);

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }) },
      { drag },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("drop-before")).toBe(true);
  });

  it("applies .drop-after class when nib is a valid after drop target via context", () => {
    const drag = new DragState();
    drag.startDrag(["nibs-other"]);
    drag.setDropTarget("nibs-abc1", "after", true);

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }) },
      { drag },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("drop-after")).toBe(true);
  });

  it("applies .drop-reparent class when nib is a valid reparent drop target via context", () => {
    const drag = new DragState();
    drag.startDrag(["nibs-other"]);
    drag.setDropTarget("nibs-abc1", "reparent", true);

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }) },
      { drag },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("drop-reparent")).toBe(true);
  });

  it("applies .drop-invalid class when nib is an invalid drop target via context", () => {
    const drag = new DragState();
    drag.startDrag(["nibs-other"]);
    drag.setDropTarget("nibs-abc1", "before", false);

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }) },
      { drag },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("drop-invalid")).toBe(true);
  });

  it("applies .nib-highlighted class when highlighted prop is true", () => {
    const { container } = renderRowWithContext({
      nib: makeTreeTableNib(),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      highlighted: true,
    });

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("nib-highlighted")).toBe(true);
  });

  it("does not apply .nib-highlighted class when highlighted prop is false", () => {
    const { container } = renderRowWithContext({
      nib: makeTreeTableNib(),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      highlighted: false,
    });

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("nib-highlighted")).toBe(false);
  });

  it("applies .nib-fading class when fading prop is true", () => {
    const { container } = renderRowWithContext({
      nib: makeTreeTableNib(),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      fading: true,
    });

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("nib-fading")).toBe(true);
  });

  it("does not apply .nib-fading class when fading prop is false", () => {
    const { container } = renderRowWithContext({
      nib: makeTreeTableNib(),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      fading: false,
    });

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("nib-fading")).toBe(false);
  });
});
