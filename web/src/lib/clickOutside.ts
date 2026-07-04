import type { Action } from "svelte/action";

export interface ClickOutsideParams {
  /** When false, the action is inert (no callback fires). */
  enabled: boolean;
  /** Called when a pointerdown lands outside `node` (and outside `ignore`). */
  onOutside: () => void;
  /**
   * An element whose subtree is also treated as "inside" — typically the
   * trigger that toggles the panel, so clicking it doesn't double-fire
   * (close via outside + toggle via its own click).
   */
  ignore?: HTMLElement | null;
}

/**
 * Svelte action: invoke `onOutside` when a pointerdown occurs outside `node`.
 *
 * Uses `pointerdown` (not `click`) so it fires before focus shifts, and listens
 * on `document` so it catches interactions anywhere in the page (including
 * portaled siblings). Intended for non-modal dismissal — there is no overlay to
 * capture the click.
 */
export const clickOutside: Action<HTMLElement, ClickOutsideParams> = (
  node,
  params,
) => {
  let current = params;

  function handlePointerDown(event: Event) {
    if (!current.enabled) return;
    const target = event.target;
    if (!(target instanceof Node)) return;
    if (node.contains(target)) return;
    if (current.ignore && current.ignore.contains(target)) return;
    current.onOutside();
  }

  document.addEventListener("pointerdown", handlePointerDown);

  return {
    update(next: ClickOutsideParams) {
      current = next;
    },
    destroy() {
      document.removeEventListener("pointerdown", handlePointerDown);
    },
  };
};
