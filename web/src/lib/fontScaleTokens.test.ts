// tsconfig sets no `types` field, but svelte-check still needs to be told about
// node's typings for the `node:fs` import below; a file-local reference keeps
// that scoped to this test instead of widening the project config.
/// <reference types="node" />
import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";

// Read the sources as TEXT, the way fouc-guard.test.ts pins index.html's inline
// script. Vite's `?raw` is the nicer idiom and works for .svelte, but NOT for
// .css: vitest stubs the CSS pipeline by default (`test.css` is false) and both
// `?raw` and import.meta.glob({query:"?raw"}) resolve to an EMPTY string — every
// assertion here would then pass vacuously. readFileSync reads both faithfully.
const read = (relative: string) => readFileSync(new URL(relative, import.meta.url), "utf8");

// EVERY source below is comment-stripped before matching, and it is the single
// most important property of this file. Two DIFFERENT exposures make it load-bearing:
//
//   - Both dropdown files spell out their max-width cap VERBATIM in their
//     DEVIATION notes, so raw-text matching would let the prose copy satisfy an
//     assertion about the live class.
//   - app.css is exposed by another route entirely: a declaration commented out
//     rather than deleted stays in the file text, so a raw-text match would
//     report on dead code while the live token has regressed.
//
// Either way the guard would stay green through the exact regression it exists
// to catch.
const stripCssComments = (source: string) => source.replace(/\/\*[\s\S]*?\*\//g, "");
const stripSvelteComments = (source: string) =>
  source
    .replace(/<!--[\s\S]*?-->/g, "")
    .replace(/\/\*[\s\S]*?\*\//g, "")
    // Only whole-line `//` comments, so a `/` inside a class string (`bg-black/10`)
    // is never touched.
    .replace(/^[ \t]*\/\/.*$/gm, "");

const css = stripCssComments(read("../app.css"));
const buttonSource = stripSvelteComments(read("./components/ui/button/button.svelte"));
const dropdownContentSource = stripSvelteComments(
  read("./components/ui/dropdown-menu/dropdown-menu-content.svelte")
);
const dropdownSubContentSource = stripSvelteComments(
  read("./components/ui/dropdown-menu/dropdown-menu-sub-content.svelte")
);
const treeTableSource = stripSvelteComments(read("./components/TreeTable.svelte"));
const activeNibViewSource = stripSvelteComments(read("./components/ActiveNibView.svelte"));

// The global font-size preference (S/M/L) works by multiplying type tokens by
// `--font-scale`. SIX things have to carry that multiplier, and this file gates
// all six:
//
//   1. the semantic `--text-{label,body,caption}-{size,leading}` tokens in :root
//   2. Tailwind's raw `--text-*` size ladder (xs through 9xl), re-pointed in the
//      plain `@theme` block near the bottom of app.css (nibs-grbo)
//   3. ui/button's `sm` arbitrary font-size, which no `--text-*` token covers
//   4. the `max-w` cap on both ui/dropdown-menu container primitives, whose
//      clipping threshold has to track the item text it bounds
//   5. TreeTable's `--row-pad-y`, so row height tracks the text it wraps
//   6. ActiveNibView's title font-size
//
// 5 and 6 are app-owned inline `calc()` usages rather than token layers, but the
// technique here is source-text matching, so gating them costs one read each —
// and `types.ts`/`fontScale.ts`/`App.svelte` all enumerate the same six.
//
// jsdom cannot compute the cascade, so the *rendered* proof lives in the
// Playwright suite (web/screenshots/capture.spec.ts) — which only runs under
// `task screenshots`, not `task test` and not CI. This file is the gate that
// does run: it reads the sources as text and asserts the invariant
// structurally, so deleting the @theme block, adding an unscaled rung, or
// dropping `var(--font-scale)` from one declaration fails the normal test run.

/** Tailwind v4's full font-size ladder (node_modules/tailwindcss/theme.css). */
const TAILWIND_TEXT_RUNGS = [
  "xs", "sm", "base", "lg", "xl",
  "2xl", "3xl", "4xl", "5xl", "6xl", "7xl", "8xl", "9xl",
] as const;

/** The semantic tokens that are themselves scaled, so aliasing one scales too. */
const SCALED_SEMANTIC_TOKENS = ["--text-body-size", "--text-caption-size", "--text-label-size"];

/**
 * `--font-scale` used as a MULTIPLICATIVE factor. Containment alone is not
 * enough: `calc(1.5rem + var(--font-scale))` mentions the variable but adds a
 * unitless 1 to a length, so the rung stops tracking the preference while
 * reading as if it still does.
 */
const MULTIPLIES_SCALE = /\*\s*var\(--font-scale\)|var\(--font-scale\)\s*\*/;

/** A bare alias of a semantic token that itself scales, e.g. `var(--text-body-size)`. */
const ALIASES_SCALED_TOKEN = new RegExp(`^var\\((?:${SCALED_SEMANTIC_TOKENS.join("|")})\\)$`);

/**
 * Extract the body of the LAST `@theme {` block (the non-inline one that owns the
 * text ladder). The `@theme inline` block at the top of the file is excluded by
 * matching `@theme` followed directly by `{`.
 */
function themeBlock(): string {
  const matches = [...css.matchAll(/@theme\s*\{([^}]*)\}/g)];
  expect(matches.length, "expected exactly one plain `@theme { ... }` block in app.css").toBe(1);
  return matches[0][1];
}

/** All `--name: value` declarations in a (comment-stripped) CSS block body. */
function declarations(block: string): Map<string, string> {
  const out = new Map<string, string>();
  for (const [, name, value] of block.matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)) {
    out.set(name, value.trim());
  }
  return out;
}

/**
 * The value of the LAST declaration of `name` in a comment-stripped source.
 * CSS is last-wins, so a duplicate declaration below the intended one is what
 * actually renders — matching the first hit would report on dead code.
 */
function liveDeclaration(source: string, name: string): string | undefined {
  const hits = [...source.matchAll(new RegExp(`(?<![\\w-])${name}\\s*:\\s*([^;]+);`, "g"))];
  // Taking the LAST hit models CSS last-wins, but it is only sound while a token
  // is declared once. With a broken base declaration and a scaling one in a later
  // `:root[data-theme=…]` block this would report green on the override while the
  // base is regressed, so pin the count rather than relying on that.
  expect(hits.length, `${name} must be declared exactly once`).toBeLessThanOrEqual(1);
  return hits.length ? hits[hits.length - 1][1].trim() : undefined;
}

/** True if `value` scales: multiplies --font-scale, or aliases a token that does. */
function scales(value: string): boolean {
  return MULTIPLIES_SCALE.test(value) || ALIASES_SCALED_TOKEN.test(value);
}

describe("app.css keeps every type-size token wired to --font-scale", () => {
  it("the semantic type tokens all multiply by --font-scale", () => {
    // These are the layer the @theme rungs below delegate to, so if they stop
    // scaling the delegation silently stops scaling with them.
    for (const role of ["label", "body", "caption"]) {
      for (const part of ["size", "leading"]) {
        const name = `--text-${role}-${part}`;
        const value = liveDeclaration(css, name);
        expect(value, `${name} not declared in app.css`).toBeDefined();
        expect(
          MULTIPLIES_SCALE.test(value!),
          `${name} = "${value}" must MULTIPLY by --font-scale`
        ).toBe(true);
      }
    }
  });

  it("covers Tailwind's FULL --text-* ladder, with no unscaled rung", () => {
    const decls = declarations(themeBlock());
    for (const rung of TAILWIND_TEXT_RUNGS) {
      const name = `--text-${rung}`;
      const value = decls.get(name);
      // A missing rung falls back to Tailwind's fixed default, so the first use
      // of that step would silently ignore the preference (nibs-grbo).
      expect(value, `${name} must be overridden in app.css's @theme block`).toBeDefined();
      expect(scales(value!), `${name} = "${value}" does not resolve --font-scale`).toBe(true);
    }
  });

  it("binds --text-xs/--text-sm to the semantic tokens rather than repeating the literal", () => {
    // The two layers must not drift: retuning --text-body-size has to move
    // `text-sm` with it, or the vendored primitives desynchronize from body text.
    const decls = declarations(themeBlock());
    expect(decls.get("--text-xs")).toBe("var(--text-caption-size)");
    expect(decls.get("--text-sm")).toBe("var(--text-body-size)");
  });

  it("leaves the --text-*--line-height companions alone", () => {
    // Tailwind's companions are UNITLESS ratios applied on top of the (already
    // scaled) size, so overriding them here would apply --font-scale twice.
    for (const name of declarations(themeBlock()).keys()) {
      expect(name, "line-height companions must not be overridden").not.toMatch(/--line-height$/);
    }
  });
});

describe("the vendored primitives that hand-carry --font-scale", () => {
  // Every class string below is written as a REGEX with escaped brackets and
  // parens. Tailwind v4 scans .ts files as plain text (verified: a bare class
  // name in a comment here IS emitted into the bundle), so an unescaped literal
  // would make the scanner mint a real utility out of a test fixture.

  it("scales the button `sm` rung, the one arbitrary font-size outside the ladder", () => {
    // ui/button pins size="sm" to an intermediate 0.8rem that no --text-* token
    // covers; a bare arbitrary font-size there is invisible to the preference
    // and inverts the xs < sm < default ladder at Small and Large.
    expect(buttonSource).toMatch(/text-\[length:calc\(0\.8rem\*var\(--font-scale\)\)\]/);
    expect(buttonSource, "no unscaled arbitrary font-size may remain").not.toMatch(
      /text-\[0\.8rem\]/
    );
  });

  it("scales the max-width cap on BOTH dropdown container primitives", () => {
    // The cap bounds size-to-content menus so an unbounded user tag cannot push
    // one past the viewport; items grow 15% at Large, so a fixed rem cap would
    // start hard-clipping labels that fit at Medium. The two files are an
    // explicit pair (sub-content's comment claims the same width contract), so
    // they are asserted together — scaling one alone silently splits them.
    for (const [name, source] of [
      ["dropdown-menu-content", dropdownContentSource],
      ["dropdown-menu-sub-content", dropdownSubContentSource],
    ] as const) {
      expect(source, `${name} must scale its max-w cap by --font-scale`).toMatch(
        /max-w-\[min\(calc\(20rem\*var\(--font-scale\)\),calc\(100vw-1rem\)\)\]/
      );
      expect(source, `${name} must not keep an unscaled rem cap`).not.toMatch(
        /max-w-\[min\(20rem,/
      );
    }
  });

  // Consumers 5 and 6: app-owned inline calc() usages rather than token layers.
  // No --text-* token reaches either, so nothing above would notice them losing
  // the multiplier — row height would stop tracking the text it wraps, and the
  // detail title would stop tracking every other heading.
  it("scales the app-owned inline calc() consumers", () => {
    for (const [name, source, property] of [
      ["TreeTable --row-pad-y", treeTableSource, "--row-pad-y"],
      ["ActiveNibView title", activeNibViewSource, "font-size"],
    ] as const) {
      const declarations = [...source.matchAll(new RegExp(`${property}\\s*:\\s*([^;"]+)`, "g"))]
        .map((m) => m[1].trim())
        .filter((value) => value.includes("var(--font-scale)"));
      expect(declarations.length, `${name}: no declaration references --font-scale`).toBeGreaterThan(0);
      for (const value of declarations) {
        expect(MULTIPLIES_SCALE.test(value), `${name} = "${value}" must MULTIPLY by --font-scale`).toBe(true);
      }
    }
  });
});
