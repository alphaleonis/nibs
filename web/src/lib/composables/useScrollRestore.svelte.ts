/**
 * Restores (and then tracks) a tree table's scroll position across a {#key
 * position} remount. When the detail panel docks right vs. bottom, App.svelte
 * re-keys the whole PaneForge PaneGroup, recreating TreeTable's scroll container
 * at scrollTop=0. The saved scroll position lives in TreeViewState (outside the
 * keyed block), same rationale as collapsedIds; this composable copies it back
 * onto the fresh container once content is present, then records subsequent
 * scrolls back into the saved slot.
 *
 * Rune-free coordination helper: the reactive `$effect` that re-attempts
 * `restore()` after the container binds and rows render lives in TreeTable. This
 * module owns the ordering logic that keeps the fresh container's scrollTop=0
 * from clobbering the saved value before the initial restore has run.
 *
 * Coordination model. Two INDEPENDENT pieces of state, each self-describing so no
 * entry point can leave the other misattributed to the wrong element:
 *
 *   owned {el,epoch}  the element we've taken over — restore() applied the saved
 *                     offset to it, OR claim() adopted it after ensureVisible.
 *                     Keyed to element IDENTITY (not composable-instance lifetime)
 *                     so a container recreated within the same TreeTable instance
 *                     (urql refetch / empty-filter clear) is treated as fresh —
 *                     AND to the scroll epoch, which covers the case identity
 *                     cannot see: a VIEW SWITCH keeps the same container and
 *                     replaces its content, so the offset saved for the outgoing
 *                     view describes geometry the incoming one does not have.
 *                     Advancing the epoch retires ownership without recreating
 *                     anything, which both re-arms restore() onto the reset offset
 *                     and stops onScroll from recording the clamp the shorter
 *                     incoming view provokes as if it were user intent.
 *   lastWrite {el,top} the exact element + value of our most recent PROGRAMMATIC
 *                     scroll write. A scroll event on THAT element whose scrollTop
 *                     still equals THAT value is that write's own echo (including
 *                     the browser's clamp, captured by reading scrollTop back) —
 *                     never user intent — so onScroll skips persisting it. Because
 *                     the record carries its own element, a write stranded on a
 *                     torn-down container can NEVER be mistaken for a live
 *                     container's echo. A single closure boolean standing in for
 *                     "was the next scroll ours" cannot say WHICH element a write
 *                     belongs to; element-keying the record is what closes that
 *                     cross-element bleed.
 *
 * The single rule: onScroll persists a scroll UNLESS it exactly reproduces our last
 * programmatic write to the same element. That one rule covers clamped echoes,
 * no-op strands, and claim(): a no-op or clamped write simply produces a lastWrite
 * whose (el, top) a genuine user scroll cannot match.
 */
export function useScrollRestore(opts: {
  getScrollContainer: () => HTMLElement | null;
  getSavedScrollTop: () => number;
  setSavedScrollTop: (n: number) => void;
  hasContent: () => boolean;
  /** The scroll-ownership generation (TreeViewState.scrollEpoch). */
  getEpoch: () => number;
}): { onScroll: (event: Event) => void; restore: () => void; claim: () => void } {
  let owned: { el: HTMLElement; epoch: number } | null = null;
  let lastWrite: { el: HTMLElement; top: number } | null = null;

  /** True while this element is ours under the CURRENT epoch. */
  function isOwned(container: HTMLElement): boolean {
    return owned !== null && owned.el === container && owned.epoch === opts.getEpoch();
  }

  function restore(): void {
    const container = opts.getScrollContainer();
    if (!container || isOwned(container)) return; // no container, or already ours
    // Leave ownership unset until content exists, so a later call can still restore
    // once rows populate after the remount (else scrollTop would clamp to 0 against
    // an empty table).
    if (!opts.hasContent()) return;
    container.scrollTop = opts.getSavedScrollTop();
    owned = { el: container, epoch: opts.getEpoch() };
    // Read scrollTop back so lastWrite.top captures the browser's clamp: the echo
    // this write provokes reports this exact value on this exact element, so
    // onScroll can recognize and skip it. A no-op write (saved === current, e.g. the
    // common top-of-list case) records top === current and provokes no echo — and a
    // genuine later scroll reports a different value, so nothing is ever swallowed.
    lastWrite = { el: container, top: container.scrollTop };
  }

  function onScroll(event: Event): void {
    // Key off the element that actually fired (the handler is bound to the scroll
    // container), not an ambient getScrollContainer() read — so a stale event from a
    // just-detached container is attributed to that container, never the live one.
    const container = event.currentTarget as HTMLElement | null;
    // Record only once THIS element is ours under the current epoch — ignores the
    // fresh container's scrollTop=0 noise before restore, and the clamp a view
    // switch provokes after ownership was retired, so neither can overwrite the
    // saved value we still need to apply.
    if (!container || !isOwned(container)) return;
    // Skip the echo of our own programmatic write: same element, same (clamped)
    // value. Consume it once; a genuine user scroll reports a different value and is
    // persisted.
    if (lastWrite && lastWrite.el === container && lastWrite.top === container.scrollTop) {
      lastWrite = null;
      return;
    }
    opts.setSavedScrollTop(container.scrollTop);
  }

  function claim(): void {
    const container = opts.getScrollContainer();
    if (!container) return;
    // ensureVisible scrolled the deep-linked row into view. Persist that offset
    // synchronously so it is durable even if a refetch unmounts the container before
    // the async scroll event fires; take ownership so restore() won't overwrite it.
    // The claim is epoch-keyed like restore()'s: a claim that outlived a view
    // switch would hold ownership across it and defeat the reset the switch owes
    // the incoming view.
    // Self-locates via getScrollContainer() (single source of truth), matching
    // restore() and the sibling composables.
    //
    // claim() performs NO programmatic write of its own, so it arms no echo — and
    // because lastWrite is element-keyed, any record stranded from a prior container
    // is inert here (its el can't equal this container). Element-keying is what
    // removes any need for claim() to normalize a suppress flag.
    //
    // What this guarantees vs. the restore() effect is order-dependent, not
    // absolute: the target row ends up VISIBLE either way, but the exact resting
    // offset differs by flush order. If claim() runs after restore() already applied
    // the saved offset and left the row visible, scrollIntoView({block:"nearest"}) is
    // a no-op and the container keeps the restored offset rather than a fresh
    // deep-link offset. Callers that need the deep-link offset specifically must
    // ensure ensureVisible runs before restore().
    opts.setSavedScrollTop(container.scrollTop);
    owned = { el: container, epoch: opts.getEpoch() };
  }

  return { onScroll, restore, claim };
}
