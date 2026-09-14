/**
 * Restores a tree table's saved scroll offset onto a fresh scroll container
 * (App's `{#key position}` remount, a refetch, a view switch), then records
 * user scrolls. TreeTable's `$effect` calls `restore()`.
 *
 * `owned {el, epoch}` is the container we took over. A new element or epoch (a
 * view switch keeps the element but replaces its content) re-arms restore() and
 * stops onScroll recording until then. `lastWrite {el, top}` is our last
 * programmatic write, read back to include the browser's clamp; a scroll event
 * matching it is that write's echo and is not persisted.
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
    // Wait for rows, or scrollTop clamps to 0 against an empty table.
    if (!opts.hasContent()) return;
    container.scrollTop = opts.getSavedScrollTop();
    owned = { el: container, epoch: opts.getEpoch() };
    lastWrite = { el: container, top: container.scrollTop };
  }

  function onScroll(event: Event): void {
    // The element that fired, so a late event from a detached container is not
    // attributed to the live one.
    const container = event.currentTarget as HTMLElement | null;
    if (!container || !isOwned(container)) return;
    // Our own write's echo: consume once.
    if (lastWrite && lastWrite.el === container && lastWrite.top === container.scrollTop) {
      lastWrite = null;
      return;
    }
    opts.setSavedScrollTop(container.scrollTop);
  }

  function claim(): void {
    const container = opts.getScrollContainer();
    if (!container) return;
    // Persist ensureVisible's offset now, since a refetch may unmount the
    // container before its scroll event fires, and take ownership so restore()
    // does not overwrite it. If restore() already ran and left the row visible,
    // scrollIntoView was a no-op and the restored offset stands.
    opts.setSavedScrollTop(container.scrollTop);
    owned = { el: container, epoch: opts.getEpoch() };
  }

  return { onScroll, restore, claim };
}
