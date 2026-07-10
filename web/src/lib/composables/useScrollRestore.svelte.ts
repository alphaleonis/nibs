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
}): { onScroll: () => void; restore: () => void; claim: () => void } {
  // restoredEl: the container element we've already restored OR that ensureVisible
  // has claimed via claim(). Keyed to element IDENTITY, not composable-instance
  // lifetime — so a container recreated within the same TreeTable instance (urql
  // refetch / empty-filter clear) gets a fresh restore. (nibs-n47p review #2)
  let restoredEl: HTMLElement | null = null;
  // One-shot: armed by restore()'s programmatic write, consumed by the single
  // native scroll event that write provokes. A real browser CLAMPS an
  // over-large scrollTop assignment to scrollHeight - clientHeight and then
  // fires a scroll event; without this flag onScroll() would persist that
  // clamped value, permanently shrinking the saved offset on a shorter refetch.
  // Armed ONLY when the write actually moved the container — a browser fires a
  // scroll event only on a move, so a no-op write (target === current, e.g. the
  // common saved=0 / fresh-container-at-0 case) provokes no echo; arming there
  // would strand the flag and swallow the user's next genuine scroll.
  // (nibs-n47p review #2; nibs-qpvw review #1)
  let suppressNextScroll = false;

  function restore(): void {
    const container = opts.getScrollContainer();
    if (!container || container === restoredEl) return; // no container, or already handled
    // Leave restoredEl unset so a later call can still restore once rows populate
    // after the remount (else scrollTop would clamp to 0 against an empty table).
    if (!opts.hasContent()) return;
    const before = container.scrollTop;
    container.scrollTop = opts.getSavedScrollTop();
    restoredEl = container;
    // Reading scrollTop back captures the browser's clamp too. Arm only when the
    // write moved the container (no move → no echo → arming would strand the
    // flag). Assigning unconditionally also clears any flag stranded from a
    // prior container.
    suppressNextScroll = container.scrollTop !== before;
  }

  function onScroll(): void {
    const container = opts.getScrollContainer();
    // Record only once THIS element is restored/claimed — ignores the fresh
    // container's scrollTop=0 noise before restore, so it can't overwrite the
    // saved value we still need to apply.
    if (!container || container !== restoredEl) return;
    if (suppressNextScroll) { suppressNextScroll = false; return; } // one-shot restore-echo guard
    opts.setSavedScrollTop(container.scrollTop);
  }

  function claim(): void {
    const container = opts.getScrollContainer();
    if (!container) return;
    // ensureVisible scrolled the deep-linked row into view. Persist that offset
    // synchronously so it is durable even if a refetch unmounts the container
    // before the async scroll event fires; mark the element handled so restore()
    // won't overwrite it. Self-locates via getScrollContainer() (single source of
    // truth) so restoredEl can only ever be the live container, matching restore()
    // / onScroll() and the sibling composables. (nibs-n47p review #1)
    //
    // What this guarantees vs. the restore() effect is order-dependent, not
    // absolute: the target row ends up VISIBLE either way, but the exact resting
    // offset differs by flush order. If claim() runs after restore() already
    // applied the saved offset and left the row visible, scrollIntoView(
    // {block:"nearest"}) is a no-op and the container keeps the restored offset
    // rather than a fresh deep-link offset. Callers that need the deep-link
    // offset specifically must ensure ensureVisible runs before restore().
    opts.setSavedScrollTop(container.scrollTop);
    restoredEl = container;
  }

  return { onScroll, restore, claim };
}
