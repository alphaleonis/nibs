import { tinykeys } from "tinykeys";

/**
 * Disables tinykeys' own target filtering, which otherwise drops key events
 * whose target matches `[contenteditable],input,select,textarea` unless that
 * target is also the listener's element — so for these window-bound bindings
 * every keypress made while an input holds focus.
 *
 * Which shortcuts yield to a focused input is this app's decision to make one
 * at a time, not the library's to make for all of them: `n` and `e` opt out
 * while typing via `isInputFocused()`, whereas `Escape` deliberately does not,
 * because the Escape hierarchy (close view -> deselect -> clear focus) has to
 * close an open editor while the caret is still in it. A blanket filter
 * underneath those guards would silently override that choice.
 */
const NO_IGNORE = () => false;

/**
 * A mapping from key combo strings (tinykeys format) to handler functions.
 * Examples: "Escape", "$mod+k", "Control+Shift+N"
 */
export type ShortcutMap = Record<string, (e: KeyboardEvent) => void>;

/**
 * Metadata for a registered shortcut, used by a future help overlay.
 */
export interface ShortcutEntry {
  combo: string;
  description: string;
}

// Internal registry tracking all active shortcuts with descriptions.
// Uses a Set per combo to handle multiple callers registering the same key.
const registry = new Map<string, Set<string>>();

/**
 * Returns a snapshot of all currently registered shortcuts.
 * If multiple descriptions exist for the same combo, each gets its own entry.
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
  // Register descriptions
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

/**
 * Bind shortcuts to the global window object. Convenience wrapper around
 * bindShortcuts. Returns an unsubscribe function.
 */
export function bindGlobalShortcuts(
  shortcuts: ShortcutMap,
  descriptions?: Record<string, string>,
): () => void {
  return bindShortcuts(window, shortcuts, descriptions);
}

/**
 * Svelte action that binds keyboard shortcuts to an element.
 * Usage: <div use:shortcuts={shortcutMap}>
 *
 * Element-scoped shortcuts intentionally skip the global registry since they
 * are contextual to specific UI elements and shouldn't appear in a help overlay.
 * For globally discoverable shortcuts, use `bindShortcuts` or `bindGlobalShortcuts`.
 *
 * The shortcut map is bound once at mount. If you need dynamic shortcuts,
 * use `bindShortcuts` directly in an `$effect`.
 *
 * Automatically cleans up on element destroy.
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
