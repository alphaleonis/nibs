import { test, expect, type Page } from "@playwright/test";

// Proves the typeface setup has its intended EFFECT in a real browser.
//
// The bug it guards: the app declared no font-family and inherited whatever
// sans the OS offered, while several rules asked for weight 500 — and Segoe UI,
// what that stack resolves to on Windows, has no Medium face. Edge and Firefox
// substituted differently, so the same page rendered heavier in one than the
// other. Nothing errored and the computed styles were identical in both, which
// is exactly why it went unnoticed and why a declaration check is not enough.
//
// The CSS declarations are pinned in src/lib/typography.test.ts (`task test`).
// Only assertions needing a renderer live here.

async function open(page: Page) {
  await page.addInitScript(() => {
    localStorage.setItem("nibs-filter-preferences", JSON.stringify({ filter: {}, viewLevel: "flat" }));
  });
  await page.goto("/");
  await expect(page.locator("tr[data-nib-id]").first()).toBeVisible({ timeout: 10_000 });
  await page.evaluate(() => document.fonts.ready);
}

test.describe("typeface", () => {
  test("the row title actually renders in Inter", async ({ page }) => {
    await open(page);

    const family = await page
      .locator('button[data-testid="title-text"]')
      .first()
      .evaluate((el) => getComputedStyle(el).fontFamily.split(",")[0].replace(/["']/g, ""));

    expect(family).toBe("Inter");
    expect(await page.evaluate(() => document.fonts.check("500 14px Inter"))).toBe(true);
  });

  // No rule currently asks for 500 — the UI settled on 400 throughout. This
  // checks the variable AXIS is live regardless: if the font ever degraded to a
  // static face, or the stack fell back to a system font, intermediate weights
  // would collapse onto their neighbours again and the next non-400 weight
  // anyone reaches for would silently reintroduce the Edge/Firefox split.
  test("the variable weight axis is live, so 500 is distinct from 400 and 600", async ({ page }) => {
    await open(page);

    // Ink density rather than advance width: a font lacking a Medium can still
    // shift metrics slightly, but it cannot get meaningfully darker.
    const ink = await page.evaluate(() => {
      const measure = (weight: number) => {
        const cv = document.createElement("canvas");
        cv.width = 420;
        cv.height = 30;
        const c = cv.getContext("2d")!;
        c.fillStyle = "#fff";
        c.fillRect(0, 0, cv.width, cv.height);
        c.fillStyle = "#000";
        c.font = `${weight} 14px Inter`;
        c.fillText("Implement bcrypt password hashing", 2, 20);
        const d = c.getImageData(0, 0, cv.width, cv.height).data;
        let sum = 0;
        for (let i = 0; i < d.length; i += 4) sum += 255 - d[i];
        return sum / 1000;
      };
      return { w400: measure(400), w500: measure(500), w600: measure(600) };
    });

    // Strictly increasing: 500 must sit between 400 and 600, not on top of either.
    expect(ink.w500, `500 (${ink.w500.toFixed(1)}) must be darker than 400 (${ink.w400.toFixed(1)})`).toBeGreaterThan(
      ink.w400 * 1.02,
    );
    expect(ink.w600, `600 (${ink.w600.toFixed(1)}) must be darker than 500 (${ink.w500.toFixed(1)})`).toBeGreaterThan(
      ink.w500 * 1.02,
    );
  });

  test("the font files are served from the immutable asset path", async ({ page, request }) => {
    await open(page);

    const urls = await page.evaluate(
      () =>
        [
          ...new Set(
            [...document.styleSheets]
              .flatMap((s) => {
                try {
                  return [...s.cssRules].map((r) => r.cssText);
                } catch {
                  return [];
                }
              })
              .join("")
              .match(/\/assets\/[^)"']*\.woff2/g) ?? [],
          ),
        ],
    );

    expect(urls.length).toBe(2); // upright + italic
    for (const url of urls) {
      const res = await request.get(url);
      expect(res.status(), `${url} did not serve`).toBe(200);
      expect(res.headers()["content-type"]).toContain("woff2");
      // Content-hashed, so cmd/spa.go caches it forever rather than no-store.
      expect(res.headers()["cache-control"]).toContain("immutable");
    }
  });
});
