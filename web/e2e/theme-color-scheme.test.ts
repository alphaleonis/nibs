import { test, expect, type Page } from "@playwright/test";
import { THEMES } from "../src/lib/types";

// `color-scheme` is what tells the BROWSER which way a palette leans, and it is
// invisible to every other layer of the app: Tailwind's `dark:` variant keys off
// the `.dark` class, jsdom does not implement the cascade, and the screenshot
// captures cannot see it either because headless Chromium draws overlay
// scrollbars that are absent from an idle frame. So this runs in a real engine
// and reads the COMPUTED value — the only place the daylight override's
// specificity (`:root[data-theme=...]` over bare `:root`) is actually exercised.
//
// Guard proof (nibs-kdln): deleting `color-scheme: light` from the daylight
// block fails the daylight case with "dark", and deleting `color-scheme: dark`
// from `:root` fails the other three with "normal".

async function openWithTheme(page: Page, theme: string) {
  await page.addInitScript((t) => {
    localStorage.setItem(
      "nibs-filter-preferences",
      JSON.stringify({ filter: {}, viewLevel: "milestones", theme: t }),
    );
  }, theme);
  await page.goto("/");
  await expect(page.locator("tr[data-nib-id]").first()).toBeVisible({ timeout: 10_000 });
}

const root = (page: Page, property: string) =>
  page.evaluate(
    (prop) => getComputedStyle(document.documentElement).getPropertyValue(prop),
    property,
  );

for (const theme of THEMES) {
  test(`theme ${theme.value} — declares a color-scheme matching its light/dark lean`, async ({
    page,
  }) => {
    await openWithTheme(page, theme.value);

    // THEMES[].dark is the app's own record of which way the palette leans, so
    // asserting against it keeps this honest when a fifth palette is added:
    // a new theme that forgets `color-scheme` fails here without touching this file.
    expect(await root(page, "color-scheme")).toBe(theme.dark ? "dark" : "light");
  });

  test(`theme ${theme.value} — scrollbar is painted from the palette, not the UA default`, async ({
    page,
  }) => {
    await openWithTheme(page, theme.value);

    const scrollbarColor = await root(page, "scrollbar-color");

    // Resolve --muted-foreground through a real property rather than reading the
    // custom property directly. getPropertyValue on a custom property returns its
    // SPECIFIED text, which the production build minifies ("oklch(50% .012 75)"),
    // while scrollbar-color reports a resolved value ("oklch(0.5 0.012 75)") — so
    // comparing the two forms fails on serialization alone. Routing the token
    // through `color` puts both sides through the same serializer.
    const mutedForeground = await page.evaluate(() => {
      const probe = document.createElement("div");
      probe.style.color = "var(--muted-foreground)";
      document.documentElement.append(probe);
      const resolved = getComputedStyle(probe).color;
      probe.remove();
      return resolved;
    });

    // The thumb must resolve to THIS palette's --muted-foreground. Comparing
    // against the live token (rather than a hardcoded oklch triple) is what
    // makes the assertion mean "follows the theme" instead of "is some color":
    // it fails if the declaration is dropped, and it fails if it is rewritten
    // to a constant that stops tracking the palette.
    expect(mutedForeground).not.toBe("");
    expect(scrollbarColor).toContain(mutedForeground);
    // `transparent` serializes to its computed form, not the keyword.
    expect(scrollbarColor).toContain("rgba(0, 0, 0, 0)");
  });
}

test("palettes do not all resolve to the same scrollbar color", async ({ page }) => {
  // The per-theme assertions above would ALL still pass if every palette happened
  // to define an identical --muted-foreground — the guard would then be blind to
  // a scrollbar that never actually changes between themes. Collecting the
  // resolved values and demanding more than one distinct entry closes that hole.
  const seen = new Set<string>();
  for (const theme of THEMES) {
    await openWithTheme(page, theme.value);
    seen.add(await root(page, "scrollbar-color"));
  }
  expect(seen.size).toBeGreaterThan(1);
});
