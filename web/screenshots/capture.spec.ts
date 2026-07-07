import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { VIEW_LEVELS } from "../src/lib/types";

// Captures PNGs of the key web UI states into web/screenshots/output/ so an
// agent (or human) can visually verify UI changes. Run via `task screenshots`.
//
// When the theme engine lands (nibs-vmaq), extend this to loop the captures
// once per theme. When the board view lands (nibs-sg09), add a capture for it.

const OUT = join(import.meta.dirname, "output");
mkdirSync(OUT, { recursive: true });

async function openApp(page: Page, viewLevel: (typeof VIEW_LEVELS)[number] = "milestones") {
  await page.addInitScript(level => {
    localStorage.setItem(
      "nibs-filter-preferences",
      JSON.stringify({ filter: {}, viewLevel: level }),
    );
  }, viewLevel);
  await page.goto("/");
  await expect(page.locator("tr[data-nib-id]").first()).toBeVisible({ timeout: 10_000 });
}

function shot(page: Page, name: string) {
  return page.screenshot({ path: join(OUT, `${name}.png`), animations: "disabled" });
}

for (const level of VIEW_LEVELS) {
  test(`table — ${level} view level`, async ({ page }) => {
    await openApp(page, level);
    await shot(page, `table-${level}`);
  });
}

test("detail panel", async ({ page }) => {
  await openApp(page);
  await page.locator("tr[data-nib-id]").first().locator('[data-action="title"]').click();
  await expect(page.locator('[data-testid="detail-panel"]')).toBeVisible({ timeout: 5_000 });
  await expect(page.locator('[data-testid="detail-loading"]')).toBeHidden({ timeout: 5_000 });
  await shot(page, "detail-panel");
});

test("editor modal", async ({ page }) => {
  await openApp(page);
  await page.locator("tr[data-nib-id]").first().click({ button: "right" });
  await expect(page.locator('[data-testid="context-menu"]')).toBeVisible({ timeout: 3_000 });
  await page.locator('[data-testid="ctx-edit"]').click();
  await expect(page.locator('[data-testid="editor-modal"]')).toBeVisible({ timeout: 5_000 });
  // Wait for the CodeMirror editor to mount so the body area isn't blank.
  await expect(page.locator(".cm-content").first()).toBeVisible({ timeout: 5_000 });
  await shot(page, "editor-modal");
});

test("context menu", async ({ page }) => {
  await openApp(page);
  await page.locator("tr[data-nib-id]").first().click({ button: "right" });
  await expect(page.locator('[data-testid="context-menu"]')).toBeVisible({ timeout: 3_000 });
  await shot(page, "context-menu");
});
