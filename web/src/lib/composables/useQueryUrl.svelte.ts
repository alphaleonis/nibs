// URL sync for the filter query (`?q=`). Touches only its own key, merged into
// the current params, so it coexists with useHistoryNav's `?nib=`. Writes are
// debounced `replaceState` calls that carry the existing history state through.

/** Minimal history surface: only replaceState is needed here. */
export interface ReplaceCapableHistory {
  replaceState(data: unknown, unused: string, url?: string | null): void;
}

export interface QueryUrl {
  /** Schedule a debounced write to `?q=`; the latest call wins. An empty string
   *  removes the param. */
  push(query: string): void;
  /** Write any pending debounced value immediately (cancels the timer). */
  flush(): void;
  /** Drop any pending debounced write without applying it. */
  cancel(): void;
  /** The current `?q=` value, or `null` when absent (not the same as empty). */
  currentQuery(): string | null;
}

const QUERY_PARAM = "q";

/** `?q=` from a `location.search` string; `null` when absent. */
export function queryFromSearch(search: string): string | null {
  const params = new URLSearchParams(search);
  return params.has(QUERY_PARAM) ? (params.get(QUERY_PARAM) ?? "") : null;
}

export function createQueryUrl(opts: {
  history?: ReplaceCapableHistory;
  getLocation?: () => { search: string; pathname: string };
  /** History state to carry through; defaults to `window.history.state`. */
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
