<script lang="ts">
  import { getContextClient, queryStore } from "@urql/svelte";
  import { ArrowUpCircle, X } from "@lucide/svelte";
  import { UPDATE_STATUS_QUERY } from "$lib/queries";
  import { isUpdateDismissed, dismissUpdate } from "$lib/updateBanner";

  const client = getContextClient();
  const result = queryStore({ client, query: UPDATE_STATUS_QUERY });

  // Bumped when the user dismisses so the banner hides immediately (the
  // persisted flag is also read on the next load via isUpdateDismissed).
  let dismissedThisSession = $state<string | null>(null);

  const status = $derived($result.data?.updateStatus as
    | { current: string; latest: string; updateAvailable: boolean }
    | undefined);

  const show = $derived(
    !!status?.updateAvailable &&
      !!status.latest &&
      dismissedThisSession !== status.latest &&
      !isUpdateDismissed(status.latest),
  );

  function handleDismiss() {
    if (status?.latest) {
      dismissUpdate(status.latest);
      dismissedThisSession = status.latest;
    }
  }
</script>

{#if show && status}
  <div
    data-testid="update-banner"
    role="status"
    class="flex items-center gap-2 border-b border-warning/40 bg-warning/10 px-3 py-1.5 text-sm text-foreground"
  >
    <ArrowUpCircle size={16} class="shrink-0 text-warning" />
    <span class="min-w-0 flex-1">
      nibs <span class="font-semibold">{status.latest}</span> is available
      {#if status.current}<span class="text-muted-foreground">(you're on {status.current})</span>{/if}
      — run <code class="rounded bg-muted px-1 py-0.5 text-xs">nibs upgrade</code>
    </span>
    <button
      type="button"
      data-testid="update-banner-dismiss"
      onclick={handleDismiss}
      aria-label="Dismiss update notification"
      class="shrink-0 rounded p-0.5 text-muted-foreground hover:bg-accent hover:text-foreground"
    >
      <X size={16} />
    </button>
  </div>
{/if}
