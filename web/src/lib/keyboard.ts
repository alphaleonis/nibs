import { tinykeys } from "tinykeys";

/**
 * Replaces tinykeys' default filter, which drops repeated and IME-composing
 * events and any event whose target is an input, select, textarea or
 * contenteditable other than the listener's element.
 *
 * Each binding decides whether to yield to a focused input (`isInputFocused()`
 * in useKeyboardShortcuts); Escape does not, so it can close an editor that
 * holds the caret.
 */
const NO_IGNORE = () => false;

/**
 * A mapping from key combo strings (tinykeys format) to handler functions.
 * Examples: "Escape", "$mod+k", "Control+Shift+N"
 */
export type ShortcutMap = Record<string, (e: KeyboardEvent) => void>;

/** A registered shortcut and its description. */
export interface ShortcutEntry {
  combo: string;
  description: string;
}

// Descriptions per combo; a Set because several callers may register one combo.
const registry = new Map<string, Set<string>>();

/**
 * Returns a snapshot of all currently registered shortcuts, one entry per
 * description.
 */
export function getRegisteredShortcuts(): ShortcutEntry[] {
  const entries: ShortcutEntry[] = [];
  for (const [combo, descriptions] of registry) {
    for (const description of descriptions) {
      entries.push({ combo, description });
    }
  }
  return entries;
}

/**
 * Register shortcuts with descriptions in the registry, then bind them via
 * tinykeys. Returns an unsubscribe function that removes both the tinykeys
 * listener and the registry entries.
 */
export function bindShortcuts(
  target: HTMLElement | Window,
  shortcuts: ShortcutMap,
  descriptions?: Record<string, string>,
): () => void {
  if (descriptions) {
    for (const [combo, desc] of Object.entries(descriptions)) {
      let set = registry.get(combo);
      if (!set) {
        set = new Set();
        registry.set(combo, set);
      }
      set.add(desc);
    }
  }

  const unsubscribe = tinykeys(target, shortcuts, { ignore: NO_IGNORE });

  return () => {
    unsubscribe();
    if (descriptions) {
      for (const [combo, desc] of Object.entries(descriptions)) {
        const set = registry.get(combo);
        if (set) {
          set.delete(desc);
          if (set.size === 0) {
            registry.delete(combo);
          }
        }
      }
    }
  };
}

/** `bindShortcuts` on `window`. Returns an unsubscribe function. */
export function bindGlobalShortcuts(
  shortcuts: ShortcutMap,
  descriptions?: Record<string, string>,
): () => void {
  return bindShortcuts(window, shortcuts, descriptions);
}

/**
 * Svelte action binding shortcuts to an element: `<div use:shortcuts={map}>`.
 * Not added to the registry. The map is bound once at mount; for dynamic
 * shortcuts, call `bindShortcuts` in an `$effect`.
 */
export function shortcuts(
  node: HTMLElement,
  shortcutMap: ShortcutMap,
): { destroy: () => void } {
  const unsubscribe = tinykeys(node, shortcutMap, { ignore: NO_IGNORE });
  return {
    destroy: unsubscribe,
  };
}
