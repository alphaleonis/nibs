import type { SelectionState } from "../selection.svelte";

export interface HistoryLike {
  pushState(data: unknown, unused: string, url?: string | null): void;
  replaceState(data: unknown, unused: string, url?: string | null): void;
}

export interface HistoryNav {
  navigateToNib(id: string): void;
  closePanel(): void;
  handlePopState(e: { state: unknown }): void;
  syncFromUrl(): void;
}

export function nibIdFromSearch(search: string): string | null {
  const id = new URLSearchParams(search).get("nib");
  return id ? id : null;
}

/** True when `state` is a history entry we own, shaped `{ nibId: string|null }`.
 *  `history.state` is external, persisted data (survives reload/redeploy and is
 *  writable by any script on the origin), so we validate the value type too — a
 *  hostile `{ nibId: 42 }` must NOT pass and flow a non-string into selection. */
function isNibState(state: unknown): state is { nibId: string | null } {
  if (!state || typeof state !== "object" || !("nibId" in state)) return false;
  const nibId = (state as { nibId: unknown }).nibId;
  return typeof nibId === "string" || nibId === null;
}

export function createHistoryNav(opts: {
  selection: SelectionState;
  history?: HistoryLike;
  getLocation?: () => { search: string; pathname: string };
  /** True while a blocking overlay (editor modal / type picker / confirm dialog)
   *  is open. Back/Forward must not navigate the panel behind it (nibs-g1fy). */
  isBlocked?: () => boolean;
}): HistoryNav {
  const { selection } = opts;
  const history = opts.history ?? window.history;
  const getLocation = opts.getLocation ?? (() => window.location);
  const isBlocked = opts.isBlocked ?? (() => false);

  const nibUrl = (id: string) => `?nib=${encodeURIComponent(id)}`;
  const closeUrl = () => getLocation().pathname || "/";

  function navigateToNib(id: string) {
    // Gate ONLY the history push, not the select: `select()` is a full resync
    // (selectedNibId, focusedNibId, selectedIds, anchorId), so it must run even
    // when the nib is already open — otherwise focus/anchor drift (e.g. after
    // arrow-key nav) survives a re-navigation and can misdirect Delete/Edit
    // (nibs-58c3). The guard's single job is "don't push a duplicate entry".
    //
    // Boundary: selectedNibId can also change WITHOUT going through nav — a
    // multi-select collapse-to-one (toggleSelect/rangeSelect) opens the panel
    // and collapse-to-zero closes it, neither writing history. So URL/history
    // may lag selectedNibId after a bulk gesture; that's an accepted residual
    // for multi-select (not detail-panel navigation) (nibs-58c3).
    if (selection.selectedNibId !== id) history.pushState({ nibId: id }, "", nibUrl(id));
    selection.select(id);
  }

  function closePanel() {
    // Gate ONLY the history push; close() is idempotent, so always calling it is
    // harmless and keeps the guard's single job = "don't push a duplicate entry"
    // (nibs-58c3). See navigateToNib for the multi-select desync boundary.
    if (selection.selectedNibId !== null) history.pushState({ nibId: null }, "", closeUrl());
    selection.close();
  }

  function handlePopState(e: { state: unknown }) {
    // A blocking overlay (editor modal / type picker / confirm dialog) is open:
    // don't navigate the panel behind it. Re-anchor history on the currently
    // shown selection so Back/Forward is a no-op and the URL stays consistent
    // with the (covered) panel. We intentionally do NOT close the overlay here —
    // the editor modal's own close path guards unsaved changes; the user
    // dismisses via Escape/Cancel, then Back/Forward resumes (nibs-g1fy).
    if (isBlocked()) {
      if (selection.selectedNibId !== null) {
        history.pushState({ nibId: selection.selectedNibId }, "", nibUrl(selection.selectedNibId));
      } else {
        history.pushState({ nibId: null }, "", closeUrl());
      }
      return;
    }
    const id = isNibState(e.state) ? e.state.nibId : nibIdFromSearch(getLocation().search);
    if (id) {
      selection.select(id);
      selection.ensureVisible(id);
    } else {
      selection.close();
    }
  }

  function syncFromUrl() {
    const id = nibIdFromSearch(getLocation().search);
    if (id) {
      history.replaceState({ nibId: id }, "", nibUrl(id));
      selection.select(id);
      selection.ensureVisible(id);
    } else {
      // Normalize a dirty initial URL (`/?nib=`, stray params) back to a clean
      // path and seed a well-formed `{nibId:null}` owned state on the root
      // entry, so a later Back reaches a recognizable state we can honor.
      history.replaceState({ nibId: null }, "", closeUrl());
    }
  }

  return {
    navigateToNib,
    closePanel,
    handlePopState,
    syncFromUrl,
  };
}
