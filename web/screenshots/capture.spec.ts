import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { VIEW_LEVELS, THEMES } from "../src/lib/types";
import type { Theme } from "../src/lib/types";

// Captures PNGs of the key web UI states into web/screenshots/output/ so an
// agent (or human) can visually verify UI changes. Run via `task screenshots`.
//
// The theme engine (nibs-vmaq, nibs-fen5) is exercised below: each palette gets a
// table + detail + settings capture so the palettes can be compared side by side.
// The captures drive the REAL app (page.goto loads index.html), so the light/dark
// `.dark` class is toggled by the FOUC guard + App's $effect exactly as at runtime
// — the light Daylight palette (nibs-fen5) therefore renders with `.dark` cleared.
// When the board view lands (nibs-sg09), add a capture for it.

const OUT = join(import.meta.dirname, "output");
mkdirSync(OUT, { recursive: true });

async function openApp(
  page: Page,
  viewLevel: (typeof VIEW_LEVELS)[number] = "milestones",
  theme?: Theme,
) {
  await page.addInitScript(
    ({ level, t }) => {
      localStorage.setItem(
        "nibs-filter-preferences",
        JSON.stringify({ filter: {}, viewLevel: level, ...(t ? { theme: t } : {}) }),
      );
    },
    { level: viewLevel, t: theme },
  );
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

// Per-theme captures (nibs-vmaq, nibs-fen5): table + detail panel under each
// palette so an agent can confirm Graphite reads as a softer/warmer dark, Dracula
// is clearly purple-tinted, Daylight renders as a warm LIGHT theme (shadcn inputs/
// borders light, not dark), and pills/indicators/body text stay readable in all.
for (const { value } of THEMES) {
  test(`theme ${value} — table`, async ({ page }) => {
    await openApp(page, "milestones", value);
    await shot(page, `theme-${value}-table`);
  });

  test(`theme ${value} — detail panel`, async ({ page }) => {
    await openApp(page, "milestones", value);
    await page.locator("tr[data-nib-id]").first().locator('[data-action="title"]').click();
    await expect(page.locator('[data-testid="detail-panel"]')).toBeVisible({ timeout: 5_000 });
    await expect(page.locator('[data-testid="detail-loading"]')).toBeHidden({ timeout: 5_000 });
    await shot(page, `theme-${value}-detail`);
  });
}

// Settings sheet open with the Theme dropdown visible, per palette.
for (const { value } of THEMES) {
  test(`theme ${value} — settings sheet`, async ({ page }) => {
    await openApp(page, "milestones", value);
    await page.getByTitle("Settings").click();
    await expect(page.getByTestId("theme-select")).toBeVisible({ timeout: 3_000 });
    await shot(page, `theme-${value}-settings`);
  });
}
