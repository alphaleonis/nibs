export class SelectionState {
  selectedNibId: string | null = $state(null);
  focusedNibId: string | null = $state(null);
  selectedIds: Set<string> = $state(new Set());
  anchorId: string | null = $state(null);
  pendingEnsureVisibleId: string | null = $state(null);
  panelOpen: boolean = $derived(this.selectedNibId !== null);
  hasMultiSelect: boolean = $derived(this.selectedIds.size > 1);

  select(nibId: string): void {
    this.selectedNibId = nibId;
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

  /** Ctrl/Cmd+click: toggle nib in/out of selectedIds, update anchor */
  toggleSelect(nibId: string): void {
    const next = new Set(this.selectedIds);
    if (next.has(nibId)) {
      next.delete(nibId);
    } else {
      next.add(nibId);
    }
    this.selectedIds = next;
    this.anchorId = nibId;
    this.focusedNibId = nibId;
    // If we end up with exactly one selected, also set it as the detail-panel selection
    if (next.size === 1) {
      this.selectedNibId = [...next][0];
    } else {
      // Multi-select: don't show detail panel for any single nib
      this.selectedNibId = null;
    }
  }

  /** Shift+click: select range from anchor to nibId using the visible row order */
  rangeSelect(nibId: string, visibleIds: string[]): void {
    const anchor = this.anchorId ?? nibId;
    const startIndex = visibleIds.indexOf(anchor);
    const endIndex = visibleIds.indexOf(nibId);
    if (startIndex < 0 || endIndex < 0) return;

    const lo = Math.min(startIndex, endIndex);
    const hi = Math.max(startIndex, endIndex);
    const rangeIds = visibleIds.slice(lo, hi + 1);

    this.selectedIds = new Set(rangeIds);
    this.focusedNibId = nibId;
    // Don't change anchorId — it stays at the original click point
    if (rangeIds.length === 1) {
      this.selectedNibId = rangeIds[0];
    } else {
      this.selectedNibId = null;
    }
  }

  /** Space key: toggle focused row in/out of selectedIds */
  toggleFocusedSelection(): void {
    if (!this.focusedNibId) return;
    this.toggleSelect(this.focusedNibId);
  }

  /** Returns true if nibId is in selectedIds */
  isSelected(nibId: string): boolean {
    return this.selectedIds.has(nibId);
  }

  /** Clears selectedIds and anchor */
  deselectAll(): void {
    this.selectedIds = new Set();
    this.anchorId = null;
  }

  /** Clears everything: selectedIds, selectedNibId, focusedNibId, anchor, pendingEnsureVisibleId */
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
