/**
 * Pure state-machine kernel for the active-nib view presenter.
 *
 * No DOM, no runes, no async — a total `reduce(state, action)` over the
 * `ViewState` union plus the `abandonsBuffer` dirty-guard predicate. The
 * reactive shell (`useActiveView.svelte.ts`) owns the form/live lifecycle and
 * routes every transition through this kernel.
 *
 * Design invariant: `presentation` (docked | expanded) is a PAYLOAD field, NOT
 * part of the discriminant tag — so EXPAND/COLLAPSE only swap presentation while
 * keeping the same nibId/defaults buffer identity. `closed` carries no
 * presentation (there is nothing to present), so EXPAND/COLLAPSE while closed
 * are no-ops rather than fabricating a presentation.
 */

export type Presentation = "docked" | "expanded";

export type ViewState =
  | { kind: "closed" }
  | { kind: "viewing"; nibId: string; presentation: Presentation }
  | { kind: "gone"; nibId: string; presentation: Presentation }
  | { kind: "creating"; defaults: { type: string; parent?: string }; presentation: Presentation };

export type Action =
  | { type: "OPEN"; nibId: string }
  | { type: "EXPAND" }
  | { type: "COLLAPSE" }
  | { type: "START_CREATE"; defaults: { type: string; parent?: string } }
  | { type: "SAVED"; nibId: string }
  | { type: "DELETED" }
  | { type: "CLOSE" };

/** Current presentation, defaulting to docked for `closed` (which has none). */
function presentationOf(s: ViewState): Presentation {
  return s.kind === "closed" ? "docked" : s.presentation;
}

/** Return `s` with a swapped presentation; `closed` (no presentation) is a no-op. */
function withPresentation(s: ViewState, p: Presentation): ViewState {
  switch (s.kind) {
    case "closed":
      return s;
    case "viewing":
      return { ...s, presentation: p };
    case "gone":
      return { ...s, presentation: p };
    case "creating":
      return { ...s, presentation: p };
  }
}

/** True when the state holds an editable/creatable working-copy buffer. */
function hasBuffer(s: ViewState): boolean {
  return s.kind === "viewing" || s.kind === "gone" || s.kind === "creating";
}

/** The nib id the current buffer targets, or null (create buffer / no buffer). */
function bufferNibId(s: ViewState): string | null {
  return s.kind === "viewing" || s.kind === "gone" ? s.nibId : null;
}

/**
 * Total reducer. Illegal `(state, action)` pairs return the state unchanged.
 */
export function reduce(s: ViewState, a: Action): ViewState {
  switch (a.type) {
    case "OPEN":
      return { kind: "viewing", nibId: a.nibId, presentation: presentationOf(s) };

    case "EXPAND":
      return withPresentation(s, "expanded");

    case "COLLAPSE":
      return withPresentation(s, "docked");

    case "START_CREATE":
      return { kind: "creating", defaults: a.defaults, presentation: presentationOf(s) };

    case "SAVED":
      // Create -> edit hand-off: adopt the freshly-minted id atomically.
      if (s.kind !== "creating") return s;
      return { kind: "viewing", nibId: a.nibId, presentation: s.presentation };

    case "DELETED":
      if (s.kind !== "viewing") return s;
      return { kind: "gone", nibId: s.nibId, presentation: s.presentation };

    case "CLOSE":
      return { kind: "closed" };
  }
}

/**
 * The dirty-guard predicate: true when applying `a` to `s` would discard an
 * unsaved working-copy buffer. The shell prompts to confirm only when this is
 * true AND the active form is dirty.
 */
export function abandonsBuffer(s: ViewState, a: Action): boolean {
  switch (a.type) {
    case "OPEN":
      // Opening a *different* target replaces the buffer; re-opening the same
      // nib is a resync, not an abandon.
      return hasBuffer(s) && bufferNibId(s) !== a.nibId;
    case "START_CREATE":
    case "CLOSE":
      return hasBuffer(s);
    default:
      return false;
  }
}
