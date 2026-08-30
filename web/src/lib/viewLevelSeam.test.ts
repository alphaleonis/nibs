// tsconfig sets no `types` field, so svelte-check is told about node's typings
// for the `node:fs` import below with a file-local reference, matching
// typography.test.ts rather than widening the project config.
/// <reference types="node" />
import { describe, it, expect } from "vitest";
import { readdirSync, readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { join } from "node:path";

// Reads the app's own source as TEXT. The path must reach `new URL` through a
// VARIABLE: Vite statically rewrites the literal form
// `new URL("./x", import.meta.url)` into an asset reference, which stops it
// being a plain file URL. Same trap typography.test.ts documents for app.css.
const resolveFromHere = (relative: string) => fileURLToPath(new URL(relative, import.meta.url));
const SRC = resolveFromHere("../");

/**
 * Switching the view level is not a plain preference write: a grouping lens
 * hides containers ranked above its tier, so the switch can leave the selected
 * and focused rows with no row to be on. `switchViewLevel` is the seam that
 * pairs the write with that reconcile — and a seam is worth only as much as the
 * bypasses it forbids. Toolbar wrote `prefs.viewLevel` directly until this
 * existed, which is exactly the shape this refuses.
 *
 * The two allowlisted files are the write's own implementation:
 *   - preferences.svelte.ts — constructor hydration from storage, which is boot,
 *     not a switch, and must fire no transition.
 *   - resolvePrefs.ts — the seam itself.
 */
const ALLOWED = ["lib/preferences.svelte.ts", "lib/resolvePrefs.ts"];

// `=(?!=)` rather than a bare `=`: the bare form also matches `==` and `===`, so
// it would report every comparison as a bypass — or, worse, pass today only
// because no comparison happens to be written with a space before the operator.
const DIRECT_WRITE = /\.viewLevel\s*=(?!=)/;

// Test files are out of scope: they construct state directly on purpose and ship
// nothing. Generated clients are machine-written and never carry app logic.
const SKIP_DIRS = new Set(["generated", "gql"]);
const isTestFile = (name: string) => /\.(test|spec)\.(ts|js)$/.test(name);
const isSource = (name: string) => /\.(ts|svelte)$/.test(name) && !isTestFile(name);

/** Every app source file under `dir`, as paths relative to src/. */
function sourceFiles(dir: string, prefix = ""): string[] {
  const found: string[] = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      if (SKIP_DIRS.has(entry.name)) continue;
      found.push(...sourceFiles(join(dir, entry.name), `${prefix}${entry.name}/`));
    } else if (entry.isFile() && isSource(entry.name)) {
      found.push(`${prefix}${entry.name}`);
    }
  }
  return found;
}

describe("the view-level write seam", () => {
  const files = sourceFiles(SRC);

  it("scans the app source (a vacuous pass is this test's own failure mode)", () => {
    // If the walk resolved nowhere, the assertion below would pass against an
    // empty list and report a seam that is not being checked at all.
    expect(files.length).toBeGreaterThan(50);
    expect(files).toContain("lib/components/Toolbar.svelte");
    expect(files).toContain("App.svelte");
  });

  it("matches a write and not a comparison", () => {
    expect(DIRECT_WRITE.test('prefs.viewLevel = "epics";')).toBe(true);
    expect(DIRECT_WRITE.test("this.viewLevel  =  initial.viewLevel;")).toBe(true);
    expect(DIRECT_WRITE.test("if (prefs.viewLevel === level) return;")).toBe(false);
    expect(DIRECT_WRITE.test("if (a.viewLevel == b) return;")).toBe(false);
    expect(DIRECT_WRITE.test('{ viewLevel: "none" }')).toBe(false);
  });

  it("routes every view-level write through switchViewLevel", () => {
    const offenders = files.filter(
      (file) => !ALLOWED.includes(file) && DIRECT_WRITE.test(readFileSync(join(SRC, file), "utf8")),
    );

    expect(offenders, "write these through switchViewLevel(), not straight at the preference").toEqual([]);
  });

  it("allowlists only files that still hold the write", () => {
    // Keeps the allowlist from rotting into a standing exemption for a file that
    // no longer writes the level at all.
    for (const file of ALLOWED) {
      expect(DIRECT_WRITE.test(readFileSync(join(SRC, file), "utf8")), file).toBe(true);
    }
  });

  // `treeView` is optional on both `switchViewLevel` and Toolbar — a control can
  // render with no table to reconcile, which ~110 Toolbar test renders rely on.
  // The cost is that App's is the ONE production wire and dropping it type-checks
  // and leaves every behavioral assertion green, silently reducing the switch to
  // the bare preference write it used to be.
  it("hands App's Toolbar the tree view the reconcile runs on", () => {
    const app = readFileSync(join(SRC, "App.svelte"), "utf8");
    const start = app.indexOf("<Toolbar");
    expect(start, "App.svelte no longer renders <Toolbar>").toBeGreaterThan(-1);
    const end = app.indexOf("/>", start);
    expect(end, "App.svelte's <Toolbar> is no longer self-closing").toBeGreaterThan(-1);
    const tag = app.slice(start, end);

    expect(
      /\{treeView\}|treeView=\{/.test(tag),
      "pass {treeView} to Toolbar — without it switchViewLevel writes the level and reconciles nothing",
    ).toBe(true);
  });
});
