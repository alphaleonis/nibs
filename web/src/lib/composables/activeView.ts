/**
 * Pure state machine for the active-nib view: a total `reduce(state, action)`
 * plus the `abandonsBuffer` dirty-guard predicate. `useActiveView.svelte.ts`
 * routes every transition through it.
 *
 * `presentation` is a payload field, not part of the tag, so EXPAND/COLLAPSE
 * keep the buffer identity. Both are no-ops while `closed`.
 */

export type Presentation = "docked" | "expanded";

/**
 * Why a `gone` buffer's nib left its path. An archived nib still exists and
 * still accepts a save; a deleted one does not. Derive savability from this,
 * not from the `gone` tag.
 */
export type GoneReason = "deleted" | "archived";

export type ViewState =
  | { kind: "closed" }
  | { kind: "viewing"; nibId: string; presentation: Presentation }
  | { kind: "gone"; nibId: string; presentation: Presentation; reason: GoneReason }
  | { kind: "creating"; defaults: { type: string; parent?: string }; presentation: Presentation };

export type Action =
  | { type: "OPEN"; nibId: string }
  | { type: "EXPAND" }
  | { type: "COLLAPSE" }
  | { type: "START_CREATE"; defaults: { type: string; parent?: string } }
  | { type: "SAVED"; nibId: string }
  | { type: "DELETED" }
  | { type: "ARCHIVED" }
  | { type: "UNARCHIVED" }
  | { type: "CLOSE" };

function presentationOf(s: ViewState): Presentation {
  return s.kind === "closed" ? "docked" : s.presentation;
}

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

function hasBuffer(s: ViewState): boolean {
  return s.kind === "viewing" || s.kind === "gone" || s.kind === "creating";
}

function bufferNibId(s: ViewState): string | null {
  return s.kind === "viewing" || s.kind === "gone" ? s.nibId : null;
}

/** Illegal `(state, action)` pairs return the state unchanged. */
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
      // Create -> edit hand-off.
      if (s.kind !== "creating") return s;
      return { kind: "viewing", nibId: a.nibId, presentation: s.presentation };

    case "DELETED":
    case "ARCHIVED": {
      const reason: GoneReason = a.type === "ARCHIVED" ? "archived" : "deleted";
      if (s.kind === "viewing") {
        return { kind: "gone", nibId: s.nibId, presentation: s.presentation, reason };
      }
      // A deletion supersedes an archive; nothing supersedes a deletion.
      if (s.kind === "gone" && s.reason === "archived" && reason === "deleted") {
        return { ...s, reason };
      }
      return s;
    }

    // Returns an archived buffer to `viewing` under the same buffer key. A
    // deletion is terminal.
    case "UNARCHIVED":
      if (s.kind === "gone" && s.reason === "archived") {
        return { kind: "viewing", nibId: s.nibId, presentation: s.presentation };
      }
      return s;

    case "CLOSE":
      return { kind: "closed" };
  }
}

/**
 * True unless the buffer's nib was deleted. An archived nib still exists in
 * archive/ and accepts the write.
 */
export function canSaveState(s: ViewState): boolean {
  return !(s.kind === "gone" && s.reason === "deleted");
}

/**
 * True when applying `a` to `s` would discard the working-copy buffer. The
 * shell confirms only when this is true and the form is dirty.
 */
export function abandonsBuffer(s: ViewState, a: Action): boolean {
  switch (a.type) {
    case "OPEN":
      // Re-opening the same nib is a resync, not an abandon.
      return hasBuffer(s) && bufferNibId(s) !== a.nibId;
    case "START_CREATE":
    case "CLOSE":
      return hasBuffer(s);
    default:
      return false;
  }
}
