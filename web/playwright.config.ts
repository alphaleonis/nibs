import { defineConfig } from "@playwright/test";
import { cpSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

// e2e mutates nibs, so it runs against a throwaway copy of the sample fixture.
const fixture = resolve(import.meta.dirname, "..", "testdata", "fixtures", "sample-project");

// Playwright loads this module in the runner and again in each worker. Reuse
// the runner's copy through the inherited environment, so workers see the store
// the server is serving; live-areas.test.ts edits it from outside the browser.
const store =
  process.env.NIBS_E2E_STORE ??
  (() => {
    const tmp = mkdtempSync(join(tmpdir(), "nibs-e2e-"));
    cpSync(join(fixture, ".nibs"), join(tmp, ".nibs"), { recursive: true });
    return join(tmp, ".nibs");
  })();
process.env.NIBS_E2E_STORE = store;

export default defineConfig({
  testDir: "./e2e",
  timeout: 30_000,
  retries: 0,
  // All tests share one server and fixture copy. A mutation in one test repaints
  // rows, with a highlight, in every other open page, which breaks background
  // assertions such as open-detail-gesture's "loses its fill". Give mutating
  // tests their own server or fixture before raising this.
  workers: 1,
  use: {
    baseURL: "http://127.0.0.1:3131",
    headless: true,
    viewport: { width: 1440, height: 900 },
  },
  webServer: {
    // The store carries its own config, so the fixture keeps its prefix.
    command: `cd .. && go run . serve --port 3131 --no-open --nibs-path "${store}"`,
    url: "http://127.0.0.1:3131",
    // Never reuse: a leftover server could be pointed at real data.
    reuseExistingServer: false,
    timeout: 30_000,
  },
  projects: [
    {
      name: "chromium",
      use: { browserName: "chromium" },
    },
  ],
});
