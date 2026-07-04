import type { SelectionState } from "../selection.svelte";
import type { RowData } from "../tableData";

export function useKeyboardNav(opts: {
  selection: SelectionState;
  getRows: () => RowData[];
  getVisibleRowIds: () => string[];
  getCollapsedIds: () => Set<string>;
  toggleNode: (id: string) => void;
  getScrollContainer: () => HTMLElement | null;
  onDragKeyDown: (e: KeyboardEvent) => void;
  navigateToNib: (id: string) => void;
}): {
  handleKeydown: (e: KeyboardEvent) => void;
} {
  const { selection } = opts;

  function scrollFocusedRowIntoView(nibId: string) {
    const scrollContainer = opts.getScrollContainer();
    if (!scrollContainer) return;
    const tr = scrollContainer.querySelector(`tr[data-nib-id="${nibId}"]`);
    if (tr) {
      tr.scrollIntoView({ block: "nearest" });
    }
  }

  function handleKeydown(event: KeyboardEvent) {
    // Escape cancels drag before anything else
    opts.onDragKeyDown(event);
    if (event.defaultPrevented) return;

    // Skip if focus is inside an input/interactive element
    const target = event.target as HTMLElement | null;
    if (target) {
      const tag = target.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
      if (target.isContentEditable) return;
      // Let native button activation (Enter/Space) work for action buttons inside rows
      if ((event.key === "Enter" || event.key === " ") && tag === "BUTTON" && target.closest("[data-action]")) return;
    }

    const currentRows = opts.getRows();
    if (currentRows.length === 0) return;

    const focusedIndex = selection.focusedNibId
      ? currentRows.findIndex(r => r.nib.id === selection.focusedNibId)
      : -1;

    switch (event.key) {
      case "ArrowDown": {
        event.preventDefault();
        if (focusedIndex < 0) {
          const nibId = currentRows[0].nib.id;
          selection.focus(nibId);
          requestAnimationFrame(() => scrollFocusedRowIntoView(nibId));
        } else if (focusedIndex < currentRows.length - 1) {
          const nibId = currentRows[focusedIndex + 1].nib.id;
          selection.focus(nibId);
          if (event.shiftKey) selection.rangeSelect(nibId, opts.getVisibleRowIds());
          requestAnimationFrame(() => scrollFocusedRowIntoView(nibId));
        }
        break;
      }
      case "ArrowUp": {
        event.preventDefault();
        if (focusedIndex < 0) {
          const nibId = currentRows[0].nib.id;
          selection.focus(nibId);
          requestAnimationFrame(() => scrollFocusedRowIntoView(nibId));
        } else if (focusedIndex > 0) {
          const nibId = currentRows[focusedIndex - 1].nib.id;
          selection.focus(nibId);
          if (event.shiftKey) selection.rangeSelect(nibId, opts.getVisibleRowIds());
          requestAnimationFrame(() => scrollFocusedRowIntoView(nibId));
        }
        break;
      }
      case "ArrowLeft": {
        event.preventDefault();
        if (focusedIndex >= 0) {
          const row = currentRows[focusedIndex];
          const collapsedIds = opts.getCollapsedIds();
          if (row.hasChildren && !collapsedIds.has(row.nib.id)) {
            opts.toggleNode(row.nib.id);
          } else if (row.nib.parentId) {
            const parentIndex = currentRows.findIndex(r => r.nib.id === row.nib.parentId);
            if (parentIndex >= 0) {
              const nibId = currentRows[parentIndex].nib.id;
              selection.focus(nibId);
              requestAnimationFrame(() => scrollFocusedRowIntoView(nibId));
            }
          }
        }
        break;
      }
      case "ArrowRight": {
        event.preventDefault();
        if (focusedIndex >= 0) {
          const row = currentRows[focusedIndex];
          const collapsedIds = opts.getCollapsedIds();
          if (row.hasChildren && collapsedIds.has(row.nib.id)) {
            opts.toggleNode(row.nib.id);
          }
        }
        break;
      }
      case "Enter": {
        event.preventDefault();
        if (focusedIndex >= 0) {
          opts.navigateToNib(currentRows[focusedIndex].nib.id);
        }
        break;
      }
      case " ": {
        event.preventDefault();
        selection.toggleFocusedSelection();
        break;
      }
      default:
        return;
    }
  }

  return {
    handleKeydown,
  };
}
