import { test, expect, type Page } from "@playwright/test";

// The "open detail on double-click" preference is a real-browser gesture: jsdom's
// dblclick is synthesized by the test library rather than produced by the
// browser's own click/dblclick sequencing, so this is the only place the actual
// gesture is exercised. The open-row marker is checked here too, because its
// second visual channel is a border the pointer must not erase — hover
// specificity and computed style are things jsdom cannot answer.

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

  test("single mode: a single click opens the detail panel", async ({ page }) => {
    const rows = await openAppWithGesture(page, "single");
    const detailPanel = page.locator('[data-testid="active-nib-view"]');
    const firstRow = rows.first();

    await firstRow.locator('[data-action="title"]').click();

    await expect(detailPanel).toBeVisible({ timeout: 5_000 });
    // The open-row marker is gated on "double": in single mode the selection and
    // the panel are always the same row, so today's `.active`-only appearance is
    // unchanged for every profile that never opts in.
    await expect(firstRow).toHaveClass(/active/);
    await expect(firstRow).not.toHaveClass(/opened/);
    await expect(firstRow).not.toHaveAttribute("aria-current", "true");
  });
});
