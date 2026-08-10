import { test, expect, type Page, type Locator } from "@playwright/test";

// The row context menu's submenus, driven the way a user drives them: hover the
// sub-trigger, then click an item. That hover path cannot be tested in jsdom —
// bits-ui keeps a hovered submenu open by measuring pointer coordinates against
// element rects, and jsdom has no layout to measure (see
// src/lib/testing/menu.ts, which opens submenus by keyboard there instead). So
// this is the only place the hover path is covered at all.

async function openApp(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem(
      "nibs-filter-preferences",
      JSON.stringify({ filter: {}, viewLevel: "flat" }),
    );
  });
  await page.goto("/");
  await expect(page.locator("tr[data-nib-id]").first()).toBeVisible({ timeout: 15_000 });
}

async function openContextMenu(page: Page, row: Locator) {
  await row.locator('[data-testid="nib-title"]').click({ button: "right" });
  await expect(page.locator('[data-testid="context-menu"]')).toBeVisible();
}

test("Filter related submenu: hover opens it and clicking an item applies the filter", async ({
  page,
}) => {
  await openApp(page);

  const row = page.locator("tr[data-nib-id]").first();
  const nibId = await row.getAttribute("data-nib-id");
  await openContextMenu(page, row);

  const trigger = page.locator('[data-testid="ctx-filter-related-trigger"]');
  await expect(trigger).toBeVisible();
  await trigger.hover();

  const item = page.locator('[data-testid="ctx-filter-parentId"]');
  await expect(item).toBeVisible({ timeout: 5_000 });
  await item.click();

  // The menu closes and the filter narrows to that row's children.
  await expect(page.locator('[data-testid="context-menu"]')).toBeHidden();
  await expect(page.locator('[data-testid="filter-token"]').first()).toBeVisible({
    timeout: 5_000,
  });
  const tokens = page.locator('[data-testid="filter-token"]');
  const texts = await tokens.allTextContents();
  expect(texts.join(" | ")).toContain(nibId!.replace(/^tnib-/, ""));
});

test("Status submenu: hover opens it and clicking a status applies it", async ({ page }) => {
  await openApp(page);

  const row = page.locator("tr[data-nib-id]").first();
  const nibId = await row.getAttribute("data-nib-id");
  await openContextMenu(page, row);

  const trigger = page.locator('[data-testid="ctx-status-trigger"]');
  await expect(trigger).toBeVisible();
  await trigger.hover();

  const item = page.locator('[data-testid="ctx-status-in-progress"]');
  await expect(item).toBeVisible({ timeout: 5_000 });
  await item.click();

  await expect(page.locator('[data-testid="context-menu"]')).toBeHidden();
  await expect(
    page.locator(`tr[data-nib-id="${nibId}"] [data-testid="nib-status"]`),
  ).toHaveText(/in-progress/, { timeout: 10_000 });
});

test("Priority submenu: hover opens it and clicking a priority applies it", async ({ page }) => {
  await openApp(page);

  const row = page.locator("tr[data-nib-id]").first();
  await openContextMenu(page, row);

  const trigger = page.locator('[data-testid="ctx-priority-trigger"]');
  await expect(trigger).toBeVisible();
  await trigger.hover();

  const item = page.locator('[data-testid="ctx-priority-high"]');
  await expect(item).toBeVisible({ timeout: 5_000 });
  await item.click();

  await expect(page.locator('[data-testid="context-menu"]')).toBeHidden();
  // The priority indicator appears on the row's title cell.
  await expect(
    page.locator("tr[data-nib-id]").first().locator('[data-testid="priority-icon"]'),
  ).toBeVisible({ timeout: 10_000 });
});

// Menus are opened and thrown away over and over in a real session, and the
// jsdom breakage only showed up after a handful of them had come and gone in one
// document — so repeat the whole open -> hover -> click cycle rather than
// trusting a single pass. What is under test is the SUBMENU LIFECYCLE, not any
// particular menu item, so the item is chosen for leaving the row set alone.
//
// It must not be a "Filter related" item (nibs-f980). Applying a relationship
// filter re-renders the table to the matching rows — `ancestor:<a task>` matches
// nothing, taking 89 rows to 0 — which destroys the very list the loop indexes
// with .nth(). The old version survived only by OUTRUNNING the debounced
// re-render on all 20 iterations: it read the table before the filter landed, so
// the row count never appeared to change. Losing that race even once left
// .nth(1) resolving against an empty table, and since a Playwright action has no
// timeout of its own it blocked until the 30 s test timeout — a bimodal 4.1 s or
// 30 s, roughly 1 run in 15. Setting a priority mutates one row's data and
// nothing about which rows are listed, so every iteration is deterministic;
// `order identical: true` across 20 iterations was measured before choosing it.
// The Filter-related submenu keeps its own single-pass coverage above.
test("repeated submenu use in one session keeps working", async ({ page }) => {
  await openApp(page);
  const rows = page.locator("tr[data-nib-id]");

  for (let i = 0; i < 20; i++) {
    const row = rows.nth(i % 5);
    // Read the id BEFORE acting, the way the single-pass tests above do. A
    // locator is lazy, so re-resolving .nth() after the click would report
    // whatever occupies that index afterwards rather than the row acted on.
    const nibId = (await row.getAttribute("data-nib-id"))!;
    await openContextMenu(page, row);

    const trigger = page.locator('[data-testid="ctx-priority-trigger"]');
    await expect(trigger).toBeVisible();
    await trigger.hover();

    const item = page.locator('[data-testid="ctx-priority-high"]');
    await expect(item, `iteration ${i}: submenu never opened`).toBeVisible({ timeout: 5_000 });
    await item.click();

    await expect(
      page.locator('[data-testid="context-menu"]'),
      `iteration ${i}: menu still open after item click`,
    ).toBeHidden();

    // Address the row by id rather than by index: an assertion on .nth(i % 5)
    // would still pass if the table reordered under it, by checking a row the
    // iteration never touched.
    await expect(
      page.locator(`tr[data-nib-id="${nibId}"] [data-testid="priority-icon"]`),
      `iteration ${i}: priority not applied to ${nibId}`,
    ).toBeVisible({ timeout: 10_000 });
  }
});
