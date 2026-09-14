<script lang="ts">
  import * as Tooltip from "$lib/components/ui/tooltip/index.js";
  import type { Snippet } from "svelte";

  let {
    tooltip,
    side = "bottom",
    ariaHidden = false,
    triggerElement = "button",
    trigger,
  }: {
    /** Tooltip text only, not the accessible name. Set the trigger's own
     *  aria-label inside `trigger`, except under `ariaHidden`. */
    tooltip: string;
    side?: "top" | "right" | "bottom" | "left";
    /**
     * Hide the tooltip content from assistive technology. Set it when the trigger
     * sits inside an `aria-hidden` subtree: the content is portaled to `<body>` and
     * does not inherit that hiding.
     */
    ariaHidden?: boolean;
    /** What `trigger` renders. `"other"` strips bits-ui's `type` prop, which is
     *  valid only on a `<button>`. */
    triggerElement?: "button" | "other";
    /**
     * Renders the element the tooltip attaches to; spread the given `props` onto it.
     *
     * - CHAIN: the snippet renders a bits-ui trigger that merges props itself
     *   (`DropdownMenu.Trigger`, `Popover.Trigger`), so both components' handlers
     *   fire.
     * - OVERRIDE: the snippet renders a raw element. Anything written after
     *   `{...props}` wins — the caller's own `onclick`, or a `tabindex` over
     *   bits-ui's default of 0 — and hover/focus still open the tooltip.
     */
    trigger: Snippet<[{ props: Record<string, unknown> }]>;
  } = $props();

  // Copy with an object spread only: bits-ui's trigger attachment is stored under a
  // symbol key, which Object.entries/keys/fromEntries drop, and without it the
  // trigger never registers.
  function triggerProps(props: Record<string, unknown>): Record<string, unknown> {
    if (triggerElement === "button") return props;
    const rest = { ...props };
    delete rest.type;
    return rest;
  }
</script>

<!-- Mount inside the root a bits-ui trigger needs (e.g. <DropdownMenu.Root>).

     Keep the markup below on one line: the trigger lands in the caller's flow, where
     a newline between these tags becomes a text node and corrupts a
     `whitespace-pre` context such as Toolbar's filter-token layer. Toolbar.test.ts
     asserts that layer's exact textContent. -->
<Tooltip.Root><Tooltip.Trigger>{#snippet child({ props })}{@render trigger({ props: triggerProps(props) })}{/snippet}</Tooltip.Trigger><Tooltip.Content {side} aria-hidden={ariaHidden ? "true" : undefined}>{tooltip}</Tooltip.Content></Tooltip.Root>
