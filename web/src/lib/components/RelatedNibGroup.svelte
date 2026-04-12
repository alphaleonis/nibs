<script lang="ts">
  import { ChevronDown, ChevronRight, Plus } from "@lucide/svelte";
  import StatusDot from "./StatusDot.svelte";

  export interface RelatedNibItem {
    id: string;
    title: string;
    status: string;
  }

  interface Props {
    label: string;
    items: RelatedNibItem[];
    onnibselect?: (id: string) => void;
    onaction?: () => void;
    actionLabel?: string;
    testId: string;
  }

  let { label, items, onnibselect, onaction, actionLabel, testId }: Props = $props();

  let collapsed: boolean = $state(false);
</script>

{#snippet toggleButton()}
  <button
    class="detail-related-group-header"
    data-testid="detail-group-toggle"
    aria-expanded={!collapsed}
    aria-label={label}
    onclick={() => collapsed = !collapsed}
  >
    {#if collapsed}
      <ChevronRight size={14} />
    {:else}
      <ChevronDown size={14} />
    {/if}
    <span class="detail-label">{label}</span>
  </button>
{/snippet}

<div class="detail-related-group" data-testid={testId}>
  {#if onaction}
    <div class="detail-related-group-header-row">
      {@render toggleButton()}
      <button
        class="detail-related-add-child"
        data-testid="detail-related-add-child"
        title={actionLabel}
        onclick={() => onaction?.()}
      >
        <Plus size={14} />
      </button>
    </div>
  {:else}
    {@render toggleButton()}
  {/if}
  {#if !collapsed}
    <div class="detail-related-group-items">
      {#each items as item}
        <button
          class="detail-related-link"
          data-testid="detail-related-link"
          onclick={() => onnibselect?.(item.id)}
        >
          <StatusDot status={item.status} />
          {item.title}
        </button>
      {/each}
    </div>
  {/if}
</div>

<style>
  .detail-related-group {
    display: flex;
    flex-direction: column;
    gap: 0.25rem;
  }

  .detail-related-group-header {
    display: flex;
    align-items: center;
    gap: 0.375rem;
    background: none;
    border: none;
    padding: 0.25rem 0;
    cursor: pointer;
    color: var(--muted-foreground);
  }

  .detail-related-group-header:hover {
    color: var(--foreground);
  }

  .detail-related-group-header-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  .detail-related-add-child {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    padding: 0.125rem;
    color: var(--muted-foreground);
    background: none;
    border: none;
    border-radius: 0.25rem;
    cursor: pointer;
    opacity: 1;
  }

  .detail-related-add-child:hover {
    color: var(--foreground);
    background-color: var(--accent);
  }

  .detail-label {
    color: var(--muted-foreground);
    white-space: nowrap;
  }

  .detail-related-group-items {
    display: flex;
    flex-direction: column;
    gap: 0.125rem;
    padding-left: 0.25rem;
  }

  .detail-related-link {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    background: none;
    border: none;
    color: var(--link);
    padding: 0.25rem 0.5rem;
    border-radius: 0.375rem;
    cursor: pointer;
    text-align: left;
    width: 100%;
  }

  .detail-related-link:hover {
    background-color: var(--accent);
    color: var(--link-hover);
  }
</style>
