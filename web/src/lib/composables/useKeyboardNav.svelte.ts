import type { SelectionState } from "../selection.svelte";
import type { RowData } from "../tableData";
import type { OpenDetailGesture } from "../types";
import { isBucketId } from "../tree";

export function useKeyboardNav(opts: {
  selection: SelectionState;
  getRows: () => RowData[];
  getVisibleRowIds: () => string[];
  getCollapsedIds: () => ReadonlySet<string>;
  toggleNode: (id: string) => void;
  getScrollContainer: () => HTMLElement | null;
  onDragKeyDown: (e: KeyboardEvent) => void;
  navigateToNib: (id: string) => void;
  /** The resolved open-detail preference. Read at keypress time so a settings
   *  change takes effect without rebuilding the composable. */
  getOpenDetailOn: () => OpenDetailGesture;
}): {
  handleKeydown: (e: KeyboardEvent) => void;
} {
  const { selection } = opts;

  /** Whether a bulk-selection gesture may move the detail panel. False under the
   *  "open on double-click" preference, where the panel is decoupled from the
   *  selection and only an explicit open gesture writes it — the keyboard twin of
   *  the shift/ctrl-click rule in TreeTable's click handler. */
  function retargetPanel(): boolean {
    return opts.getOpenDetailOn() !== "double";
  }

  function scrollFocusedRowIntoView(nibId: string) {
    const scrollContainer = opts.getScrollContainer();
    if (!scrollContainer) return;
    const tr = scrollContainer.querySelector(`tr[data-nib-id="${nibId}"]`);
    if (tr) {
      tr.scrollIntoView({ block: "nearest" });
    }
  }

  // Which row an Enter/Space acts on. Prefer the row that the KEY EVENT came
  // from — i.e. the DOM-focused control's `<tr data-nib-id>` ancestor (the Tab
  // case, where DOM focus sits on a row's title button). Fall back to the virtual
  // `focusedNibId` only when the event has no row ancestor, which is the arrow-key
  // case: arrow nav leaves DOM focus on the grid container and tracks the focused
  // row solely in `focusedNibId`. Resolving from the event is what lets Enter/Space
  // act on the Tab-focused row rather than a stale `focusedNibId` — e.g. after
  // Escape clears `focusedNibId` while the title button keeps DOM focus.
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
      // Let native button activation (Enter/Space) work for the row's action
      // buttons — toggle and add-child — but NOT the title. The title routes
      // through the keyboard path below so Space toggles selection (instead of
      // firing a native click that opens the nib) and Enter opens it. Both the
      // Enter and Space cases resolve WHICH row to act on from the event's own
      // DOM row ancestor, so they work even when DOM focus (Tab) and the virtual
      // `focusedNibId` disagree — no focus-sync side channel required.
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
          if (event.shiftKey) selection.rangeSelect(nibId, opts.getVisibleRowIds(), { retargetPanel: retargetPanel() });
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
          if (event.shiftKey) selection.rangeSelect(nibId, opts.getVisibleRowIds(), { retargetPanel: retargetPanel() });
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
        const targetId = resolveTargetId(event, currentRows, focusedIndex);
        if (targetId === null) break;
        // A synthetic grouping bucket is not a real nib — Enter toggles its group
        // (like its caret) instead of opening a detail view for an unresolvable id.
        if (isBucketId(targetId)) {
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
        // Buckets are not selectable (SelectionState.toggleSelect rejects them),
        // so Space on a bucket is a no-op. toggleSelect also moves focus/anchor to
        // the toggled row — matching mouse Ctrl/Cmd-click — so the Space-toggled
        // row becomes the focused row.
        if (isBucketId(targetId)) break;
        selection.toggleSelect(targetId, { retargetPanel: retargetPanel() });
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
