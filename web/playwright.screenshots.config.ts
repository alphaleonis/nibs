import { defineConfig } from "@playwright/test";
import { cpSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

// Screenshot captures run against a throwaway copy of the sample-project
// fixture (same idea as `task demo`) so output is deterministic and never
// touches real nibs data. The temp dir is left for the OS to clean up.
const fixture = resolve(import.meta.dirname, "..", "testdata", "fixtures", "sample-project");
const tmp = mkdtempSync(join(tmpdir(), "nibs-screenshots-"));
cpSync(join(fixture, ".nibs"), join(tmp, ".nibs"), { recursive: true });
cpSync(join(fixture, ".nibs.yml"), join(tmp, ".nibs.yml"));

export default defineConfig({
  testDir: "./screenshots",
  timeout: 30_000,
  retries: 0,
  use: {
    baseURL: "http://127.0.0.1:3132",
    headless: true,
    viewport: { width: 1440, height: 900 },
  },
  webServer: {
    // --config, not --nibs-path: the server runs from the repo root, and
    // --nibs-path moves only the data directory — config discovery would still
    // walk up from the cwd and apply THIS project's prefix to fixture data.
    // Naming the fixture's config resolves its data directory alongside it.
    command: `cd .. && go run . serve --port 3132 --no-open --config "${join(tmp, ".nibs.yml")}"`,
    url: "http://127.0.0.1:3132",
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
