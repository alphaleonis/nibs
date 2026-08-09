// tsconfig sets no `types` field, so svelte-check is told about node's typings
// for the `node:fs` import below with a file-local reference, matching
// fontScaleTokens.test.ts rather than widening the project config.
/// <reference types="node" />
import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";

// Read app.css as TEXT, for two reasons that both silently produce a vacuous
// or broken test if ignored — the same pair fontScaleTokens.test.ts handles:
//
//   1. Vite's `?raw` does NOT work for .css. vitest stubs the CSS pipeline by
//      default (`test.css` is false) and `?raw` resolves to an EMPTY string, so
//      every assertion below would pass against "".
//   2. The path must reach `new URL` through a VARIABLE. Vite statically
//      rewrites the literal form `new URL("./x", import.meta.url)` as an asset
//      reference, which for a .css path yields a served URL and makes
//      readFileSync throw "The URL must be of scheme file". Indirecting through
//      a parameter is what keeps it a plain file URL.
const read = (relative: string) => readFileSync(new URL(relative, import.meta.url), "utf8");
const css = read("../app.css");

// Pins the typeface setup. The app used to declare no font-family at all and
// inherit Tailwind's default system stack, while several rules asked for weight
// 500 — and Segoe UI, what that stack resolves to on Windows, has no Medium
// face. Each engine substituted differently, so the same page rendered heavier
// in Edge than in Firefox with identical computed styles and no error anywhere.
//
// Self-hosting a variable Inter removes the class of problem: every weight the
// design might ask for exists, on every browser and OS. The declarations below
// are silent when broken — dropping --font-sans makes the @font-face rules
// inert with no warning — which is what makes them worth pinning.
//
// The companion checks that the font actually LOADS and that its weight axis
// really works need a renderer, and live in screenshots/typography.spec.ts.

describe("app typeface", () => {
  const faces = [...css.matchAll(/@font-face\s*\{[^}]*\}/g)].map((m) => m[0]);

  it("self-hosts Inter in both upright and italic", () => {
    const inter = faces.filter((f) => /font-family:\s*"Inter"/.test(f));
    expect(inter).toHaveLength(2);
    expect(inter.some((f) => /font-style:\s*normal/.test(f))).toBe(true);
    // Markdown emphasis in the nib body and the editor both use italic; with no
    // real face the browser synthesizes a slanted upright, which is not Inter's
    // drawn italic.
    expect(inter.some((f) => /font-style:\s*italic/.test(f))).toBe(true);
  });

  it("declares the full variable weight axis on every face", () => {
    // A static weight here would strip the axis and put any non-400 weight back
    // at the mercy of the browser's substitution — the original bug.
    expect(faces.length).toBeGreaterThan(0);
    for (const f of faces) expect(f).toMatch(/font-weight:\s*100\s+900/);
  });

  it("loads the faces from local assets, not a CDN", () => {
    // A remote font would make the UI depend on the network, which `nibs serve`
    // (loopback, assets embedded in the binary) otherwise never does.
    for (const f of faces) {
      expect(f).toMatch(/url\("\.\/assets\/fonts\/[^"]+\.woff2"\)/);
      expect(f).not.toMatch(/https?:/);
    }
  });

  it("points --font-sans at Inter first, with a fallback tail", () => {
    // Tailwind's preflight sets html's font-family from --default-font-family,
    // which is var(--font-sans) — so this declaration is what actually applies
    // Inter to the app. Without it the @font-face rules above are inert.
    const m = css.match(/--font-sans:\s*([^;]+);/);
    expect(m, "--font-sans is not declared").not.toBeNull();
    const stack = m![1].split(",").map((s) => s.trim().replace(/^["']|["']$/g, ""));
    expect(stack[0]).toBe("Inter");
    // A tail matters for the pre-load frame and for glyphs outside the subset.
    expect(stack.length).toBeGreaterThan(1);
    expect(stack).toContain("sans-serif");
  });
});
