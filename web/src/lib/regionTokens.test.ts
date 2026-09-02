// tsconfig sets no `types` field, but svelte-check still needs to be told about
// node's typings for the `node:fs` import below; a file-local reference keeps
// that scoped to this test instead of widening the project config.
/// <reference types="node" />
import { describe, it, expect } from "vitest";
import { readFileSync } from "node:fs";

// Read as TEXT, following fontScaleTokens.test.ts: vitest stubs the CSS pipeline
// (`test.css` is false), so `?raw` on a .css file resolves to an EMPTY string
// and every assertion here would pass vacuously.
const read = (relative: string) => readFileSync(new URL(relative, import.meta.url), "utf8");
// Comments stripped for the same reason that file gives: a declaration commented
// out rather than deleted stays in the file text, so raw-text matching would
// report on dead code while the live token has regressed.
const css = read("../app.css").replace(/\/\*[\s\S]*?\*\//g, "");
const badge = read("./components/DragBadge.svelte").replace(/<!--[\s\S]*?-->/g, "");

/**
 * The declarations inside ONE rule block, so an assertion cannot be satisfied by
 * a declaration in a different one. `--region-queue` is declared THREE times
 * across the file — the `:root` base plus the dracula and daylight overrides —
 * and a whole-file match says nothing about which palette still has it:
 * deleting the `:root` base value, which the two palettes shipping without an
 * override use, left the unscoped version green.
 *
 * The first `}` closes the block only while the block is flat, so that is
 * checked rather than assumed: an at-rule nested inside one would put the real
 * end later and silently shrink the slice to a prefix.
 */
function block(selector: string): string {
  const open = css.indexOf(selector);
  if (open === -1) throw new Error(`app.css has no \`${selector}\` block`);
  const start = open + selector.length;
  const end = css.indexOf("}", start);
  if (end === -1) throw new Error(`app.css's \`${selector}\` block is unterminated`);
  const body = css.slice(start, end);
  if (body.includes("{")) throw new Error(`app.css's \`${selector}\` block is no longer flat`);
  return body;
}

/**
 * The two color tokens the drop affordances are drawn in — the queue axis's, and
 * the one an ASSIGNMENT takes. Both are declared in the same three blocks and
 * both reach a consumer, so they are checked by one rule rather than by two
 * copies of it.
 */
const TOKENS = [
  { name: "region-queue", spelled: "border-region-queue" },
  { name: "region-assign", spelled: "border-region-assign" },
];

const declared = (name: string) => new RegExp(`^\\s*--${name}:\\s*oklch\\(`, "m");

// Tailwind v4 needs a color in TWO layers: the bare variable, and an `@theme
// inline` registration that turns it into utilities. With only the first,
// `border-region-queue` is not a class Tailwind emits at all — nothing errors,
// nothing is logged, and the border simply takes whatever the cascade leaves.
// A project rule (CLAUDE.md) that has bitten before, and the reason the badge is
// styled through the utility rather than through a `var()` in a style block: a
// `var()` reaches the variable directly and so cannot notice the registration
// going missing.
describe.each(TOKENS)("--$name", ({ name, spelled }) => {
  it("is declared as a bare variable and registered as a Tailwind color", () => {
    // The base `:root` value specifically: only dracula and daylight override
    // it, so midnight (which has no block at all) and graphite (whose block
    // declares no override) resolve through this one.
    expect(block(":root {")).toMatch(declared(name));
    expect(block("@theme inline {")).toContain(`--color-${name}: var(--${name});`);
  });

  it("is retuned for the light palette, where the dark-theme value washes out", () => {
    expect(block(':root[data-theme="daylight"] {')).toMatch(declared(name));
  });

  it("is retuned for dracula, whose accents are the palette's own", () => {
    // The third and last declaration. Without this the guard covered two of the
    // three, and deleting this override left the suite green — the shape the
    // block() scoping exists to prevent, one palette further along.
    expect(block(':root[data-theme="dracula"] {')).toMatch(declared(name));
  });

  it("is what the drag badge spells its border as", () => {
    // Pins the consumer to the registered name: renaming one half of the pair
    // without the other leaves this file the only thing that notices, since
    // Tailwind emits nothing and reports nothing for an unknown color.
    expect(badge).toContain(spelled);
  });
});

describe("the row indicator", () => {
  // The row draws through `var()` in a scoped style block rather than through a
  // utility, so the registration above is not what it depends on — the bare
  // variable is, and this is what says so.
  const rowStyles = read("./components/TreeTableRow.svelte").replace(/\/\*[\s\S]*?\*\//g, "");

  it.each(TOKENS)("paints its --$name form", ({ name }) => {
    expect(rowStyles).toContain(`var(--${name})`);
  });
});
