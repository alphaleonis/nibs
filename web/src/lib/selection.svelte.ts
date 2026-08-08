import { isBucketId } from "./tree";

/** Options shared by the two bulk-selection writers.
 *
 *  `retargetPanel` controls the ONE thing those writers do beyond building the
 *  selection set: following the set into `selectedNibId` when it collapses to
 *  exactly one id, and nulling it otherwise. It defaults to true — the
 *  historical behavior, where the panel tracks a collapsed selection.
 *
 *  Pass `false` when the detail panel is decoupled from the selection (the
 *  "open on double-click" preference). There `selectedNibId` means "what the
 *  panel is showing" and has exactly one writer path — the explicit open
 *  gestures — so a bulk gesture must neither open the panel, close it, nor
 *  retarget it at the swept rows. */
export interface BulkSelectOptions {
  retargetPanel?: boolean;
}

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
   *  so it is never admitted — one of the four `selectedIds` writers that add
   *  ids, alongside `selectOnly`, `toggleSelect` and `rangeSelect`. Reachable
   *  with a bucket id via `view.open` on an arrow-focused bucket (keyboard `e`),
   *  a right-click, or a stale `?nib=<bucket>` URL; the mouse row-click path is
   *  already intercepted by TreeTable's `openOrToggleBucket`. */
  select(nibId: string): void {
    if (isBucketId(nibId)) return;
    this.selectedNibId = nibId;
    this.focusedNibId = nibId;
    this.selectedIds = new Set([nibId]);
    this.anchorId = nibId;
  }

  /** Select a single nib WITHOUT opening it in the detail panel — the
   *  select-without-open contract behind the "open on double-click" preference,
   *  and the right-click-an-unselected-row path under it.
   *
   *  `selectedNibId` is deliberately left untouched: `panelOpen` is derived from
   *  it, so not writing it is exactly what keeps the panel from opening — and
   *  keeps an already-open nib on screen instead of retargeting the panel to the
   *  clicked row. Selection and the panel are allowed to point at different rows
   *  as a result; TreeTableRow renders those two states distinctly.
   *
   *  A synthetic grouping-bucket id is never admitted, same as `select` /
   *  `toggleSelect` / `rangeSelect` — one of the four `selectedIds` add-writers
   *  that enforce that invariant. */
  selectOnly(nibId: string): void {
    if (isBucketId(nibId)) return;
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

  /** Ctrl/Cmd+click, or Space on a focused row: toggle nib in/out of
   *  selectedIds, update anchor. A synthetic grouping-bucket id is unresolvable
   *  for any bulk action, so it is never admitted — one of the four `selectedIds`
   *  add-writers (with `select`, `selectOnly` and `rangeSelect`) that enforce the
   *  invariant.
   *  This guard also covers the keyboard path, where a bucket row can be focused
   *  (arrow) and Space-toggled, which the range slice does not reach.
   *  `opts.retargetPanel` — see BulkSelectOptions. */
  toggleSelect(nibId: string, opts: BulkSelectOptions = {}): void {
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
    if (opts.retargetPanel === false) return;
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
   * shift+arrow) — funnel through here. This is one of the four `selectedIds`
   * add-writers that enforce "no synthetic id in `selectedIds`"; see also
   * `select`, `selectOnly` and `toggleSelect`. (This does NOT cover consumers that read
   * `focusedNibId` or a right-click target directly — e.g. the Delete dispatch;
   * that is a separate concern outside SelectionState.)
   *
   * `opts.retargetPanel` — see BulkSelectOptions.
   */
  rangeSelect(nibId: string, visibleIds: string[], opts: BulkSelectOptions = {}): void {
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
    if (opts.retargetPanel === false) return;
    if (rangeIds.length === 1) {
      this.selectedNibId = rangeIds[0];
    } else {
      this.selectedNibId = null;
    }
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

  /** Clears everything: selectedIds, selectedNibId, focusedNibId, anchor,
   *  pendingEnsureVisibleId.
   *
   *  NOT for post-mutation cleanup, despite reading like the obvious call for
   *  it: `selectedNibId` and the action target can be different rows (the "open
   *  on double-click" preference, and plain arrow-key nav in either mode), so
   *  nulling `selectedNibId` here tears down a panel showing a nib the mutation
   *  never touched — and discards its unsaved edits, since nothing on that path
   *  runs the dirty guard. Use `clearAfterMutation` in `actionTarget.ts`, which
   *  retires only what the mutation consumed.
   *
   *  The two are not interchangeable in the other direction either: this clears
   *  `pendingEnsureVisibleId` and `clearAfterMutation` does not. */
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
