import type { SelectionState } from "../selection.svelte";
import { isSyntheticRowId } from "../tree";

export interface HistoryLike {
  pushState(data: unknown, unused: string, url?: string | null): void;
  replaceState(data: unknown, unused: string, url?: string | null): void;
}

export interface HistoryNav {
  navigateToNib(id: string): void;
  closePanel(): void;
  /** Replace the current entry with the no-nib URL (no Back stop). The caller
   *  owns the selection state. */
  replaceClosed(): void;
  handlePopState(e: { state: unknown }): void;
  syncFromUrl(): void;
}

export function nibIdFromSearch(search: string): string | null {
  const id = new URLSearchParams(search).get("nib");
  return id ? id : null;
}

/** True for a `{ nibId: string|null }` entry we own. Checks the value type too:
 *  `history.state` persists and any same-origin script can write it. */
function isNibState(state: unknown): state is { nibId: string | null } {
  if (!state || typeof state !== "object" || !("nibId" in state)) return false;
  const nibId = (state as { nibId: unknown }).nibId;
  return typeof nibId === "string" || nibId === null;
}

export function createHistoryNav(opts: {
  selection: SelectionState;
  history?: HistoryLike;
  getLocation?: () => { search: string; pathname: string };
  /** True while Back/Forward must not move the panel. */
  isBlocked?: () => boolean;
}): HistoryNav {
  const { selection } = opts;
  const history = opts.history ?? window.history;
  const getLocation = opts.getLocation ?? (() => window.location);
  const isBlocked = opts.isBlocked ?? (() => false);

  // Merge into the current params so `?q=` (useQueryUrl) survives; only `nib`
  // is touched.
  const nibUrl = (id: string) => {
    const params = new URLSearchParams(getLocation().search);
    params.set("nib", id);
    return `?${params.toString()}`;
  };
  const closeUrl = () => {
    const params = new URLSearchParams(getLocation().search);
    params.delete("nib");
    const qs = params.toString();
    const path = getLocation().pathname || "/";
    return qs ? `${path}?${qs}` : path;
  };

  function navigateToNib(id: string) {
    // Never push a synthetic bucket id; it would survive reload and Back.
    if (isSyntheticRowId(id)) return;
    // Gate only the push. select() is a full resync, so run it even when this
    // nib is already open. Multi-select gestures change selectedNibId without
    // writing history, so the URL may lag after one.
    if (selection.selectedNibId !== id) history.pushState({ nibId: id }, "", nibUrl(id));
    selection.select(id);
  }

  function closePanel() {
    // Gate only the push; close() is idempotent.
    if (selection.selectedNibId !== null) history.pushState({ nibId: null }, "", closeUrl());
    selection.close();
  }

  function replaceClosed() {
    history.replaceState({ nibId: null }, "", closeUrl());
  }

  function handlePopState(e: { state: unknown }) {
    // Blocked: re-push the current selection so Back/Forward is a no-op and the
    // URL matches what is shown.
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
      // Normalize a dirty initial URL and seed an owned `{nibId: null}` state so
      // a later Back lands on an entry we recognize.
      history.replaceState({ nibId: null }, "", closeUrl());
    }
  }

  return {
    navigateToNib,
    closePanel,
    replaceClosed,
    handlePopState,
    syncFromUrl,
  };
}
