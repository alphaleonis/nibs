import { test, expect, type Page } from "@playwright/test";

// The "open detail on double-click" preference is a real-browser gesture: jsdom's
// dblclick is synthesized by the test library rather than produced by the
// browser's own click/dblclick sequencing, so this is the only place the actual
// gesture is exercised. The open-row marker is checked here too, because it is a
// border the pointer must not erase and the fill beside it belongs to a different
// state — hover specificity and computed style are things jsdom cannot answer.

// Seed the preference before navigation. The localStorage key and blob shape
// mirror savePreferences (which persists the query under `q` and the rest of the
// preferences structured); loadPreferences validates each field, so an unknown
// `openDetailOn` value would be silently dropped back to the default.
async function openAppWithGesture(page: Page, openDetailOn: "single" | "double") {
  await page.addInitScript((gesture) => {
    localStorage.setItem(
      "nibs-filter-preferences",
      JSON.stringify({ q: "", viewLevel: "flat", openDetailOn: gesture }),
    );
  }, openDetailOn);
  await page.goto("/");
  const rows = page.locator("tr[data-nib-id]");
  await expect(rows.first()).toBeVisible({ timeout: 15_000 });
  return rows;
}

test.describe("open detail gesture", () => {
  test("double mode: a single click selects the row without opening the detail panel", async ({ page }) => {
    const rows = await openAppWithGesture(page, "double");
    const detailPanel = page.locator('[data-testid="active-nib-view"]');

    const firstRow = rows.first();
    await firstRow.locator('[data-action="title"]').click();

    // The row is selected...
    await expect(firstRow).toHaveClass(/active/);
    // ...and is NOT marked as the open row — the sharpest statement of the
    // contract, since `.active` alone would also hold if the panel had opened.
    await expect(firstRow).not.toHaveClass(/opened/);
    // ...and the panel stays closed. Give the app a beat so a would-be open has
    // time to happen before asserting it did not.
    await expect(detailPanel).toBeHidden();
    await page.waitForTimeout(500);
    await expect(detailPanel).toBeHidden();
  });

  test("double mode: only the double-click opens the detail panel", async ({ page }) => {
    const rows = await openAppWithGesture(page, "double");
    const detailPanel = page.locator('[data-testid="active-nib-view"]');
    const firstRow = rows.first();

    // The single click first: without it this case passes identically in single
    // mode and so cannot detect the gate being removed.
    await firstRow.locator('[data-action="title"]').click();
    await expect(detailPanel).toBeHidden();

    await firstRow.locator('[data-action="title"]').dblclick();

    await expect(detailPanel).toBeVisible({ timeout: 5_000 });
    await expect(firstRow).toHaveClass(/opened/);
    await expect(firstRow).toHaveAttribute("aria-current", "true");
  });

  test("double mode: the open row's leading accent survives hover", async ({ page }) => {
    const rows = await openAppWithGesture(page, "double");
    const firstRow = rows.first();
    const secondRow = rows.nth(1);

    await firstRow.locator('[data-action="title"]').dblclick();
    await expect(page.locator('[data-testid="active-nib-view"]')).toBeVisible({ timeout: 5_000 });

    const accent = () =>
      firstRow.evaluate((el) => {
        const s = getComputedStyle(el);
        return { width: s.borderInlineStartWidth, color: s.borderInlineStartColor };
      });

    // Move the pointer OFF the open row: hover repaints the row background, so
    // the fill alone stops distinguishing open from selected while the pointer
    // sits there — the border is the channel that has to survive it.
    await secondRow.hover();
    const unhovered = await accent();
    expect(unhovered.width).not.toBe("0px");
    expect(unhovered.color).not.toBe("rgba(0, 0, 0, 0)");

    await firstRow.hover();
    expect(await accent()).toEqual(unhovered);
  });

  test("the accent gutter keeps header and body columns aligned", async ({ page }) => {
    // The gutter is reserved on every body row (transparent until the row is
    // open) so switching a row to "open" cannot shift its first column. Header
    // rows are not `.tree-row`, so an unreserved or mismatched gutter would show
    // up as headers sitting off their columns — invisible to jsdom, which does no
    // layout at all.
    await openAppWithGesture(page, "single");

    const offsets = await page.evaluate(() => {
      const table = document.querySelector("table")!;
      const left = (el: Element) => Math.round(el.getBoundingClientRect().left * 100) / 100;
      return {
        headers: Array.from(table.querySelectorAll("thead th"), left),
        cells: Array.from(table.querySelectorAll("tbody tr:first-child td"), left),
      };
    });

    expect(offsets.headers.length).toBeGreaterThan(1);
    expect(offsets.cells).toEqual(offsets.headers);
  });

  test("double mode: the open row loses its fill when the selection moves to another row", async ({ page }) => {
    // The two-gesture divergence this marker split exists for: after opening A and
    // then plain-clicking B, a delete consumes B alone. B must carry the fill and A
    // must not — a judgement about painted background that only a real browser can
    // make, since the two rules are resolved by the cascade.
    const rows = await openAppWithGesture(page, "double");
    const opened = rows.first();
    const selected = rows.nth(1);

    await opened.locator('[data-action="title"]').dblclick();
    await expect(page.locator('[data-testid="active-nib-view"]')).toBeVisible({ timeout: 5_000 });
    await selected.locator('[data-action="title"]').click();

    await expect(opened).toHaveClass(/opened/);
    await expect(opened).not.toHaveClass(/active/);
    await expect(opened).toHaveAttribute("aria-selected", "false");
    await expect(selected).toHaveClass(/active/);
    await expect(selected).not.toHaveClass(/opened/);
    await expect(selected).toHaveAttribute("aria-selected", "true");

    // Park the pointer off every row: hover repaints the background and would make
    // whichever row it sits on look filled.
    await page.mouse.move(0, 0);
    const paint = (row: typeof opened) =>
      row.evaluate((el) => {
        const s = getComputedStyle(el);
        return { background: s.backgroundColor, accent: s.borderInlineStartColor };
      });

    const openedPaint = await paint(opened);
    const selectedPaint = await paint(selected);
    const plainPaint = await paint(rows.nth(4));

    // Fill is the action set's channel and nothing else's: the open row must paint
    // EXACTLY like an untouched row, not merely differently from the selected one.
    // A weaker "the two differ" check passes just as happily on a `.opened` rule
    // that keeps a deeper fill of its own — which is the thing being removed.
    expect(openedPaint.background).toBe(plainPaint.background);
    expect(selectedPaint.background).not.toBe(plainPaint.background);
    // The accent is the open row's alone, and no other row reserves a colored one.
    expect(openedPaint.accent).not.toBe(plainPaint.accent);
    expect(selectedPaint.accent).toBe(plainPaint.accent);
  });

  test("single mode: a single click opens the detail panel", async ({ page }) => {
    const rows = await openAppWithGesture(page, "single");
    const detailPanel = page.locator('[data-testid="active-nib-view"]');
    const firstRow = rows.first();

    await firstRow.locator('[data-action="title"]').click();

    await expect(detailPanel).toBeVisible({ timeout: 5_000 });
    // In single mode a click writes both sets, so the row is at once the action
    // set and the panel's target and carries both channels.
    await expect(firstRow).toHaveClass(/active/);
    await expect(firstRow).toHaveClass(/opened/);
    await expect(firstRow).toHaveAttribute("aria-selected", "true");
    await expect(firstRow).toHaveAttribute("aria-current", "true");
  });
});
