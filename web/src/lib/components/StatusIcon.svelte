<script lang="ts">
  import { Circle } from "@lucide/svelte";
  import { statusIcons } from "../icons";
  import { statusDotColors } from "../badges";
  import { cn } from "$lib/utils.js";

  interface Props {
    status: string;
    size?: number;
    class?: string;
  }

  let { status, size = 14, class: className = "" }: Props = $props();

  // Fall back to a plain circle + muted tint for any unknown status.
  let Icon = $derived(statusIcons[status] ?? Circle);
  let color = $derived(statusDotColors[status] ?? "var(--muted-foreground)");
</script>

<!-- Decorative: lucide sets aria-hidden. Where no status text is adjacent (e.g.
     related-nib lists) the glyph is the only status cue, an open a11y gap. -->
<Icon
  {size}
  data-testid="status-icon"
  style="color: {color}; display: inline;"
  class={cn("inline shrink-0", className)}
/>
