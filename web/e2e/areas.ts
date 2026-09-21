import type { Locator, Page } from "@playwright/test";

/**
 * The row heading `area` in the Areas view.
 *
 * Matched on the row id rather than on the accessible name: the name is the
 * concatenation of the row's cells, and the leading actions cell contributes
 * its button's label first — a section row reads "Area actions web (2) The
 * browser client". Matching the area anywhere in that string would also accept
 * a nib row whose title happens to carry the word. The id is what `sectionRowId`
 * derives from the section key, so it names exactly one row and cannot collide
 * with nib content.
 */
export function areaSectionRow(page: Page, area: string): Locator {
  return page.locator(`tr[data-nib-id="/section:${area}_"]`);
}
