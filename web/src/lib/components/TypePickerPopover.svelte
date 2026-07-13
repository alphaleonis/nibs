<script lang="ts">
  import { getValidChildTypes } from "../typeHierarchy";
  import TypeIcon from "./TypeIcon.svelte";
  import * as Popover from "$lib/components/ui/popover/index.js";
  import type { AnchorRect } from "../composables/useActiveView.svelte";

  interface Props {
    parentType: string;
    onselect: (type: string) => void;
    oncancel: () => void;
    /** Viewport rect of the control that opened the picker; the popover anchors
     *  to it. Omitted (tests) falls back to screen center. */
    anchor?: AnchorRect;
  }

  let { parentType, onselect, oncancel, anchor }: Props = $props();

  let validTypes = $derived(getValidChildTypes(parentType));
  let popoverOpen = $state(true);
  // Selecting a type closes the popover, which fires onOpenChange(false); that
  // must NOT be reported as a cancel (it would double-fire alongside onselect).
  let selecting = false;

  // Pin the invisible trigger over the opener's rect so the popover opens there
  // (position:fixed lives in the CSS; these override the centered fallback).
  let anchorStyle = $derived(
    anchor
      ? `top:${anchor.y}px; left:${anchor.x}px; width:${anchor.width}px; height:${anchor.height}px; transform:none;`
      : "top:50%; left:50%;",
  );

  function handleOpenChange(open: boolean) {
    if (!open && !selecting) {
      oncancel();
    }
  }

  function handleSelect(type: string) {
    selecting = true;
    popoverOpen = false;
    onselect(type);
  }
</script>

<Popover.Root bind:open={popoverOpen} onOpenChange={handleOpenChange}>
  <!-- Invisible trigger pinned over the control that opened the picker. -->
  <Popover.Trigger data-testid="type-picker-trigger" class="type-picker-anchor" style={anchorStyle}>
    Select child type
  </Popover.Trigger>

  <Popover.Content data-testid="type-picker-popover" class="type-picker-content">
    <div class="type-picker-header">Select child type</div>
    <div class="type-picker-list">
      {#each validTypes as childType}
        <!-- Raw button: popover menu item styled to match DropdownMenu items,
             not a standalone Button primitive. -->
        <button
          class="type-picker-item"
          data-testid="type-picker-item"
          onclick={() => handleSelect(childType)}
        >
          <TypeIcon type={childType} size={14} />
          {childType}
        </button>
      {/each}
    </div>
  </Popover.Content>
</Popover.Root>

<style>
  /* Invisible anchor: position:fixed here; top/left (and width/height when a
     rect is given) are set inline from the `anchor` prop. */
  :global(.type-picker-anchor) {
    position: fixed;
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
    border-radius: var(--radius-sm);
    cursor: pointer;
    text-align: left;
    text-transform: capitalize;
  }

  .type-picker-item:hover {
    background-color: var(--accent);
  }
</style>
