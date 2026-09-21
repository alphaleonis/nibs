<script lang="ts">
  /**
   * One styled tooltip for a whole container, attached by delegation: any element
   * under `container` carrying `data-tooltip="<text>"` reveals it on hover.
   *
   * A <WithTooltip> per element mounts a bits-ui Provider and Root each, so the
   * table's cost scales with the store; one root per container does not. TreeTable
   * already delegates clicks off the same element. The per-cell alternative was
   * measured once in jsdom — mounting a 301-row table with the five truncating
   * columns wrapped (1505 roots) went from 0.91s to 2.97s. That is a
   * component-instantiation comparison, not a browser rendering number, and the
   * bench file is not in the tree to re-run.
   *
   * The reveal is pointer-only: a delegating element is not a tooltip trigger and
   * gains no `aria-describedby`, so each one must still name itself to assistive
   * technology through its own text or `aria-label`. `expectDelegatingElementsAreNamed`
   * in `$lib/testing/nativeTitle` is what holds that.
   */
  import * as Tooltip from "$lib/components/ui/tooltip/index.js";

  /** Elements opt in by carrying this attribute; markup writes it literally. */
  const TOOLTIP_ATTR = "data-tooltip";

  let {
    container = null,
    side = "bottom",
    delay = 300,
    suppressed = undefined,
  }: {
    /** The element to delegate from; tooltips are read off its descendants. */
    container?: HTMLElement | null;
    side?: "top" | "right" | "bottom" | "left";
    /** Hover dwell before the first reveal, matching the app's Tooltip.Provider. */
    delay?: number;
    /**
     * Read before every reveal: true holds the tooltip shut. A getter, so the
     * caller's reactive state is read at hover time rather than captured here.
     */
    suppressed?: (() => boolean) | undefined;
  } = $props();

  let text = $state("");
  let open = $state(false);
  // The hovered element's viewport rect, which the anchor below mirrors so
  // floating-ui positions against the element rather than the pointer.
  let box = $state({ x: 0, y: 0, w: 0, h: 0 });

  let hovered: HTMLElement | null = null;
  let timer: ReturnType<typeof setTimeout> | null = null;

  function cancel() {
    if (timer !== null) {
      clearTimeout(timer);
      timer = null;
    }
  }

  function close() {
    cancel();
    hovered = null;
    open = false;
  }

  function show(el: HTMLElement, value: string) {
    const r = el.getBoundingClientRect();
    box = { x: r.left, y: r.top, w: r.width, h: r.height };
    text = value;
    open = true;
  }

  function handlePointerOver(e: Event) {
    const target = e.target;
    const el = target instanceof Element ? (target.closest(`[${TOOLTIP_ATTR}]`) as HTMLElement | null) : null;
    if (el === hovered) return;
    const value = el?.getAttribute(TOOLTIP_ATTR) ?? "";
    if (el === null || value === "") {
      close();
      return;
    }
    // A drag keeps reaching this listener: useTreeDrag tracks on `window` without
    // pointer capture and its ghost takes no pointer events, so pausing over a
    // cell mid-drag would raise the tooltip over the drop indicator.
    if (suppressed?.() === true) {
      close();
      return;
    }
    cancel();
    // Already showing: move to the new target at once. Re-waiting the dwell would
    // read as the tooltip lagging the pointer along a row.
    if (open) {
      hovered = el;
      show(el, value);
      return;
    }
    // Set before the timer starts, so the observer below also speaks for the
    // dwell: it closes, which cancels the timer.
    hovered = el;
    timer = setTimeout(() => {
      timer = null;
      show(el, value);
    }, delay);
  }

  $effect(() => {
    const c = container;
    if (!c) return;
    // `pointerover` rather than `pointerenter`: only the bubbling one reaches a
    // delegating listener.
    c.addEventListener("pointerover", handlePointerOver);
    c.addEventListener("pointerleave", close);
    c.addEventListener("pointerdown", close);
    // Captured, because `scroll` does not bubble and the scroller is often a
    // descendant rather than `container` itself — ActiveNibView passes `.anv`,
    // which is `overflow: hidden`, while `.anv-body` and `.anv-rail` each scroll.
    c.addEventListener("scroll", close, true);
    // Nothing above fires when a re-render removes the hovered element under a
    // stationary pointer, which strands the tooltip on a stale rect over whatever
    // took its place — or, mid-dwell, opens it at the viewport corner, since a
    // detached node's rect is all zeros. A MutationObserver callback is a
    // microtask, so it lands before the dwell's next macrotask either way.
    const observer = new MutationObserver(() => {
      if (hovered !== null && !hovered.isConnected) close();
    });
    observer.observe(c, { childList: true, subtree: true });
    return () => {
      c.removeEventListener("pointerover", handlePointerOver);
      c.removeEventListener("pointerleave", close);
      c.removeEventListener("pointerdown", close);
      c.removeEventListener("scroll", close, true);
      observer.disconnect();
      cancel();
    };
  });

  // Escape closes through bits-ui's own escape layer, which flips `open` under
  // `bind:open` without reaching `close()`. Left set, `hovered` would make the
  // identity check in `handlePointerOver` refuse to reopen on the same element.
  // A pending timer is the dwell, where `open` is legitimately still false.
  $effect(() => {
    if (!open && timer === null) hovered = null;
  });

  /** bits-ui's trigger defaults `type="button"`, which no `<div>` accepts. Copy
   *  with an object spread only, for the reason WithTooltip's `triggerProps` gives. */
  function anchorProps(props: Record<string, unknown>): Record<string, unknown> {
    const rest = { ...props };
    delete rest.type;
    return rest;
  }
</script>

<Tooltip.Root bind:open>
  <Tooltip.Trigger>
    {#snippet child({ props })}
      <!-- A stand-in for whichever element is hovered: bits-ui positions the content
           against its trigger, so the one trigger tracks that element's rect. It
           takes no pointer events, so hovering it can neither open nor hold the
           tooltip — `open` above is the only thing that does. -->
      <div
        {...anchorProps(props)}
        data-testid="delegated-tooltip-anchor"
        aria-hidden="true"
        tabindex="-1"
        style="position: fixed; pointer-events: none; left: {box.x}px; top: {box.y}px; width: {box.w}px; height: {box.h}px;"
      ></div>
    {/snippet}
  </Tooltip.Trigger>
  <Tooltip.Content {side}>{text}</Tooltip.Content>
</Tooltip.Root>
