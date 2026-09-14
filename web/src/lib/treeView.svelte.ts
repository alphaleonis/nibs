import type { ViewLevel } from "./types";
import { PerViewMap } from "./perViewMap.svelte";
import type { ViewTransition } from "./viewTransition";

/**
 * The tree's expand/collapse and scroll state. App creates it outside its
 * `{#key position}` block, which remounts TreeTable on a dock toggle, so the
 * state survives the remount.
 */
export class TreeViewState {
  /**
   * IDs of collapsed nodes. Change only through the methods below, which assign a
   * fresh Set so Svelte tracks the change. The getter returns this live Set;
   * `ReadonlySet` is not enforced at runtime.
   */
  #collapsedIds: Set<string> = $state(new Set());

  /**
   * The remembered scroll offset of the view on screen; other views' offsets
   * are parked in `#scrollByLevel`.
   *
   * A restore the container clamps (a shorter list, a taller pane) leaves this
   * at the user's offset, so it is re-applied once there is room.
   */
  scrollTop: number = $state(0);

  /**
   * Parked scroll offsets of the views not on screen. Not persisted.
   * `stored ?? dflt` is safe only because the payload is a number (see
   * `PerViewMapOpts.resolve`).
   */
  #scrollByLevel = new PerViewMap<number>({
    defaultValue: 0,
    resolve: (stored, dflt) => stored ?? dflt,
  });

  /**
   * A view switch recorded by `beginTransition` and not yet reconciled.
   * TreeTable's applier effect reads it once and calls `clearTransition`.
   * `prefs.viewLevel` cannot stand in: it never says the view just changed, or
   * from what.
   */
  #pendingTransition: ViewTransition | null = $state(null);

  /**
   * Bumped when a transition swaps `scrollTop` for another view's offset.
   * `useScrollRestore` keys ownership on (element, epoch), so a bump makes it
   * restore the incoming offset into the same container.
   */
  #scrollEpoch: number = $state(0);

  /**
   * The view level on screen: the destination of the last reconciled
   * transition. Unlike `prefs.viewLevel`, it still names the outgoing view until
   * the applier reconciles, so the applier parks the outgoing offset under it.
   * It lags a transition, so do not use it as `switchViewLevel`'s `from`.
   */
  #activeLevel: ViewLevel = $state()!;

  /** Required: the first parked scroll offset is filed under this level, so a
   *  default differing from the restored preference would misfile it. */
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

  /** Record a view switch for the applier to reconcile. Call before the write
   *  that changes the level. A second call before the first is consumed
   *  replaces it. */
  beginTransition(from: ViewLevel, to: ViewLevel): void {
    this.#pendingTransition = { from, to };
  }

  /** Consume the pending slot, advancing the on-screen level to its destination. */
  clearTransition(): void {
    const pending = this.#pendingTransition;
    if (pending) this.#activeLevel = pending.to;
    this.#pendingTransition = null;
  }

  /** Park the live offset under `from`, adopt `to`'s parked offset (0 if none),
   *  and bump the epoch so the restore re-applies.
   *
   *  `from === to` occurs when two switches collapse into one pending slot and
   *  land back where they started. The planner decides from `transition.from`
   *  and cannot see it, so the no-op check lives here. */
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
    this.setCollapsed(ids);
  }

  /** Replace the collapsed set wholesale with the given ids. */
  setCollapsed(ids: Iterable<string>): void {
    this.#collapsedIds = new Set(ids);
  }
}
