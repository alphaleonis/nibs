import type { ViewLevel } from "./types";
import { DEFAULT_VIEW_LEVEL } from "./types";
import type { ViewTransition } from "./viewTransition";

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

  /**
   * A view switch recorded by `switchViewLevel` and not yet reconciled. A
   * consumed-and-cleared pending slot, modeled on
   * `SelectionState.pendingEnsureVisibleId`: the write path records it, TreeTable's
   * applier effect reads it once, acts, and calls `clearTransition`.
   *
   * It exists because the switch itself carries information the new state cannot:
   * `prefs.viewLevel` says which view is on screen, never that it just changed or
   * what it changed FROM.
   */
  #pendingTransition: ViewTransition | null = $state(null);

  /**
   * Bumped whenever a transition retires the saved scroll offset. Scroll
   * ownership in `useScrollRestore` is keyed on (element, epoch), so advancing
   * this retires the current claim on an element that is NOT being recreated —
   * which is exactly a view switch, where the same container now shows different
   * content and its offset means something else.
   */
  #scrollEpoch: number = $state(0);

  /**
   * The view level currently ON SCREEN — the destination of the last transition
   * that was reconciled, seeded at construction.
   *
   * Distinct from `prefs.viewLevel`, which flips synchronously at the write and
   * so already names the incoming view while the outgoing one is still rendered.
   * Nothing reads it in production yet: it is scaffolding for per-view collapse
   * and scroll memory (nibs-g6sb), which has to attribute the outgoing view's
   * offset to the view it came from and so cannot ask the preference.
   *
   * It is NOT a source for `switchViewLevel`'s `from`, which it superficially
   * resembles: it lags a full transition (it advances only when one is consumed)
   * and is absent entirely for a caller holding no `TreeViewState`.
   */
  #activeLevel: ViewLevel = $state(DEFAULT_VIEW_LEVEL);

  constructor(initialLevel: ViewLevel = DEFAULT_VIEW_LEVEL) {
    this.#activeLevel = initialLevel;
  }

  /** Read-only view of the collapsed-node ids. Reading it inside a
   *  `$derived`/`$effect` still tracks, because the getter reads the $state. */
  get collapsedIds(): ReadonlySet<string> {
    return this.#collapsedIds;
  }

  /** The unreconciled view switch, or null. */
  get pendingTransition(): ViewTransition | null {
    return this.#pendingTransition;
  }

  /** Current scroll-ownership generation; see `#scrollEpoch`. */
  get scrollEpoch(): number {
    return this.#scrollEpoch;
  }

  /** The view level on screen; see `#activeLevel`. */
  get activeLevel(): ViewLevel {
    return this.#activeLevel;
  }

  /** Record a view switch for the applier to reconcile. Called BEFORE the write
   *  that changes the level, so `from` is still readable. A second call before
   *  the first is consumed replaces it: only the last destination is rendered,
   *  and the reconcile is against the view that ends up on screen. */
  beginTransition(from: ViewLevel, to: ViewLevel): void {
    this.#pendingTransition = { from, to };
  }

  /** Consume the pending slot, advancing the on-screen level to its destination. */
  clearTransition(): void {
    const pending = this.#pendingTransition;
    if (pending) this.#activeLevel = pending.to;
    this.#pendingTransition = null;
  }

  /** Retire the saved scroll offset and the ownership keyed to it, so the
   *  incoming view starts at the top instead of at a position measured in the
   *  outgoing view's geometry. */
  resetScroll(): void {
    this.scrollTop = 0;
    this.#scrollEpoch++;
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
