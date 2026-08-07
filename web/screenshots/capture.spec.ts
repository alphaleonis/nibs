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

// The relationship/existence half of the grammar, which the overlay learned to
// color only once spans.ts started routing through `recognizeRelationship`. The
// query deliberately mixes all four outcomes so they can be compared in one frame:
// a metadata token (type:bug), a relationship-id token (ancestor:…), an existence
// token (has:parent), an unrecognized field (foo:bar) and a bare word (login) —
// the last two must stay visibly muted while the first three read as real tokens.
test("filter box — relationship token highlighting", async ({ page }) => {
  await openApp(page);
  const input = page.getByTestId("filter-keyword");
  await input.click();
  await input.fill("priority:normal status:todo,in-progress ancestor:tnib-e001 has:parent foo:bar login");
  await page.locator('[role="search"]').screenshot({
    path: join(OUT, "filter-highlight-relationship-cropped.png"),
  });
});

// Hierarchy-specific empty state: two tree predicates ANDed together can carve out
// a slice nothing occupies, and the generic "No nibs found" left no way to see the
// shape of the dead end. Names the active relationships and offers the escape hatch.
// `ancestor:X descendant:X` is one of the measured zero-row combinations.
test("table — hierarchy empty state", async ({ page }) => {
  await page.addInitScript(() => {
    localStorage.setItem(
      "nibs-filter-preferences",
      JSON.stringify({
        filter: { ancestorId: "tnib-e001", descendantId: "tnib-e001" },
        viewLevel: "milestones",
      }),
    );
  });
  await page.goto("/");
  await expect(page.getByTestId("empty-hierarchy")).toBeVisible({ timeout: 10_000 });
  await shot(page, "table-empty-hierarchy");
});

// Query syntax help: a `?` at the end of the filter band opens a generated
// reference for the whole grammar. Captured open, so the panel's readability at its
// max height (and the trigger's placement after every facet) can be checked.
test("filter band — query syntax help panel", async ({ page }) => {
  await openApp(page);
  await page.getByTestId("query-help-trigger").click();
  await expect(page.getByTestId("query-help-panel")).toBeVisible({ timeout: 5_000 });
  await shot(page, "query-help-panel");
});

// Autocomplete dropdown must render ABOVE the table rows below it (not behind).
// Viewport shot (not cropped) so the dropdown's stacking vs the table is visible.
test("filter box — completion dropdown over table rows", async ({ page }) => {
  await openApp(page);
  const input = page.getByTestId("filter-keyword");
  await input.click();
  // The user's exact repro: trailing comma → a TALL 4-item dropdown that extends
  // down into the table, actually exercising the z-stacking (a short 1-item list
  // fits in the gap and hides the bug).
  await input.pressSequentially("status:draft,todo,");
  await expect(page.getByTestId("filter-suggestions")).toBeAttached();
  await expect(page.locator("tr[data-nib-id]").first()).toBeVisible();
  // Regression guard: the tall completion dropdown must paint OVER the sticky
  // table header. Both used z-index 20 (the header via --z-sticky, the filter band
  // reusing it), so the later-in-DOM header won and covered the dropdown; the band
  // now sits at --z-toolbar (above --z-sticky). Assert the dropdown is the topmost
  // element at a point inside the dropdown∩header overlap.
  const topmostAtHeaderOverlap = await page.evaluate(() => {
    const drop = document.querySelector('[data-testid="filter-suggestions"]')?.getBoundingClientRect();
    const thead = document.querySelector("thead")?.getBoundingClientRect();
    if (!drop || !thead) return "missing";
    const y = Math.max(drop.top, thead.top) + 4;
    if (y >= Math.min(drop.bottom, thead.bottom)) return "no-overlap";
    const el = document.elementFromPoint(drop.left + 8, y) as HTMLElement | null;
    return el?.closest('[data-testid="filter-suggestions"]') ? "dropdown" : (el?.tagName ?? "unknown");
  });
  expect(topmostAtHeaderOverlap).toBe("dropdown");
  await shot(page, "filter-completion-over-table");
});

// Hovering a token tints it as a chip and carries no in-box control: a remove
// button could only overlap the token's own trailing glyph, so removal is
// click-to-select + Delete, advertised by the app's styled tooltip. The screenshot
// keeps the visual record of the tint AND of the tooltip, which is the only way to
// check it reads like its siblings in the band — jsdom has no layout, hover timing
// or theming, and Playwright can assert the tooltip exists but not that it matches.
test("filter box — token hover affordance", async ({ page }) => {
  await openApp(page);
  const input = page.getByTestId("filter-keyword");
  await input.click();
  await input.pressSequentially("type:bug status:todo");
  const token = page.getByTestId("filter-token").first();
  await token.hover();
  const tip = page.locator('[data-slot="tooltip-content"]', {
    hasText: "Click to select · Delete to remove",
  });
  await expect(tip).toBeVisible();
  await expect(token).not.toHaveAttribute("title", /./);
  await expect(token.locator("button")).toHaveCount(0);
  // The tooltip is portaled to <body>, so it renders OUTSIDE the search band and an
  // element-scoped screenshot of the band would crop it away. Clip to the union of
  // the two boxes instead, so both land in one frame whichever side it opens on.
  const band = await page.locator('[role="search"]').boundingBox();
  const tipBox = await tip.boundingBox();
  if (!band || !tipBox) throw new Error("filter band or tooltip is not laid out");
  const x = Math.min(band.x, tipBox.x);
  const y = Math.min(band.y, tipBox.y);
  await page.screenshot({
    path: join(OUT, "filter-token-hover.png"),
    clip: {
      x,
      y,
      width: Math.max(band.x + band.width, tipBox.x + tipBox.width) - x,
      height: Math.max(band.y + band.height, tipBox.y + tipBox.height) - y,
    },
    animations: "disabled",
  });
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

// Responsive filter band: the query box should be wide, and below a breakpoint the
// facet dropdowns should stack BELOW the box (box takes the full row) rather than
// squeezing beside it. Captured at a wide, a mid, and a narrow viewport with a long
// query filled so the box width is actually exercised. Cropped to the filter band.
for (const w of [1280, 760, 560]) {
  test(`filter band — responsive layout @${w}px`, async ({ page }) => {
    await page.setViewportSize({ width: w, height: 760 });
    await openApp(page);
    const input = page.getByTestId("filter-keyword");
    await input.click();
    await input.fill("type:bug,task -tags:wip status:todo");
    await page.keyboard.press("Escape"); // close the completion popover so stacked facets are visible
    await page.locator('[role="search"]').screenshot({ path: join(OUT, `filter-band-${w}.png`) });
  });
}

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

// The filter band at Small and Large: a cropped band shot (clipped button labels
// / overlapping controls) plus a viewport shot with a facet menu open (menu
// rhythm — item padding and leading at that scale).
for (const fontSize of ["small", "large"] as const) {
  test(`font size ${fontSize} — filter band and facet menu`, async ({ page }) => {
    await openApp(page, "milestones", "graphite", undefined, fontSize);
    const input = page.getByTestId("filter-keyword");
    await input.click();
    await input.fill("type:bug,task -tags:wip status:todo");
    await page.keyboard.press("Escape"); // close the completion popover so the facets are visible
    // Park the pointer off the band: the click leaves it mid-box, and at Large the
    // longer glyph run puts a token under it — the token layer's hover tint then
    // washes out the highlight colors and the capture stops being comparable.
    await page.mouse.move(0, 0);
    await page.locator('[role="search"]').screenshot({ path: join(OUT, `fontsize-${fontSize}-filter-band.png`) });
    await page.getByRole("button", { name: /^status/i }).click();
    await expect(page.getByTestId("status-preset-open")).toBeVisible({ timeout: 5_000 });
    await shot(page, `fontsize-${fontSize}-filter-menu`);
  });
}

// Reads the type metrics the font-size guard below compares. `family` is carried
// alongside `size` because the query box's stacked layers must agree on BOTH.
async function readBandMetrics(page: Page) {
  return page.evaluate(() => {
    const read = (selector: string) => {
      const el = document.querySelector(selector);
      if (!el) throw new Error(`missing element: ${selector}`);
      const style = getComputedStyle(el);
      return { size: parseFloat(style.fontSize), family: style.fontFamily };
    };
    return {
      input: read('[data-testid="filter-keyword"]'),
      backdrop: read('[data-testid="filter-highlight"]'),
      tokens: read('[data-testid="filter-tokens"]'),
      facet: read('[role="search"] [data-slot="dropdown-menu-trigger"]'),
      body: read('[data-testid="nib-id"]'),
    };
  });
}

// The two hand-written `--font-scale` consumers that live in the vendored
// primitives rather than in a `--text-*` token, so nothing in readBandMetrics
// reaches them:
//
//   buttonSm — ui/button's `size="sm"` slot, an arbitrary 0.8rem that sits
//     between `xs` (0.75rem) and `default` (0.875rem). Every element the band
//     metrics read resolves through `--text-sm`/`--text-body-size`; the facet
//     trigger in particular is `size: 'default'`, so it exercises the ALREADY
//     covered rung. ActiveNibView's id button is the reachable `sm` Button.
//   menuCap — ui/dropdown-menu's `max-w` cap. A width, not a font-size, so no
//     type measurement anywhere can see it.
async function readPrimitiveMetrics(page: Page) {
  // The `sm` button lives in the detail panel; the cap needs a menu open.
  await page.locator("tr[data-nib-id]").first().locator('[data-action="title"]').click();
  await expect(page.locator('[data-testid="active-nib-view"]')).toBeVisible({ timeout: 5_000 });
  await expect(page.locator('[data-testid="anv-id"]')).toBeVisible({ timeout: 5_000 });
  // Scoped to the filter band: the open detail panel has its own status control.
  await page.locator('[role="search"]').getByRole("button", { name: /^status/i }).click();
  await expect(page.getByTestId("status-preset-open")).toBeVisible({ timeout: 5_000 });

  return page.evaluate(() => {
    const el = (selector: string) => {
      const found = document.querySelector(selector);
      if (!found) throw new Error(`missing element: ${selector}`);
      return found;
    };
    return {
      buttonSm: parseFloat(getComputedStyle(el('[data-testid="anv-id"]')).fontSize),
      menuCap: parseFloat(getComputedStyle(el('[data-slot="dropdown-menu-content"]')).maxWidth),
    };
  });
}

// Regression guard: the global font-size preference must reach the FILTER BAND
// and the vendored primitives, not only the semantic .text-label/.text-body/
// .text-caption utilities. The shadcn primitives (ui/input, ui/button,
// ui/dropdown-menu/*) hardcode Tailwind's raw text-xs/sm/base utilities, so
// app.css re-points those `--text-*` tokens at `--font-scale`; the two sizes no
// token covers are hand-multiplied in the components themselves. Before that,
// the query box and the facet triggers sat at a fixed 14px at every setting
// while the table around them grew and shrank.
//
// A fresh page per setting: the preference is read from localStorage at boot, so
// each measurement needs its own load rather than a re-navigation.
//
// This file runs ONLY under `task screenshots` — not `task test`, not CI. The
// gated half of this guard is src/lib/fontScaleTokens.test.ts, which asserts the
// same invariant structurally against the SOURCE TEXT under vitest. This test
// covers what that one cannot: a defeat that leaves the source intact but breaks
// at build or merge time (a Tailwind bump changing arbitrary-bracket parsing,
// `cn()`/tailwind-merge precedence dropping the size-slot class).
test("font size — filter band scales with the global preference", async ({ context }) => {
  const measure = async (fontSize: FontSize) => {
    const page = await context.newPage();
    await openApp(page, "milestones", "graphite", undefined, fontSize);
    const metrics = await readBandMetrics(page);
    const primitives = await readPrimitiveMetrics(page);
    await page.close();
    return { ...metrics, ...primitives };
  };

  const small = await measure("small");
  const large = await measure("large");

  // The query box is three stacked layers — the colored highlight backdrop, the
  // transparent-text input, and the token hit-layer — each laid out by glyph
  // flow alone. If their type metrics ever diverge the highlight and the click
  // targets slide off the caret, so they must agree exactly at every setting.
  for (const m of [small, large]) {
    expect(m.backdrop.size).toBeCloseTo(m.input.size, 3);
    expect(m.tokens.size).toBeCloseTo(m.input.size, 3);
    expect(m.backdrop.family).toBe(m.input.family);
    expect(m.tokens.family).toBe(m.input.family);
  }

  // The preference must actually move the band...
  expect(large.input.size).toBeGreaterThan(small.input.size);
  expect(large.facet.size).toBeGreaterThan(small.facet.size);

  // ...by the SAME ABSOLUTE SIZE as the already-scaling semantic body token, so
  // the band keeps its rhythm with the table it sits above. Absolute equality,
  // not just a matching ratio: `--text-sm` is defined as `var(--text-body-size)`
  // (app.css), and a ratio check would still pass if someone re-forked it onto
  // its own literal and then retuned only one of the two bases.
  for (const m of [small, large]) {
    expect(m.input.size).toBeCloseTo(m.body.size, 3);
    expect(m.facet.size).toBeCloseTo(m.body.size, 3);
  }

  // The body ratio is asserted to be >1 too, so a total scaling failure cannot
  // make the comparison vacuously true.
  const bodyRatio = large.body.size / small.body.size;
  expect(bodyRatio).toBeGreaterThan(1);
  expect(large.input.size / small.input.size).toBeCloseTo(bodyRatio, 2);
  expect(large.facet.size / small.facet.size).toBeCloseTo(bodyRatio, 2);

  // ui/button `sm`: an arbitrary rung outside the --text-* ladder, so it needs
  // its own multiplier. Ratio, not absolute equality — 0.8rem deliberately sits
  // BETWEEN `xs` and `default`, and that ordering is what must survive scaling.
  expect(large.buttonSm / small.buttonSm).toBeCloseTo(bodyRatio, 2);
  for (const m of [small, large]) {
    expect(m.buttonSm).toBeGreaterThan(m.body.size * (0.75 / 0.875));
    expect(m.buttonSm).toBeLessThan(m.body.size);
  }

  // ui/dropdown-menu's width cap: a LAYOUT dimension, the only one that scales.
  // It bounds size-to-content menus, so a fixed rem cap would start clipping
  // labels at Large that fit at Medium. Same ratio as the text it bounds.
  expect(large.menuCap).toBeGreaterThan(small.menuCap);
  expect(large.menuCap / small.menuCap).toBeCloseTo(bodyRatio, 2);
});

// The Status facet dropdown: one "Open" preset plus the per-status checkboxes.
// There used to be two presets ("Open" and "Open + deferred"); once deferred
// became a closed status their sets became identical, so the second was removed
// rather than relabeled. This capture is how that is checked visually — jsdom
// asserts the preset count, but only a render shows the menu still reads well
// and that all six statuses remain individually selectable.
test("status facet — presets and per-status checkboxes", async ({ page }) => {
  await openApp(page);
  await page.getByRole("button", { name: /^status/i }).click();
  await expect(page.getByTestId("status-preset-open")).toBeVisible({ timeout: 5_000 });
  await shot(page, "status-facet-dropdown");
});


