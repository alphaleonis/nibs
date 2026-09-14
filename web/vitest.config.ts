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
    // A memory bound, so a fixed count rather than a share of cores: each worker
    // is a jsdom environment, and the lane runs under scripts/run-capped.sh's
    // 4G default ceiling. Measured at 2.6 GB with 4 workers; unbounded, the
    // fleet exceeded 6 GB on a 24-core machine.
    maxWorkers: 4,
    // bits-ui floods passing runs with Svelte `derived_inert` dev warnings from
    // node_modules; failing tests keep their console output.
    silent: "passed-only",
    environment: "jsdom",
    include: ["src/**/*.test.ts"],
    globals: true,
    setupFiles: ["src/test-setup.ts"],
  },
});
