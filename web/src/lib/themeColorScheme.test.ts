// tsconfig sets no `types` field, but svelte-check still needs to be told about
// node's typings for the `node:fs` import below; a file-local reference keeps
// that scoped to this test instead of widening the project config.
/// <reference types="node" />
import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";
import { THEMES } from "./types";

// `color-scheme` is the declaration that tells the BROWSER which way a palette
// leans (nibs-kdln). Nothing else in the app carries that signal — Tailwind's
// `dark:` variant keys off the `.dark` class, which the UA cannot see — so
// without it every native control (scrollbars above all, plus `<select>` popups
// and spellcheck underlines) paints as a light-mode widget on the dark palettes.
//
// The REAL guard for this is web/e2e/theme-color-scheme.test.ts, which reads
// computed values in Chromium and so exercises the cascade. This file exists
// because that lane is not wired into CI: ci.yml runs `task test` and
// `task web:check`, neither of which reaches `task playwright`. So the e2e
// suite proves the behavior and this one keeps it from being deleted unnoticed.
// A text assertion cannot see specificity or inheritance — do not add coverage
// here that belongs in the e2e file.
const read = (relative: string) => readFileSync(new URL(relative, import.meta.url), "utf8");

// Comment-stripping is load-bearing, exactly as in fontScaleTokens.test.ts: the
// notes around these declarations QUOTE them (":root declares `color-scheme:
// dark`"), so raw-text matching would let the prose satisfy an assertion about
// the live rule — and a declaration commented out rather than deleted would keep
// this green through the precise regression it exists to catch.
const stripCssComments = (source: string) => source.replace(/\/\*[\s\S]*?\*\//g, "");

const css = stripCssComments(read("../app.css"));

// Grab a top-level rule's body by selector. Every :root block here holds plain
// declarations with no nested braces, so a non-greedy run to the first `}` is
// exact rather than merely convenient.
function findRuleBody(selector: string): string | null {
  const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  const match = css.match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`));
  return match ? match[1] : null;
}

function ruleBody(selector: string): string {
  const body = findRuleBody(selector);
  if (body === null) throw new Error(`no rule found for selector: ${selector}`);
  return body;
}

// `midnight` is the ONLY palette with no `[data-theme]` block of its own — it is
// the bare `:root` defaults, which the other three then override. So a missing
// block is legitimate here and means "inherits :root wholesale", not "regressed".
const themeBody = (value: string) => findRuleBody(`:root[data-theme="${value}"]`);

describe("theme color-scheme", () => {
  it("declares a dark color-scheme on the base :root", () => {
    // Bare `:root` also matches the element carrying data-theme, so the three
    // dark palettes inherit this and only the light one needs an override.
    expect(ruleBody(":root")).toMatch(/color-scheme:\s*dark\s*;/);
  });

  it("overrides to light on the one light palette, and only there", () => {
    const light = THEMES.filter((t) => !t.dark);
    const dark = THEMES.filter((t) => t.dark);

    // Anchored to THEMES rather than the literal "daylight" so that adding a
    // second light palette fails here instead of silently shipping a dark
    // color-scheme under a light theme.
    expect(light.length).toBeGreaterThan(0);
    for (const theme of light) {
      // A light palette MUST carry its own block — inheriting :root would leave
      // it declaring `dark`, so an absent block is a failure, not an inheritance.
      expect(themeBody(theme.value), `${theme.value} has no [data-theme] block`).not.toBeNull();
      expect(themeBody(theme.value)).toMatch(/color-scheme:\s*light\s*;/);
    }

    // The dark palettes must NOT restate it — they inherit from :root, and a
    // stray `color-scheme: light` in one of them is the regression that would
    // reintroduce the bug for that theme alone.
    for (const theme of dark) {
      expect(themeBody(theme.value) ?? "").not.toMatch(/color-scheme:\s*light/);
    }
  });

  it("paints the scrollbar from a theme token rather than a fixed color", () => {
    const root = ruleBody(":root");
    expect(root).toMatch(/scrollbar-color:\s*var\(--muted-foreground\)/);

    // The point of the declaration is that it TRACKS the palette. Pinning it to
    // `var(...)` is what distinguishes "follows the theme" from "is some color";
    // the e2e suite confirms the token actually resolves per palette.
    const scrollbar = root.match(/scrollbar-color:[^;]*/)?.[0] ?? "";
    expect(scrollbar).toContain("var(");
  });
});
