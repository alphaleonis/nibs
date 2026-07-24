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
  it("clicking the header BODY (the <th> itself, not the old label button) sorts that field", () => {
    // The whole sortable <th> is the sort control now: a click on the cell's
    // padding — away from the label span — still cycles the sort. Removing the
    // <th> onclick handler makes this fail (the bite test for nibs-5ela).
    const { container, onSort } = renderHeader();
    const idTh = container.querySelector("thead th[data-col-key='id']") as HTMLElement;
    idTh.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(onSort).toHaveBeenCalledWith("id");
  });

  it("clicking the label span sorts too (the click bubbles to the <th> control)", () => {
    const { container, onSort } = renderHeader();
    const label = container.querySelector("[data-testid='table-sort-id']") as HTMLElement;
    label.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(onSort).toHaveBeenCalledWith("id");
  });

  it("the inner sort <button> is absent; the label text + arrow remain", () => {
    const { container } = renderHeader({ activeSort: { field: "id", direction: "asc" } as TableSort });
    // No focusable inner button, and no aria-label override — the columnheader's
    // accessible name stays its visible label so cell/column association reads the
    // plain column name.
    expect(container.querySelector('button[aria-label^="Sort by"]')).toBeNull();
    const idTh = container.querySelector("thead th[data-col-key='id']") as HTMLElement;
    expect(idTh.getAttribute("aria-label")).toBeNull();
    // Label text + direction arrow still render inside the header.
    const label = container.querySelector("[data-testid='table-sort-id']") as HTMLElement;
    expect(label.textContent?.trim()).toBe("ID");
    expect(container.querySelector("[data-testid='table-sort-arrow-id']")).toBeInTheDocument();
  });

  it("a completed drag suppresses the following sort click (consumeClickSuppression)", () => {
    const columnDrag = makeDragStub({ consumeClickSuppression: vi.fn(() => true) });
    const { container, onSort } = renderHeader({ columnDrag });
    const idTh = container.querySelector("thead th[data-col-key='id']") as HTMLElement;
    idTh.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(columnDrag.consumeClickSuppression).toHaveBeenCalled();
    expect(onSort).not.toHaveBeenCalled();
  });

  it("a click whose target is the resize edge-handle does NOT sort", () => {
    const { container, onSort } = renderHeader();
    const handle = container.querySelector("thead th[data-col-key='id'] .resize-handle") as HTMLElement;
    handle.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    expect(onSort).not.toHaveBeenCalled();
  });
});

describe("TableHeader — keyboard sort (the <th> is the control)", () => {
  it("makes sortable <th>s focusable (tabindex=0) and leaves the actions column untabbable", () => {
    const { container } = renderHeader();
    const idTh = container.querySelector("thead th[data-col-key='id']") as HTMLElement;
    expect(idTh.getAttribute("tabindex")).toBe("0");
    // The 32px actions column is not a sort control.
    const actionsTh = container.querySelector("thead th:not([data-col-key])") as HTMLElement;
    expect(actionsTh.getAttribute("tabindex")).toBeNull();
    expect(actionsTh.getAttribute("aria-label")).toBeNull();
  });

  it("Enter on a focused sortable <th> sorts that field", () => {
    const { container, onSort } = renderHeader();
    const idTh = container.querySelector("thead th[data-col-key='id']") as HTMLElement;
    idTh.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    expect(onSort).toHaveBeenCalledWith("id");
  });

  it("Space on a focused sortable <th> sorts and prevents default (no page scroll)", () => {
    const { container, onSort } = renderHeader();
    const idTh = container.querySelector("thead th[data-col-key='id']") as HTMLElement;
    const ev = new KeyboardEvent("keydown", { key: " ", bubbles: true, cancelable: true });
    idTh.dispatchEvent(ev);
    expect(onSort).toHaveBeenCalledWith("id");
    expect(ev.defaultPrevented).toBe(true);
  });

  it("consumes key repeats and modifier chords without sorting", () => {
    const { container, onSort } = renderHeader();
    const idTh = container.querySelector("thead th[data-col-key='id']") as HTMLElement;
    const repeat = new KeyboardEvent("keydown", { key: "Enter", repeat: true, bubbles: true, cancelable: true });
    const modified = new KeyboardEvent("keydown", { key: "Enter", ctrlKey: true, bubbles: true, cancelable: true });
    idTh.dispatchEvent(repeat);
    idTh.dispatchEvent(modified);
    // A repeat/modifier press does not sort...
    expect(onSort).not.toHaveBeenCalled();
    // ...but the header still consumes the key (preventDefault before the sort
    // gate) so it can never leak to the grid keyboard-nav handler on the scroll
    // container. The TreeTable-level regression proves the no-leak end to end.
    expect(repeat.defaultPrevented).toBe(true);
    expect(modified.defaultPrevented).toBe(true);
  });

  it("keydown on the non-sortable actions column never sorts", () => {
    const { container, onSort } = renderHeader();
    const actionsTh = container.querySelector("thead th:not([data-col-key])") as HTMLElement;
    actionsTh.dispatchEvent(new KeyboardEvent("keydown", { key: "Enter", bubbles: true, cancelable: true }));
    expect(onSort).not.toHaveBeenCalled();
  });

  it("the sortable <th> reflects the active sort direction via aria-sort", () => {
    const { container } = renderHeader({ activeSort: { field: "id", direction: "asc" } as TableSort });
    const idTh = container.querySelector("thead th[data-col-key='id']") as HTMLElement;
    expect(idTh.getAttribute("aria-sort")).toBe("ascending");
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
