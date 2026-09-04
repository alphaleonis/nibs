import { test, expect } from "@playwright/test";
import { execFileSync } from "node:child_process";
import { resolve } from "node:path";

// A `nibs area rename` in another terminal rewrites the store's areas.yml. The
// running server reloads it and pushes the new vocabulary, so the Areas view
// re-sections in an open page — no restart, no reload (nibs-5cuk).
//
// Only a real engine can close this: it needs a genuine WebSocket carrying a
// genuine subscription, a real second process editing the store, and the
// server's own file watcher between them. The jsdom half (App binding the pushed
// config) is App.liveAreas.test.ts; neither covers the other.
//
// The rename is undone at the end because the whole suite shares one fixture
// copy and one worker.

const repoRoot = resolve(import.meta.dirname, "..", "..");

function areaRename(from: string, to: string) {
  const store = process.env.NIBS_E2E_STORE;
  if (!store) throw new Error("NIBS_E2E_STORE is unset; playwright.config.ts sets it");
  execFileSync("go", ["run", ".", "area", "rename", from, to, "--nibs-path", store], {
    cwd: repoRoot,
    encoding: "utf8",
  });
}

test("an area renamed from another process re-sections the open page", async ({ page }) => {
  // `go run` compiles on the first call, which the default timeout does not
  // allow for alongside the page work.
  test.setTimeout(120_000);

  await page.goto("/");
  await expect(page.locator("tr[data-nib-id]").first()).toBeVisible({ timeout: 10_000 });

  await page.locator('button[aria-label^="View:"]').click();
  await page.getByRole("menuitemradio", { name: "Areas" }).click();
  await expect(page.locator('button[aria-label^="View:"]')).toHaveAttribute(
    "aria-label",
    "View: Areas",
  );

  const webSection = page.getByRole("row", { name: /^web\b/ }).first();
  await expect(webSection).toBeVisible({ timeout: 10_000 });

  try {
    areaRename("web", "frontend");

    // No reload and no navigation between the rename and this assertion: the
    // only thing that can change the page is the server's push.
    await expect(page.getByRole("row", { name: /^frontend\b/ }).first()).toBeVisible({
      timeout: 15_000,
    });
    await expect(page.getByRole("row", { name: /^web\b/ })).toHaveCount(0);
  } finally {
    areaRename("frontend", "web");
  }
});
