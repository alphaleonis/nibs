import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { VIEW_LEVELS, THEMES } from "../src/lib/types";
import type { Theme, DetailPanelPosition, FontSize } from "../src/lib/types";

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
  position?: DetailPanelPosition,
  fontSize?: FontSize,
) {
  await page.addInitScript(
    ({ level, t, pos, fs }) => {
      localStorage.setItem(
        "nibs-filter-preferences",
        JSON.stringify({
          filter: {},
          viewLevel: level,
          ...(t ? { theme: t } : {}),
          ...(pos ? { detailPanelPosition: pos } : {}),
          ...(fs ? { fontSize: fs } : {}),
        }),
      );
    },
    { level: viewLevel, t: theme, pos: position, fs: fontSize },
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
  await expect(page.locator('[data-testid="active-nib-view"]')).toBeVisible({ timeout: 5_000 });
  await expect(page.locator('[data-testid="anv-title"]')).toBeVisible({ timeout: 5_000 });
  await shot(page, "detail-panel");
});

test("active view — editing", async ({ page }) => {
  // The unified view with the body editor toggled on (CodeMirror + preview) —
  // the buffered edit experience that replaced the standalone editor modal.
  await openApp(page);
  await page.locator("tr[data-nib-id]").first().locator('[data-action="title"]').click();
  await expect(page.locator('[data-testid="active-nib-view"]')).toBeVisible({ timeout: 5_000 });
  await page.locator('[data-testid="anv-edit-toggle"]').click();
  // Wait for the CodeMirror editor to mount so the body area isn't blank.
  await expect(page.locator(".cm-content").first()).toBeVisible({ timeout: 5_000 });
  await shot(page, "active-view-editing");
});

test("active view — expanded modal", async ({ page }) => {
  // The same view promoted to the full-screen modal presentation (wide, two-col).
  await openApp(page);
  await page.locator("tr[data-nib-id]").first().locator('[data-action="title"]').click();
  await expect(page.locator('[data-testid="active-nib-view"]')).toBeVisible({ timeout: 5_000 });
  await page.locator('[data-testid="anv-expand"]').click();
  await expect(page.locator('[data-testid="active-nib-modal"]')).toBeVisible({ timeout: 5_000 });
  await shot(page, "active-view-expanded");
});

test("detail panel — bottom dock", async ({ page }) => {
  // Detail view docked at the bottom (table on top, preview below) — nibs-x9xl.
  await openApp(page, "milestones", undefined, "bottom");
  await page.locator("tr[data-nib-id]").first().locator('[data-action="title"]').click();
  await expect(page.locator('[data-testid="active-nib-view"]')).toBeVisible({ timeout: 5_000 });
  await expect(page.locator('[data-testid="anv-title"]')).toBeVisible({ timeout: 5_000 });
  await shot(page, "detail-panel-bottom");
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
    await expect(page.locator('[data-testid="active-nib-view"]')).toBeVisible({ timeout: 5_000 });
    await expect(page.locator('[data-testid="anv-title"]')).toBeVisible({ timeout: 5_000 });
    await shot(page, `theme-${value}-detail`);
  });
}

// Settings sheet open with the Theme dropdown visible, per palette.
for (const { value } of THEMES) {
  test(`theme ${value} — settings sheet`, async ({ page }) => {
    await openApp(page, "milestones", value);
    await page.getByRole("button", { name: "Settings" }).click();
    await expect(page.getByTestId("theme-select")).toBeVisible({ timeout: 3_000 });
    await shot(page, `theme-${value}-settings`);
  });
}

// Global font-size preference (nibs-gymz): spot-check the type scale at Small and
// Large in a light (daylight) and a dark (graphite) palette. The whole app scales
// off the single --font-scale root variable, so the table shows it across many rows.
for (const theme of ["daylight", "graphite"] as const) {
  for (const fontSize of ["small", "large"] as const) {
    test(`font size ${fontSize} — ${theme} table`, async ({ page }) => {
      await openApp(page, "milestones", theme, undefined, fontSize);
      await shot(page, `fontsize-${fontSize}-${theme}-table`);
    });
  }
}
