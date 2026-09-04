import { test, expect, type Page } from "@playwright/test";

// What a drag says about itself while it is in flight, and what a refused one
// says on release. jsdom can assert the classes and drive the pointer path with
// a stubbed `document.elementFromPoint` (useTreeDrag.test.ts's `hoverRow` does
// exactly that), but it has no layout and no computed colors — the line, the
// band and the badge's legibility over the ghost are pixels, so they are checked
// here.
//
// Both ordering axes are here, in the one view that carries both: the Milestones
// view's membership lens declares a MILESTONE region on each milestone section,
// so a milestone's members take the queue axis while the rows inside an expanded
// epic take the parent axis. The component-level suites (TreeTableRow.test.ts,
// DragBadge.test.ts, regionBand.test.ts) decide WHICH rows get which affordance
// against regions built by hand; what is checked here is what those affordances
// look like once an engine has laid them out.
//
// The cross-queue refusal and the assignment it offers instead are in
// drop-refusal-toast.test.ts, with the rest of the refusal path.

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

/**
 * Collapses `nibId`, waiting for a row that was inside it to leave the table.
 *
 * The queue gestures below need two members of one milestone queue within a
 * screen of each other, and in the fully expanded default view a whole epic's
 * subtree sits between them.
 */
async function collapse(page: Page, nibId: string, innerRow: string) {
  await page.locator(`tr[data-nib-id="${nibId}"] [data-action="toggle"]`).click();
  await expect(page.locator(`tr[data-nib-id="${innerRow}"]`)).toHaveCount(0);
}

/**
 * A design token as the engine resolves it, in the same serialization the
 * computed `box-shadow` and `border-top-color` below are read in — so an
 * assertion names the token it means rather than a literal color a theme retune
 * would invalidate.
 */
// Begin a drag from `nibId` and leave it held, which is the only state the
// ordering bands are drawn in (nibs-ke8o). Returns the release.
//
// The pointer is moved a few pixels within the source row: enough to pass the
// drag threshold, not far enough to aim at anything, so a band read afterwards
// is the row's own and carries no drop treatment.
async function holdDrag(page: Page, nibId: string): Promise<() => Promise<void>> {
  await centerOn(page, nibId);
  const source = await boxOf(page, nibId);
  await page.mouse.move(source.x + 300, source.y + source.height / 2);
  await page.mouse.down();
  await page.mouse.move(source.x + 314, source.y + source.height / 2, { steps: 5 });
  await expect(page.locator("body")).toHaveCSS("cursor", "grabbing");
  return async () => {
    await page.keyboard.press("Escape");
    await page.mouse.up();
  };
}

async function resolvedColor(page: Page, token: string): Promise<string> {
  return page.evaluate((name) => {
    const probe = document.createElement("span");
    probe.style.cssText = `position:fixed;left:-9999px;color:var(${name});`;
    document.body.appendChild(probe);
    const color = getComputedStyle(probe).color;
    probe.remove();
    return color;
  }, token);
}

/** The color actually painted at one viewport point, read back through a canvas. */
async function pixelAt(page: Page, x: number, y: number): Promise<string> {
  const png = (await page.screenshot()).toString("base64");
  return page.evaluate(
    async ({ b64, px, py }) => {
      const img = new Image();
      img.src = "data:image/png;base64," + b64;
      await img.decode();
      const canvas = document.createElement("canvas");
      canvas.width = img.width;
      canvas.height = img.height;
      const ctx = canvas.getContext("2d");
      if (ctx === null) throw new Error("no 2d context");
      ctx.drawImage(img, 0, 0);
      const [r, g, b] = ctx.getImageData(Math.round(px), Math.round(py), 1, 1).data;
      return `rgb(${r},${g},${b})`;
    },
    { b64: png, px: x, py: y },
  );
}

/** Hides one element for a measurement, and puts it back. */
async function setHidden(page: Page, testId: string, hidden: boolean) {
  await page.evaluate(
    ({ id, on }) => {
      const el = document.querySelector(`[data-testid="${id}"]`);
      if (!(el instanceof HTMLElement)) throw new Error(`no ${id}`);
      el.style.visibility = on ? "hidden" : "";
    },
    { id: testId, on: hidden },
  );
}

/** The rendered row ids, in table order. */
async function rowIds(page: Page): Promise<(string | undefined)[]> {
  return page.evaluate(() =>
    [...document.querySelectorAll("tr[data-nib-id]")].map((tr) => (tr as HTMLElement).dataset.nibId),
  );
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

  // Held, because the bands are a drag affordance and are drawn only during one.
  const release = await holdDrag(page, "tnib-f008");

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
  await release();
});

// The same seam on the MILESTONE axis, which the parent-axis test above cannot
// reach: a queue's boundary is drawn in the queue's own token and a heavier
// rule, so the row where one milestone's queue gives way is findable from across
// the table. Both a queue band and an ordinary one are among this view's
// rows, so the comparison is between two rows of one render.
test("a queue boundary is banded in the queue's own weight and color", async ({ page }) => {
  await openMilestones(page);

  const queueColor = await resolvedColor(page, "--region-queue");
  const plainColor = await resolvedColor(page, "--border");
  expect(queueColor).not.toBe(plainColor);

  // Held, because the bands are a drag affordance and are drawn only during one.
  const release = await holdDrag(page, "tnib-f008");

  const bands = await page.evaluate(() => {
    const read = (selector: string) => {
      const el = document.querySelector(selector);
      if (el === null) return null;
      const cs = getComputedStyle(el);
      return { width: parseFloat(cs.borderTopWidth), color: cs.borderTopColor };
    };
    return {
      queue: read("tr.region-band.region-band-queue"),
      plain: read("tr.region-band:not(.region-band-queue)"),
    };
  });
  if (bands.queue === null || bands.plain === null) {
    throw new Error("expected both a queue band and an ordinary band among the rows");
  }

  expect(bands.queue.color).toBe(queueColor);
  expect(bands.plain.color).toBe(plainColor);
  expect(bands.queue.width).toBeGreaterThan(bands.plain.width);
  await release();
});

// The two ordering semantics, one after the other in ONE view, so the claim that
// they read differently is a comparison rather than two separate readings. Both
// gestures are escaped, so neither writes.
test("the queue axis and the parent axis draw their lines in different colors", async ({ page }) => {
  await openMilestones(page);

  const queueColor = await resolvedColor(page, "--region-queue");
  const parentColor = await resolvedColor(page, "--ring");
  expect(queueColor).not.toBe(parentColor);

  // PARENT AXIS: two of tnib-e001's six structural children, inside the
  // expanded epic. The lens declares no region for them, so each falls back to
  // its own resolved parent.
  await centerOn(page, "tnib-b002");
  const parentSource = await boxOf(page, "tnib-b001");
  const parentTarget = await boxOf(page, "tnib-b002");
  await page.mouse.move(parentSource.x + 300, parentSource.y + parentSource.height / 2);
  await page.mouse.down();
  await page.mouse.move(parentSource.x + 314, parentSource.y + parentSource.height / 2, { steps: 5 });
  await expect(page.locator("body")).toHaveCSS("cursor", "grabbing");
  await page.mouse.move(parentTarget.x + 300, parentTarget.y + 4, { steps: 8 });

  const parentRow = page.locator('tr[data-nib-id="tnib-b002"]');
  await expect(page.getByTestId("drag-badge-label")).toHaveText("Reorder in the children of User Authentication");
  await expect(parentRow).toHaveClass(/drop-before/);
  await expect(parentRow).not.toHaveClass(/drop-queue/);
  const parentShadow = await parentRow.evaluate((el) => getComputedStyle(el).boxShadow);
  await page.keyboard.press("Escape");
  await page.mouse.up();

  // QUEUE AXIS: two members of v1.0's queue. Collapsing the first makes the two
  // rows adjacent, where the fully expanded view has a whole epic between them.
  await collapse(page, "tnib-e002", "tnib-f004");
  await centerOn(page, "tnib-e002");
  const queueSource = await boxOf(page, "tnib-e003");
  const queueTarget = await boxOf(page, "tnib-e002");
  await page.mouse.move(queueSource.x + 300, queueSource.y + queueSource.height / 2);
  await page.mouse.down();
  await page.mouse.move(queueSource.x + 314, queueSource.y + queueSource.height / 2, { steps: 5 });
  await expect(page.locator("body")).toHaveCSS("cursor", "grabbing");
  await page.mouse.move(queueTarget.x + 300, queueTarget.y + 4, { steps: 8 });

  const queueRow = page.locator('tr[data-nib-id="tnib-e002"]');
  await expect(page.getByTestId("drag-badge-label")).toHaveText("Reorder in the v1.0 MVP Launch queue");
  await expect(queueRow).toHaveClass(/drop-queue/);
  const queueShadow = await queueRow.evaluate((el) => getComputedStyle(el).boxShadow);
  await page.keyboard.press("Escape");
  await page.mouse.up();

  // One line each, 2px on the row's top edge — same shape, different token.
  expect(parentShadow).toBe(`${parentColor} 0px 2px 0px 0px inset`);
  expect(queueShadow).toBe(`${queueColor} 0px 2px 0px 0px inset`);
});

// The badge and the drag ghost occupy the same pixels — the ghost is a full
// table row hanging off the cursor, the badge sits beside it — so the sentence
// naming the destination is readable only if it paints over the ghost rather
// than through it. Both are `position: fixed` overlays whose layers are `var()`
// custom properties, and only an engine resolves those into a stacking order.
test("the queue badge is drawn over the drag ghost, in the queue's border", async ({ page }) => {
  await openMilestones(page);
  const queueColor = await resolvedColor(page, "--region-queue");
  await collapse(page, "tnib-e002", "tnib-f004");
  await centerOn(page, "tnib-e002");

  const source = await boxOf(page, "tnib-e003");
  const target = await boxOf(page, "tnib-e002");
  // Grabbed at the row's middle, so the ghost hangs symmetrically about the
  // cursor and spans the band of pixels the badge is placed in.
  await page.mouse.move(source.x + 300, source.y + source.height / 2);
  await page.mouse.down();
  await page.mouse.move(source.x + 314, source.y + source.height / 2, { steps: 5 });
  await expect(page.locator("body")).toHaveCSS("cursor", "grabbing");
  await page.mouse.move(target.x + 300, target.y + 4, { steps: 8 });

  const badge = page.getByTestId("drag-badge");
  await expect(badge).toHaveCSS("border-top-color", queueColor);

  const badgeBox = await badge.boundingBox();
  const ghostBox = await page.getByTestId("drag-preview").boundingBox();
  if (!badgeBox || !ghostBox) throw new Error("badge and ghost must both be laid out");

  // Inside the badge's right padding: clear of the label, off the 1px border,
  // and — asserted, not assumed — over the ghost.
  const x = badgeBox.x + badgeBox.width - 4;
  const y = badgeBox.y + badgeBox.height / 2;
  expect(x).toBeGreaterThan(ghostBox.x);
  expect(x).toBeLessThan(ghostBox.x + ghostBox.width);
  expect(y).toBeGreaterThan(ghostBox.y);
  expect(y).toBeLessThan(ghostBox.y + ghostBox.height);

  const overBoth = await pixelAt(page, x, y);
  await setHidden(page, "drag-preview", true);
  const badgeAlone = await pixelAt(page, x, y);
  await setHidden(page, "drag-preview", false);
  await setHidden(page, "drag-badge", true);
  const ghostAlone = await pixelAt(page, x, y);
  await setHidden(page, "drag-badge", false);

  // The ghost tints nothing the badge covers — the badge's fill is opaque and
  // above it.
  expect(overBoth).toBe(badgeAlone);
  // And the ghost really is under that point, so the line above cannot pass on
  // a badge that happens to sit over bare background.
  expect(ghostAlone).not.toBe(badgeAlone);

  await page.keyboard.press("Escape");
  await page.mouse.up();
});

// The only gesture in this file whose release WRITES, and therefore the last: it
// moves tnib-e003 ahead of tnib-e002 in v1.0's queue, and the suite shares one
// fixture copy across every file. No other test reads those two against each
// other — the cross-queue test in drop-refusal-toast.test.ts positions against
// tnib-e001 and tnib-e002, whose order this leaves untouched — so it does not
// matter which file runs first.
test("a queue reorder lands the row where its line pointed", async ({ page }) => {
  await openMilestones(page);
  await collapse(page, "tnib-e002", "tnib-f004");
  await centerOn(page, "tnib-e002");

  // The move is a move only from this starting order.
  const before = await rowIds(page);
  expect(before.indexOf("tnib-e003")).toBeGreaterThan(before.indexOf("tnib-e002"));

  const source = await boxOf(page, "tnib-e003");
  const target = await boxOf(page, "tnib-e002");
  await page.mouse.move(source.x + 300, source.y + source.height / 2);
  await page.mouse.down();
  await page.mouse.move(source.x + 314, source.y + source.height / 2, { steps: 5 });
  await expect(page.locator("body")).toHaveCSS("cursor", "grabbing");
  await page.mouse.move(target.x + 300, target.y + 4, { steps: 8 });

  // Where the line points, before the release carries it out.
  await expect(page.getByTestId("drag-badge-label")).toHaveText("Reorder in the v1.0 MVP Launch queue");
  await expect(page.locator('tr[data-nib-id="tnib-e002"]')).toHaveClass(/drop-before/);
  await expect(page.locator('tr[data-nib-id="tnib-e002"]')).toHaveClass(/drop-queue/);

  await page.mouse.up();

  // Polled: the release starts a mutation and a refetch. The slot, not just the
  // side — between the queue member above the anchor and the anchor itself.
  await expect
    .poll(
      async () => {
        const ids = await rowIds(page);
        const at = (id: string) => ids.indexOf(id);
        return {
          present: at("tnib-e002") !== -1 && at("tnib-e003") !== -1,
          afterE001: at("tnib-e003") > at("tnib-e001"),
          beforeE002: at("tnib-e003") < at("tnib-e002"),
          stillInV1: at("tnib-e003") > at("tnib-m001") && at("tnib-e003") < at("tnib-m002"),
        };
      },
      { timeout: 10_000 },
    )
    .toEqual({ present: true, afterE001: true, beforeE002: true, stillInV1: true });
});

// nibs-v39j: the queue band and the queue drop line paint the same edge of the
// same row, and both used `--region-queue` — so aiming a drop at a banded row
// took the seam from 2px of cyan to 3px of cyan and changed nothing else.
// Measured, not argued: the two do not stack end to end, they overlap by 1px.
//
// The rule chosen is the composition the PARENT axis already uses and which the
// same measurement found legible there — 1px `--border` band beneath a 2px
// coloured indicator. On the queue axis the band yields its colour for as long
// as the drop is aimed at it, so the change is one of SHAPE (one cyan rule
// becomes a grey hairline under a cyan rule) rather than of thickness.
//
// Only a real engine can close this: the band is a `border-block-start` and the
// indicator an `inset box-shadow`, and whether they compose or overlap is a
// question about painting that jsdom does not answer.
test("a queue band yields its color to the drop line aimed at it", async ({ page }) => {
  await openMilestones(page);

  const queueColor = await resolvedColor(page, "--region-queue");
  const borderColor = await resolvedColor(page, "--border");
  expect(queueColor).not.toBe(borderColor);

  const row = page.locator('tr[data-nib-id="tnib-e002"]');
  await collapse(page, "tnib-e002", "tnib-f004");
  await centerOn(page, "tnib-e002");

  // Both readings happen inside ONE held drag, because that is the only state
  // either mark exists in (nibs-ke8o). The comparison is therefore banded-and-
  // unaimed against banded-and-aimed, which is the pair this rule is about.
  const release = await holdDrag(page, "tnib-e003");
  const target = await boxOf(page, "tnib-e002");

  // The premise: this row really does carry a queue band while nothing is aimed
  // at it. Without this the test could pass on a row that never had one.
  await expect(row).toHaveClass(/region-band-queue/);
  await expect(row).not.toHaveClass(/drop-before/);
  const unaimed = await row.evaluate((el) => {
    const cs = getComputedStyle(el);
    return { width: cs.borderTopWidth, color: cs.borderTopColor };
  });
  expect(unaimed).toEqual({ width: "2px", color: queueColor });

  await page.mouse.move(target.x + 300, target.y + 4, { steps: 8 });

  await expect(row).toHaveClass(/drop-before/);
  await expect(row).toHaveClass(/drop-queue/);
  const aimed = await row.evaluate((el) => {
    const cs = getComputedStyle(el);
    return { width: cs.borderTopWidth, color: cs.borderTopColor, shadow: cs.boxShadow };
  });

  await release();

  // The band steps back to the neutral hairline the parent axis uses...
  expect(aimed.width).toBe("1px");
  expect(aimed.color).toBe(borderColor);
  // ...while the indicator keeps the queue's own colour, so the edge carries two
  // tokens rather than one drawn heavier.
  expect(aimed.shadow).toBe(`${queueColor} 0px 2px 0px 0px inset`);

  // The band yields only while aimed at. After the release the drag is over, so
  // the band is gone with it — what must not survive is the drop treatment.
  await expect(row).not.toHaveClass(/drop-before/);
  await expect(row).not.toHaveClass(/region-band/);
});
