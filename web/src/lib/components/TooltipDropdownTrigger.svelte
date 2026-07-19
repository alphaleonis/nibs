<script lang="ts">
  import * as Tooltip from "$lib/components/ui/tooltip/index.js";
  import type { Snippet } from "svelte";

  let {
    tooltip,
    side = "bottom",
    trigger,
  }: {
    /** Tooltip text ONLY (not the accessible name — unlike TooltipButton.label).
     *  The caller MUST set the trigger's own aria-label inside the `trigger`
     *  snippet (usually to the same string). Named `tooltip`, not `label`, to
     *  make that contract explicit. */
    tooltip: string;
    side?: "top" | "right" | "bottom" | "left";
    /**
     * Renders the `DropdownMenu.Trigger`. Receives the `Tooltip.Trigger`'s `props`
     * (its hover/focus attachment + a11y attrs); the caller spreads them onto a
     * `DropdownMenu.Trigger {...props}`.
     *
     * CHAIN semantics: the caller's `DropdownMenu.Trigger` is a bits-ui component
     * that internally runs `mergeProps(restProps, triggerState.props)`, so the
     * tooltip's spread handlers CHAIN with the dropdown's own open handlers — both
     * fire (hover/focus opens the tooltip; click/keydown opens the menu). This is
     * the opposite of `TooltipButton`, where an explicit `onclick` OVERRIDES.
     */
    trigger: Snippet<[{ props: Record<string, unknown> }]>;
  } = $props();
</script>

<!-- Mount this INSIDE the caller's <DropdownMenu.Root> (so the `trigger` snippet's
     DropdownMenu.Trigger resolves DropdownMenu context from its lexical scope) with
     <DropdownMenu.Content> as its sibling. Only the Tooltip.* wrapping lives here. -->
<Tooltip.Root>
  <Tooltip.Trigger>
    {#snippet child({ props })}
      {@render trigger({ props })}
    {/snippet}
  </Tooltip.Trigger>
  <Tooltip.Content {side}>{tooltip}</Tooltip.Content>
</Tooltip.Root>
