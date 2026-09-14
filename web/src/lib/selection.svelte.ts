import { isSyntheticRowId } from "./tree";

/** Whether the detail panel follows a bulk gesture's selection.
 *
 *   - "follow": the panel shows the single selected id, and closes when the set
 *     is empty or multi. For `openDetailOn: "single"`.
 *   - "detach": `selectedNibId` is untouched. For `openDetailOn: "double"`, where
 *     only explicit open gestures open, close or retarget the panel. */
export type PanelPolicy = "follow" | "detach";

export class SelectionState {
  selectedNibId: string | null = $state(null);
  focusedNibId: string | null = $state(null);
  /** Never holds a synthetic row id: every method that adds ids rejects them. */
  selectedIds: Set<string> = $state(new Set());
  anchorId: string | null = $state(null);
  pendingEnsureVisibleId: string | null = $state(null);
  panelOpen: boolean = $derived(this.selectedNibId !== null);
  hasMultiSelect: boolean = $derived(this.selectedIds.size > 1);

  /** Select a single nib and open it in the detail panel. Ignores synthetic row
   *  ids, which arrive via `e` on a focused bucket, a right-click, or a stale
   *  `?nib=` URL. */
  select(nibId: string): void {
    if (isSyntheticRowId(nibId)) return;
    this.selectedNibId = nibId;
    this.focusedNibId = nibId;
    this.selectedIds = new Set([nibId]);
    this.anchorId = nibId;
  }

  /** Select a single nib WITHOUT opening it, for the "open on double-click"
   *  preference. Leaves `selectedNibId` untouched, so an open panel keeps its nib
   *  while the selection points elsewhere. Ignores synthetic row ids. */
  selectOnly(nibId: string): void {
    if (isSyntheticRowId(nibId)) return;
    this.focusedNibId = nibId;
    this.selectedIds = new Set([nibId]);
    this.anchorId = nibId;
  }

  /** Closes the detail panel. Intentionally preserves selectedIds and anchorId
   *  so that the Escape hierarchy can deselect in a separate step. */
  close(): void {
    this.selectedNibId = null;
  }

  focus(nibId: string): void {
    this.focusedNibId = nibId;
  }

  clearFocus(): void {
    this.focusedNibId = null;
  }

  /** Ctrl/Cmd+click, or Space on a focused row: toggle the nib in `selectedIds`
   *  and move the anchor. Ignores synthetic row ids. */
  toggleSelect(nibId: string, panel: PanelPolicy): void {
    if (isSyntheticRowId(nibId)) return;
    const next = new Set(this.selectedIds);
    if (next.has(nibId)) {
      next.delete(nibId);
    } else {
      next.add(nibId);
    }
    this.selectedIds = next;
    this.anchorId = nibId;
    this.focusedNibId = nibId;
    if (panel === "detach") return;
    if (next.size === 1) {
      this.selectedNibId = [...next][0];
    } else {
      this.selectedNibId = null;
    }
  }

  /**
   * Shift+click / shift+arrow: select from the anchor to `nibId` in visible row
   * order.
   *
   * Synthetic rows inside the range are dropped rather than truncating it, so the
   * nibs on both sides stay selected; a synthetic endpoint contributes no id. A
   * real nib heading a section is included like any other row.
   */
  rangeSelect(nibId: string, visibleIds: string[], panel: PanelPolicy): void {
    const anchor = this.anchorId ?? nibId;
    const startIndex = visibleIds.indexOf(anchor);
    const endIndex = visibleIds.indexOf(nibId);
    if (startIndex < 0 || endIndex < 0) return;

    const lo = Math.min(startIndex, endIndex);
    const hi = Math.max(startIndex, endIndex);
    const rangeIds = visibleIds.slice(lo, hi + 1).filter((id) => !isSyntheticRowId(id));

    this.selectedIds = new Set(rangeIds);
    this.focusedNibId = nibId;
    // Don't change anchorId — it stays at the original click point
    if (panel === "detach") return;
    if (rangeIds.length === 1) {
      this.selectedNibId = rangeIds[0];
    } else {
      this.selectedNibId = null;
    }
  }

  isSelected(nibId: string): boolean {
    return this.selectedIds.has(nibId);
  }

  /**
   * Prunes `selectedIds`, the anchor and focus to ids in `matchingIds`, so a bulk
   * action never applies to rows a filter or view switch took away. Leaves
   * `selectedNibId` alone.
   *
   * Writes `selectedIds` only when something is dropped, so it is safe in an
   * `$effect`.
   */
  retainOnly(matchingIds: ReadonlySet<string>): void {
    let changed = false;
    const next = new Set<string>();
    for (const id of this.selectedIds) {
      if (matchingIds.has(id)) {
        next.add(id);
      } else {
        changed = true;
      }
    }
    if (changed) {
      this.selectedIds = next;
    }
    if (this.anchorId !== null && !matchingIds.has(this.anchorId)) {
      this.anchorId = null;
    }
    if (this.focusedNibId !== null && !matchingIds.has(this.focusedNibId)) {
      this.focusedNibId = null;
    }
  }

  /** Clears selectedIds and anchor */
  deselectAll(): void {
    this.selectedIds = new Set();
    this.anchorId = null;
  }

  /** Clears everything, including `selectedNibId` and `pendingEnsureVisibleId`.
   *
   *  Not for post-mutation cleanup: it would close a panel showing a nib the
   *  mutation never touched, discarding unsaved edits. Use `clearAfterMutation`
   *  (actionTarget.ts). */
  clearAll(): void {
    this.selectedIds = new Set();
    this.selectedNibId = null;
    this.focusedNibId = null;
    this.anchorId = null;
    this.pendingEnsureVisibleId = null;
  }

  /** Request that TreeTable expand ancestors and scroll nibId into view */
  ensureVisible(nibId: string): void {
    this.pendingEnsureVisibleId = nibId;
  }

  /** Clear the pending ensureVisible request (called by TreeTable after processing) */
  clearEnsureVisible(): void {
    this.pendingEnsureVisibleId = null;
  }
}
