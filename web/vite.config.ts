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
    // A tripwire for a heavy dependency landing in the eager graph, not a size
    // budget: `nibs serve` embeds the assets and serves them from loopback.
    // CodeMirror stays lazy (MarkdownEditor's dynamic imports). To attribute
    // the chunk, build with --sourcemap.
    chunkSizeWarningLimit: 1000,
  },
  server: {
    proxy: {
      "/graphql": "http://localhost:3000",
      "/health": "http://localhost:3000",
    },
  },
});
