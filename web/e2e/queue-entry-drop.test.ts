import { test, expect } from "@playwright/test";

// Dropping work onto a milestone HEADER assigns it into that queue, with no
// refusal in the way. The gesture names the section rather than a position
// beside one of its members, which is the same rule an area section already
// follows — and the reason a drop between two members still refuses and offers
// the assignment as a remedy instead (drop-refusal-toast.test.ts covers that
// half).
//
// Chromium is what proves the whole path: jsdom has no
// `document.elementFromPoint`, so the pointer path that finds the drop target
// cannot run over a real table there, and it reports every row's rect as 0x0 at
// the origin — which makes the middle band unreachable by construction.
//
// Guard proof: restoring the refusal (`if (false)` on the entry branch in
// dropPlan.ts) fails this at the BADGE assertion with "element(s) not found" —
// a refused plan draws no badge at all, so the failure lands before the drop is
// even released.
test("dropping onto a milestone header assigns into its queue, at the front", async ({ page }) => {
  // Milestones and epics only, so both queues fit on one screen and neither the
  // source row nor the header is reached by scrolling mid-gesture.
  await page.addInitScript(() => {
    localStorage.setItem(
      "nibs-filter-preferences",
      JSON.stringify({ viewLevel: "milestones", filter: { type: ["milestone", "epic"] } }),
    );
  });
  await page.goto("/");

  // tnib-e005 is in v1.1's queue; tnib-m001 heads v1.0's, the queue being
  // joined. NOT tnib-e006, which drop-refusal-toast.test.ts assigns into v1.0 —
  // the lane serves one throwaway copy of the fixture to the whole suite, so a
  // source another test moves makes this one's queues depend on test order.
  const source = page.locator('tr[data-nib-id="tnib-e005"]');
  const header = page.locator('tr[data-nib-id="tnib-m001"]');
  await expect(source).toBeVisible({ timeout: 10_000 });
  await expect(header).toBeVisible();

  const from = (await source.boundingBox())!;
  const to = (await header.boundingBox())!;

  await page.mouse.move(from.x + from.width / 2, from.y + from.height / 2);
  await page.mouse.down();
  await page.mouse.move(from.x + from.width / 2 + 12, from.y + from.height / 2, { steps: 5 });
  // The middle 40% of the header, which is the band that reads as "into this
  // section" rather than as a position beside it.
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 10 });

  // The badge says what the drop will do BEFORE it happens — the accepted plan's
  // own label, not a refusal.
  await expect(page.getByTestId("drag-badge-label")).toContainText("Assign to");

  await page.mouse.up();

  // No refusal at all: the gesture named the queue, so it is taken.
  await page.waitForTimeout(600);
  expect(await page.locator("[data-sonner-toast]").count()).toBe(0);

  // Row order is the observable outcome: the epic now heads v1.0's section,
  // because an entry indicator names no neighbour and enters at the front.
  await expect
    .poll(
      async () =>
        page.evaluate(() => {
          const ids = [...document.querySelectorAll("tr[data-nib-id]")].map(
            (tr) => (tr as HTMLElement).dataset.nibId,
          );
          const at = (id: string) => ids.indexOf(id);
          return {
            inV1Section: at("tnib-e005") > at("tnib-m001") && at("tnib-e005") < at("tnib-m002"),
            firstInQueue: at("tnib-e005") === at("tnib-m001") + 1,
          };
        }),
      { timeout: 10_000 },
    )
    .toEqual({ inV1Section: true, firstInQueue: true });
});
