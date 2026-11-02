<script lang="ts">
  interface Props {
    /** The server's sentence, or "" for no failure to report. */
    message?: string;
  }

  let { message = "" }: Props = $props();
</script>

{#if message}
  <span data-testid="area-edit-error" class="text-destructive">{message}</span>
{/if}
