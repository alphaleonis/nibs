<script lang="ts">
  import { Link, Lock } from "@lucide/svelte";
  import { cn } from "$lib/utils.js";

  interface Props {
    /** Which relation this badge represents. */
    kind: "blocked" | "blocking";
    /** Number of related nibs — surfaced in the tooltip. */
    count: number;
    /** `pill` = tinted label pill (default); `icon` = bare icon. */
    variant?: "icon" | "pill";
    class?: string;
  }

  let { kind, count, variant = "pill", class: className = "" }: Props = $props();

  interface KindConfig {
    icon: typeof Lock;
    label: string;
    /** Tailwind utilities for the pill's tint + label color. */
    pillClasses: string;
    /** Foreground color for the bare icon variant. */
    iconColor: string;
    title: (count: number) => string;
  }

  const CONFIG: Record<Props["kind"], KindConfig> = {
    blocked: {
      icon: Lock,
      label: "Blocked",
      pillClasses: "bg-blocked-bg text-blocked",
      iconColor: "var(--blocked)",
      title: (c) => `Blocked by ${c} nib(s)`,
    },
    blocking: {
      icon: Link,
      label: "Blocking",
      pillClasses: "bg-blocking-bg text-blocking",
      iconColor: "var(--blocking)",
      title: (c) => `Blocking ${c} nib(s)`,
    },
  };

  const config = $derived(CONFIG[kind]);
  const Icon = $derived(config.icon);
  const title = $derived(config.title(count));
</script>

{#if variant === "icon"}
  <span
    data-testid={`${kind}-icon`}
    class={cn("inline-flex items-center shrink-0", className)}
    style="color: {config.iconColor};"
    {title}
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
    {title}
  >
    <Icon size={12} />
    {config.label}
  </span>
{/if}
