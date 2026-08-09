import { defineConfig } from "vitest/config";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import path from "node:path";

export default defineConfig({
  plugins: [svelte()],
  resolve: {
    conditions: ["browser"],
    alias: {
      $lib: path.resolve("./src/lib"),
    },
  },
  test: {
    // Bounded because each worker is a jsdom environment, and the default
    // fan-out is per-core: on a 24-core machine the unbounded fleet exceeds
    // 6 GB, which is how one `task test` run came to fill a 10 GiB cgroup and
    // get the process that launched it OOM-killed (nibs-0kip).
    //
    // Measured here, 2304 tests against the 4G ceiling scripts/run-capped.sh
    // applies: 4 workers = 2.6 GB / 48s, 8 workers = 3.8 GB / 36s, unbounded =
    // OOM. Eight buys 25% wall clock at 95% of the ceiling, which is no margin
    // for a suite that only grows; four leaves a third of the budget spare.
    //
    // A fixed count rather than a percentage of cores: this is a memory bound,
    // and a percentage scales the fleet up on exactly the machines where that
    // overshoots.
    maxWorkers: 4,
    // Console output from passing tests is dropped; failing tests keep theirs
    // in full. bits-ui's dismissible-layer reads a svelte-toolbelt box after
    // its owning effect is destroyed, and Svelte's dev build warns every time
    // — 21,130 `derived_inert` lines from Toolbar.test.ts alone, which buried
    // real failures in the CI log. The warning comes entirely from
    // node_modules (18 unique stacks, none through web/src), so there is
    // nothing here to fix; suppressing it for green runs only keeps the signal
    // for the runs that need it.
    silent: "passed-only",
    environment: "jsdom",
    include: ["src/**/*.test.ts"],
    globals: true,
    setupFiles: ["src/test-setup.ts"],
  },
});
