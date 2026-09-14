import { THEMES } from "./types";
import type { Theme } from "./types";

/**
 * Apply a palette: set `data-theme` on <html> (app.css keys palettes off it)
 * and toggle `.dark` from the theme's `dark` flag, which Tailwind's `dark:`
 * variant keys off. Unknown themes get `.dark`.
 *
 * The FOUC guard in index.html duplicates this toggle; keep the two in sync.
 */
export function applyTheme(theme: Theme): void {
  document.documentElement.dataset.theme = theme;
  const meta = THEMES.find((t) => t.value === theme);
  document.documentElement.classList.toggle("dark", meta?.dark ?? true);
}
