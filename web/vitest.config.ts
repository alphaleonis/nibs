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
    // Node >= 26 predefines `localStorage` as an own property of globalThis,
    // valued undefined unless --localstorage-file is given. vitest's
    // populateGlobal copies jsdom's window properties onto the Node global, but
    // getWindowKeys drops any key ALREADY present there unless it is named in
    // vitest's own KEYS list — and KEYS names the `Storage` class, never the
    // `localStorage`/`sessionStorage` instances. So jsdom's storage never lands
    // and every access throws "Cannot read properties of undefined". Removing
    // Node's stub restores the shape vitest expects, where the key is absent.
    execArgv: ["--no-experimental-webstorage"],
    environment: "jsdom",
    include: ["src/**/*.test.ts"],
    globals: true,
    setupFiles: ["src/test-setup.ts"],
  },
});
