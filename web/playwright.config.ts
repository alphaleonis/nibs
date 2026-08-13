import { defineConfig } from "@playwright/test";
import { cpSync, mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";

// e2e runs against a throwaway copy of the sample-project fixture (same idea as
// `task demo` and the screenshot captures): the suite drives real mutations —
// status, priority — and must never reach the developer's own nibs.
const fixture = resolve(import.meta.dirname, "..", "testdata", "fixtures", "sample-project");
const tmp = mkdtempSync(join(tmpdir(), "nibs-e2e-"));
cpSync(join(fixture, ".nibs"), join(tmp, ".nibs"), { recursive: true });
cpSync(join(fixture, ".nibs.yml"), join(tmp, ".nibs.yml"));

export default defineConfig({
  testDir: "./e2e",
  timeout: 30_000,
  retries: 0,
  // One worker, deliberately. Every test shares ONE server over ONE fixture copy,
  // and the web UI holds a live GraphQL subscription — so a test that mutates a
  // nib (context-menu.test.ts changes status and priority) repaints rows in every
  // OTHER test's page, where NibChangeTracker paints a 1s highlight animation over
  // the changed row. Any assertion comparing painted backgrounds then depends on
  // which files happen to share a worker: open-detail-gesture's "loses its fill"
  // case fails reproducibly when scheduled alongside context-menu, and passes
  // alongside a non-mutating file. Serializing removes the whole class of
  // interference; the suite runs in ~15s, so the parallelism buys nothing worth
  // the flakiness. Restoring parallelism means giving mutating tests their own
  // server or fixture first, not just raising this number.
  workers: 1,
  use: {
    baseURL: "http://127.0.0.1:3131",
    headless: true,
    viewport: { width: 1440, height: 900 },
  },
  webServer: {
    // --config, not --nibs-path: the server runs from the repo root, and
    // --nibs-path moves only the data directory — config discovery would still
    // walk up from the cwd and apply THIS project's prefix to fixture data.
    // Naming the fixture's config resolves its data directory alongside it.
    command: `cd .. && go run . serve --port 3131 --no-open --config "${join(tmp, ".nibs.yml")}"`,
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
