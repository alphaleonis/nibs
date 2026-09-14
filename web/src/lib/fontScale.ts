import { FONT_SCALES } from "./types";
import type { FontSize } from "./types";

/**
 * Apply the font-size preference as the `--font-scale` multiplier on <html>.
 * app.css scales the semantic type tokens and Tailwind's `--text-*` ladder by
 * it; rem and the spacing scale do not change. A dimension that must track the
 * type without a token multiplies by `var(--font-scale)` itself.
 *
 * Needs no pre-paint FOUC guard, unlike the theme.
 */
export function applyFontScale(fontSize: FontSize): void {
  document.documentElement.style.setProperty("--font-scale", String(FONT_SCALES[fontSize]));
}
