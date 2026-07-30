import { FONT_SCALES } from "./types";
import type { FontSize } from "./types";

/**
 * Apply the global font-size preference by writing its multiplier onto the
 * `--font-scale` custom property on <html>. app.css multiplies the semantic
 * type-scale tokens (label/body/caption — both *-size and *-leading) and
 * Tailwind's whole raw text-size ladder (`--text-*`, xs through 9xl) by this
 * value. Components read it directly wherever no token covers the dimension:
 * TreeTable's `--row-pad-y` (so row height grows with the type), ActiveNibView's title,
 * ui/button's `sm` size, and the `max-w` cap on ui/dropdown-menu's two container
 * primitives. Root font-size, the rem unit and the spacing scale stay put — only
 * the type scale and the boxes that have to track it move. A tiny reflow on
 * change is acceptable — unlike theme color, this needs no pre-paint FOUC guard.
 *
 * Kept as a tiny pure helper (mirrors applyTheme in theme.ts) so it can be
 * unit-tested directly; App.svelte drives it from a $effect on prefs.fontSize.
 */
export function applyFontScale(fontSize: FontSize): void {
  document.documentElement.style.setProperty("--font-scale", String(FONT_SCALES[fontSize]));
}
