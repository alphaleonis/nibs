import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { createQueryUrl, queryFromSearch, type ReplaceCapableHistory } from "./useQueryUrl.svelte";

interface RecordedCall {
  kind: "push" | "replace";
  state: unknown;
  url: string | null | undefined;
}

// A history mock that records BOTH replaceState and pushState, so a test can
// assert the composable never uses pushState (no Back-stack entries).
function makeMockHistory(): ReplaceCapableHistory & {
  calls: RecordedCall[];
  pushState(data: unknown, unused: string, url?: string | null): void;
} {
  const calls: RecordedCall[] = [];
  return {
    calls,
    replaceState(data, _unused, url) {
      calls.push({ kind: "replace", state: data, url });
    },
    pushState(data, _unused, url) {
      calls.push({ kind: "push", state: data, url });
    },
  };
}

function setup(overrides: {
  location?: { search: string; pathname: string };
  state?: unknown;
  delay?: number;
} = {}) {
  const history = makeMockHistory();
  const location = overrides.location ?? { search: "", pathname: "/" };
  const queryUrl = createQueryUrl({
    history,
    getLocation: () => location,
    getState: () => (overrides.state !== undefined ? overrides.state : null),
    delay: overrides.delay ?? 300,
  });
  return { queryUrl, history, location };
}

describe("queryFromSearch", () => {
  it.each([
    { search: "?q=type:bug", expected: "type:bug" },
    { search: "?q=type%3Abug", expected: "type:bug" },
    { search: "?nib=x&q=login", expected: "login" },
    { search: "?q=", expected: "" }, // present but empty ≠ absent
    { search: "", expected: null },
    { search: "?nib=x", expected: null },
  ])("parses $search -> $expected", ({ search, expected }) => {
    expect(queryFromSearch(search)).toBe(expected);
  });
});

describe("useQueryUrl", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("currentQuery reads the ?q= param (null when absent)", () => {
    expect(setup({ location: { search: "?q=type%3Abug", pathname: "/" } }).queryUrl.currentQuery()).toBe("type:bug");
    expect(setup({ location: { search: "?nib=x", pathname: "/" } }).queryUrl.currentQuery()).toBeNull();
  });

  it("push debounces: no write before the delay, one replaceState after it", () => {
    const { queryUrl, history } = setup({ delay: 300 });

    queryUrl.push("type:bug");
    // Nothing written yet — still inside the debounce window.
    expect(history.calls).toEqual([]);

    vi.advanceTimersByTime(299);
    expect(history.calls).toEqual([]);

    vi.advanceTimersByTime(1);
    expect(history.calls).toEqual([{ kind: "replace", state: null, url: "/?q=type%3Abug" }]);
  });

  it("uses replaceState, never pushState (no Back-stack spam)", () => {
    const { queryUrl, history } = setup();
    queryUrl.push("type:bug");
    vi.advanceTimersByTime(300);
    expect(history.calls.every((c) => c.kind === "replace")).toBe(true);
    expect(history.calls.some((c) => c.kind === "push")).toBe(false);
  });

  it("coalesces rapid pushes — only the LAST value is written once", () => {
    const { queryUrl, history } = setup({ delay: 300 });

    queryUrl.push("type:bug");
    vi.advanceTimersByTime(100);
    queryUrl.push("type:feature");
    vi.advanceTimersByTime(100);
    queryUrl.push("status:todo");
    // No write yet (each push reset the timer).
    expect(history.calls).toEqual([]);

    vi.advanceTimersByTime(300);
    expect(history.calls).toEqual([{ kind: "replace", state: null, url: "/?q=status%3Atodo" }]);
  });

  it("an empty query removes the ?q= param entirely", () => {
    const { queryUrl, history } = setup({ location: { search: "?q=type%3Abug", pathname: "/" } });

    queryUrl.push("");
    vi.advanceTimersByTime(300);

    // No ?q= in the result, and the leftover URL is just the clean path.
    expect(history.calls).toEqual([{ kind: "replace", state: null, url: "/" }]);
  });

  it("preserves an existing ?nib= param (coexists — never clobbers selection)", () => {
    const { queryUrl, history } = setup({ location: { search: "?nib=tnib-9", pathname: "/" } });

    queryUrl.push("type:bug");
    vi.advanceTimersByTime(300);

    expect(history.calls).toEqual([{ kind: "replace", state: null, url: "/?nib=tnib-9&q=type%3Abug" }]);
  });

  it("removing ?q= keeps a sibling ?nib= param intact", () => {
    const { queryUrl, history } = setup({ location: { search: "?nib=tnib-9&q=old", pathname: "/" } });

    queryUrl.push("");
    vi.advanceTimersByTime(300);

    expect(history.calls).toEqual([{ kind: "replace", state: null, url: "/?nib=tnib-9" }]);
  });

  it("carries the current history state through the replaceState (does not clobber {nibId})", () => {
    const { queryUrl, history } = setup({ state: { nibId: "tnib-9" } });

    queryUrl.push("type:bug");
    vi.advanceTimersByTime(300);

    expect(history.calls).toEqual([{ kind: "replace", state: { nibId: "tnib-9" }, url: "/?q=type%3Abug" }]);
  });

  it("flush writes any pending value immediately (no timer wait)", () => {
    const { queryUrl, history } = setup();

    queryUrl.push("type:bug");
    expect(history.calls).toEqual([]);

    queryUrl.flush();
    expect(history.calls).toEqual([{ kind: "replace", state: null, url: "/?q=type%3Abug" }]);

    // The timer was consumed by flush — advancing does not write again.
    vi.advanceTimersByTime(300);
    expect(history.calls).toHaveLength(1);
  });

  it("cancel drops a pending write so it never lands", () => {
    const { queryUrl, history } = setup();

    queryUrl.push("type:bug");
    queryUrl.cancel();
    vi.advanceTimersByTime(300);

    expect(history.calls).toEqual([]);
  });
});
