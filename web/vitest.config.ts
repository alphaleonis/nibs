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
    environment: "jsdom",
    include: ["src/**/*.test.ts"],
    globals: true,
    setupFiles: ["src/test-setup.ts"],
  },
});
