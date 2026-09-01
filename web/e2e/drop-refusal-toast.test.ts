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

// The refusal that LEADS somewhere: a cross-queue drop cannot be a move, but the
// assignment it names can be taken from the toast. Chromium is what proves the
// whole path — the pointer gesture, sonner's action button, the two mutations it
// runs, and the table settling with the row in its new section.
//
// Guard proof (nibs-rmq6): forcing handleDrop's `action:` to `undefined` in
// App.svelte fails this at the button locator; dropping the `reorderNib` step
// from `assignAndPlace` fails the final ordering assertion, because the server
// enters a newly assigned nib LAST in its queue.
test("the cross-queue refusal offers an assignment that lands where the drop pointed", async ({ page }) => {
  // Milestones and epics only: the fixture's two queues then fit on one screen,
  // so the source row and the row it is aimed at are both under the pointer's
  // reach without scrolling mid-gesture. A type filter does not block drag — only
  // free text, a column sort and the Flat view do.
  await page.addInitScript(() => {
    localStorage.setItem(
      "nibs-filter-preferences",
      JSON.stringify({ viewLevel: "milestones", filter: { type: ["milestone", "epic"] } }),
    );
  });
  await page.goto("/");

  // tnib-e006 is in v1.1's queue; tnib-e002 is in v1.0's, the queue being aimed
  // at. Dropping on tnib-e002's TOP edge points at the position before it.
  const source = page.locator('tr[data-nib-id="tnib-e006"]');
  const anchor = page.locator('tr[data-nib-id="tnib-e002"]');
  await expect(source).toBeVisible({ timeout: 10_000 });
  await expect(anchor).toBeVisible();

  const from = (await source.boundingBox())!;
  const to = (await anchor.boundingBox())!;

  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
  await page.mouse.down();
  await page.mouse.move(from.x + from.width / 2 + 12, from.y + from.height / 2, { steps: 5 });
  // The top 30% of the row, which is the zone that reads as "before this row".
  await page.mouse.move(to.x + to.width / 2, to.y + to.height * 0.1, { steps: 10 });
  await page.mouse.up();

  const toast = page.locator("[data-sonner-toast]");
  await expect(toast).toContainText("assignment rather than a move");
  const assign = toast.getByRole("button", { name: /^Assign to / });
  await expect(assign).toBeVisible();
  await assign.click();

  // Row order is the observable outcome: the moved epic now sits inside v1.0's
  // section, between the epic it was dropped after and the one it was dropped
  // before. Polled, because the click starts two mutations and a refetch.
  await expect
    .poll(
      async () =>
        page.evaluate(() => {
          const ids = [...document.querySelectorAll("tr[data-nib-id]")].map(
            (tr) => (tr as HTMLElement).dataset.nibId,
          );
          const at = (id: string) => ids.indexOf(id);
          return {
            present: at("tnib-e006") !== -1,
            inV1Section: at("tnib-e006") > at("tnib-m001") && at("tnib-e006") < at("tnib-m002"),
            // The position the drop pointed at, which the assignment alone would
            // not give: the server enters a newly assigned nib last in its queue.
            afterE001: at("tnib-e006") > at("tnib-e001"),
            beforeAnchor: at("tnib-e006") < at("tnib-e002"),
          };
        }),
      { timeout: 10_000 },
    )
    .toEqual({ present: true, inV1Section: true, afterE001: true, beforeAnchor: true });
});
