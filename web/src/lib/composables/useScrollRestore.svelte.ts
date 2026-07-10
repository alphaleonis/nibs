/**
 * Restores (and then tracks) a tree table's scroll position across a {#key
 * position} remount. When the detail panel docks right vs. bottom, App.svelte
 * re-keys the whole PaneForge PaneGroup, recreating TreeTable's scroll container
 * at scrollTop=0. The saved scroll position lives in TreeViewState (outside the
 * keyed block), same rationale as collapsedIds; this composable copies it back
 * onto the fresh container once content is present, then records subsequent
 * scrolls back into the saved slot (nibs-n47p).
 *
 * Rune-free coordination helper: the reactive `$effect` that re-attempts
 * `restore()` after the container binds and rows render lives in TreeTable. This
 * module owns the ordering logic that keeps the fresh container's scrollTop=0
 * from clobbering the saved value before the initial restore has run.
 */
export function useScrollRestore(opts: {
  getScrollContainer: () => HTMLElement | null;
  getSavedScrollTop: () => number;
  setSavedScrollTop: (n: number) => void;
  hasContent: () => boolean;
}): { onScroll: () => void; restore: () => void; cancel: () => void } {
  // restoredEl: the container element we've already restored OR that ensureVisible
  // has claimed via cancel(). Keyed to element IDENTITY, not composable-instance
  // lifetime — so a container recreated within the same TreeTable instance (urql
  // refetch / empty-filter clear) gets a fresh restore. (nibs-n47p review #2)
  let restoredEl: HTMLElement | null = null;

  function restore(): void {
    const container = opts.getScrollContainer();
    if (!container || container === restoredEl) return; // no container, or already handled
    // Leave restoredEl unset so a later call can still restore once rows populate
    // after the remount (else scrollTop would clamp to 0 against an empty table).
    if (!opts.hasContent()) return;
    container.scrollTop = opts.getSavedScrollTop();
    restoredEl = container;
  }

  function onScroll(): void {
    const container = opts.getScrollContainer();
    // Record only once THIS element is restored/claimed — ignores the fresh
    // container's scrollTop=0 noise before restore, so it can't overwrite the
    // saved value we still need to apply.
    if (!container || container !== restoredEl) return;
    opts.setSavedScrollTop(container.scrollTop);
  }

  function cancel(): void {
    // Claim the current container as handled WITHOUT writing scrollTop, so an
    // external scroll (ensureVisible's scrollIntoView) wins over restore
    // regardless of effect flush order. onScroll still records for this element,
    // so the deep-linked position becomes the saved offset. (nibs-n47p review #1)
    restoredEl = opts.getScrollContainer();
  }

  return { onScroll, restore, cancel };
}
