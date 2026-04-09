import "@testing-library/jest-dom/vitest";
import { afterEach, afterAll } from "vitest";

// Polyfill ResizeObserver missing in jsdom (used by Floating UI / bits-ui portaled content)
if (typeof globalThis.ResizeObserver === "undefined") {
  globalThis.ResizeObserver = class ResizeObserver {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof globalThis.ResizeObserver;
}

// Polyfill pointer capture APIs missing in jsdom (used by bits-ui Select)
if (typeof Element.prototype.hasPointerCapture !== "function") {
  Element.prototype.hasPointerCapture = function () {
    return false;
  };
}
if (typeof Element.prototype.releasePointerCapture !== "function") {
  Element.prototype.releasePointerCapture = function () {};
}
if (typeof Element.prototype.setPointerCapture !== "function") {
  Element.prototype.setPointerCapture = function () {};
}

// Polyfill scrollIntoView missing in jsdom (used by bits-ui Select items)
if (typeof Element.prototype.scrollIntoView !== "function") {
  Element.prototype.scrollIntoView = function () {};
}

// Polyfill matchMedia missing in jsdom (used by svelte-sonner Toaster)
if (typeof window.matchMedia !== "function") {
  window.matchMedia = function (query: string) {
    return {
      matches: false,
      media: query,
      onchange: null,
      addListener: function () {},
      removeListener: function () {},
      addEventListener: function () {},
      removeEventListener: function () {},
      dispatchEvent: function () {
        return false;
      },
    } as MediaQueryList;
  };
}

// Floating UI cannot compute real positions in jsdom, so bits-ui portaled
// containers get `visibility: hidden` on their wrapper div. This causes
// testing-library to skip those elements when querying by role/text. We use a
// MutationObserver to force visibility to "visible" on these floating wrappers
// so that portaled content is queryable in tests.
const floatingObserver = new MutationObserver((mutations) => {
  for (const mutation of mutations) {
    for (const node of mutation.addedNodes) {
      if (node instanceof HTMLElement && node.style.visibility === "hidden") {
        // Floating UI sets `position: fixed` on the wrapper div
        if (node.style.position === "fixed") {
          node.style.visibility = "visible";
        }
      }
    }
    // Also handle style attribute changes
    if (
      mutation.type === "attributes" &&
      mutation.attributeName === "style" &&
      mutation.target instanceof HTMLElement
    ) {
      const el = mutation.target;
      if (el.style.visibility === "hidden" && el.style.position === "fixed") {
        el.style.visibility = "visible";
      }
    }
  }
});
floatingObserver.observe(document.body, {
  childList: true,
  subtree: true,
  attributes: true,
  attributeFilter: ["style"],
});

// bits-ui dialog components add scroll-lock styles (pointer-events: none,
// overflow: hidden) to document.body. These may persist across tests in jsdom
// since the body element is shared. Reset after each test.
afterEach(() => {
  document.body.style.cssText = "";
  document.body.removeAttribute("data-scroll-locked");
});

// bits-ui body-scroll-lock schedules a 24ms setTimeout for deferred cleanup
// when dialogs/popovers unmount. If the last test in a file triggers this,
// the timer fires after jsdom has torn down, causing "document is not defined".
// Flushing pending timers here (afterAll runs before environment teardown)
// ensures the cleanup runs while document still exists.
afterAll(async () => {
  await new Promise((resolve) => setTimeout(resolve, 50));
});
