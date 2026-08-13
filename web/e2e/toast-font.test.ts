import { test, expect, type Page } from "@playwright/test";

// svelte-sonner hardcodes its own font stack on [data-sonner-toaster] (starting
// at ui-sans-serif/system-ui), so a toast never inherits the app's Mona Sans and
// renders in the platform UI font instead. jsdom does not implement the cascade
// and the screenshot captures cannot catch it either — the toaster is portaled to
// document.body and only exists while a toast is live — so this runs in a real
// engine and reads the COMPUTED value.
//
// Guard proof (nibs-pldv): removing `font-family` from the Toaster wrapper's
// inline style fails this with sonner's own stack ("ui-sans-serif, system-ui, …")
// in place of the app's.

/** Open the app with a persisted sort, which blocks drag-reorder. */
async function openSorted(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem(
      "nibs-filter-preferences",
      JSON.stringify({
        viewLevel: "milestones",
        tableSort: { field: "status", direction: "asc" },
      }),
    );
  });
  await page.goto("/");
  await expect(page.locator("tr[data-nib-id]").first()).toBeVisible({ timeout: 10_000 });
}

test("a toast renders in the app's font, not the platform UI font", async ({ page }) => {
  await openSorted(page);

  // Attempting a drag on a sort-blocked row raises the explanation toast — a
  // trigger that needs no clipboard or network permissions.
  const row = page.locator("tr[data-nib-id]").nth(3);
  const box = (await row.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 10, box.y + box.height / 2 + 70, { steps: 10 });
  await page.mouse.up();

  const toast = page.locator("[data-sonner-toast]").first();
  await expect(toast).toBeVisible({ timeout: 5_000 });

  const [toastFont, appFont] = await page.evaluate(() => [
    getComputedStyle(document.querySelector("[data-sonner-toast]")!).fontFamily,
    getComputedStyle(document.body).fontFamily,
  ]);

  // Asserting against the app's own resolved stack rather than a literal keeps
  // this honest if the font is ever swapped.
  expect(toastFont).toBe(appFont);
});
