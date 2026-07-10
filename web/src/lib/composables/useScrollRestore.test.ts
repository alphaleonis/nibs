import { describe, it, expect } from "vitest";
import { useScrollRestore } from "./useScrollRestore.svelte";

// A fake scroll container: only `scrollTop` matters for these unit tests.
function fakeContainer(): HTMLElement {
  return { scrollTop: 0 } as unknown as HTMLElement;
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

    // No content yet: restore() must not touch the container and must NOT claim
    // it, so a later attempt can still apply the saved value.
    r.restore();
    expect(container.scrollTop).toBe(0);

    // Content mounts: the still-unrestored element now receives the saved value.
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
    // within the same TreeTable instance) must restore again — the guard is keyed
    // to element identity, not composable-instance lifetime. (review #2)
    container = fakeContainer();
    r.restore();
    expect(container.scrollTop).toBe(999);
  });

  it("onScroll() records the container's scrollTop into saved after that element is restored", () => {
    let saved = 0;
    const container = fakeContainer();
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    r.restore();
    // restore() arms the one-shot suppress, so the FIRST onScroll() is the
    // echo of restore()'s own write and is ignored — consume it here.
    r.onScroll();
    // A genuine user scroll AFTER the echo must be recorded.
    container.scrollTop = 120;
    r.onScroll();

    expect(saved).toBe(120);
  });

  it("onScroll() does not overwrite saved before the element is restored", () => {
    let saved = 250;
    const container = fakeContainer();
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    // Fresh container reports scrollTop=0 before we've restored; onScroll must
    // ignore it so the saved value survives to be applied by restore().
    r.onScroll();

    expect(saved).toBe(250);
  });

  it("onScroll() / restore() are safe no-ops when the container is null, and restore still fires once a real container appears", () => {
    let saved = 250;
    let container: HTMLElement | null = null;
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    // claim() self-locates via getScrollContainer() and early-returns on null,
    // matching restore()/onScroll() and the sibling composables — so it is a safe
    // no-op before a real container exists.
    expect(() => r.restore()).not.toThrow();
    expect(() => r.onScroll()).not.toThrow();
    expect(() => r.claim()).not.toThrow();
    expect(saved).toBe(250);

    // The null calls must not have corrupted the guard state: once a real
    // container with content appears, restore() still applies the saved value.
    container = fakeContainer();
    r.restore();
    expect(container.scrollTop).toBe(250);
  });

  it("claim() persists container.scrollTop into saved synchronously", () => {
    // Regression (nibs-n47p, review #1): ensureVisible scrolls the deep-linked row
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

    // A DIFFERENT element B (container recreated) is not claimed → restore fires,
    // proving the claim is keyed to element identity, not the composable instance.
    // (Use a distinct saved value to show restore actually applies it to B.)
    saved = 999;
    container = fakeContainer();
    r.restore();
    expect(container.scrollTop).toBe(999);
  });

  it("suppresses the one onScroll() echo that restore()'s own (possibly clamped) write provokes", () => {
    // Regression (nibs-qpvw, review #1): a real browser silently CLAMPS scrollTop
    // to scrollHeight - clientHeight; the fake stores any number verbatim, so we
    // simulate the clamp by manually lowering scrollTop after restore(). Without
    // suppression, restore()'s programmatic write fires a native scroll event
    // whose onScroll() would persist the CLAMPED value, permanently shrinking the
    // saved offset on a shorter (e.g. delete-driven) refetch.
    let saved = 5000;
    const container = fakeContainer();
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    r.restore();
    expect(container.scrollTop).toBe(5000); // programmatic write (fake does not clamp)

    // Simulate the browser clamping the too-large offset down to the max.
    container.scrollTop = 1800;
    // This onScroll() is the echo of our own restore() write — it must be ignored.
    r.onScroll();

    expect(saved).toBe(5000); // saved is NOT shrunk to the clamped 1800
  });

  it("the restore-echo suppression is ONE-SHOT: the next genuine user scroll is recorded", () => {
    let saved = 5000;
    const container = fakeContainer();
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    r.restore();
    container.scrollTop = 1800; // simulated browser clamp
    r.onScroll();               // suppressed echo
    expect(saved).toBe(5000);

    // A genuine user scroll AFTER the single suppressed echo must be recorded.
    container.scrollTop = 1600;
    r.onScroll();

    expect(saved).toBe(1600);
  });

  it("does NOT arm suppression when restore()'s write is a no-op (saved === current scrollTop)", () => {
    // Regression (nibs-qpvw, review #1): a browser fires a scroll event only when a
    // scrollTop assignment actually MOVES the container. The common top-of-list case
    // (saved === 0, fresh/remounted container already at 0) is a no-op write that
    // fires no echo — so arming suppressNextScroll unconditionally strands the flag,
    // and it then swallows the user's next GENUINE scroll (onScroll returns before
    // setSavedScrollTop). restore() must arm only when the write actually moved.
    let saved = 0;
    const container = fakeContainer(); // starts at scrollTop 0
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    // No-op write: saved (0) equals the container's current scrollTop (0).
    r.restore();
    expect(container.scrollTop).toBe(0);

    // The next scroll is a genuine user scroll (no echo was fired), so it MUST be
    // recorded — not swallowed by a stranded suppression flag.
    container.scrollTop = 140;
    r.onScroll();

    expect(saved).toBe(140);
  });
});
