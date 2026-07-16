import { describe, it, expect } from "vitest";
import { SelectionState } from "../selection.svelte";
import { createHistoryNav, nibIdFromSearch, type HistoryLike } from "./useHistoryNav.svelte";

interface RecordedCall {
  kind: "push" | "replace";
  state: unknown;
  url: string | null | undefined;
}

function makeMockHistory(): HistoryLike & { calls: RecordedCall[] } {
  const calls: RecordedCall[] = [];
  return {
    calls,
    pushState(data: unknown, _unused: string, url?: string | null) {
      calls.push({ kind: "push", state: data, url });
    },
    replaceState(data: unknown, _unused: string, url?: string | null) {
      calls.push({ kind: "replace", state: data, url });
    },
  };
}

function setup(overrides: {
  selection?: SelectionState;
  location?: { search: string; pathname: string };
  isBlocked?: () => boolean;
} = {}) {
  const selection = overrides.selection ?? new SelectionState();
  const history = makeMockHistory();
  const location = overrides.location ?? { search: "", pathname: "/" };
  const nav = createHistoryNav({
    selection,
    history,
    getLocation: () => location,
    isBlocked: overrides.isBlocked,
  });
  return { nav, selection, history };
}

describe("useHistoryNav", () => {
  it("navigateToNib pushes ?nib=<id> with state {nibId} and selects it", () => {
    const { nav, selection, history } = setup();

    nav.navigateToNib("tnib-42");

    expect(history.calls).toEqual([
      { kind: "push", state: { nibId: "tnib-42" }, url: "?nib=tnib-42" },
    ]);
    expect(selection.selectedNibId).toBe("tnib-42");
  });

  it("navigateToNib does NOT push ?nib=<bucket> for a synthetic grouping-bucket id (nibs-oxaq)", () => {
    // A right-clicked/arrow-focused "No X" bucket row can route here. select()
    // already no-ops on a bucket id, but the history push must also be skipped —
    // otherwise a stale ?nib=__no_milestone__ survives reload/Back and tries to
    // select a nonexistent nib.
    const { nav, selection, history } = setup();

    nav.navigateToNib("__no_milestone__");

    expect(history.calls).toEqual([]);
    expect(selection.selectedNibId).toBeNull();
  });

  it("navigateToNib is idempotent when the nib is already selected", () => {
    const selection = new SelectionState();
    selection.select("tnib-7");
    const { nav, history } = setup({ selection });

    nav.navigateToNib("tnib-7");

    expect(history.calls).toEqual([]);
    expect(selection.selectedNibId).toBe("tnib-7");
  });

  it("navigateToNib switches between two different already-selected nibs (pushes + selects)", () => {
    const selection = new SelectionState();
    selection.select("a");
    const { nav, history } = setup({ selection });

    nav.navigateToNib("b");

    // Regression: a truthy guard (`if (selectedNibId) return`) would skip
    // this push. NB this does NOT lock the `=== id` early-return (a≠b pushes
    // either way) — the focus-resync test below is that lock.
    expect(history.calls).toEqual([
      { kind: "push", state: { nibId: "b" }, url: "?nib=b" },
    ]);
    expect(selection.selectedNibId).toBe("b");
  });

  it("navigateToNib to the already-open nib resyncs focus without a new push", () => {
    const selection = new SelectionState();
    selection.select("a");
    selection.focus("b"); // focus drifts while selectedNibId stays "a"
    const { nav, history } = setup({ selection });

    nav.navigateToNib("a");

    // Regression: the guard early-returned the whole body on
    // `selectedNibId === id`, skipping select()'s focus/anchor resync.
    // No duplicate history entry (selectedNibId is already "a")...
    expect(history.calls).toEqual([]);
    // ...but select() must still re-run and resync focus back to "a".
    expect(selection.focusedNibId).toBe("a");
    expect(selection.selectedNibId).toBe("a");
  });

  it("closePanel pushes {nibId:null} to the pathname and closes the panel", () => {
    const selection = new SelectionState();
    selection.select("tnib-9");
    const { nav, history } = setup({ selection, location: { search: "?nib=tnib-9", pathname: "/" } });

    nav.closePanel();

    expect(history.calls).toEqual([
      { kind: "push", state: { nibId: null }, url: "/" },
    ]);
    expect(selection.selectedNibId).toBeNull();
  });

  it("closePanel is a no-op when no nib is selected", () => {
    const { nav, selection, history } = setup();

    nav.closePanel();

    expect(history.calls).toEqual([]);
    expect(selection.selectedNibId).toBeNull();
  });

  it("replaceClosed heals the URL in place (replaceState to clean path), leaving selection to the caller", () => {
    // Used after deleting/archiving the open nib, or when landing on a nib that
    // no longer exists: heal the current entry in place (replace, no Back-stop),
    // so a stale ?nib=<gone> doesn't survive reload/Back. Selection is the
    // caller's responsibility (clearAll / close), so replaceClosed must not touch it.
    const selection = new SelectionState();
    selection.select("gone");
    const { nav, history } = setup({ selection, location: { search: "?nib=gone", pathname: "/" } });

    nav.replaceClosed();

    expect(history.calls).toEqual([
      { kind: "replace", state: { nibId: null }, url: "/" },
    ]);
    expect(selection.selectedNibId).toBe("gone");
  });

  it("handlePopState with {nibId:'b'} selects b + ensureVisible, no history writes", () => {
    const { nav, selection, history } = setup();

    nav.handlePopState({ state: { nibId: "b" } });

    expect(selection.selectedNibId).toBe("b");
    expect(selection.pendingEnsureVisibleId).toBe("b");
    expect(history.calls).toEqual([]);
  });

  it("handlePopState with {nibId:null} closes the panel, no history writes", () => {
    const selection = new SelectionState();
    selection.select("tnib-5");
    const { nav, history } = setup({ selection });

    nav.handlePopState({ state: { nibId: null } });

    expect(selection.selectedNibId).toBeNull();
    expect(history.calls).toEqual([]);
  });

  it("handlePopState with null state falls back to URL and selects that nib", () => {
    const { nav, selection, history } = setup({ location: { search: "?nib=c", pathname: "/" } });

    nav.handlePopState({ state: null });

    expect(selection.selectedNibId).toBe("c");
    expect(selection.pendingEnsureVisibleId).toBe("c");
    expect(history.calls).toEqual([]);
  });

  it("handlePopState with a non-string nibId ({nibId:42}) rejects the state and falls back to URL", () => {
    const { nav, selection, history } = setup({ location: { search: "?nib=fallback", pathname: "/" } });

    // Hostile / foreign history.state: the key exists but the value is not a
    // string|null. isNibState must reject it so no non-string reaches selection.
    nav.handlePopState({ state: { nibId: 42 } });

    expect(selection.selectedNibId).toBe("fallback");
    expect(selection.pendingEnsureVisibleId).toBe("fallback");
    expect(history.calls).toEqual([]);
  });

  it("handlePopState with a non-nib-shaped object ({foo:1}) falls back to URL", () => {
    const { nav, selection, history } = setup({ location: { search: "?nib=fallback", pathname: "/" } });

    nav.handlePopState({ state: { foo: 1 } });

    expect(selection.selectedNibId).toBe("fallback");
    expect(history.calls).toEqual([]);
  });

  it("handlePopState is a no-op behind an open overlay: keeps selection, re-anchors history", () => {
    // A blocking modal (editor/type-picker/confirm) is open. Back/Forward must not
    // navigate the covered panel; history is re-anchored to the shown nib so URL
    // stays consistent and selection is untouched.
    const selection = new SelectionState();
    selection.select("a");
    const { nav, history } = setup({ selection, isBlocked: () => true });

    nav.handlePopState({ state: { nibId: "b" } });

    expect(selection.selectedNibId).toBe("a"); // did NOT navigate to b
    expect(selection.pendingEnsureVisibleId).toBeNull(); // no ensureVisible for b
    expect(history.calls).toEqual([
      { kind: "push", state: { nibId: "a" }, url: "?nib=a" },
    ]);
  });

  it("handlePopState behind an overlay with a closed panel re-anchors to the clean path", () => {
    const selection = new SelectionState(); // nothing selected
    const { nav, history } = setup({
      selection,
      isBlocked: () => true,
      location: { search: "?nib=b", pathname: "/" },
    });

    nav.handlePopState({ state: { nibId: "b" } });

    expect(selection.selectedNibId).toBeNull();
    expect(history.calls).toEqual([
      { kind: "push", state: { nibId: null }, url: "/" },
    ]);
  });

  it("syncFromUrl with ?nib=d selects d + ensureVisible and replaceState, no pushState", () => {
    const { nav, selection, history } = setup({ location: { search: "?nib=d", pathname: "/" } });

    nav.syncFromUrl();

    expect(selection.selectedNibId).toBe("d");
    expect(selection.pendingEnsureVisibleId).toBe("d");
    expect(history.calls).toEqual([
      { kind: "replace", state: { nibId: "d" }, url: "?nib=d" },
    ]);
  });

  it("syncFromUrl with empty search does not select and replaceState to pathname", () => {
    const { nav, selection, history } = setup({ location: { search: "", pathname: "/" } });

    nav.syncFromUrl();

    expect(selection.selectedNibId).toBeNull();
    expect(history.calls).toEqual([
      { kind: "replace", state: { nibId: null }, url: "/" },
    ]);
  });

  describe("nibIdFromSearch", () => {
    it.each([
      { search: "?nib=x", expected: "x" },
      { search: "?a=1&nib=y", expected: "y" },
      { search: "", expected: null },
      { search: "?nib=", expected: null },
      { search: "?nib=tnib-42", expected: "tnib-42" },
      { search: "?other=z", expected: null },
    ])("parses $search -> $expected", ({ search, expected }) => {
      expect(nibIdFromSearch(search)).toBe(expected);
    });
  });
});
