import type { ColumnKey } from "../types";
import { ALL_COLUMN_KEYS, DEFAULT_COLUMN_WIDTHS } from "../types";

export const MIN_COLUMN_WIDTH = 40;
export const MAX_COLUMN_WIDTH = 4000;

export interface ColumnResize {
  /** The column currently being resized (null when idle). */
  readonly resizingColumn: ColumnKey | null;
  /** Edge-handle pointerdown — begins tracking a drag-resize. */
  onPointerDown: (e: PointerEvent, column: ColumnKey) => void;
  onPointerMove: (e: PointerEvent) => void;
  onPointerUp: () => void;
  /** Double-click the edge handle to auto-fit the column to its content. */
  onDblClick: (column: ColumnKey, showColumn: (key: ColumnKey) => boolean) => void;
}

export function useColumnResize(opts: {
  getTableEl: () => HTMLTableElement | null;
  getColumnWidths: () => Record<ColumnKey, number>;
  setColumnWidth: (key: ColumnKey, width: number) => void;
  onResizeEnd?: () => void;
}): ColumnResize {
  let resizingColumn: ColumnKey | null = $state(null);
  let resizeStartX = 0;
  let resizeStartWidth = 0;

  function onPointerDown(e: PointerEvent, column: ColumnKey) {
    e.preventDefault();
    resizingColumn = column;
    resizeStartX = e.clientX;
    resizeStartWidth = opts.getColumnWidths()[column] ?? DEFAULT_COLUMN_WIDTHS[column];
    (e.target as HTMLElement).setPointerCapture(e.pointerId);
  }

  function onPointerMove(e: PointerEvent) {
    if (!resizingColumn) return;
    const delta = e.clientX - resizeStartX;
    const newWidth = Math.min(MAX_COLUMN_WIDTH, Math.max(MIN_COLUMN_WIDTH, resizeStartWidth + delta));
    opts.setColumnWidth(resizingColumn, newWidth);
  }

  function onPointerUp() {
    resizingColumn = null;
    opts.onResizeEnd?.();
  }

  function measureColumnFitWidth(key: ColumnKey, showColumn: (k: ColumnKey) => boolean): number | null {
    const tableEl = opts.getTableEl();
    if (!tableEl) return null;
    // Find the column's <td> index, accounting for hidden columns
    let colIdx = 1; // skip actions column at index 0
    for (const k of ALL_COLUMN_KEYS) {
      if (k === key) break;
      if (showColumn(k)) colIdx++;
    }
    // Temporarily switch to auto layout so the browser computes natural content widths.
    const savedLayout = tableEl.style.tableLayout;
    const savedWidth = tableEl.style.width;
    tableEl.style.tableLayout = "auto";
    tableEl.style.width = "0";
    let maxWidth = 0;
    for (let i = 0; i < tableEl.rows.length; i++) {
      const cell = tableEl.rows[i].cells[colIdx];
      if (cell) maxWidth = Math.max(maxWidth, cell.offsetWidth);
    }
    // Restore fixed layout
    tableEl.style.tableLayout = savedLayout;
    tableEl.style.width = savedWidth;
    // Add padding buffer
    return maxWidth > 0 ? maxWidth + 8 : null;
  }

  function onDblClick(column: ColumnKey, showColumn: (k: ColumnKey) => boolean) {
    const width = Math.max(
      MIN_COLUMN_WIDTH,
      Math.min(MAX_COLUMN_WIDTH, measureColumnFitWidth(column, showColumn) ?? DEFAULT_COLUMN_WIDTHS[column]),
    );
    opts.setColumnWidth(column, width);
    opts.onResizeEnd?.();
  }

  return {
    get resizingColumn() { return resizingColumn; },
    onPointerDown,
    onPointerMove,
    onPointerUp,
    onDblClick,
  };
}
