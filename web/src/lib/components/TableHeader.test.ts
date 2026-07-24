import { render } from "@testing-library/svelte";
import { describe, it, expect, vi } from "vitest";
import TableHeader from "./TableHeader.svelte";
import type { ColumnKey, TableSort } from "../types";
import { DEFAULT_COLUMN_WIDTHS } from "../types";
import type { ColumnDrag } from "../composables/useColumnDrag.svelte";
import type { ColumnResize } from "../composables/useColumnResize.svelte";
import { SelectionState } from "../selection.svelte";
import { DragState } from "../drag.svelte";
import { makeTestContext } from "../contexts";

// Stub composables: TableHeader only calls their handlers and reads their drag
// state, so plain objects with spies satisfy the ColumnResize / ColumnDrag
// contracts and let us assert the exact wiring the <th> shell performs.
function makeResizeStub(overrides: Partial<ColumnResize> = {}): ColumnResize {
  return {
    resizingColumn: null,
    onPointerDown: vi.fn(),
    onPointerMove: vi.fn(),
    onPointerUp: vi.fn(),
    onDblClick: vi.fn(),
    ...overrides,
  };
}

function makeDragStub(overrides: Partial<ColumnDrag> = {}): ColumnDrag {
  return {
    draggedKey: null,
    targetKey: null,
    targetSide: null,
    isDragging: false,
    onHeaderPointerDown: vi.fn(),
    consumeClickSuppression: vi.fn(() => false),
    ...overrides,
  };
}

function renderHeader(props: Record<string, unknown> = {}) {
  const columnResize = (props.columnResize as ColumnResize | undefined) ?? makeResizeStub();
  const columnDrag = (props.columnDrag as ColumnDrag | undefined) ?? makeDragStub();
  const onSort = (props.onSort as ((field: string) => void) | undefined) ?? vi.fn();
  const onExpandAll = (props.onExpandAll as (() => void) | undefined) ?? vi.fn();
  const onCollapseAll = (props.onCollapseAll as (() => void) | undefined) ?? vi.fn();
  const result = render(TableHeader, {
    props: {
      columns: ["id", "title", "state"] as ColumnKey[],
      columnWidths: DEFAULT_COLUMN_WIDTHS,
      activeSort: null as TableSort | null,
      showColumn: () => true,
      columnResize,
      columnDrag,
      onSort,
      onExpandAll,
      onCollapseAll,
      ...props,
    } as any,
    context: makeTestContext(new SelectionState(), new DragState()),
  });
  return { ...result, columnResize, columnDrag, onSort, onExpandAll, onCollapseAll };
}

describe("TableHeader — render", () => {
  it("renders a <thead> with the actions column and one <th> per column, in order", () => {
    const { container } = renderHeader({ columns: ["state", "title", "id"] as ColumnKey[] });
    expect(container.querySelector("thead")).toBeInTheDocument();
    const keys = Array.from(container.querySelectorAll("thead th[data-col-key]")).map((th) =>
      th.getAttribute("data-col-key"),
    );
    expect(keys).toEqual(["state", "title", "id"]);
    // Actions column (expand/collapse-all) precedes the data columns.
    expect(container.querySelector("[data-testid='expand-all']")).toBeInTheDocument();
    expect(container.querySelector("[data-testid='collapse-all']")).toBeInTheDocument();
  });

  it("applies each column's width to its <th>", () => {
    const { container } = renderHeader();
    const idTh = container.querySelector("thead th[data-col-key='id']") as HTMLElement;
    expect(idTh.style.width).toBe(`${DEFAULT_COLUMN_WIDTHS.id}px`);
  });

  it("renders a resize handle inside every column header", () => {
    const { container } = renderHeader();
    const handles = container.querySelectorAll("thead th[data-col-key] .resize-handle");
    expect(handles.length).toBe(3);
  });

  it("reflects the active sort as aria-sort + a direction arrow; others report none", () => {
    const { container } = renderHeader({ activeSort: { field: "state", direction: "desc" } as TableSort });
    const stateTh = container.querySelector("thead th[data-col-key='state']") as HTMLElement;
    const idTh = container.querySelector("thead th[data-col-key='id']") as HTMLElement;
    expect(stateTh.getAttribute("aria-sort")).toBe("descending");
    expect(idTh.getAttribute("aria-sort")).toBe("none");
    expect(container.querySelector("[data-testid='table-sort-arrow-state']")).toBeInTheDocument();
    expect(container.querySelector("[data-testid='table-sort-arrow-id']")).not.toBeInTheDocument();
  });
});

describe("TableHeader — expand/collapse-all wiring", () => {
  it("clicking expand-all / collapse-all invokes the callbacks", () => {
    const { container, onExpandAll, onCollapseAll } = renderHeader();
    (container.querySelector("[data-testid='expand-all']") as HTMLElement).click();
    (container.querySelector("[data-testid='collapse-all']") as HTMLElement).click();
    expect(onExpandAll).toHaveBeenCalledTimes(1);
    expect(onCollapseAll).toHaveBeenCalledTimes(1);
  });
});

describe("TableHeader — sort wiring", () => {
  it("clicking a sort button cycles that field via onSort", () => {
    const { container, onSort } = renderHeader();
    const btn = container.querySelector("[data-testid='table-sort-id']") as HTMLElement;
    btn.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(onSort).toHaveBeenCalledWith("id");
  });

  it("a completed drag suppresses the following sort click (consumeClickSuppression)", () => {
    const columnDrag = makeDragStub({ consumeClickSuppression: vi.fn(() => true) });
    const { container, onSort } = renderHeader({ columnDrag });
    const btn = container.querySelector("[data-testid='table-sort-id']") as HTMLElement;
    btn.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(columnDrag.consumeClickSuppression).toHaveBeenCalled();
    expect(onSort).not.toHaveBeenCalled();
  });
});

describe("TableHeader — resize/reorder arbitration wiring", () => {
  it("pointerdown on a header body starts a reorder via columnDrag", () => {
    const { container, columnDrag, columnResize } = renderHeader();
    const idTh = container.querySelector("thead th[data-col-key='id']") as HTMLElement;
    idTh.dispatchEvent(new PointerEvent("pointerdown", { clientX: 10, clientY: 10, button: 0, bubbles: true }));
    expect(columnDrag.onHeaderPointerDown).toHaveBeenCalledTimes(1);
    expect((columnDrag.onHeaderPointerDown as any).mock.calls[0][0]).toBe("id");
    // The header body is not the resize edge — no resize started.
    expect(columnResize.onPointerDown).not.toHaveBeenCalled();
  });

  it("pointerdown on the resize edge-handle resizes and does NOT start a reorder", () => {
    const { container, columnDrag, columnResize } = renderHeader();
    const idTh = container.querySelector("thead th[data-col-key='id']") as HTMLElement;
    const handle = idTh.querySelector(".resize-handle") as HTMLElement;
    handle.dispatchEvent(new PointerEvent("pointerdown", { clientX: 100, clientY: 5, button: 0, bubbles: true }));
    expect(columnResize.onPointerDown).toHaveBeenCalledTimes(1);
    // Arbitration: the header-drag guard bails on the resize handle.
    expect(columnDrag.onHeaderPointerDown).not.toHaveBeenCalled();
  });

  it("double-clicking the resize handle auto-fits via onDblClick", () => {
    const { container, columnResize } = renderHeader();
    const handle = container.querySelector("thead th[data-col-key='state'] .resize-handle") as HTMLElement;
    handle.dispatchEvent(new MouseEvent("dblclick", { bubbles: true }));
    expect(columnResize.onDblClick).toHaveBeenCalledTimes(1);
    expect((columnResize.onDblClick as any).mock.calls[0][0]).toBe("state");
  });
});
