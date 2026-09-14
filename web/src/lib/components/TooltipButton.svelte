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
    /** Tooltip text and the button's aria-label. */
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
      <!-- Spread order matters: caller props, then the tooltip's so a forwarded
           prop cannot clobber its handlers, then `onclick` last so the button's
           action replaces the tooltip's close-on-click handler. -->
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
