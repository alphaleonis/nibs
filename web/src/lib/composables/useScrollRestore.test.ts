import { describe, it, expect } from "vitest";
import { useScrollRestore } from "./useScrollRestore.svelte";

// A fake scroll container: only `scrollTop` matters for these unit tests. Stores
// any assigned value verbatim (no clamping — use clampingContainer() for that).
function fakeContainer(): HTMLElement {
  return { scrollTop: 0 } as unknown as HTMLElement;
}

// A fake container that CLAMPS on assignment the way a real element does
// (`scrollTop` is pinned to [0, maxScrollTop]). This lets restore()'s read-back
// capture the clamp exactly as it would in a browser — jsdom has no layout, so
// without this the clamp path can't be exercised at the unit level.
function clampingContainer(maxScrollTop: number): HTMLElement {
  let top = 0;
  return {
    get scrollTop() {
      return top;
    },
    set scrollTop(v: number) {
      top = Math.min(Math.max(0, v), maxScrollTop);
    },
  } as unknown as HTMLElement;
}

// onScroll now keys off the scroll event's own currentTarget (the element the DOM
// handler is bound to), not an ambient getScrollContainer() read.
function scrollEvent(container: HTMLElement | null): Event {
  return { currentTarget: container } as unknown as Event;
}

describe("useScrollRestore", () => {
  it("restore() sets the container's scrollTop to the saved value when content is present", () => {
    let saved = 250;
    const container = fakeContainer();
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    r.restore();

    expect(container.scrollTop).toBe(250);
  });

  it("restore() is a no-op while hasContent() is false, then restores once content is present", () => {
    let saved = 250;
    let hasContent = false;
    const container = fakeContainer();
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => hasContent,
    });

    // No content yet: restore() must not touch the container and must NOT take
    // ownership, so a later attempt can still apply the saved value.
    r.restore();
    expect(container.scrollTop).toBe(0);

    // Content mounts: the still-unowned element now receives the saved value.
    hasContent = true;
    r.restore();
    expect(container.scrollTop).toBe(250);
  });

  it("restore() restores at most once per element, but a fresh element restores again", () => {
    let saved = 250;
    let container = fakeContainer();
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    r.restore();
    expect(container.scrollTop).toBe(250);

    // Same element, changed saved value: second restore() must NOT re-apply it.
    saved = 999;
    r.restore();
    expect(container.scrollTop).toBe(250);

    // A NEW container element (e.g. urql refetch / empty-filter recreated the div
    // within the same TreeTable instance) must restore again — ownership is keyed
    // to element identity, not composable-instance lifetime.
    container = fakeContainer();
    r.restore();
    expect(container.scrollTop).toBe(999);
  });

  it("onScroll() records a genuine user scroll once the element is owned", () => {
    let saved = 0;
    const container = fakeContainer();
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    // restore()'s write here is a no-op (saved 0 → container already 0), so it
    // arms lastWrite={top:0}. A genuine user scroll reports a DIFFERENT value, so
    // it does not match the echo record and is persisted. (This is the case that
    // used to strand the old boolean flag.)
    r.restore();
    container.scrollTop = 120;
    r.onScroll(scrollEvent(container));

    expect(saved).toBe(120);
  });

  it("onScroll() does not overwrite saved before the element is owned", () => {
    let saved = 250;
    const container = fakeContainer();
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    // Fresh container reports scrollTop=0 before we've restored; onScroll must
    // ignore it (currentTarget !== ownedEl) so the saved value survives.
    r.onScroll(scrollEvent(container));

    expect(saved).toBe(250);
  });

  it("onScroll() / restore() / claim() are safe no-ops when the container is null, and restore still fires once a real container appears", () => {
    let saved = 250;
    let container: HTMLElement | null = null;
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    // restore()/claim() self-locate via getScrollContainer() and early-return on
    // null; onScroll() early-returns on a null currentTarget.
    expect(() => r.restore()).not.toThrow();
    expect(() => r.onScroll(scrollEvent(null))).not.toThrow();
    expect(() => r.claim()).not.toThrow();
    expect(saved).toBe(250);

    // The null calls must not have corrupted the guard state: once a real
    // container with content appears, restore() still applies the saved value.
    container = fakeContainer();
    r.restore();
    expect(container.scrollTop).toBe(250);
  });

  it("claim() persists container.scrollTop into saved synchronously", () => {
    // Regression: ensureVisible scrolls the deep-linked row
    // into view, then claim() must write that offset into the saved slot
    // IMMEDIATELY — not defer it to the async native scroll event, which a refetch
    // can preempt by unmounting the container first.
    let saved = 0;
    const container = fakeContainer();
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    container.scrollTop = 500;
    r.claim();

    // Persisted synchronously, with no onScroll() call in between.
    expect(saved).toBe(500);
  });

  it("claim() is per-element: restore() cannot overwrite the claimed element, but a fresh element still restores", () => {
    let saved = 250;
    let container = fakeContainer();
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    // ensureVisible's scrollIntoView moved element A, then claim() handled it.
    container.scrollTop = 500;
    r.claim();

    // restore() must not clobber the deep-link scroll back to the saved value.
    r.restore();
    expect(container.scrollTop).toBe(500);

    // A DIFFERENT element B (container recreated) is not owned → restore fires,
    // proving ownership is keyed to element identity, not the composable instance.
    saved = 999;
    container = fakeContainer();
    r.restore();
    expect(container.scrollTop).toBe(999);
  });

  it("suppresses the one onScroll() echo of restore()'s own moved write (value-keyed), then records the next genuine scroll", () => {
    let saved = 250;
    const container = clampingContainer(1000); // room to move to 250, no clamp
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    r.restore();
    expect(container.scrollTop).toBe(250); // programmatic write moved the container

    // The echo of our own write: same element, same value → must NOT persist
    // (harmless here since value is unchanged, but it must be treated as ours).
    r.onScroll(scrollEvent(container));
    expect(saved).toBe(250);

    // A genuine user scroll AFTER the echo reports a different value → recorded.
    container.scrollTop = 400;
    r.onScroll(scrollEvent(container));
    expect(saved).toBe(400);
  });

  it("a browser-CLAMPED restore write is not echoed back into saved (read-back captures the clamp)", () => {
    // Regression: the saved offset (5000) is larger than the
    // shorter refetched list allows; the browser clamps the write to maxScrollTop
    // (1800) and fires a scroll event reporting 1800. restore() reads scrollTop
    // back AFTER the write, so lastWrite.top captures the clamped 1800 — the echo
    // matches and is skipped, so saved is NOT shrunk to 1800.
    let saved = 5000;
    const container = clampingContainer(1800);
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    r.restore();
    expect(container.scrollTop).toBe(1800); // clamped by the container

    // The echo the clamped write provokes (scrollTop already 1800) must be ignored.
    r.onScroll(scrollEvent(container));
    expect(saved).toBe(5000); // saved is preserved for a later regrow, not shrunk

    // One-shot: a genuine user scroll after the echo is recorded.
    container.scrollTop = 1600;
    r.onScroll(scrollEvent(container));
    expect(saved).toBe(1600);
  });

  it("a no-op restore write does not strand suppression: the next genuine scroll is recorded", () => {
    // Regression — now a consequence of value-keying rather
    // than a special "arm only when moved" branch. The common top-of-list case
    // (saved 0, fresh container already at 0) is a no-op write; lastWrite.top is 0.
    // The user's next scroll reports a different value, does not match the echo
    // record, and is persisted — nothing is swallowed.
    let saved = 0;
    const container = fakeContainer();
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    r.restore();
    expect(container.scrollTop).toBe(0);

    container.scrollTop = 140;
    r.onScroll(scrollEvent(container));

    expect(saved).toBe(140);
  });

  it("a suppression armed on a torn-down container cannot swallow a genuine scroll on the next container (cross-generation strand)", () => {
    // Regression (review 2026-07-10 21-18-21): the fourth consecutive finding. With
    // the old closure boolean, a flag armed by a clamped restore() on container A,
    // whose echo was dropped when a refetch tore A down, would strand and swallow
    // the user's first genuine scroll on container B (adopted by claim()). Because
    // lastWrite now carries its OWN element, a record armed on A can never match a
    // scroll on B — the whole bug class is structurally impossible.
    let saved = 5000;
    let container: HTMLElement = clampingContainer(1800);
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    // restore() on container A: clamps 5000 → 1800, arms lastWrite={A, 1800}.
    r.restore();
    expect(container.scrollTop).toBe(1800);

    // A refetch tears A down before its echo dispatches — the echo is dropped and
    // lastWrite is stranded pointing at the (now dead) container A. A fresh
    // container B mounts; a pending deep-link runs claim() on B.
    const containerB = fakeContainer();
    container = containerB; // getScrollContainer() now resolves to B
    r.claim(); // takes ownership of B; lastWrite (pointing at A) is left inert
    expect(saved).toBe(0); // B is at scrollTop 0; claim persisted that

    // The user's FIRST genuine scroll on B must be recorded — not swallowed by the
    // stranded {A, 1800} record (A !== B).
    containerB.scrollTop = 300;
    r.onScroll(scrollEvent(containerB));
    expect(saved).toBe(300);
  });
});
