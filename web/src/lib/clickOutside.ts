import type { Action } from "svelte/action";

export interface ClickOutsideParams {
  /** When false, the action is inert (no callback fires). */
  enabled: boolean;
  /** Called when a pointerdown lands outside `node` (and outside `ignore`). */
  onOutside: () => void;
  /**
   * Targets also treated as "inside": typically the trigger that toggles the
   * panel (else a click on it closes and re-opens), plus content the panel
   * renders through a portal. bits-ui portals default to `document.body`, so
   * that content is not a descendant of `node`.
   *
   * An element or array matches when one `.contains(target)`; a predicate
   * matches when it returns true.
   */
  ignore?: HTMLElement | HTMLElement[] | ((target: Node) => boolean) | null;
}

/** Resolve whether `target` counts as "inside" for a given `ignore` spec. */
function isIgnored(
  ignore: ClickOutsideParams["ignore"],
  target: Node,
): boolean {
  if (!ignore) return false;
  try {
    if (typeof ignore === "function") return ignore(target);
    // Skip refs that have not mounted yet.
    if (Array.isArray(ignore)) return ignore.some((el) => !!el && el.contains(target));
    return ignore.contains(target);
  } catch {
    // A throwing predicate counts as "not inside" so dismissal still works.
    return false;
  }
}

/**
 * Svelte action: invoke `onOutside` when a pointerdown occurs outside `node`,
 * for non-modal dismissal. Listens for `pointerdown` on `document` so it fires
 * before focus shifts. Register portaled content through
 * {@link ClickOutsideParams.ignore}.
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
    if (isIgnored(current.ignore, target)) return;
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
