import { test, expect } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { readFileSync } from "node:fs";
import { join, resolve } from "node:path";
import { areaSectionRow } from "./areas";

// The fixture already declares root areas, so the empty-state form is gone and
// New → Area is the only browser path to another root. The area is removed at the
// end because the whole suite shares one fixture copy and one worker.

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
