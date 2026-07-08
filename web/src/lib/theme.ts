import { THEMES } from "./types";
import type { Theme } from "./types";

/**
 * Apply a palette by (1) setting the `data-theme` attribute on <html>, which
 * app.css keys the swappable palettes off, and (2) toggling the `.dark` class to
 * match the theme's `dark` flag. Both are a live repaint with no reload. Kept as
 * a tiny pure helper so it can be unit-tested directly (App.svelte drives it from
 * a $effect on prefs.theme).
 *
 * The `.dark` class OWNS the light/dark axis (nibs-fen5): app.css wires Tailwind's
 * `dark:` variant to it (`@custom-variant dark (&:is(.dark *))`), so shadcn `dark:`
 * utilities only fire when it is present. Driving it from the per-theme flag lets a
 * light palette (e.g. Daylight) switch those utilities off. Unknown themes default
 * to dark (safe fallback). The FOUC guard in index.html duplicates this toggle so
 * the class is correct before first paint — see the THEMES note in types.ts.
 */
export function applyTheme(theme: Theme): void {
  document.documentElement.dataset.theme = theme;
  const meta = THEMES.find((t) => t.value === theme);
  document.documentElement.classList.toggle("dark", meta?.dark ?? true);
}
