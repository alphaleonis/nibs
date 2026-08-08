import { test, expect } from "@playwright/test";

// Helper: wait for the tree table to load with data
async function waitForTable(page: import("@playwright/test").Page) {
  await page.goto("/");
  const rows = page.locator("tr[data-nib-id]");
  await expect(rows.first()).toBeVisible({ timeout: 10_000 });
  return rows;
}

test.describe("smoke", () => {
  test("app loads and renders the tree table with nibs", async ({ page }) => {
    const rows = await waitForTable(page);
    const count = await rows.count();
    expect(count).toBeGreaterThan(0);
  });

  test("clicking a row title opens the detail panel", async ({ page }) => {
    const rows = await waitForTable(page);
    const titleBtn = rows.first().locator('[data-action="title"]');
    await titleBtn.click();

    const detailPanel = page.locator('[data-testid="active-nib-view"]');
    await expect(detailPanel).toBeVisible({ timeout: 5_000 });
  });
});

test.describe("multi-select", () => {
  test("Ctrl+click toggles selection on multiple rows", async ({ page }) => {
    const rows = await waitForTable(page);

    // Click first row (non-action area → select)
    const firstRow = rows.first();
    await firstRow.locator('[data-testid="nib-type"]').click();
    await expect(firstRow).toHaveClass(/active/);

    // Ctrl+click second row (→ toggleSelect)
    const secondRow = rows.nth(1);
    await secondRow.locator('[data-testid="nib-type"]').click({ modifiers: ["ControlOrMeta"] });

    // Both should be selected
    await expect(firstRow).toHaveClass(/active/);
    await expect(secondRow).toHaveClass(/active/);
  });

  test("Shift+click range-selects rows", async ({ page }) => {
    const rows = await waitForTable(page);

    // Click first row to anchor
    await rows.first().locator('[data-testid="nib-type"]').click();
    await expect(rows.first()).toHaveClass(/active/);

    // Shift+click third row (→ rangeSelect: rows 0, 1, 2)
    await rows.nth(2).locator('[data-testid="nib-type"]').click({ modifiers: ["Shift"] });

    // All three should be selected
    await expect(rows.first()).toHaveClass(/active/);
    await expect(rows.nth(1)).toHaveClass(/active/);
    await expect(rows.nth(2)).toHaveClass(/active/);
  });
});

test.describe("context menu", () => {
  test("right-click opens context menu with actions", async ({ page }) => {
    const rows = await waitForTable(page);

    // Right-click the first row
    await rows.first().click({ button: "right" });

    // Context menu should appear with standard actions
    const menu = page.locator('[data-testid="context-menu"]');
    await expect(menu).toBeVisible({ timeout: 3_000 });
    await expect(page.locator('[data-testid="ctx-open"]')).toBeVisible();
    await expect(page.locator('[data-testid="ctx-edit"]')).toBeVisible();
    await expect(page.locator('[data-testid="ctx-delete"]')).toBeVisible();
  });

  test("status submenu shows all statuses", async ({ page }) => {
    const rows = await waitForTable(page);

    await rows.first().click({ button: "right" });
    await expect(page.locator('[data-testid="context-menu"]')).toBeVisible({ timeout: 3_000 });

    // Open status submenu
    await page.locator('[data-testid="ctx-status-trigger"]').click();

    // Should show status options
    await expect(page.locator('[data-testid="ctx-status-todo"]')).toBeVisible();
    await expect(page.locator('[data-testid="ctx-status-in-progress"]')).toBeVisible();
    await expect(page.locator('[data-testid="ctx-status-completed"]')).toBeVisible();
  });
});

test.describe("keyboard navigation", () => {
  test("arrow keys move focus between rows", async ({ page }) => {
    const rows = await waitForTable(page);

    // Focus the grid container
    const grid = page.locator('[role="grid"]');
    await grid.focus();

    // ArrowDown should focus the first row
    await page.keyboard.press("ArrowDown");
    await expect(rows.first()).toHaveClass(/focused/);

    // ArrowDown again should focus the second row
    await page.keyboard.press("ArrowDown");
    await expect(rows.nth(1)).toHaveClass(/focused/);
    // First row should lose focus
    await expect(rows.first()).not.toHaveClass(/focused/);
  });

  test("Escape hierarchy: detail panel → deselect → clear focus", async ({ page }) => {
    const rows = await waitForTable(page);

    // Click title to open detail panel
    await rows.first().locator('[data-action="title"]').click();
    const detailPanel = page.locator('[data-testid="active-nib-view"]');
    await expect(detailPanel).toBeVisible({ timeout: 5_000 });

    // Escape closes detail panel
    await page.keyboard.press("Escape");
    await expect(detailPanel).not.toBeVisible();

    // Row should still be selected (active) after panel close
    await expect(rows.first()).toHaveClass(/active/);

    // Escape again deselects
    await page.keyboard.press("Escape");
    await expect(rows.first()).not.toHaveClass(/active/);
  });
});

test.describe("drag-and-drop", () => {
  test("dragging a row initiates drag with visual feedback", async ({ page }) => {
    const rows = await waitForTable(page);

    // Click and drag from anywhere on the row (no handle needed)
    const firstRow = rows.first();
    const rowBox = await firstRow.boundingBox();
    if (!rowBox) throw new Error("Row not visible");

    // Pointer down + move past threshold (5px) to initiate drag
    const startX = rowBox.x + rowBox.width / 2;
    const startY = rowBox.y + rowBox.height / 2;
    await page.mouse.move(startX, startY);
    await page.mouse.down();
    await page.mouse.move(startX, startY + 30, { steps: 5 });

    // The dragged row should have the dragged visual state
    await expect(firstRow).toHaveClass(/dragged/);

    // A ghost preview should be visible
    const preview = page.locator('[data-testid="drag-preview"]');
    await expect(preview).toBeVisible();

    // Clean up: release
    await page.mouse.up();
  });

  test("Escape cancels drag", async ({ page }) => {
    const rows = await waitForTable(page);

    const firstRow = rows.first();
    const rowBox = await firstRow.boundingBox();
    if (!rowBox) throw new Error("Row not visible");

    // Start drag
    const startX = rowBox.x + rowBox.width / 2;
    const startY = rowBox.y + rowBox.height / 2;
    await page.mouse.move(startX, startY);
    await page.mouse.down();
    await page.mouse.move(startX, startY + 30, { steps: 5 });
    await expect(firstRow).toHaveClass(/dragged/);

    // Escape should cancel
    await page.keyboard.press("Escape");
    await expect(firstRow).not.toHaveClass(/dragged/);

    // Preview should be gone
    const preview = page.locator('[data-testid="drag-preview"]');
    await expect(preview).not.toBeVisible();
  });
});
