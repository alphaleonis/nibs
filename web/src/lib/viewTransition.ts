import type { ViewLevel } from "./types";

/** A recorded view switch that has not been reconciled yet. */
export interface ViewTransition {
  from: ViewLevel;
  to: ViewLevel;
}

/** What the table looked like at the moment the switch was applied. */
export interface ViewTransitionSnapshot {
  focusedNibId: string | null;
  /**
   * The nib the detail panel is showing. Read to choose an anchor, never written:
   * it is also the `?nib=` URL, and retiring it would need the URL heal and the
   * unsaved-edits guard.
   */
  selectedNibId: string | null;
  /** Every id in the new view's tree, its bucket ids included, regardless of
   *  collapse, so a collapsed parent is not mistaken for a departed one. */
  memberIds: ReadonlySet<string>;
}

/** Each field feeds an existing sink. */
export interface ViewTransitionPlan {
  /** -> selection.retainOnly() */
  retainIds: ReadonlySet<string> | null;
  /** -> selection.ensureVisible() */
  anchorId: string | null;
  /** -> treeView.switchScroll() */
  switchScroll: boolean;
}

/**
 * Decide what a view switch does to the selection and the viewport.
 *
 * A grouping lens can hide a selected row: `buildViewTree` hides a container
 * ranked above the lens's tier, so a milestone selected in the Tree view has no
 * row under the Epics lens and would stay a bulk-action target off screen.
 */
export function planViewTransition(
  transition: ViewTransition,
  snapshot: ViewTransitionSnapshot,
): ViewTransitionPlan {
  // Same level: nothing left the view, so prune nothing. This decides pruning
  // only; TreeViewState.switchScroll checks scroll identity against the origin
  // the applier supplies, which can differ from `transition.from`.
  if (transition.from === transition.to) {
    return { retainIds: null, anchorId: null, switchScroll: false };
  }

  const { memberIds, focusedNibId, selectedNibId } = snapshot;

  // Anchor on the focused row if the new view has it, else on the panel's nib.
  const anchorId =
    focusedNibId !== null && memberIds.has(focusedNibId)
      ? focusedNibId
      : selectedNibId !== null && memberIds.has(selectedNibId)
        ? selectedNibId
        : null;

  // Scroll offsets are per view: park the outgoing offset and adopt the incoming
  // view's. See the precedence note in TreeTable's applier effect.
  return { retainIds: memberIds, anchorId, switchScroll: true };
}
