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

  it("onScroll() / restore() / cancel() are safe no-ops when the container is null, and restore still fires once a real container appears", () => {
    let saved = 250;
    let container: HTMLElement | null = null;
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    expect(() => r.restore()).not.toThrow();
    expect(() => r.onScroll()).not.toThrow();
    expect(() => r.cancel()).not.toThrow();
    expect(saved).toBe(250);

    // The null calls must not have corrupted the guard state: once a real
    // container with content appears, restore() still applies the saved value.
    container = fakeContainer();
    r.restore();
    expect(container.scrollTop).toBe(250);
  });

  it("cancel() claims the current element so restore() cannot overwrite an external scroll, and onScroll() still records it", () => {
    let saved = 250;
    const container = fakeContainer();
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    // ensureVisible's scrollIntoView moved the container, then claimed it.
    container.scrollTop = 500;
    r.cancel();

    // restore() must not clobber the deep-link scroll back to the saved value.
    r.restore();
    expect(container.scrollTop).toBe(500);

    // onScroll() still records for the claimed element, so the deep-linked
    // position becomes the new saved offset.
    r.onScroll();
    expect(saved).toBe(500);
  });

  it("cancel() is per-element: a different element still gets restored", () => {
    let saved = 250;
    let container = fakeContainer();
    const r = useScrollRestore({
      getScrollContainer: () => container,
      getSavedScrollTop: () => saved,
      setSavedScrollTop: (n) => { saved = n; },
      hasContent: () => true,
    });

    // cancel() claims element A.
    container.scrollTop = 500;
    r.cancel();

    // A different element B (container recreated) is not claimed → restore fires.
    container = fakeContainer();
    r.restore();
    expect(container.scrollTop).toBe(250);
  });
});
