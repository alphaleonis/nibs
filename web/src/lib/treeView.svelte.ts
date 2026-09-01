import type { ViewLevel } from "./types";
import { PerViewMap } from "./perViewMap.svelte";
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
   * The remembered vertical scroll offset of the view currently on screen; every
   * other view's is parked in `#scrollByLevel` until it comes back.
   *
   * It TRACKS the container rather than mirroring it: a genuine scroll records
   * the container's value here, but a restore whose write the container clamped
   * (a momentarily shorter list, or the taller pane a dock toggle produces)
   * leaves this at the offset the user actually chose, so the position is honored
   * again once the room is back rather than ratcheting toward the top. Parking
   * it under a view carries that offset forward unchanged, but every route back
   * to the viewport goes through the restore write, which re-clamps — so an
   * offset larger than the current geometry can never put the viewport
   * somewhere impossible.
   *
   * Held here (outside App's {#key position} block) so it survives the PaneForge
   * PaneGroup remount on a dock toggle, same rationale as collapsedIds. Unlike collapsedIds
   * this is a plain public primitive with no in-place-mutation footgun, so it
   * needs no private-field/getter encapsulation — it matches SelectionState's
   * public $state fields.
   */
  scrollTop: number = $state(0);

  /**
   * The parked scroll offset of every view that is NOT on screen, so a switch
   * away and back lands where the user left off instead of at the top.
   *
   * Deliberately EPHEMERAL (no persistence group): the preferences blob has a
   * single key and no version field, and `parsePerViewMap` silently discards a
   * level whose name it no longer recognizes — so a renamed view level would
   * drop its data without a sound. A scroll offset surviving a reload is not
   * worth entering that hazard for.
   *
   * The payload being a NUMBER is what admits the non-copying `stored ?? dflt`
   * combinator here — see the aliasing rule on `PerViewMapOpts.resolve`.
   */
  #scrollByLevel = new PerViewMap<number>({
    defaultValue: 0,
    resolve: (stored, dflt) => stored ?? dflt,
  });

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
   * Bumped whenever a transition swaps `scrollTop` for another view's offset.
   * Scroll ownership in `useScrollRestore` is keyed on (element, epoch), so
   * advancing this retires the current claim on an element that is NOT being
   * recreated — which is exactly a view switch, where the same container now
   * shows different content and the offset it is sitting at means something
   * else. Retiring ownership is what re-arms `restore()` onto the incoming
   * view's offset.
   */
  #scrollEpoch: number = $state(0);

  /**
   * The view level currently ON SCREEN — the destination of the last transition
   * that was reconciled, seeded at construction.
   *
   * Distinct from `prefs.viewLevel`, which flips synchronously at the write and
   * so already names the incoming view while the outgoing one is still rendered.
   * The applier passes it to `switchScroll` as the key to park the outgoing
   * offset under: that offset was measured in the geometry of the view that was
   * actually rendered, which is what this names and the preference does not.
   *
   * It is NOT a source for `switchViewLevel`'s `from`, which it superficially
   * resembles: it lags a full transition (it advances only when one is consumed)
   * and is absent entirely for a caller holding no `TreeViewState`.
   */
  // No seed: the constructor assigns unconditionally, so an initializer here is
  // discarded before any caller can observe it — and one naming a level would
  // contradict the required parameter below.
  #activeLevel: ViewLevel = $state()!;

  /** `initialLevel` is required rather than defaulted: it is the key the FIRST
   *  parked scroll offset is filed under, so a construction site that let it
   *  fall back to the default while the restored preference named another view
   *  would file that offset under a view the user was never in — silently, and
   *  with nothing to hint at it later. */
  constructor(initialLevel: ViewLevel) {
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

  /** Hand the live scroll offset over from one view to another: park it under
   *  the view it was measured in, adopt whatever the destination last left
   *  behind (0 if it has never been scrolled), and retire the ownership keyed to
   *  the old offset so the restore re-applies against the new one.
   *
   *  A view hands the offset to ITSELF when two switches collapse into one
   *  pending slot and land back where they started; the planner cannot see that
   *  (it decides from `transition.from`, while the applier supplies the origin
   *  from `activeLevel`), so the identity check belongs here, the one place both
   *  levels are known. The swap would be value-preserving anyway — what it costs
   *  is an epoch bump, which retires scroll ownership and makes the restore
   *  re-apply an offset nothing asked to change. */
  switchScroll(from: ViewLevel, to: ViewLevel): void {
    if (from === to) return;
    this.#scrollByLevel.setLevel(from, this.scrollTop);
    this.scrollTop = this.#scrollByLevel.resolve(to);
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
