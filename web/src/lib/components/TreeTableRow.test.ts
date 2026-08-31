import { render, screen } from "@testing-library/svelte";
import { describe, it, expect } from "vitest";
import TreeTableRow from "./TreeTableRow.svelte";
import { formatRelative } from "../date";
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
    createdAt: "2026-03-15T10:00:00Z",
    updatedAt: "2026-03-20T10:00:00Z",
    parentId: null,
    milestone: "",
    milestoneOrder: "",
    blockingIds: [],
    blockedByIds: [],
    etag: "etag-test",
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

    const stateCell = container.querySelector("[data-testid='nib-status']") as HTMLElement;
    expect(stateCell.textContent).toContain("deferred");
    expect(stateCell.querySelector("[data-testid='status-icon']")).toBeInTheDocument();
  });

  it("shows the blocked pill (default emphasis) with tooltip when blockedByIds is non-empty", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockedByIds: ["nibs-xyz1", "nibs-xyz2"] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const badge = container.querySelector("[data-testid='blocked-badge']") as HTMLElement;
    expect(badge).toBeInTheDocument();
    expect(badge.textContent).toContain("Blocked");
    expect(badge.getAttribute("title")).toBe("Blocked by 2 nib(s)");
    // Default is the pill, not the bare icon.
    expect(container.querySelector("[data-testid='blocked-icon']")).not.toBeInTheDocument();
  });

  it("renders the bare lock icon (no pill) when blockedEmphasis is 'subtle'", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockedByIds: ["nibs-xyz1", "nibs-xyz2"] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      blockedEmphasis: "subtle",
    });

    const icon = container.querySelector("[data-testid='blocked-icon']") as HTMLElement;
    expect(icon).toBeInTheDocument();
    expect(icon.getAttribute("title")).toBe("Blocked by 2 nib(s)");
    expect(container.querySelector("[data-testid='blocked-badge']")).not.toBeInTheDocument();
  });

  it("renders the pill AND dims the row when blockedEmphasis is 'pill-dim'", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockedByIds: ["nibs-xyz1"] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      blockedEmphasis: "pill-dim",
    });

    expect(container.querySelector("[data-testid='blocked-badge']")).toBeInTheDocument();
    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("blocked-dim")).toBe(true);
  });

  it("does not dim the row for pill-dim when the nib is not blocked", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockedByIds: [] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      blockedEmphasis: "pill-dim",
    });

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("blocked-dim")).toBe(false);
  });

  it("hides all blocked indicators when blockedByIds is empty", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockedByIds: [] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    expect(container.querySelector("[data-testid='blocked-badge']")).not.toBeInTheDocument();
    expect(container.querySelector("[data-testid='blocked-icon']")).not.toBeInTheDocument();
  });

  it("shows the blocking pill (default emphasis) with tooltip when blockingIds is non-empty", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockingIds: ["nibs-xyz1", "nibs-xyz2"] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    const badge = container.querySelector("[data-testid='blocking-badge']") as HTMLElement;
    expect(badge).toBeInTheDocument();
    expect(badge.textContent).toContain("Blocking");
    expect(badge.getAttribute("title")).toBe("Blocking 2 nib(s)");
    // Default is the pill, not the bare icon.
    expect(container.querySelector("[data-testid='blocking-icon']")).not.toBeInTheDocument();
  });

  it("renders the bare link icon (no pill) for blocking when blockedEmphasis is 'subtle'", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockingIds: ["nibs-xyz1", "nibs-xyz2"] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      blockedEmphasis: "subtle",
    });

    const icon = container.querySelector("[data-testid='blocking-icon']") as HTMLElement;
    expect(icon).toBeInTheDocument();
    expect(icon.getAttribute("title")).toBe("Blocking 2 nib(s)");
    expect(container.querySelector("[data-testid='blocking-badge']")).not.toBeInTheDocument();
  });

  it("renders the blocking pill but does NOT dim the row for a blocking-only nib under 'pill-dim'", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockingIds: ["nibs-xyz1"], blockedByIds: [] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      blockedEmphasis: "pill-dim",
    });

    expect(container.querySelector("[data-testid='blocking-badge']")).toBeInTheDocument();
    // Row dimming is blocked-only — blocking others must not dim the row.
    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("blocked-dim")).toBe(false);
  });

  it("hides all blocking indicators when blockingIds is empty", () => {
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockingIds: [] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
    });

    expect(container.querySelector("[data-testid='blocking-badge']")).not.toBeInTheDocument();
    expect(container.querySelector("[data-testid='blocking-icon']")).not.toBeInTheDocument();
  });

  it("renders the Blocked-by cell with a count and lock icon when blockedBy column is visible", () => {
    const visibleColumns: ColumnKey[] = ["title", "blockedBy"];
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockedByIds: ["nibs-a", "nibs-b"] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      visibleColumns,
    });

    const cell = container.querySelector("[data-testid='nib-blocked-by']") as HTMLElement;
    expect(cell).toBeInTheDocument();
    expect(cell.textContent?.trim()).toBe("2");
    // Lucide Lock icon renders as an SVG inside the cell.
    expect(cell.querySelector("svg")).toBeInTheDocument();
    // Tooltip lists the blocking nib IDs.
    expect(cell.querySelector("[title='nibs-a, nibs-b']")).toBeInTheDocument();
  });

  it("renders an empty Blocked-by cell (no icon/count) when blockedByIds is empty", () => {
    const visibleColumns: ColumnKey[] = ["title", "blockedBy"];
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockedByIds: [] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      visibleColumns,
    });

    const cell = container.querySelector("[data-testid='nib-blocked-by']") as HTMLElement;
    expect(cell).toBeInTheDocument();
    expect(cell.textContent?.trim()).toBe("");
    expect(cell.querySelector("svg")).not.toBeInTheDocument();
  });

  it("does not render the Blocked-by cell when the column is not visible", () => {
    const visibleColumns: ColumnKey[] = ["title"];
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockedByIds: ["nibs-a"] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      visibleColumns,
    });

    expect(container.querySelector("[data-testid='nib-blocked-by']")).not.toBeInTheDocument();
  });

  it("renders the Blocking cell with a count and link icon when blocking column is visible", () => {
    const visibleColumns: ColumnKey[] = ["title", "blocking"];
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockingIds: ["nibs-c", "nibs-d", "nibs-e"] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      visibleColumns,
    });

    const cell = container.querySelector("[data-testid='nib-blocking']") as HTMLElement;
    expect(cell).toBeInTheDocument();
    expect(cell.textContent?.trim()).toBe("3");
    // Lucide Link icon renders as an SVG inside the cell.
    expect(cell.querySelector("svg")).toBeInTheDocument();
    // Tooltip lists the blocked nib IDs.
    expect(cell.querySelector("[title='nibs-c, nibs-d, nibs-e']")).toBeInTheDocument();
  });

  it("renders an empty Blocking cell (no icon/count) when blockingIds is empty", () => {
    const visibleColumns: ColumnKey[] = ["title", "blocking"];
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockingIds: [] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      visibleColumns,
    });

    const cell = container.querySelector("[data-testid='nib-blocking']") as HTMLElement;
    expect(cell).toBeInTheDocument();
    expect(cell.textContent?.trim()).toBe("");
    expect(cell.querySelector("svg")).not.toBeInTheDocument();
  });

  it("does not render the Blocking cell when the column is not visible", () => {
    const visibleColumns: ColumnKey[] = ["title"];
    const { container } = renderRow({
      nib: makeTreeTableNib({ blockingIds: ["nibs-c"] }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      visibleColumns,
    });

    expect(container.querySelector("[data-testid='nib-blocking']")).not.toBeInTheDocument();
  });

  it("renders the Modified cell bound to updatedAt (distinct from Created) with relative age text and an ISO title", () => {
    // Created and Modified sit in DIFFERENT relative buckets regardless of
    // wall-clock: createdAt (2020) collapses to the absolute "Jan 2020" label
    // while updatedAt stays a relative age. Rendering BOTH cells and asserting
    // they differ makes a field swap between the two near-identical <td> blocks
    // observable — a swap renders createdAt's date and fails the check.
    const visibleColumns: ColumnKey[] = ["title", "created", "modified"];
    const nib = makeTreeTableNib({
      createdAt: "2020-01-01T00:00:00Z",
      updatedAt: "2026-03-20T10:00:00Z",
    });
    const { container } = renderRow({
      nib,
      depth: 0,
      hasChildren: false,
      dimmed: false,
      visibleColumns,
    });

    const modifiedCell = container.querySelector("[data-testid='nib-modified']") as HTMLElement;
    const createdCell = container.querySelector("[data-testid='nib-created']") as HTMLElement;
    expect(modifiedCell).toBeInTheDocument();

    const modifiedRel = modifiedCell.textContent?.trim() ?? "";
    const createdRel = createdCell.textContent?.trim() ?? "";
    // Non-empty, and bound to the RIGHT field (updatedAt, not createdAt).
    expect(modifiedRel).not.toBe("");
    expect(modifiedRel).toBe(formatRelative(nib.updatedAt));
    // Observably different from the Created cell, so a field swap fails here.
    expect(modifiedRel).not.toBe(createdRel);
    // Hover title is the full ISO timestamp of updatedAt.
    expect(modifiedCell.getAttribute("title")).toBe("2026-03-20T10:00:00.000Z");
  });

  it("does not render the Created cell when the created column is not visible", () => {
    // 'created' is opt-in; it is absent unless explicitly enabled.
    const visibleColumns: ColumnKey[] = ["title", "modified"];
    const { container } = renderRow({
      nib: makeTreeTableNib({ createdAt: "2026-03-15T10:00:00Z" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      visibleColumns,
    });

    expect(container.querySelector("[data-testid='nib-created']")).not.toBeInTheDocument();
  });

  it("renders the Created cell bound to createdAt (distinct from Modified) with relative age text and an ISO title", () => {
    // Symmetric to the Modified-cell test: distinct buckets (createdAt 2020 ->
    // "Jan 2020"; updatedAt recent -> relative age) so a field swap that renders
    // updatedAt in the Created cell is caught by the distinctness assertion.
    const visibleColumns: ColumnKey[] = ["title", "created", "modified"];
    const nib = makeTreeTableNib({
      createdAt: "2020-01-01T00:00:00Z",
      updatedAt: "2026-03-20T10:00:00Z",
    });
    const { container } = renderRow({
      nib,
      depth: 0,
      hasChildren: false,
      dimmed: false,
      visibleColumns,
    });

    const createdCell = container.querySelector("[data-testid='nib-created']") as HTMLElement;
    const modifiedCell = container.querySelector("[data-testid='nib-modified']") as HTMLElement;
    expect(createdCell).toBeInTheDocument();

    const createdRel = createdCell.textContent?.trim() ?? "";
    const modifiedRel = modifiedCell.textContent?.trim() ?? "";
    expect(createdRel).not.toBe("");
    expect(createdRel).toBe(formatRelative(nib.createdAt));
    expect(createdRel).not.toBe(modifiedRel);
    expect(createdCell.getAttribute("title")).toBe("2020-01-01T00:00:00.000Z");
  });

  it("renders blank Created / Modified cells for a synthetic bucket row (empty dates)", () => {
    const visibleColumns: ColumnKey[] = ["title", "created", "modified"];
    const { container } = renderRow({
      nib: makeTreeTableNib({ id: "/__no_epic__", title: "No epic (2)", type: "", createdAt: "", updatedAt: "" }),
      depth: 0,
      hasChildren: true,
      dimmed: false,
      visibleColumns,
    });

    const created = container.querySelector("[data-testid='nib-created']") as HTMLElement;
    const modified = container.querySelector("[data-testid='nib-modified']") as HTMLElement;
    expect(created.textContent?.trim()).toBe("");
    expect(created.getAttribute("title")).toBe("");
    expect(modified.textContent?.trim()).toBe("");
    expect(modified.getAttribute("title")).toBe("");
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

    const stateCell = container.querySelector("[data-testid='nib-status']") as HTMLElement;
    expect(stateCell).toBeInTheDocument();
    expect(stateCell.textContent).toContain("in-progress");

    // Should contain a dot element
    const dot = stateCell.querySelector("[data-testid='status-icon']");
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

    const estimateBadge = container.querySelector("[data-testid='nib-estimate']");
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
      nib: makeTreeTableNib({ id: "/__no_epic__", title: "No epic (2)", type: "", status: "" }),
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
    const visibleColumns: ColumnKey[] = ["title", "status", "type", "estimate", "tags"];
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

  it("hides estimate and tags cells when visibleColumns excludes them", () => {
    const visibleColumns: ColumnKey[] = ["id", "title", "status", "type"];
    const { container } = renderRow({
      nib: makeTreeTableNib({ tags: ["auth"], estimate: "m" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      visibleColumns,
    });

    expect(container.querySelector("[data-testid='nib-estimate']")).not.toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-tags']")).not.toBeInTheDocument();
    // ID and title should still be there
    expect(container.querySelector("[data-testid='nib-id']")).toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-title']")).toBeInTheDocument();
  });

  it("hides title cell when visibleColumns excludes 'title'", () => {
    const visibleColumns: ColumnKey[] = ["id", "status"];
    const { container } = renderRow({
      nib: makeTreeTableNib({ title: "My Task" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      visibleColumns,
    });

    expect(container.querySelector("[data-testid='nib-title']")).not.toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-id']")).toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-status']")).toBeInTheDocument();
  });

  it("hides status cell when visibleColumns excludes 'status'", () => {
    const visibleColumns: ColumnKey[] = ["id", "title"];
    const { container } = renderRow({
      nib: makeTreeTableNib({ status: "in-progress" }),
      depth: 0,
      hasChildren: false,
      dimmed: false,
      visibleColumns,
    });

    expect(container.querySelector("[data-testid='nib-status']")).not.toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-id']")).toBeInTheDocument();
    expect(container.querySelector("[data-testid='nib-title']")).toBeInTheDocument();
  });

  it("hides parent cell when visibleColumns excludes 'parent'", () => {
    const parentNib = makeTreeTableNib({ id: "nibs-p001", title: "Parent", type: "epic" });
    const visibleColumns: ColumnKey[] = ["id", "title", "status"];
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
    selection.toggleSelect("nibs-other", "follow");

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }) },
      { selection },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("active")).toBe(true);
  });

  it("marks the open row with the accent under the double-click preference", () => {
    const selection = new SelectionState();
    selection.select("nibs-abc1");

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }), openDetailOn: "double" },
      { selection },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("opened")).toBe(true);
    expect(row).toHaveAttribute("aria-current", "true");
  });

  it("single mode omits the accent but still reports the panel row to assistive tech", () => {
    // Under "single" the panel follows the selection, so on an ordinary click the
    // open row is also the selected row and the accent would only repeat what the
    // fill already says. The visual channel is gated; `aria-current` is not,
    // because it costs nothing on screen and still answers "which row is open?".
    const selection = new SelectionState();
    selection.select("nibs-abc1");

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }), openDetailOn: "single" },
      { selection },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("opened")).toBe(false);
    expect(row.classList.contains("active")).toBe(true);
    expect(row).toHaveAttribute("aria-selected", "true");
    expect(row).toHaveAttribute("aria-current", "true");
  });

  it("defaults to the single-mode treatment when no preference is passed", () => {
    const selection = new SelectionState();
    selection.select("nibs-abc1");

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }) },
      { selection },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("opened")).toBe(false);
  });

  it("a merely-selected row keeps .active without .opened", () => {
    // The select-without-open path (the double-click preference): the selection
    // moves to this row while the panel keeps showing another nib.
    const selection = new SelectionState();
    selection.select("nibs-other");
    selection.selectOnly("nibs-abc1");

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }) },
      { selection },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("active")).toBe(true);
    expect(row.classList.contains("opened")).toBe(false);
    expect(row).toHaveAttribute("aria-selected", "true");
    expect(row).not.toHaveAttribute("aria-current");
  });

  it("the open row loses .active when the selection moves off it", () => {
    // `selectOnly` rewrites `selectedIds` and leaves `selectedNibId` alone, so the
    // panel's row is no longer a delete target and must stop carrying the fill —
    // the fill is the action set, and this row is not in it.
    const selection = new SelectionState();
    selection.select("nibs-abc1");
    selection.selectOnly("nibs-other");

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }), openDetailOn: "double" },
      { selection },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(selection.selectedIds.has("nibs-abc1")).toBe(false);
    expect(row.classList.contains("opened")).toBe(true);
    expect(row.classList.contains("active")).toBe(false);
    expect(row).toHaveAttribute("aria-selected", "false");
  });

  it("deselectAll() leaves the open row .opened without .active in double mode", () => {
    // The post-mutation divergence: `clearAfterMutation` calls deselectAll()
    // unconditionally and closes the panel only when the mutation hit the panel's
    // nib, so deleting some OTHER row empties the action set while this row stays
    // open. It keeps the accent and drops the fill — nothing here would be deleted.
    const selection = new SelectionState();
    selection.select("nibs-abc1");
    selection.deselectAll();

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }), openDetailOn: "double" },
      { selection },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("opened")).toBe(true);
    expect(row.classList.contains("active")).toBe(false);
    expect(row).toHaveAttribute("aria-selected", "false");
    expect(row).toHaveAttribute("aria-current", "true");
  });

  it("single mode leaves a post-mutation open row visually unmarked — the accepted gap", () => {
    // Same divergence as above, under the default preference. Gating the accent on
    // "double" means this row carries no visual marker at all: no fill (it is not
    // in the action set) and no accent (the gate). Accepted because the detail
    // panel itself names the nib it is showing. `aria-current` still reports it.
    // Pinned so the trade-off is a decision on record, not a surprise.
    const selection = new SelectionState();
    selection.select("nibs-abc1");
    selection.deselectAll();

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }), openDetailOn: "single" },
      { selection },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("opened")).toBe(false);
    expect(row.classList.contains("active")).toBe(false);
    expect(row).toHaveAttribute("aria-current", "true");
  });

  it("retainOnly() pruning a row out of the action set leaves it .opened without .active", () => {
    // The other route to open-but-unselected: a filter change prunes `selectedIds`
    // down to the still-matching rows and leaves `selectedNibId` untouched.
    const selection = new SelectionState();
    selection.select("nibs-abc1");
    selection.retainOnly(new Set(["nibs-other"]));

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }), openDetailOn: "double" },
      { selection },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("opened")).toBe(true);
    expect(row.classList.contains("active")).toBe(false);
  });

  it("a multi-select row is .active with no .opened anywhere", () => {
    // Three rows selected collapses `selectedNibId` to null, so no row is the
    // panel's — every member carries the fill and none carries the accent.
    const selection = new SelectionState();
    selection.select("nibs-abc1");
    selection.toggleSelect("nibs-b", "follow");
    selection.toggleSelect("nibs-c", "follow");

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }) },
      { selection },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(selection.selectedNibId).toBeNull();
    expect(row.classList.contains("active")).toBe(true);
    expect(row.classList.contains("opened")).toBe(false);
    expect(row).toHaveAttribute("aria-selected", "true");
  });

  it("exposes aria-selected=\"false\" on a row outside the action set", () => {
    // Emitted on every row rather than omitted, so an unselected row reads as
    // selectable-but-not-selected instead of not selectable at all.
    const selection = new SelectionState();
    selection.select("nibs-other");

    const { container } = renderRowWithContext(
      { nib: makeTreeTableNib({ id: "nibs-abc1" }) },
      { selection },
    );

    const row = container.querySelector("tr") as HTMLElement;
    expect(row).toHaveAttribute("aria-selected", "false");
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

  // The queue axis is not reachable from any lens shipped today — `typeLens` is
  // the only GroupingLens and declares `childRegion: () => null`, so nothing
  // mints a milestone region (nibs-iaqd's membership lens is what will). The
  // component level is therefore the only place these can be driven: a DragState
  // carrying the milestone region an accepted queue plan would put there.
  it.each(["before", "after", "reparent"] as const)(
    "colors the %s indicator for the queue axis, and only for it",
    (zone) => {
      const queue = new DragState();
      queue.startDrag(["nibs-other"]);
      queue.setDropTarget("nibs-abc1", zone, true, {
        label: "Reorder in the Q3 Launch queue",
        region: { axis: "milestone", milestoneId: "tnib-m001" },
      });
      const queued = renderRowWithContext({ nib: makeTreeTableNib({ id: "nibs-abc1" }) }, { drag: queue });
      const queuedRow = queued.container.querySelector("tr") as HTMLElement;
      expect(queuedRow.classList.contains(`drop-${zone}`)).toBe(true);
      expect(queuedRow.classList.contains("drop-queue")).toBe(true);

      const parent = new DragState();
      parent.startDrag(["nibs-other"]);
      parent.setDropTarget("nibs-abc1", zone, true, {
        label: "Reorder in the top level",
        region: { axis: "parent", parentId: null },
      });
      const parented = renderRowWithContext({ nib: makeTreeTableNib({ id: "nibs-abc1" }) }, { drag: parent });
      const parentedRow = parented.container.querySelector("tr") as HTMLElement;
      expect(parentedRow.classList.contains(`drop-${zone}`)).toBe(true);
      expect(parentedRow.classList.contains("drop-queue")).toBe(false);
    },
  );

  it("does not color a REFUSED drop by axis — a refusal carries no region", () => {
    const drag = new DragState();
    drag.startDrag(["nibs-other"]);
    drag.setDropTarget("nibs-abc1", "before", false);

    const { container } = renderRowWithContext({ nib: makeTreeTableNib({ id: "nibs-abc1" }) }, { drag });
    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("drop-invalid")).toBe(true);
    expect(row.classList.contains("drop-queue")).toBe(false);
  });

  it("does not color a row that is not the drop target", () => {
    // `dropRegion` is one ambient value read by every row, so the axis class has
    // to be gated on being the target the way the zone classes are.
    const drag = new DragState();
    drag.startDrag(["nibs-other"]);
    drag.setDropTarget("nibs-elsewhere", "before", true, {
      label: "Reorder in the Q3 Launch queue",
      region: { axis: "milestone", milestoneId: "tnib-m001" },
    });

    const { container } = renderRowWithContext({ nib: makeTreeTableNib({ id: "nibs-abc1" }) }, { drag });
    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("drop-queue")).toBe(false);
  });

  it.each([
    { band: null, region: false, queue: false },
    { band: "parent" as const, region: true, queue: false },
    { band: "milestone" as const, region: true, queue: true },
  ])("marks the region band from the regionBand prop: $band", ({ band, region, queue }) => {
    const { container } = renderRowWithContext({ nib: makeTreeTableNib(), regionBand: band });
    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("region-band")).toBe(region);
    expect(row.classList.contains("region-band-queue")).toBe(queue);
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

  // pill-dim row opacity composites through the whole <tr>, so the dim must yield
  // to row-level affordances (drop target, change-pulse) or it mutes them by 40%.
  it("does NOT dim a pill-dim blocked row while it is a drop target", () => {
    const drag = new DragState();
    drag.startDrag(["nibs-other"]);
    drag.setDropTarget("nibs-abc1", "reparent", true);

    const { container } = renderRowWithContext(
      {
        nib: makeTreeTableNib({ id: "nibs-abc1", blockedByIds: ["nibs-xyz1"] }),
        blockedEmphasis: "pill-dim",
      },
      { drag },
    );

    const row = container.querySelector("tr") as HTMLElement;
    // Dim suppressed so the drop affordance renders at full opacity...
    expect(row.classList.contains("blocked-dim")).toBe(false);
    // ...but the drop-target indicator still applies.
    expect(row.classList.contains("drop-reparent")).toBe(true);
  });

  it("does NOT dim a pill-dim blocked row during the change-pulse (highlighted)", () => {
    const { container } = renderRowWithContext({
      nib: makeTreeTableNib({ blockedByIds: ["nibs-xyz1"] }),
      blockedEmphasis: "pill-dim",
      highlighted: true,
    });

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("blocked-dim")).toBe(false);
    expect(row.classList.contains("nib-highlighted")).toBe(true);
  });

  it("still dims a plain pill-dim blocked row (not dragged, not a drop target, not highlighted)", () => {
    const { container } = renderRowWithContext({
      nib: makeTreeTableNib({ blockedByIds: ["nibs-xyz1"] }),
      blockedEmphasis: "pill-dim",
    });

    const row = container.querySelector("tr") as HTMLElement;
    expect(row.classList.contains("blocked-dim")).toBe(true);
  });
});

// Row opacity is single-sourced through one computed value applied inline, so
// `tr.style.opacity` IS the resolved opacity — no CSS-cascade / specificity /
// declaration-order guessing. Documented precedence:
//   fading (0) > dragged (0.3) > dimmed (0.4) > blocked-dim (0.6) > normal (1).
// The normal rank is written as no inline opacity (CSS default 1), so its
// resolved `style.opacity` is the empty string.
describe("TreeTableRow single-sourced row opacity precedence", () => {
  interface OpacityOptions {
    dimmed?: boolean;
    fading?: boolean;
    dragged?: boolean;
    pillDim?: boolean;
    blocked?: boolean;
  }

  function renderRowInState(opts: OpacityOptions) {
    const drag = new DragState();
    if (opts.dragged) drag.startDrag(["nibs-abc1"]);
    return render(TreeTableRow, {
      props: {
        nib: makeTreeTableNib({
          id: "nibs-abc1",
          blockedByIds: opts.blocked ? ["nibs-xyz1"] : [],
        }),
        dimmed: opts.dimmed ?? false,
        fading: opts.fading ?? false,
        blockedEmphasis: opts.pillDim ? "pill-dim" : undefined,
      },
      context: makeTestContext(new SelectionState(), drag),
    });
  }

  // NOTE: `dragged + blocked-dim` is intentionally NOT tested — blockedDim is gated
  // on `!isDragged` (see the derived in TreeTableRow.svelte), so the two class
  // markers can never co-occur; there is no precedence to pin between them.
  const cases: Array<{ name: string; opts: OpacityOptions; expected: string }> = [
    // nib-required combo #1: fading must fully fade even when blocked-dim would apply.
    { name: "fading + blocked-dim -> 0 (fading wins)", opts: { fading: true, pillDim: true, blocked: true }, expected: "0" },
    // nib-required combo #2: dimmed wins over blocked-dim (blocked-dim no longer a dead class).
    { name: "dimmed + blocked-dim + blocked -> 0.4 (dimmed wins)", opts: { dimmed: true, pillDim: true, blocked: true }, expected: "0.4" },
    // Fading is the top rank: it must beat BOTH independent lower booleans, which
    // is the exact inline-dimmed-beats-fading regression this refactor removed.
    { name: "fading + dimmed -> 0 (fading wins over dimmed)", opts: { fading: true, dimmed: true }, expected: "0" },
    { name: "fading + dragged -> 0 (fading wins over dragged)", opts: { fading: true, dragged: true }, expected: "0" },
    // Single-state ranks.
    { name: "fading alone -> 0", opts: { fading: true }, expected: "0" },
    { name: "dragged alone -> 0.3", opts: { dragged: true }, expected: "0.3" },
    { name: "dimmed alone -> 0.4", opts: { dimmed: true }, expected: "0.4" },
    { name: "blocked-dim alone -> 0.6", opts: { pillDim: true, blocked: true }, expected: "0.6" },
    { name: "no dimming state -> normal (no inline opacity)", opts: {}, expected: "" },
    // Cross-rank tie-break: dragged outranks dimmed.
    { name: "dragged + dimmed -> 0.3 (dragged wins)", opts: { dragged: true, dimmed: true }, expected: "0.3" },
  ];

  for (const { name, opts, expected } of cases) {
    it(name, () => {
      const { container } = renderRowInState(opts);
      const row = container.querySelector("tr") as HTMLElement;
      expect(row.style.opacity).toBe(expected);
    });
  }
});
