<script lang="ts">
  /**
   * Header chip shown only while the live subscription is down.
   *
   * The failure it exists for is invisible by nature (nibs-1seo): a dropped
   * socket looks exactly like a nib nobody has touched, so the view silently
   * stops matching disk. It is deliberately absent while `connected` — the
   * common case, where a permanent badge would be noise — and while `connecting`,
   * which is start-up rather than a lost connection.
   */
  import { WifiOff } from "@lucide/svelte";
  import type { ConnectionStatus } from "../connectionRecovery";

  let { status }: { status: ConnectionStatus } = $props();
</script>

{#if status === "disconnected"}
  <span
    data-testid="connection-status"
    role="status"
    aria-live="polite"
    class="flex shrink-0 items-center gap-1.5 rounded-full border border-warning/40 bg-warning/10
           px-2 py-0.5 text-xs font-normal text-warning"
    title="Live updates are disconnected — this view may be behind. Reconnecting automatically."
  >
    <WifiOff size={12} aria-hidden="true" />
    Reconnecting…
  </span>
{/if}
