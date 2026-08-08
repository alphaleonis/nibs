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
// trusting a single pass.
test("repeated submenu use in one session keeps working", async ({ page }) => {
  await openApp(page);

  for (let i = 0; i < 20; i++) {
    const row = page.locator("tr[data-nib-id]").nth(i % 5);
    await openContextMenu(page, row);

    const trigger = page.locator('[data-testid="ctx-filter-related-trigger"]');
    await expect(trigger).toBeVisible();
    await trigger.hover();

    const item = page.locator('[data-testid="ctx-filter-ancestorId"]');
    await expect(item, `iteration ${i}: submenu never opened`).toBeVisible({ timeout: 5_000 });
    await item.click();

    await expect(
      page.locator('[data-testid="context-menu"]'),
      `iteration ${i}: menu still open after item click`,
    ).toBeHidden();
    const tokens = await page.locator('[data-testid="filter-token"]').allTextContents();
    const nibId = (await row.getAttribute("data-nib-id"))!;
    expect(tokens.join(" | "), `iteration ${i}: filter not applied`).toContain(
      nibId.replace(/^tnib-/, ""),
    );
  }
});
