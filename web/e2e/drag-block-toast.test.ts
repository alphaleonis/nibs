import { test, expect, type Page } from "@playwright/test";

// Repeated blocked drag attempts must collapse into ONE toast rather than
// stacking. The collapsing happens inside svelte-sonner — it dedupes by toast id
// and updates in place — so only a real engine can prove it: a jsdom test can
// confirm we pass an id, but not what sonner does with it.
//
// Guard proof (nibs-typ9): dropping `id` from the toast.info options in
// TreeTable's onblockeddrag fails this with 3 toasts instead of 1.

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

/** Press a row and move past the drag threshold, which raises the block toast. */
async function attemptDrag(page: Page, rowIndex: number) {
  const row = page.locator("tr[data-nib-id]").nth(rowIndex);
  const box = (await row.boundingBox())!;
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 10, box.y + box.height / 2 + 70, { steps: 10 });
  await page.mouse.up();
}

test("repeated blocked drag attempts leave exactly one toast", async ({ page }) => {
  await openSorted(page);

  await attemptDrag(page, 3);
  // Retrying here is safe: one attempt can only ever produce one toast, so this
  // just waits for the first render.
  await expect(page.locator("[data-sonner-toast]")).toHaveCount(1);

  // Different rows, so this cannot pass by the gesture simply failing to fire —
  // each attempt is a fresh drag on a fresh row.
  await attemptDrag(page, 4);
  await attemptDrag(page, 5);

  // Deliberately NOT a retrying matcher. A stack of three expires one at a time,
  // so `toHaveCount(1)` would poll straight through the moment exactly one is
  // left and pass against stacking. All three attempts finish well inside the
  // toast duration, so an immediate count is the honest question: how many are
  // alive right now?
  expect(await page.locator("[data-sonner-toast]").count()).toBe(1);
  await expect(page.locator("[data-sonner-toast]")).toContainText("Reordering is off");
});
