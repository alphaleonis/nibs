import { describe, it, expect } from "vitest";
import { THEMES, DEFAULT_THEME } from "./types";
import { STORAGE_KEY } from "./storage";
// Vite `?raw` import: inline index.html as a string (typed via vite/client). This
// avoids node built-ins so svelte-check stays clean without @types/node.
import html from "../../index.html?raw";

// The pre-paint FOUC guard in index.html is a self-contained, import-free
// duplicate of several TS constants (theme allowlist, storage key, default). Neither
// svelte-check nor any other test exercises that inline script, so this drift guard
// fails loudly if the duplicated values fall out of sync. See the comment block in
// index.html and the THEMES / STORAGE_KEY notes in src/lib.

describe("index.html FOUC guard stays in sync with the TS source of truth", () => {
  it("inline theme allowlist matches THEMES exactly", () => {
    // Extract the `var themes = [...]` array literal from the inline script.
    const match = html.match(/var themes = (\[[^\]]*\]);/);
    expect(match, "could not find `var themes = [...]` in index.html").not.toBeNull();
    const inlineThemes = JSON.parse(match![1].replace(/'/g, '"'));
    expect(inlineThemes).toEqual(THEMES.map((t) => t.value));
  });

  it("inline dark-flag map matches each THEME's `dark` boolean", () => {
    // Sync-point 5: the guard sets the `.dark` class before first
    // paint from `var dark = { graphite: 1, ... }`. app.css keys Tailwind's `dark:`
    // variant off that class, so a stale flag would flash light themes as dark (or
    // vice-versa) until App's $effect corrected it. Extract the object literal and
    // assert it matches THEMES exactly (1 = dark, 0 = light).
    const match = html.match(/var dark = (\{[^}]*\});/);
    expect(match, "could not find `var dark = {...}` in index.html").not.toBeNull();
    // Quote the bare object keys so the literal parses as JSON.
    const json = match![1].replace(/([A-Za-z_]\w*)\s*:/g, '"$1":');
    const inlineDark = JSON.parse(json) as Record<string, number>;
    const expected = Object.fromEntries(THEMES.map((t) => [t.value, t.dark ? 1 : 0]));
    expect(inlineDark).toEqual(expected);
  });

  it("inline storage key matches STORAGE_KEY", () => {
    expect(html).toContain(`localStorage.getItem("${STORAGE_KEY}")`);
  });

  it("inline default theme matches DEFAULT_THEME", () => {
    // The guard seeds `t` with the default and falls back to it on any error.
    expect(html).toContain(`var t = "${DEFAULT_THEME}";`);
  });

  it("inline guard reads the top-level `.theme` property, matching FilterPreferences", () => {
    // Sync-point 3: the guard depends on the persisted shape exposing `.theme` at
    // the top level (savePreferences serializes FilterPreferences flat). A future
    // rename/nesting of that field (e.g. `.theme` → `.paletteTheme`) would leave
    // the other assertions green while the guard silently fell back to the default
    // for every returning user with a saved non-default theme — reintroducing the
    // exact FOUC flash this file exists to prevent. Pin the property path.
    expect(html).toContain("JSON.parse(raw).theme");
  });
});
