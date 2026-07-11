<script lang="ts">
  import { Lock } from "@lucide/svelte";
  import { cn } from "$lib/utils.js";

  interface Props {
    /** Number of active blockers — surfaced in the tooltip. */
    count: number;
    /** `pill` = tinted "Blocked" pill (default); `icon` = bare lock icon. */
    variant?: "icon" | "pill";
    class?: string;
  }

  let { count, variant = "pill", class: className = "" }: Props = $props();

  const title = $derived(`Blocked by ${count} nib(s)`);
</script>

{#if variant === "icon"}
  <span
    data-testid="blocked-icon"
    class={cn("inline-flex items-center shrink-0", className)}
    style="color: var(--blocked);"
    {title}
  >
    <Lock size={12} />
  </span>
{:else}
  <span
    data-testid="blocked-badge"
    class={cn(
      "inline-flex items-center gap-1 rounded-sm px-1.5 py-0.5 text-xs font-medium shrink-0 bg-blocked-bg text-blocked",
      className,
    )}
    {title}
  >
    <Lock size={12} />
    Blocked
  </span>
{/if}
