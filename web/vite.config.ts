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
    // Raised from default 500: svelte-sonner + lucide icons push the main chunk slightly over
    chunkSizeWarningLimit: 600,
  },
  server: {
    proxy: {
      "/graphql": "http://localhost:3000",
      "/health": "http://localhost:3000",
    },
  },
});
