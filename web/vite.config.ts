import { defineConfig } from "vite";
import { svelte } from "@sveltejs/vite-plugin-svelte";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

export default defineConfig({
  plugins: [svelte(), tailwindcss()],
  resolve: {
    alias: {
      $lib: path.resolve("./src/lib"),
    },
  },
  build: {
    outDir: "dist",
    // The main chunk is ~715 kB raw / ~209 kB gzip, and that is accepted rather
    // than inherited. The SPA is served over loopback by `nibs serve` from assets
    // embedded in the binary and cached `immutable` (cmd/spa.go), so raw transfer
    // size costs almost nothing; parse/compile is the only real client cost, and
    // splitting a chunk does not reduce it. CodeMirror is already lazy — roughly
    // 630 kB across nine dynamic chunks that load with the editor.
    //
    // So this limit is a regression tripwire, not a budget: it should fire when a
    // heavy dependency lands in the eager graph by accident, not on ordinary
    // growth. To re-measure before changing it, build with --sourcemap and
    // attribute the chunk through its .map.
    chunkSizeWarningLimit: 1000,
  },
  server: {
    proxy: {
      "/graphql": "http://localhost:3000",
      "/health": "http://localhost:3000",
    },
  },
});
