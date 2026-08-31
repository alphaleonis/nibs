import { test, expect } from "@playwright/test";

// The view a session opens in: the app default with nothing stored, and the
// user's own pick once there is one.
//
// Only a real engine can close this. jsdom covers the pieces — loadPreferences
// reading the key, resolveViewLevel preferring it — but not the round trip they
// exist for, where the write happens in one page and the read in the next. The
// pick has to survive a genuine reload, through the same localStorage the app
// wrote, for the default to be provably confined to a first visit.
//
// A fresh browser context per test is what makes the first assertion meaningful:
// nothing is stored because nothing has run here yet.

/** The toolbar's view control. Matched on the aria-label PREFIX, since the rest
 *  of that label is the view name this test is watching change. */
const VIEW_TRIGGER = 'button[aria-label^="View:"]';

test("opens on the default view, then keeps the view the user picks across a reload", async ({ page }) => {
  await page.goto("/");
  await expect(page.locator("tr[data-nib-id]").first()).toBeVisible({ timeout: 10_000 });

  const trigger = page.locator(VIEW_TRIGGER);
  await expect(trigger).toHaveAttribute("aria-label", "View: Milestones");

  await trigger.click();
  await page.getByRole("menuitemradio", { name: "Tree" }).click();
  await expect(trigger).toHaveAttribute("aria-label", "View: Tree");

  await page.reload();
  await expect(page.locator("tr[data-nib-id]").first()).toBeVisible({ timeout: 10_000 });
  await expect(page.locator(VIEW_TRIGGER)).toHaveAttribute("aria-label", "View: Tree");
});
