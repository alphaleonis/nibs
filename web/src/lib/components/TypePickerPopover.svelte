<script lang="ts">
  import { getValidChildTypes } from "../typeHierarchy";
  import * as Popover from "$lib/components/ui/popover/index.js";

  interface Props {
    parentType: string;
    onselect: (type: string) => void;
    oncancel: () => void;
  }

  let { parentType, onselect, oncancel }: Props = $props();

  let validTypes = $derived(getValidChildTypes(parentType));
  let popoverOpen = $state(true);

  function handleOpenChange(open: boolean) {
    if (!open) {
      oncancel();
    }
  }

  function handleSelect(type: string) {
    popoverOpen = false;
    onselect(type);
  }
</script>

<Popover.Root bind:open={popoverOpen} onOpenChange={handleOpenChange}>
  <!-- Invisible trigger anchored to mouse position / center of screen -->
  <Popover.Trigger data-testid="type-picker-trigger" class="type-picker-anchor">
    Select child type
  </Popover.Trigger>

  <Popover.Content data-testid="type-picker-popover" class="type-picker-content">
    <div class="type-picker-header">Select child type</div>
    <div class="type-picker-list">
      {#each validTypes as childType}
        <button
          class="type-picker-item"
          data-testid="type-picker-item"
          onclick={() => handleSelect(childType)}
        >
          {childType}
        </button>
      {/each}
    </div>
  </Popover.Content>
</Popover.Root>

<style>
  :global(.type-picker-anchor) {
    position: fixed;
    top: 50%;
    left: 50%;
    width: 0;
    height: 0;
    padding: 0 !important;
    border: none !important;
    opacity: 0;
    pointer-events: none;
  }

  .type-picker-header {
    font-size: 0.75rem;
    font-weight: 600;
    color: var(--muted-foreground);
    padding: 0.25rem 0.5rem;
    margin-bottom: 0.25rem;
  }

  .type-picker-list {
    display: flex;
    flex-direction: column;
    gap: 0.125rem;
  }

  .type-picker-item {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    width: 100%;
    padding: 0.375rem 0.5rem;
    font-size: 0.8125rem;
    color: var(--foreground);
    background: none;
    border: none;
    border-radius: 0.25rem;
    cursor: pointer;
    text-align: left;
    text-transform: capitalize;
  }

  .type-picker-item:hover {
    background-color: var(--accent);
  }
</style>
