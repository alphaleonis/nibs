import { isBucketId } from "./tree";

export class SelectionState {
  selectedNibId: string | null = $state(null);
  focusedNibId: string | null = $state(null);
  selectedIds: Set<string> = $state(new Set());
  anchorId: string | null = $state(null);
  pendingEnsureVisibleId: string | null = $state(null);
  panelOpen: boolean = $derived(this.selectedNibId !== null);
  hasMultiSelect: boolean = $derived(this.selectedIds.size > 1);

  /** Select a single nib and open it in the detail panel. A synthetic
   *  grouping-bucket id has no detail and is unresolvable for any bulk action,
   *  so it is never admitted — this is the third and last `selectedIds` writer
   *  that adds ids, alongside `toggleSelect` and `rangeSelect`. Reachable with a
   *  bucket id via `view.open` on an arrow-focused bucket (keyboard `e`), a
   *  right-click, or a stale `?nib=<bucket>` URL; the mouse row-click path is
   *  already intercepted by TreeTable's `openOrToggleBucket`. */
  select(nibId: string): void {
    if (isBucketId(nibId)) return;
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

  /** Ctrl/Cmd+click (or Space on a focused row): toggle nib in/out of
   *  selectedIds, update anchor. A synthetic grouping-bucket id is unresolvable
   *  for any bulk action, so it is never admitted — one of the three `selectedIds`
   *  add-writers (with `select` and `rangeSelect`) that enforce the invariant.
   *  This guard specifically closes the keyboard path (arrow onto a bucket, then
   *  Space -> toggleFocusedSelection) that the range slice does not cover. */
  toggleSelect(nibId: string): void {
    if (isBucketId(nibId)) return;
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

  /**
   * Shift+click / shift+arrow: select range from anchor to nibId using the
   * visible row order.
   *
   * Synthetic "No X" grouping-bucket rows are interleaved with nib rows in
   * `visibleIds`, so a range that spans a bucket would otherwise sweep that
   * bucket's unresolvable synthetic id into `selectedIds` (and on to any bulk
   * action). We filter bucket ids OUT of the sliced range rather than truncating
   * at the bucket: a range's visual meaning is "the nibs I swept across", and a
   * bucket row is a header, not a member — so the nibs on both sides stay
   * selected. If an endpoint (anchor or target) is itself a bucket it simply
   * contributes no id while the surrounding nib range still resolves; a range
   * containing only bucket rows collapses to an empty selection. Both range
   * callers — the mouse path (TreeTable) and the keyboard path (useKeyboardNav
   * shift+arrow) — funnel through here. This is one of the three `selectedIds`
   * add-writers that enforce "no synthetic id in `selectedIds`"; see also
   * `select` and `toggleSelect`. (This does NOT cover consumers that read
   * `focusedNibId` or a right-click target directly — e.g. the Delete dispatch;
   * that is a separate concern outside SelectionState.)
   */
  rangeSelect(nibId: string, visibleIds: string[]): void {
    const anchor = this.anchorId ?? nibId;
    const startIndex = visibleIds.indexOf(anchor);
    const endIndex = visibleIds.indexOf(nibId);
    if (startIndex < 0 || endIndex < 0) return;

    const lo = Math.min(startIndex, endIndex);
    const hi = Math.max(startIndex, endIndex);
    const rangeIds = visibleIds.slice(lo, hi + 1).filter((id) => !isBucketId(id));

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

  /**
   * Prunes the multi-select set (and anchor/focus) down to only the ids present
   * in `matchingIds`, dropping any that are no longer selectable (e.g. filtered
   * out of the current view). The detail-panel selection (`selectedNibId`) is
   * intentionally left untouched — pruning targets the bulk-action set only so a
   * multi-drag / bulk mutation never applies to rows the user can no longer see.
   *
   * Safe to call from a reactive `$effect`: only reassigns `selectedIds` when
   * something is actually dropped, so an unchanged selection produces no writes
   * and cannot feed a reactive update loop.
   */
  retainOnly(matchingIds: Set<string>): void {
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
