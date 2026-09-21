<script lang="ts">
  /**
   * Header chip shown only after a lost connection: a dropped socket otherwise
   * looks like a nib nobody has touched. `connecting` is start-up, not shown.
   */
  import { WifiOff } from "@lucide/svelte";
  import WithTooltip from "./WithTooltip.svelte";
  import type { ConnectionStatus } from "../connectionRecovery";

  let { status }: { status: ConnectionStatus } = $props();

  const EXPLANATION = "Live updates are disconnected — this view may be behind. Reconnecting automatically.";
</script>

{#if status === "disconnected"}
  <!-- A lone chip in the toolbar, so it carries its own tooltip rather than
       delegating; the bits-ui trigger's default tabindex also puts the
       explanation within reach of the keyboard, which `title` never was. -->
  <WithTooltip tooltip={EXPLANATION} triggerElement="other">
    {#snippet trigger({ props })}
      <span
        {...props}
        data-testid="connection-status"
        role="status"
        aria-live="polite"
        class="flex shrink-0 items-center gap-1.5 rounded-full border border-warning/40 bg-warning/10
               px-2 py-0.5 text-xs font-normal text-warning"
      >
        <WifiOff size={12} aria-hidden="true" />
        Reconnecting…
      </span>
    {/snippet}
  </WithTooltip>
{/if}
