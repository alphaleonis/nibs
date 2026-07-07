import type { Theme } from "./types";

/**
 * Apply a palette by setting the `data-theme` attribute on <html>. The CSS in
 * app.css keys the swappable palettes off this attribute, so this is a live
 * repaint with no reload. Kept as a tiny pure helper so it can be unit-tested
 * directly (App.svelte drives it from a $effect on prefs.theme).
 *
 * NOTE: this only swaps `data-theme`; it never touches the `.dark` class that
 * index.html hardcodes on <html>. All current THEMES are dark-only, so that is
 * fine today. Adding a LIGHT theme would require this helper (and the FOUC guard
 * in index.html) to also toggle `.dark` — see the THEMES note in types.ts and
 * nibs-fen5.
 */
export function applyTheme(theme: Theme): void {
  document.documentElement.dataset.theme = theme;
}
