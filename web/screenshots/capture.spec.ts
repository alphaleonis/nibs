import { test, expect, type Page } from "@playwright/test";
import { mkdirSync } from "node:fs";
import { join } from "node:path";
import { VIEW_LEVELS, THEMES } from "../src/lib/types";
import type { Theme, DetailPanelPosition, FontSize } from "../src/lib/types";

// Captures PNGs of the key web UI states into web/screenshots/output/ so an
// agent (or human) can visually verify UI changes. Run via `task screenshots`.
//
// The theme engine is exercised below: each palette gets a
// table + detail + settings capture so the palettes can be compared side by side.
// The captures drive the REAL app (page.goto loads index.html), so the light/dark
// `.dark` class is toggled by the FOUC guard + App's $effect exactly as at runtime
// — the light Daylight palette therefore renders with `.dark` cleared.
// When the board view lands, add a capture for it.

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

// Flat view with an ACTIVE date sort: the Modified header shows its direction
// arrow and rows are ordered by recency (default is sort-off, so this state is
// otherwise uncaptured).
test("table — flat view, sorted by Modified desc", async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem(
      "nibs-filter-preferences",
      JSON.stringify({ filter: {}, viewLevel: "flat", tableSort: { field: "modified", direction: "desc" } }),
    );
  });
  await page.goto("/");
  await expect(page.locator("tr[data-nib-id]").first()).toBeVisible({ timeout: 10_000 });
  await shot(page, "table-flat-sorted-modified");
});

// Filter box syntax-highlight overlay: type a query mixing a VALID metadata token
// (type:bug), an INVALID value (status:banana → red wavy underline), and a free-text
// word (login), then crop to the filter band so per-token coloring + the inline
// underline are visible. The box is left focused so the text stays exactly as typed
// (the focus-guard skips canonicalization while focused).
test("filter box — syntax-highlight overlay", async ({ page }) => {
  await openApp(page);
  const input = page.getByTestId("filter-keyword");
  await input.click();
  await input.fill("type:bug status:banana login");
  await shot(page, "filter-highlight");
  // Cropped to the filter band for a legible close-up of the token coloring.
  await page.locator('[role="search"]').screenshot({ path: join(OUT, "filter-highlight-cropped.png") });
});

// Autocomplete dropdown must render ABOVE the table rows below it (not behind).
// Viewport shot (not cropped) so the dropdown's stacking vs the table is visible.
test("filter box — completion dropdown over table rows", async ({ page }) => {
  await openApp(page);
  const input = page.getByTestId("filter-keyword");
  await input.click();
  // The user's exact multi-value scenario: draft,todo apply (rows stay visible),
  // the trailing "in-progres" opens the completion over the populated table.
  await input.pressSequentially("status:draft,todo,in-progres");
  await expect(page.getByTestId("filter-suggestions")).toBeAttached();
  await expect(page.locator("tr[data-nib-id]").first()).toBeVisible();
  await shot(page, "filter-completion-over-table");
});

// The hover-× on a token should be an adequately-sized, clearly-separated button.
test("filter box — token remove button (hover)", async ({ page }) => {
  await openApp(page);
  const input = page.getByTestId("filter-keyword");
  await input.click();
  await input.pressSequentially("type:bug status:todo");
  await page.getByTestId("filter-token").first().hover();
  await expect(page.getByTestId("filter-token-remove").first()).toBeVisible();
  await page.locator('[role="search"]').screenshot({ path: join(OUT, "filter-token-remove-hover.png") });
});

// Invalid-token marker must read as an attached element over the table, not as
// bare "mid-air" text. status:xyz is invalid (and not a completion prefix).
test("filter box — invalid token marker over table rows", async ({ page }) => {
  await openApp(page);
  const input = page.getByTestId("filter-keyword");
  await input.click();
  await input.pressSequentially("status:xyz");
  await expect(page.getByTestId("filter-invalid")).toBeVisible();
  await shot(page, "filter-invalid-marker-over-table");
});

test("detail panel", async ({ page }) => {
  await openApp(page);
  await page.locator("tr[data-nib-id]").first().locator('[data-action="title"]').click();
  await expect(page.locator('[data-testid="active-nib-view"]')).toBeVisible({ timeout: 5_000 });
  await expect(page.locator('[data-testid="anv-title"]')).toBeVisible({ timeout: 5_000 });
  await shot(page, "detail-panel");
});

// Task-list checkboxes in rendered nib body: clickable + theme-styled.
// tnib-t005 has a MIXED checked/unchecked checklist; capture it under a
// dark (graphite) and the light (daylight) palette so the themed checkbox — themed
// border, --primary fill, --primary-foreground check, no gray native default — is
// visible in both light and dark, in both states.
for (const { value } of THEMES) {
  test(`task-list checkboxes — ${value}`, async ({ page }) => {
    await openApp(page, "none", value);
    await page.locator('tr[data-nib-id="tnib-t005"]').locator('[data-action="title"]').click();
    await expect(page.locator('[data-testid="anv-body-prose"]')).toBeVisible({ timeout: 5_000 });
    await shot(page, `task-checkboxes-${value}`);
  });
}

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
  // Detail view docked at the bottom (table on top, preview below).
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

test("add-child type picker — anchored, over an open detail view", async ({ page }) => {
  // The picker is an anchored popover (with type icons) that overlays
  // the app; opening it must NOT hide the detail view. Use an epic (>=2 valid
  // child types) so the picker actually appears instead of creating directly.
  await openApp(page);
  const epicRow = page.locator('tr[data-nib-id="tnib-e001"]');
  await epicRow.locator('[data-action="title"]').click();
  await expect(page.locator('[data-testid="active-nib-view"]')).toBeVisible({ timeout: 5_000 });
  await epicRow.hover();
  await epicRow.locator('[data-testid="row-add-child"]').click();
  await expect(page.locator('[data-testid="type-picker-popover"]')).toBeVisible({ timeout: 5_000 });
  await shot(page, "type-picker");
});

// Per-theme captures: table + detail panel under each
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

// Global font-size preference: spot-check the type scale at Small and
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
