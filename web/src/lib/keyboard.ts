import { tinykeys } from "tinykeys";

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

  const unsubscribe = tinykeys(target, shortcuts);

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
  const unsubscribe = tinykeys(node, shortcutMap);
  return {
    destroy: unsubscribe,
  };
}
