import { expect } from "vitest";

/** Offenders are reported as markup: a bare count says something is wrong but
 *  not which element to go fix. */
function describe(el: Element): string {
  const testId = el.getAttribute("data-testid");
  return `<${el.tagName.toLowerCase()}${testId === null ? "" : ` data-testid="${testId}"`}>`;
}

/**
 * Asserts nothing rendered is leaning on the browser's native tooltip.
 *
 * Reads the rendered DOM rather than the source, so it is only as wide as what
 * the caller has drawn — open the menus and panels whose markup should be swept.
 * The default root is `document.body` because bits-ui portals its content out of
 * the render container.
 */
export function expectNoNativeTitles(root: ParentNode = document.body): void {
  const offenders = Array.from(root.querySelectorAll("[title]")).map(
    (el) => `${describe(el)} title="${el.getAttribute("title")}"`,
  );
  expect(offenders).toEqual([]);
}

/**
 * Asserts every delegating element names itself to assistive technology.
 *
 * A `data-tooltip` element is not a tooltip trigger and gains no
 * `aria-describedby`, so one whose only name was the native `title` it replaced
 * is left nameless. Same reach caveat as above: only what the caller renders.
 */
export function expectDelegatingElementsAreNamed(root: ParentNode = document.body): void {
  const nameless = Array.from(root.querySelectorAll("[data-tooltip]"))
    .filter(
      (el) =>
        (el.textContent ?? "").trim() === "" &&
        !el.hasAttribute("aria-label") &&
        !el.hasAttribute("aria-labelledby"),
    )
    .map((el) => `${describe(el)} data-tooltip="${el.getAttribute("data-tooltip")}"`);
  expect(nameless).toEqual([]);
}
