import { test, expect, type Page } from "@playwright/test";

// What a drag says about itself while it is in flight, and what a refused one
// says on release. jsdom can assert the classes and drive the pointer path with
// a stubbed `document.elementFromPoint` (useTreeDrag.test.ts's `hoverRow` does
// exactly that), but it has no layout and no computed colors — the line, the
// band and the badge's legibility over the ghost are pixels, so they are checked
// here.
//
// A QUEUE drop is deliberately absent here. The Milestones view's membership
// lens now declares a MILESTONE region on each milestone section, so a queue
// gesture IS producible in the running app — but the Chromium coverage for the
// queue band, the queue drop indicator and the queue badge text is nibs-5sp0's,
// and until it lands those visuals stay covered at the component level
// (TreeTableRow.test.ts, DragBadge.test.ts, regionBand.test.ts) against regions
// built by hand. The gestures below stay on the parent axis on purpose.

/**
 * Scrolls `nibId`'s row to the middle of the scroll container.
 *
 * Call `boxOf` for every row the gesture touches AFTER this returns — a box
 * captured before the scroll is stale. Centering is not cosmetic: auto-scroll
 * fires within `AUTO_SCROLL_EDGE` (50px) of the container's edges, so a gesture
 * aimed at a row near the bottom scrolls the table out from under itself, and
 * the pointer lands on a row nobody chose.
 */
async function centerOn(page: Page, nibId: string) {
  await page.evaluate((id) => {
    document.querySelector(`tr[data-nib-id="${id}"]`)?.scrollIntoView({ block: "center" });
  }, nibId);
}

/** The row's box, or a failure naming the row rather than a null dereference. */
async function boxOf(page: Page, nibId: string) {
  const box = await page.locator(`tr[data-nib-id="${nibId}"]`).boundingBox();
  if (!box) throw new Error(`row ${nibId} is not laid out`);
  return box;
}

async function openMilestones(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem("nibs-filter-preferences", JSON.stringify({ filter: {}, viewLevel: "milestones" }));
  });
  await page.goto("/");
  await expect(page.locator("tr[data-nib-id]").first()).toBeVisible({ timeout: 10_000 });
}

// tnib-t041 and tnib-t042 are the two children of tnib-f020 ("Bulk task
// operations"), so a drag between them stays inside one ordering region and the
// badge has a container with a real title to name.
test("the drag badge names the list the release would reorder within", async ({ page }) => {
  await openMilestones(page);
  await centerOn(page, "tnib-t041");

  const source = await boxOf(page, "tnib-t042");
  const target = await boxOf(page, "tnib-t041");

  await page.mouse.move(source.x + 300, source.y + source.height / 2);
  await page.mouse.down();
  await page.mouse.move(source.x + 314, source.y + source.height / 2, { steps: 5 });
  // The drag is real: without this the badge assertion below could pass on a
  // gesture that never started.
  await expect(page.locator("body")).toHaveCSS("cursor", "grabbing");
  // Top of the target row — the "before" zone, a positioned drop.
  await page.mouse.move(target.x + 300, target.y + 4, { steps: 8 });

  // The TITLE, not the id. `planDrop` names a region by id on its own; the
  // caller supplies the namer, and this is where that shows.
  await expect(page.getByTestId("drag-badge-label")).toHaveText(
    "Reorder in the children of Bulk task operations",
  );
  // The row under the cursor shows where the line lands.
  await expect(page.locator('tr[data-nib-id="tnib-t041"]')).toHaveClass(/drop-before/);

  // Escape rather than a release: this suite shares one fixture copy across a
  // single worker, and an accepted drop would reorder it for every test after.
  await page.keyboard.press("Escape");
  await expect(page.getByTestId("drag-badge")).toHaveCount(0);
  await page.mouse.up();
});

// The badge is `position: fixed` with no ancestor establishing a containing
// block (`#app` carries no CSS at all), so the viewport is its containing block:
// with only `left` set, its available width is what is left to the right of the
// cursor, and a destination name is as long as a container's title. Measured
// here by removing the clamp: the pill reaches 1526px in a 1440px viewport —
// where a fixed box adds no scrollable overflow, so nothing scrolls to reveal
// it. This test does NOT exercise `whitespace-nowrap` or `truncate`: with the
// clamp present the badge is never squeezed, and the 22px-to-38px wrap appears
// only once the clamp is gone as well. Only a real engine lays this out; jsdom
// reports every box as 0x0, so DragBadge.test.ts can check the clamp's
// arithmetic and not its effect.
test("the drag badge stays whole and on screen against the right edge", async ({ page }) => {
  await openMilestones(page);
  await centerOn(page, "tnib-t041");

  const source = await boxOf(page, "tnib-t042");
  const target = await boxOf(page, "tnib-t041");
  const viewport = page.viewportSize();
  if (!viewport) throw new Error("no viewport size");

  await page.mouse.move(source.x + 300, source.y + source.height / 2);
  await page.mouse.down();
  await page.mouse.move(source.x + 314, source.y + source.height / 2, { steps: 5 });
  await expect(page.locator("body")).toHaveCSS("cursor", "grabbing");

  // Mid-row first: the badge's unconstrained size, and proof the long label is
  // the one on screen — the comparison below is meaningless against a short one.
  await page.mouse.move(target.x + 300, target.y + 4, { steps: 8 });
  await expect(page.getByTestId("drag-badge-label")).toHaveText(
    "Reorder in the children of Bulk task operations",
  );
  const badge = page.getByTestId("drag-badge");
  const roomy = await badge.boundingBox();
  if (!roomy) throw new Error("badge is not laid out");

  // Then the same row's far right edge, still inside the row so the target and
  // the label do not change. Auto-scroll reads the vertical edges only, so this
  // move cannot scroll the table out from under the gesture.
  await page.mouse.move(target.x + target.width - 4, target.y + 4, { steps: 8 });
  await expect(page.getByTestId("drag-badge-label")).toHaveText(
    "Reorder in the children of Bulk task operations",
  );
  const edge = await badge.boundingBox();
  if (!edge) throw new Error("badge is not laid out at the edge");

  expect(edge.x).toBeGreaterThanOrEqual(0);
  expect(edge.x + edge.width).toBeLessThanOrEqual(viewport.width);
  // Same height as in open space: one line, not a wrapped column.
  expect(edge.height).toBe(roomy.height);
  expect(edge.width).toBe(roomy.width);

  await page.keyboard.press("Escape");
  await expect(badge).toHaveCount(0);
  await page.mouse.up();
});

// The two refusals the drag path used to conflate under one `valid = false`.
// Both are reachable from the running app, and each names its own cause.
test("a refusal names which of the two causes it was", async ({ page }) => {
  await openMilestones(page);
  await centerOn(page, "tnib-t041");

  // MIXED SOURCE: one row inside f020's children, one at the top level.
  await page.locator('tr[data-nib-id="tnib-t041"] [data-action="title"]').click({ modifiers: ["Control"] });
  await page.locator('tr[data-nib-id="tnib-b012"] [data-action="title"]').click({ modifiers: ["Control"] });

  const mixedSource = await boxOf(page, "tnib-t041");
  const mixedTarget = await boxOf(page, "tnib-b012");
  await page.mouse.move(mixedSource.x + 300, mixedSource.y + mixedSource.height / 2);
  await page.mouse.down();
  await page.mouse.move(mixedSource.x + 314, mixedSource.y + mixedSource.height / 2, { steps: 5 });
  await expect(page.locator("body")).toHaveCSS("cursor", "grabbing");
  await page.mouse.move(mixedTarget.x + 300, mixedTarget.y + mixedTarget.height / 2, { steps: 8 });
  // No destination while the plan is refused — the badge keeps only the count.
  // The positive half first, so the absence below cannot pass on a badge that
  // was never rendered; then a single, non-retrying read for the absence.
  await expect(page.getByTestId("drag-badge-count")).toHaveText("2 items");
  expect(await page.getByTestId("drag-badge-label").count()).toBe(0);
  await page.mouse.up();

  const toast = page.locator("[data-sonner-toast]");
  await expect(toast).toContainText("different ordering groups");
  // Both lists it spans, by title.
  await expect(toast).toContainText("the children of Bulk task operations");
  await expect(toast).toContainText("the top level");
  const mixedMessage = (await toast.textContent()) ?? "";

  // HIDDEN MEMBER: same selection, but f020 is collapsed, so t041 leaves the
  // rendered rows while staying in the selection.
  await page.locator('tr[data-nib-id="tnib-f020"] [data-action="toggle"]').click();
  await expect(page.locator('tr[data-nib-id="tnib-t041"]')).toHaveCount(0);

  const hiddenSource = await boxOf(page, "tnib-b012");
  const hiddenTarget = await boxOf(page, "tnib-b013");
  await page.mouse.move(hiddenSource.x + 300, hiddenSource.y + hiddenSource.height / 2);
  await page.mouse.down();
  await page.mouse.move(hiddenSource.x + 314, hiddenSource.y + hiddenSource.height / 2, { steps: 5 });
  await expect(page.locator("body")).toHaveCSS("cursor", "grabbing");
  await page.mouse.move(hiddenTarget.x + 300, hiddenTarget.y + hiddenTarget.height / 2, { steps: 8 });
  await page.mouse.up();

  // A DIFFERENT sentence, naming the row that is missing and how to get it back
  // — not the mixed-selection one above. Compared as captured text rather than
  // with a retrying `not.toContainText`, which would also be satisfied by the
  // toast simply auto-dismissing.
  await expect(toast).toContainText("tnib-t041 is selected but not shown here");
  const hiddenMessage = (await toast.textContent()) ?? "";
  expect(hiddenMessage).not.toContain("different ordering groups");
  expect(hiddenMessage).not.toBe(mixedMessage);
});

// The band marks where one ordering region's run of rows ends and the next
// begins. Only a real engine resolves `--region-queue`/`--border` and lays out a
// collapsed-border table, so the rule's COMPUTED width and color are checked
// here — which catches both realistic failures, the rule removed and the color
// unresolved, though a computed value is not proof of paint. Which rows get one
// is `regionBandAt`'s own suite.
test("a region boundary is drawn as a rule the neighboring rows do not carry", async ({ page }) => {
  await openMilestones(page);

  const banded = page.locator("tr.region-band");
  await expect(banded.first()).toBeVisible();

  const widths = await page.evaluate(() => {
    const rows = [...document.querySelectorAll("tr[data-nib-id]")];
    const band = rows.find((r) => r.classList.contains("region-band"));
    const plain = rows.find((r) => !r.classList.contains("region-band"));
    if (!band || !plain) return null;
    return {
      band: getComputedStyle(band).borderTopWidth,
      bandColor: getComputedStyle(band).borderTopColor,
      plain: getComputedStyle(plain).borderTopWidth,
    };
  });
  if (widths === null) throw new Error("expected both a banded and an unbanded row");

  expect(widths.plain).toBe("0px");
  expect(parseFloat(widths.band)).toBeGreaterThan(0);
  // A width with a transparent color is the Tailwind-v4 failure this project
  // has been bitten by; assert the rule is actually painted.
  expect(widths.bandColor).not.toBe("rgba(0, 0, 0, 0)");
  expect(widths.bandColor).not.toBe("transparent");
});
