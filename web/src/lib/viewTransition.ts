import type { ViewLevel } from "./types";

/**
 * A view switch that has been recorded but not yet reconciled: the level the
 * table was showing, and the one it is switching to.
 */
export interface ViewTransition {
  from: ViewLevel;
  to: ViewLevel;
}

/** What the table looked like at the moment the switch was applied. */
export interface ViewTransitionSnapshot {
  focusedNibId: string | null;
  /**
   * The nib the detail panel is showing. Read to CHOOSE an anchor, never
   * written: no plan field reaches it, deliberately. State keyed by NIB is
   * global while state keyed by ROW is per-view, and this is nib-keyed — it is
   * also the `?nib=` URL, so retiring it on a view switch would rewrite the URL
   * and would need both the `replaceClosed()` heal and the unsaved-body-buffer
   * guard that `SelectionState.clearAll` documents.
   */
  selectedNibId: string | null;
  /** Every id the NEW view TREE contains — real nibs and its own bucket id —
   *  collapse-INDEPENDENT (buildViewTree output, not flattened rows), so a
   *  collapsed parent never looks like a departed one. */
  memberIds: ReadonlySet<string>;
}

/** Three fields, three EXISTING sinks. No new verbs on SelectionState. */
export interface ViewTransitionPlan {
  /** -> selection.retainOnly() */
  retainIds: ReadonlySet<string> | null;
  /** -> selection.ensureVisible() */
  anchorId: string | null;
  /** -> treeView.switchScroll() */
  switchScroll: boolean;
}

/**
 * Decide what a view switch owes the selection and the viewport.
 *
 * A grouping lens is lossless in WORK ITEMS but not in ROWS: `buildViewTree`
 * hides a container ranked above the lens's tier while descending into it, so a
 * milestone selected in the Tree view has no row at all under the Epics lens. It
 * would otherwise stay selected, focused, and a legal bulk-action target while
 * the user cannot see it.
 *
 * Pure over plain data — no runes, no Svelte, no DOM — so the decision is
 * testable without a component and the caller owns nothing but the three writes.
 */
export function planViewTransition(
  transition: ViewTransition,
  snapshot: ViewTransitionSnapshot,
): ViewTransitionPlan {
  // Re-picking the current lens is not a transition: nothing left the view, so
  // pruning would destroy a selection the user never asked to lose.
  // `switchViewLevel` already refuses this case before recording anything;
  // repeating it here keeps the plan correct for any caller, not just that one.
  //
  // This clause covers PRUNING only. It cannot speak for the scroll swap, whose
  // origin the applier supplies from `treeView.activeLevel` — a value this
  // function never sees, and one that differs from `transition.from` whenever two
  // switches collapse into a single pending slot. The scroll's own identity
  // refusal therefore lives in `TreeViewState.switchScroll`.
  if (transition.from === transition.to) {
    return { retainIds: null, anchorId: null, switchScroll: false };
  }

  const { memberIds, focusedNibId, selectedNibId } = snapshot;

  // Keep the row the user was working with on screen — but only if the new view
  // has one for it, which is exactly what this reconcile exists to doubt. Focus
  // wins over the panel's nib: it is where the keyboard is, and the panel stays
  // open either way.
  const anchorId =
    focusedNibId !== null && memberIds.has(focusedNibId)
      ? focusedNibId
      : selectedNibId !== null && memberIds.has(selectedNibId)
        ? selectedNibId
        : null;

  // The scroll offset is measured in the outgoing view's pixel geometry, which
  // the incoming view does not share, so it never carries ACROSS: it is parked
  // under the view it belongs to and the incoming view's own remembered offset
  // is adopted (0 the first time it is entered). A surviving anchor is then
  // scrolled to from there, moving the viewport only when the row is not already
  // visible at that offset. See the precedence note in TreeTable's applier effect.
  return { retainIds: memberIds, anchorId, switchScroll: true };
}
