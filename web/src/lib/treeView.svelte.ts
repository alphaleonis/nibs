/**
 * Owns the tree's expand/collapse state, lifted out of TreeTable so it survives
 * a TreeTable remount. App wraps the whole Resizable.PaneGroup (including the
 * TreeTable pane) in {#key position} to re-orient the split when the detail panel
 * docks right vs. bottom (PaneForge fixes split direction at pane-group creation).
 * If collapse state lived inside TreeTable as component-local $state it would reset
 * on every dock toggle, silently re-expanding every branch. Instead this state is
 * instantiated in App.svelte OUTSIDE the keyed block and shared via context —
 * mirroring SelectionState/DragState (nibs-a5sb, review #1).
 */
export class TreeViewState {
  /**
   * IDs of collapsed tree nodes. Always reassigned (never mutated in place) on
   * change so Svelte's reactivity tracks it — callers build a fresh Set and assign.
   */
  collapsedIds: Set<string> = $state(new Set());
}
