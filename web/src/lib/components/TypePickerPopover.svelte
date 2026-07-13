<script lang="ts">
  import { getValidChildTypes } from "../typeHierarchy";
  import { typeIcons } from "../icons";
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu/index.js";
  import type { AnchorRect } from "../composables/useActiveView.svelte";

  interface Props {
    parentType: string;
    onselect: (type: string) => void;
    oncancel: () => void;
    /** Viewport rect of the control that opened the picker; the menu anchors to
     *  it. Omitted (tests) falls back to screen centre. */
    anchor?: AnchorRect;
  }

  let { parentType, onselect, oncancel, anchor }: Props = $props();

  // Only the child types the backend hierarchy permits under this parent.
  let validTypes = $derived(getValidChildTypes(parentType));
  let menuOpen = $state(true);
  // Selecting closes the menu, which fires onOpenChange(false); that must NOT be
  // reported as a cancel (it would double-fire alongside onselect).
  let selecting = false;

  // Pin the invisible trigger over the opener's rect so the menu opens there
  // (position:fixed lives in the CSS; these override the centred fallback).
  let anchorStyle = $derived(
    anchor
      ? `top:${anchor.y}px; left:${anchor.x}px; width:${anchor.width}px; height:${anchor.height}px;`
      : "top:50%; left:50%;",
  );

  function handleOpenChange(open: boolean) {
    if (!open && !selecting) oncancel();
  }

  function handleSelect(type: string) {
    selecting = true;
    onselect(type);
  }
</script>

<!-- Same shadcn DropdownMenu as the "New" button's type menu (Toolbar), so it
     opens, highlights, and keyboard-navigates identically — but populated from
     getValidChildTypes so it only offers valid child types for this parent. The
     menu is controlled via bind:open; an invisible trigger pinned over the
     opener's rect only anchors the content. -->
<DropdownMenu.Root bind:open={menuOpen} onOpenChange={handleOpenChange}>
  <DropdownMenu.Trigger
    data-testid="type-picker-trigger"
    class="type-picker-anchor"
    style={anchorStyle}
    tabindex={-1}
    aria-hidden="true"
  />

  <DropdownMenu.Content align="start" class="w-48" data-testid="type-picker-popover">
    <DropdownMenu.Label>Select child type</DropdownMenu.Label>
    {#each validTypes as childType}
      {@const iconInfo = typeIcons[childType]}
      {@const TypeIconComponent = iconInfo.icon}
      <DropdownMenu.Item
        data-testid="type-picker-item"
        class="flex items-center gap-2 text-sm"
        onclick={() => handleSelect(childType)}
      >
        <TypeIconComponent size={14} style="color: {iconInfo.color};" />
        {childType}
      </DropdownMenu.Item>
    {/each}
  </DropdownMenu.Content>
</DropdownMenu.Root>

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
</style>
