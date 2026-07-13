import { FONT_SCALES } from "./types";
import type { FontSize } from "./types";

/**
 * Apply the global font-size preference by writing its multiplier onto the
 * `--font-scale` custom property on <html>. app.css multiplies ONLY the three
 * semantic type-SIZE tokens (label/body/caption) by this value, so the whole UI
 * type scale grows/shrinks while layout rem, spacing, and row-density stay put
 * (nibs-gymz). A tiny reflow on change is acceptable — unlike theme color, this
 * needs no pre-paint FOUC guard.
 *
 * Kept as a tiny pure helper (mirrors applyTheme in theme.ts) so it can be
 * unit-tested directly; App.svelte drives it from a $effect on prefs.fontSize.
 */
export function applyFontScale(fontSize: FontSize): void {
  document.documentElement.style.setProperty("--font-scale", String(FONT_SCALES[fontSize]));
}
