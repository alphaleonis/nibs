// tinykeys ships type declarations (dist/tinykeys.d.ts), but its package.json
// `exports` map has no `types` condition, so bundler-mode module resolution
// can't discover them and reports an implicit-any import. Re-declare the small,
// stable public surface we depend on. Keep in sync with the installed version.
declare module "tinykeys" {
  export interface KeyBindingMap {
    [keybinding: string]: (event: KeyboardEvent) => void;
  }

  export interface KeyBindingHandlerOptions {
    /** Cancel a keybinding sequence after this many ms between presses. */
    timeout?: number;
  }

  export interface KeyBindingOptions extends KeyBindingHandlerOptions {
    /** Event to listen on (default: "keydown"). */
    event?: "keydown" | "keyup";
    /** Use a capture listener (default: false). */
    capture?: boolean;
  }

  /** Subscribes to keybindings. Returns an unsubscribe function. */
  export function tinykeys(
    target: Window | HTMLElement,
    keyBindingMap: KeyBindingMap,
    options?: KeyBindingOptions,
  ): () => void;
}
