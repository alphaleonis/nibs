// URL sync for the filter query (`?q=<canonical query string>`).
//
// This owns ONLY the `?q=` param and is deliberately independent of
// useHistoryNav (which owns `?nib=`). Both writers merge into the CURRENT
// search params (via URLSearchParams) and touch only their own key, so they
// coexist: writing `?q=` preserves an existing `?nib=`, and vice-versa.
//
// Writes use debounced `replaceState` — never `pushState` — so a stream of
// keystrokes updates the shareable URL without polluting the Back stack. The
// existing history entry's `state` (the `{nibId}` owned by useHistoryNav) is
// carried through unchanged; only the URL string is rewritten.

/** Minimal history surface: only replaceState is needed here. */
export interface ReplaceCapableHistory {
  replaceState(data: unknown, unused: string, url?: string | null): void;
}

export interface QueryUrl {
  /** Schedule a debounced write of the canonical query string to `?q=`. An
   *  empty string removes the param entirely. The latest call within the
   *  debounce window wins. */
  push(query: string): void;
  /** Write any pending debounced value immediately (cancels the timer). */
  flush(): void;
  /** Drop any pending debounced write without applying it. */
  cancel(): void;
  /** The `?q=` value in the current URL, or `null` when the param is absent
   *  (absent ≠ present-but-empty — the caller uses this to decide URL-vs-stored
   *  precedence). */
  currentQuery(): string | null;
}

const QUERY_PARAM = "q";

/** Read the `?q=` param out of a raw `location.search` string. Returns `null`
 *  when absent so a missing param is distinguishable from an empty one. */
export function queryFromSearch(search: string): string | null {
  const params = new URLSearchParams(search);
  return params.has(QUERY_PARAM) ? (params.get(QUERY_PARAM) ?? "") : null;
}

export function createQueryUrl(opts: {
  history?: ReplaceCapableHistory;
  getLocation?: () => { search: string; pathname: string };
  /** Current history state to carry through the replaceState (defaults to the
   *  live `window.history.state`, which useHistoryNav keeps as `{nibId}`). */
  getState?: () => unknown;
  /** Debounce window in ms; smaller values are handy for tests. */
  delay?: number;
} = {}): QueryUrl {
  const history = opts.history ?? window.history;
  const getLocation = opts.getLocation ?? (() => window.location);
  const getState = opts.getState ?? (() => (typeof window !== "undefined" ? window.history.state : null));
  const delay = opts.delay ?? 300;

  let timer: ReturnType<typeof setTimeout> | null = null;
  let pending: string | null = null;

  function writeNow(query: string) {
    const loc = getLocation();
    const params = new URLSearchParams(loc.search);
    if (query === "") params.delete(QUERY_PARAM);
    else params.set(QUERY_PARAM, query);
    const qs = params.toString();
    const path = loc.pathname || "/";
    history.replaceState(getState(), "", qs ? `${path}?${qs}` : path);
  }

  function clearTimer() {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
  }

  function push(query: string) {
    pending = query;
    clearTimer();
    timer = setTimeout(() => {
      timer = null;
      const q = pending;
      pending = null;
      if (q !== null) writeNow(q);
    }, delay);
  }

  function flush() {
    clearTimer();
    if (pending !== null) {
      const q = pending;
      pending = null;
      writeNow(q);
    }
  }

  function cancel() {
    clearTimer();
    pending = null;
  }

  function currentQuery(): string | null {
    return queryFromSearch(getLocation().search);
  }

  return { push, flush, cancel, currentQuery };
}
