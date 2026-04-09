import { describe, it, expect, vi } from "vitest";
import type { ColumnKey } from "../types";
import { ALL_COLUMN_KEYS } from "../types";
import { useColumnResize } from "./useColumnResize.svelte";

describe("useColumnResize", () => {
  function setup(overrides: {
    columnWidths?: Record<ColumnKey, number>;
    onResizeEnd?: () => void;
  } = {}) {
    const setColumnWidth = vi.fn();
    const onResizeEnd = overrides.onResizeEnd ?? vi.fn();
    const getColumnWidths = () =>
      overrides.columnWidths ?? {
        id: 100,
        parent: 160,
        type: 80,
        title: 400,
        state: 120,
        effort: 70,
        tags: 150,
      };

    const tableEl = document.createElement("table");
    document.body.appendChild(tableEl);

    const result = useColumnResize({
      getTableEl: () => tableEl,
      getColumnWidths,
      setColumnWidth,
      onResizeEnd,
    });

    return { result, setColumnWidth, onResizeEnd, tableEl };
  }

  it("pointer down starts tracking, move updates width, up ends resize", () => {
    const { result, setColumnWidth, onResizeEnd } = setup();

    // Initially not resizing
    expect(result.resizingColumn).toBeNull();

    // Simulate pointer down on resize handle for the "title" column
    const handleEl = document.createElement("div");
    const downEvent = new PointerEvent("pointerdown", {
      clientX: 500,
      bubbles: true,
    });
    Object.defineProperty(downEvent, "target", { value: handleEl });
    result.onPointerDown(downEvent, "title");

    // Should now be tracking
    expect(result.resizingColumn).toBe("title");

    // Simulate pointer move — delta of +50px from start
    const moveEvent = new PointerEvent("pointermove", {
      clientX: 550,
      bubbles: true,
    });
    result.onPointerMove(moveEvent);

    // setColumnWidth should be called with the title column and 400+50=450
    expect(setColumnWidth).toHaveBeenCalledWith("title", 450);

    // Simulate pointer up
    result.onPointerUp();

    // Should stop tracking
    expect(result.resizingColumn).toBeNull();
    // onResizeEnd should fire
    expect(onResizeEnd).toHaveBeenCalledOnce();
  });

  it("double-click auto-fits column to measured content width", () => {
    const { result, setColumnWidth, onResizeEnd, tableEl } = setup();

    // Build a minimal table structure with a header row and one data row.
    // The "title" column is index 3 (after actions=0, id=1, parent=2, type=3... wait,
    // title is at ALL_COLUMN_KEYS index 3). In the real table the first column is
    // actions (index 0), then visible columns. For the auto-fit measurement, we need
    // showColumn to be provided. Let's build a simple table with known cell widths.
    const headerRow = tableEl.insertRow();
    // actions column
    const actionsCell = headerRow.insertCell();
    // Insert cells for each column key
    for (const _key of ALL_COLUMN_KEYS) {
      headerRow.insertCell();
    }

    const dataRow = tableEl.insertRow();
    dataRow.insertCell(); // actions
    for (const _key of ALL_COLUMN_KEYS) {
      dataRow.insertCell();
    }

    // Mock offsetWidth for the title column cells.
    // title is ALL_COLUMN_KEYS index 3 (id=0, parent=1, type=2, title=3),
    // so table cell index is 4 (actions=0, then 1-based for columns).
    const titleIndex = ALL_COLUMN_KEYS.indexOf("title") + 1; // +1 for actions column
    Object.defineProperty(headerRow.cells[titleIndex], "offsetWidth", { value: 200 });
    Object.defineProperty(dataRow.cells[titleIndex], "offsetWidth", { value: 250 });

    // Call onDblClick, providing showColumn that shows all columns
    const showColumn = (_key: ColumnKey) => true;
    result.onDblClick("title", showColumn);

    // Should set width to max(200,250) + 8 padding = 258
    expect(setColumnWidth).toHaveBeenCalledWith("title", 258);
    expect(onResizeEnd).toHaveBeenCalledOnce();
  });
});
