<script lang="ts">
  import { cn } from "$lib/utils.js";
  import { RELATION_CONFIG, type RelationKind } from "../relations";

  interface Props {
    /** Which relation this badge represents. */
    kind: RelationKind;
    /** Number of related nibs — surfaced in the tooltip. */
    count: number;
    /** `pill` = tinted label pill (default); `icon` = bare icon. */
    variant?: "icon" | "pill";
    class?: string;
  }

  let { kind, count, variant = "pill", class: className = "" }: Props = $props();

  // The count is revealed through `data-tooltip`, so a badge only shows one under
  // a <DelegatedTooltip> container — the table and the detail panel, its two
  // callers. Its own root instead would be one per badge per row.

  const config = $derived(RELATION_CONFIG[kind]);
  const Icon = $derived(config.icon);
  const tooltip = $derived(config.title(count));
</script>

{#if variant === "icon"}
  <span
    data-testid={`${kind}-icon`}
    class={cn("inline-flex items-center shrink-0", className)}
    style="color: {config.iconColor};"
    data-tooltip={tooltip}
    aria-label={tooltip}
  >
    <Icon size={12} />
  </span>
{:else}
  <span
    data-testid={`${kind}-badge`}
    class={cn(
      "inline-flex items-center gap-1 rounded-sm px-1.5 py-0.5 text-xs font-medium shrink-0",
      config.pillClasses,
      className,
    )}
    data-tooltip={tooltip}
  >
    <Icon size={12} />
    {config.label}
  </span>
{/if}
