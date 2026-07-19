/**
 * Owns the tree's expand/collapse state, lifted out of TreeTable so it survives
 * a TreeTable remount. App wraps the whole Resizable.PaneGroup (including the
 * TreeTable pane) in {#key position} to re-orient the split when the detail panel
 * docks right vs. bottom (PaneForge fixes split direction at pane-group creation).
 * If collapse state lived inside TreeTable as component-local $state it would reset
 * on every dock toggle, silently re-expanding every branch. Instead this state is
 * instantiated in App.svelte OUTSIDE the keyed block and shared via context —
 * mirroring SelectionState/DragState.
 */
export class TreeViewState {
  /**
   * IDs of collapsed tree nodes. Private $state exposed read-only via the
   * `collapsedIds` getter; all mutation goes through the methods below, each of
   * which assigns a FRESH Set (never mutates in place) so Svelte's reactivity
   * tracks the change. The reassign-fresh-Set invariant matches
   * SelectionState/DragState; the private-field + ReadonlySet getter here is a
   * STRICTER encapsulation than those siblings, which still expose public $state.
   *
   * The read-only guarantee is compile-time only: TS blocks `.add()`/`.delete()`
   * on the getter's typed view, but `ReadonlySet` is erased at runtime, so a cast
   * or a non-TS caller could still mutate the backing Set in place (and silently
   * skip reactivity). The getter returns the live reference deliberately —
   * `sameSet` and the reassign-identity tests depend on that identity — so writes
   * must keep going through the methods below rather than through the getter.
   */
  #collapsedIds: Set<string> = $state(new Set());

  /**
   * Saved vertical scroll offset of the tree table's scroll container. Persisted
   * here (outside App's {#key position} block) so it survives the PaneForge
   * PaneGroup remount on a detail-panel dock toggle, same rationale as
   * collapsedIds. Unlike collapsedIds this is a plain public primitive with no
   * in-place-mutation footgun, so it needs no private-field/getter encapsulation
   * — it matches SelectionState's public $state fields.
   */
  scrollTop: number = $state(0);

  /** Read-only view of the collapsed-node ids. Reading it inside a
   *  `$derived`/`$effect` still tracks, because the getter reads the $state. */
  get collapsedIds(): ReadonlySet<string> {
    return this.#collapsedIds;
  }

  /** True if the given node id is currently collapsed. */
  isCollapsed(id: string): boolean {
    return this.#collapsedIds.has(id);
  }

  /** Toggle a single node's collapsed state, reassigning a fresh Set. */
  toggle(id: string): void {
    const next = new Set(this.#collapsedIds);
    if (next.has(id)) {
      next.delete(id);
    } else {
      next.add(id);
    }
    this.#collapsedIds = next;
  }

  /** Expand everything: clear all collapsed ids. */
  expandAll(): void {
    this.#collapsedIds = new Set();
  }

  /** Collapse exactly the given ids (e.g. all parent ids). */
  collapseAll(ids: Iterable<string>): void {
    // Delegates to setCollapsed so there is a single implementation point — the
    // two names are kept for call-site readability, not divergent behavior.
    this.setCollapsed(ids);
  }

  /** Replace the collapsed set wholesale with the given ids. */
  setCollapsed(ids: Iterable<string>): void {
    this.#collapsedIds = new Set(ids);
  }
}
