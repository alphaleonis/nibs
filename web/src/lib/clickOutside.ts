import type { Action } from "svelte/action";

export interface ClickOutsideParams {
  /** When false, the action is inert (no callback fires). */
  enabled: boolean;
  /** Called when a pointerdown lands outside `node` (and outside `ignore`). */
  onOutside: () => void;
  /**
   * Extra target(s) to also treat as "inside" — typically the trigger that
   * toggles the panel (so clicking it doesn't double-fire: close via outside +
   * toggle via its own click), plus any content the panel renders through a
   * Portal. Portaled content (shadcn Select/DropdownMenu/Popover default their
   * portal target to `document.body`) is NOT a DOM descendant of `node` —
   * it's a body-level sibling — so `node.contains(target)` reads it as "outside".
   * Passing the portal container element(s) or a predicate here keeps such clicks
   * from wrongly dismissing the panel. Three accepted shapes:
   *   - a single element    → inside if `el.contains(target)`
   *   - an array of elements → inside if ANY element `.contains(target)`
   *   - a predicate          → inside if it returns true for the target
   *     (e.g. `(t) => t instanceof Element && t.closest("[data-my-portal]") !== null`)
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
    // Tolerate null/undefined array entries (a consumer may hold refs that
    // haven't mounted yet) rather than throwing on `.contains`.
    if (Array.isArray(ignore)) return ignore.some((el) => !!el && el.contains(target));
    return ignore.contains(target);
  } catch {
    // This runs from a document-global pointerdown handler: a throwing
    // consumer predicate (or malformed entry) must never break dismissal
    // everywhere. Treat an errored check as "not inside" so normal
    // outside-dismissal proceeds.
    return false;
  }
}

/**
 * Svelte action: invoke `onOutside` when a pointerdown occurs outside `node`.
 *
 * Uses `pointerdown` (not `click`) so it fires before focus shifts, and listens
 * on `document` so it catches interactions anywhere in the page (including
 * portaled siblings). Intended for non-modal dismissal — there is no overlay to
 * capture the click.
 *
 * Portal-aware via `ignore`: because portaled panel content lands as a body-level
 * sibling (not a descendant of `node`), a plain `node.contains` check would treat
 * it as outside. The consumer registers such content as extra "inside" targets
 * through `ignore` — see {@link ClickOutsideParams.ignore}.
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
