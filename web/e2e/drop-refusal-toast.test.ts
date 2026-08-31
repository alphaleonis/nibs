import { test, expect } from "@playwright/test";

// A drop the plan refuses reaches the app now instead of being dropped at the
// validity gate, and the app says why. Only a real engine can show that end to
// end: jsdom does not implement `document.elementFromPoint`, so the pointer path
// that finds a drop target cannot run over a real table there — the composable's
// own suite substitutes one row at a time.
//
// The gesture chosen is the middle of a LEAF-typed row, which asks to put the
// dragged row INSIDE it. `canHaveChildren("task")` is false and no lens declares
// an entry region for an ordinary row, so there is no group to enter and the
// plan refuses whatever is being dragged.
//
// A milestone header no longer serves as that target, though it reads like the
// obvious one: the membership lens declares the milestone's QUEUE as that row's
// childRegion, and `entryRegionOf` returns a declaration before it ever asks the
// type hierarchy — so dropping a work item into one is an in-queue move or an
// assignment refusal, not "holds no children". Only a dragged MILESTONE still
// reaches that refusal there, since no milestone can join a queue, and this test
// wants a target that refuses whatever is dragged onto it.
//
// Guard proof (nibs-2tkt): re-gating delivery on validity in useTreeDrag's
// onDragPointerUp (`dropPlan !== null && dropPlan.ok`) fails the first test with
// "element(s) not found" — no toast is rendered at all. Dropping `drop-on-self`
// from handleDrop's suppression condition in App.svelte fails the second with 1
// toast instead of 0.

test("a drop into a container that holds no children explains itself", async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem("nibs-filter-preferences", JSON.stringify({ viewLevel: "milestones" }));
  });
  await page.goto("/");

  const leaf = page.locator('tr[data-nib-id="tnib-t041"]');
  await expect(leaf).toBeVisible({ timeout: 10_000 });
  // Centered, so the one-row-tall gesture below stays away from the scroll
  // container's auto-scroll edges. Boxes are read AFTER this.
  await page.evaluate(() => {
    document.querySelector('tr[data-nib-id="tnib-t041"]')?.scrollIntoView({ block: "center" });
  });

  // Its sibling, immediately below it.
  const source = page.locator('tr[data-nib-id="tnib-t042"]');
  await expect(source).toBeVisible();

  const from = (await source.boundingBox())!;
  const to = (await leaf.boundingBox())!;

  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
  await page.mouse.down();
  // Past the 5px drag threshold first, then onto the middle 40% of the target,
  // which is the zone that reads as "into this row".
  await page.mouse.move(from.x + from.width / 2 + 12, from.y + from.height / 2, { steps: 5 });
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 10 });
  await page.mouse.up();

  await expect(page.locator("[data-sonner-toast]")).toContainText("holds no children");
});

// The other half of the same seam: refusals reaching the app must not turn the
// ordinary cancel gesture into an error. Releasing back on the row you grabbed
// clears the 5px threshold and plans `drop-on-self`, which is a cancel rather
// than a rejection and stayed silent before this path existed.
test("releasing back on the dragged row cancels without an error", async ({ page }) => {
  await page.goto("/");

  const row = page.locator("tr[data-nib-id]").first();
  await expect(row).toBeVisible({ timeout: 10_000 });
  // Without this the box can name a point below the fold, where the pointer
  // lands on nothing and no drag ever starts — which would make the assertion
  // below pass for the wrong reason.
  await row.scrollIntoViewIfNeeded();

  const box = (await row.boundingBox())!;
  const cx = box.x + box.width / 2;
  const cy = box.y + box.height / 2;

  await page.mouse.move(cx, cy);
  await page.mouse.down();
  // Past the 5px threshold, then back onto the row it started on.
  await page.mouse.move(cx + 14, cy, { steps: 5 });
  // The drag is real: without this the gesture below proves nothing.
  await expect(page.locator("body")).toHaveCSS("cursor", "grabbing");
  await page.mouse.move(cx, cy, { steps: 5 });
  await page.mouse.up();

  // Give a toast the chance to appear, then read the count ONCE. `toHaveCount(0)`
  // would be vacuous here: it retries for 5s, and a sonner toast auto-dismisses
  // inside that window, so it passes by waiting for the toast to go away rather
  // than by the toast never arriving.
  await page.waitForTimeout(600);
  expect(await page.locator("[data-sonner-toast]").count()).toBe(0);
});
