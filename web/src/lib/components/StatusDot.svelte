<script lang="ts">
  import { statusDotColors } from "../badges";

  interface Props {
    status: string;
    class?: string;
  }

  let { status, class: className = "" }: Props = $props();

  let color = $derived(statusDotColors[status] ?? "var(--muted-foreground)");
</script>

<span
  data-testid="status-dot"
  class="status-dot {className}"
  style:background-color={color}
></span>

<style>
  .status-dot {
    display: inline-block;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;
  }
</style>
