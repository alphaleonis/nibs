<script lang="ts">
  import * as Tooltip from "$lib/components/ui/tooltip/index.js";
  import { buttonVariants, type ButtonSize, type ButtonVariant } from "$lib/components/ui/button/index.js";
  import { cn } from "$lib/utils.js";
  import type { Snippet } from "svelte";
  import type { HTMLButtonAttributes } from "svelte/elements";

  let {
    label,
    side = "bottom",
    variant = "ghost",
    size = "icon",
    type = "button",
    class: className,
    onclick,
    ref = $bindable(null),
    children,
    ...restProps
  }: Omit<HTMLButtonAttributes, "class" | "aria-label" | "type"> & {
    /** Tooltip text AND the button's aria-label — kept in sync by construction.
     *  aria-label is owned by this prop, so it is not accepted via restProps. */
    label: string;
    side?: "top" | "right" | "bottom" | "left";
    variant?: ButtonVariant;
    size?: ButtonSize;
    /** Overridable; defaults to "button" so the tooltip button never submits a form. */
    type?: HTMLButtonAttributes["type"];
    class?: string;
    /** Bind to reach the underlying <button> (focus, contains, …). */
    ref?: HTMLButtonElement | null;
    children: Snippet;
  } = $props();
</script>

<Tooltip.Root>
  <Tooltip.Trigger>
    {#snippet child({ props })}
      <!-- OVERRIDE semantics. Spread order is load-bearing: caller `{...restProps}`
           FIRST, then the tooltip's `{...props}` (its hover/focus attachment) so
           those handlers can never be clobbered by a forwarded prop, then our
           explicit attributes, and `onclick` LAST so an explicit click action
           OVERRIDES the tooltip's own (inert, close-on-click) handler — the
           button's action always wins. (Contrast WithTooltip's CHAIN mode, where
           a bits-ui trigger merges the tooltip's handlers with its own.) The tooltip
           still opens on hover/focus because those handlers live in props and are
           not overridden. -->
      <button
        {...restProps}
        {...props}
        bind:this={ref}
        {type}
        aria-label={label}
        class={cn(buttonVariants({ variant, size }), className)}
        {onclick}
      >
        {@render children()}
      </button>
    {/snippet}
  </Tooltip.Trigger>
  <Tooltip.Content {side}>{label}</Tooltip.Content>
</Tooltip.Root>
