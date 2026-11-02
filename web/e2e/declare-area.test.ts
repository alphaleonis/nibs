import { test, expect } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { areaSectionRow } from "./areas";

// The fixture already declares root areas, so the empty-state form is gone. New →
// Area and the Area field's "+ New area…" are the browser paths to another root.
// Each area is removed at the end because the whole suite shares one fixture copy
// and one worker.

const repoRoot = resolve(import.meta.dirname, "..", "..");
const newArea = "mobile";

function storePath(): string {
  const store = process.env.NIBS_E2E_STORE;
  if (!store) throw new Error("NIBS_E2E_STORE is unset; playwright.config.ts sets it");
  return store;
}

function declares(area: string): boolean {
  const declared = readFileSync(join(storePath(), "areas.yml"), "utf8");
  return declared.split("\n").some((line) => line.trim() === `- name: ${area}`);
}

function areaRemove(area: string) {
  execFileSync("go", ["run", ".", "area", "rm", area, "--nibs-path", storePath()], {
    cwd: repoRoot,
    encoding: "utf8",
  });
  if (declares(area)) throw new Error(`areas.yml still declares ${area} after removing it`);
}

test("New → Area declares a second top-level area and its section row appears", async ({ page }) => {
  // `go run` compiles on the first call, which the default timeout does not
  // allow for alongside the page work.
  test.setTimeout(120_000);

  await page.goto("/");
  await expect(page.locator("tr[data-nib-id]").first()).toBeVisible({ timeout: 10_000 });

  await page.locator('button[aria-label^="View:"]').click();
  await page.getByRole("menuitemradio", { name: "Areas" }).click();
  await expect(areaSectionRow(page, "web")).toBeVisible({ timeout: 10_000 });
  await expect(areaSectionRow(page, newArea)).toHaveCount(0);

  try {
    await page.getByTestId("toolbar-add").click();
    await page.getByTestId("toolbar-add-area").click();
    const dialog = page.getByTestId("new-area-dialog");
    await dialog.getByTestId("area-path-input").fill(newArea);
    await dialog.getByRole("button", { name: "Create", exact: true }).click();

    await expect(dialog).toHaveCount(0);
    await expect(areaSectionRow(page, newArea)).toBeVisible({ timeout: 15_000 });
    expect(declares(newArea)).toBe(true);
  } finally {
    // Skipped when the declaration never landed, so the cleanup cannot mask the failure above.
    if (declares(newArea)) areaRemove(newArea);
  }
});

test("New → Area from the Tree view confirms with a toast whose Show opens the Areas view", async ({ page }) => {
  test.setTimeout(120_000);
  const area = "desktop";
  expect(declares(area)).toBe(false);

  await page.goto("/");
  await expect(page.locator("tr[data-nib-id]").first()).toBeVisible({ timeout: 10_000 });

  const viewButton = page.locator('button[aria-label^="View:"]');
  await viewButton.click();
  await page.getByRole("menuitemradio", { name: "Tree" }).click();
  await expect(viewButton).toHaveAttribute("aria-label", "View: Tree");

  try {
    await page.getByTestId("toolbar-add").click();
    await page.getByTestId("toolbar-add-area").click();
    const dialog = page.getByTestId("new-area-dialog");
    await dialog.getByTestId("area-path-input").fill(area);
    await dialog.getByRole("button", { name: "Create", exact: true }).click();

    await expect(dialog).toHaveCount(0);
    const toast = page.locator("[data-sonner-toast]").filter({ hasText: `Created area ${area}` });
    await expect(toast).toBeVisible({ timeout: 15_000 });
    // The create alone leaves the user in their view.
    await expect(viewButton).toHaveAttribute("aria-label", "View: Tree");

    await toast.getByRole("button", { name: "Show", exact: true }).click();

    await expect(viewButton).toHaveAttribute("aria-label", "View: Areas");
    await expect(areaSectionRow(page, area)).toBeVisible({ timeout: 15_000 });
  } finally {
    if (declares(area)) areaRemove(area);
  }
});

for (const { presentation, area } of [
  { presentation: "docked", area: "tablet" },
  { presentation: "expanded", area: "watch" },
] as const) {
  test(`+ New area… in the ${presentation} editor creates the area and selects it`, async ({ page }) => {
    test.setTimeout(120_000);
    expect(declares(area)).toBe(false);

    await page.goto("/?nib=tnib-t004");
    const editor = page.getByTestId("active-nib-view");
    const field = editor.getByTestId("anv-area");
    await expect(field).toHaveText("None", { timeout: 15_000 });
    if (presentation === "expanded") {
      await editor.getByTestId("anv-expand").click();
      await expect(page.getByTestId("active-nib-modal")).toBeVisible();
    }

    try {
      await field.click();
      await page.getByRole("option", { name: "New area…" }).click();
      const dialog = page.getByTestId("new-area-dialog");
      await expect(dialog).toBeVisible();

      // The dialog's Escape is its own: the app's Escape would close the editor too.
      await page.keyboard.press("Escape");
      await expect(dialog).toHaveCount(0);
      await expect(editor).toBeVisible();
      await expect(field).toBeFocused();

      await field.click();
      await page.getByRole("option", { name: "New area…" }).click();
      await dialog.getByTestId("area-path-input").fill(area);
      await dialog.getByRole("button", { name: "Create", exact: true }).click();

      await expect(dialog).toHaveCount(0);
      await expect(field).toHaveText(area);
      expect(declares(area)).toBe(true);

      // The picker lists the new area as the selection once the server's config arrives.
      await field.click();
      await expect(page.getByRole("option", { name: area })).toHaveAttribute("aria-selected", "true", {
        timeout: 15_000,
      });
    } finally {
      if (declares(area)) areaRemove(area);
    }
  });
}
