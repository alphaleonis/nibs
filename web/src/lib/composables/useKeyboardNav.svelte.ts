import type { ContainmentIndex } from "../containment";
import type { PanelPolicy, SelectionState } from "../selection.svelte";
import type { RowData } from "../tableData";
import type { OpenDetailGesture } from "../types";
import { isSyntheticRowId } from "../tree";

export function useKeyboardNav(opts: {
  selection: SelectionState;
  getRows: () => RowData[];
  getVisibleRowIds: () => string[];
  getCollapsedIds: () => ReadonlySet<string>;
  /** What the current view draws inside what — ArrowLeft's step out of a row. */
  getContainment: () => ContainmentIndex;
  toggleNode: (id: string) => void;
  getScrollContainer: () => HTMLElement | null;
  onDragKeyDown: (e: KeyboardEvent) => void;
  navigateToNib: (id: string) => void;
  /** Read at keypress time, so a settings change applies without a rebuild. */
  getOpenDetailOn: () => OpenDetailGesture;
}): {
  handleKeydown: (e: KeyboardEvent) => void;
} {
  const { selection } = opts;

  /** "detach" under open-on-double-click, where only an explicit open moves the
   *  detail panel. The keyboard twin of TreeTable's shift/ctrl-click rule. */
  function panelPolicy(): PanelPolicy {
    return opts.getOpenDetailOn() === "double" ? "detach" : "follow";
  }

  function scrollFocusedRowIntoView(nibId: string) {
    const scrollContainer = opts.getScrollContainer();
    if (!scrollContainer) return;
    // Row ids can contain quotes and backslashes: section ids derive from area
    // paths, nib ids from file names.
    const tr = scrollContainer.querySelector(`tr[data-nib-id="${CSS.escape(nibId)}"]`);
    if (tr) {
      tr.scrollIntoView({ block: "nearest" });
    }
  }

  // Which row Enter/Space acts on: the event's own row when it has one (Tab focus
  // on a row's title button), else `focusedNibId` (arrow-key nav keeps DOM focus
  // on the grid container).
  function resolveTargetId(event: KeyboardEvent, currentRows: RowData[], focusedIndex: number): string | null {
    const target = event.target as HTMLElement | null;
    const domRow = target?.closest?.("tr[data-nib-id]") as HTMLElement | null;
    if (domRow?.dataset.nibId) return domRow.dataset.nibId;
    return focusedIndex >= 0 ? currentRows[focusedIndex].nib.id : null;
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
      // Row action buttons keep native Enter/Space activation. The title does
      // not: Space toggles selection and Enter opens, below.
      if (event.key === "Enter" || event.key === " ") {
        if (tag === "BUTTON") {
          const action = target.closest("[data-action]")?.getAttribute("data-action");
          if (action && action !== "title") return;
        }
      }
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
          if (event.shiftKey) selection.rangeSelect(nibId, opts.getVisibleRowIds(), panelPolicy());
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
          if (event.shiftKey) selection.rangeSelect(nibId, opts.getVisibleRowIds(), panelPolicy());
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
          } else {
            // Step to the row that DRAWS this one — not `nib.parentId` (a grouped
            // view need not draw it) nor `displayParentId` (null inside a
            // section). A rendered row's container is rendered too; null is the
            // display root.
            const nibId = opts.getContainment().containerOf(row.nib.id);
            if (nibId !== null) {
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
        const targetId = resolveTargetId(event, currentRows, focusedIndex);
        if (targetId === null) break;
        // A synthetic bucket names no nib: toggle its group instead of opening it.
        if (isSyntheticRowId(targetId)) {
          opts.toggleNode(targetId);
        } else {
          opts.navigateToNib(targetId);
        }
        break;
      }
      case " ": {
        event.preventDefault();
        const targetId = resolveTargetId(event, currentRows, focusedIndex);
        if (targetId === null) break;
        // Buckets are not selectable. toggleSelect also moves focus and anchor
        // to the toggled row.
        if (isSyntheticRowId(targetId)) break;
        selection.toggleSelect(targetId, panelPolicy());
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
