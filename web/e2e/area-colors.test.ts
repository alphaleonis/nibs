import { test, expect, type BrowserContext } from "@playwright/test";
import { AREA_PALETTE } from "../src/lib/areas";
import { THEMES, type Theme } from "../src/lib/types";
import { areaSectionRow } from "./areas";

// jsdom does not resolve `var(--area-*)` through the theme cascade, so only a
// real engine can show that each swatch is drawn, and drawn per theme. The
// dialog is never saved, so the shared fixture keeps its declared colors.

/** Returns each swatch's computed background in `theme`, keyed by palette name. */
async function swatchColors(context: BrowserContext, theme: Theme): Promise<Record<string, string>> {
  const page = await context.newPage();
  try {
    await page.addInitScript((t) => {
      localStorage.setItem(
        "nibs-filter-preferences",
        JSON.stringify({ filter: {}, viewLevel: "areas", theme: t }),
      );
    }, theme);
    await page.goto("/");
    const row = areaSectionRow(page, "web");
    await expect(row).toBeVisible({ timeout: 15_000 });
    await row.locator('[data-testid="row-area-actions"]').click();
    await page.locator('[data-testid="area-row-menu"]').getByRole("menuitem", { name: "Edit…" }).click();
    await expect(page.locator('[data-testid="area-edit-form"]')).toBeVisible();

    const colors: Record<string, string> = {};
    for (const name of AREA_PALETTE) {
      colors[name] = await page
        .locator(`[data-testid="area-swatch-${name}"]`)
        .evaluate((el) => getComputedStyle(el).backgroundColor);
    }
    return colors;
  } finally {
    await page.close();
  }
}

for (const theme of THEMES) {
  test(`theme ${theme.value} — every area swatch is drawn, each in its own color`, async ({ context }) => {
    const colors = await swatchColors(context, theme.value);

    for (const name of AREA_PALETTE) {
      expect(colors[name], name).not.toBe("");
      // `transparent` serializes to its computed form, not the keyword.
      expect(colors[name], name).not.toBe("rgba(0, 0, 0, 0)");
    }
    expect(new Set(Object.values(colors)).size).toBe(AREA_PALETTE.length);
  });
}

test("Daylight draws every area swatch differently from each dark theme", async ({ context }) => {
  const daylight = await swatchColors(context, "daylight");

  for (const theme of THEMES.filter((t) => t.dark)) {
    const dark = await swatchColors(context, theme.value);
    for (const name of AREA_PALETTE) {
      expect(daylight[name], `${name} in ${theme.value}`).not.toBe(dark[name]);
    }
  }
});

// Geometry, which jsdom does not compute. The probe is a zero-height box with
// `vertical-align: middle` inside the title, so its top is the title's x-height
// center.
test("the area dot sits on the title's x-height and leaves the type icon's gap", async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem("nibs-filter-preferences", JSON.stringify({ filter: {}, viewLevel: "areas" }));
  });
  await page.goto("/");
  const row = areaSectionRow(page, "web");
  await expect(row).toBeVisible({ timeout: 15_000 });
  await page.evaluate(() => document.fonts.ready);

  const area = await row.evaluate((tr) => {
    const dot = tr.querySelector('[data-testid="section-color"]')!.getBoundingClientRect();
    const title = tr.querySelector('[data-testid="title-text"]')!;
    const probe = document.createElement("span");
    probe.style.cssText = "display: inline-block; height: 0; vertical-align: middle;";
    title.append(probe);
    const xCenter = probe.getBoundingClientRect().top;
    probe.remove();
    return { dotCenter: dot.top + dot.height / 2, xCenter, gap: title.getBoundingClientRect().left - dot.right };
  });
  // A member of `web`, so it renders a type icon in the same column.
  const nib = await page.locator('tr[data-nib-id="tnib-b005"]').evaluate((tr) => {
    const icon = tr.querySelector(".type-icon-gap svg")!.getBoundingClientRect();
    return { gap: tr.querySelector('[data-testid="title-text"]')!.getBoundingClientRect().left - icon.right };
  });

  expect(Math.abs(area.dotCenter - area.xCenter)).toBeLessThanOrEqual(0.5);
  expect(area.gap).toBeCloseTo(nib.gap, 1);
});
