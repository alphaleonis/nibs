<script lang="ts" module>
  /** What the area menu's entries ask the host to open. */
  export type AreaMenuAction = "edit" | "add-child" | "remove";

  /** The [⋯] button the menu hangs off, or the pointer position of a right-click. */
  export type AreaMenuAnchor = HTMLElement | { x: number; y: number };
</script>

<script lang="ts">
  import * as DropdownMenu from "$lib/components/ui/dropdown-menu/index.js";

  interface Props {
    anchor: AreaMenuAnchor;
    onselect: (action: AreaMenuAction) => void;
    /** Every dismissal, including the one that follows a selection. */
    onclose: () => void;
  }

  let { anchor, onselect, onclose }: Props = $props();

  let button = $derived(anchor instanceof HTMLElement ? anchor : null);
  // The hidden trigger places a pointer-opened menu. A button anchor is passed
  // as the element, so the menu follows the button and not its click-time rect.
  let point = $derived.by(() => {
    if (!(anchor instanceof HTMLElement)) return anchor;
    const rect = anchor.getBoundingClientRect();
    return { x: rect.left, y: rect.bottom };
  });

  function choose(action: AreaMenuAction) {
    onselect(action);
    onclose();
  }
</script>

<DropdownMenu.Root
  open={true}
  onOpenChange={(open) => {
    if (!open) onclose();
  }}
>
  <DropdownMenu.Trigger
    style="position: fixed; left: {point.x}px; top: {point.y}px; width: 0; height: 0; padding: 0; border: 0; opacity: 0; pointer-events: none;"
  ></DropdownMenu.Trigger>
  <DropdownMenu.Content
    data-testid="area-row-menu"
    class="w-48"
    align="start"
    side="bottom"
    customAnchor={button}
  >
    <DropdownMenu.Item data-testid="area-menu-edit" onSelect={() => choose("edit")}>Edit…</DropdownMenu.Item>
    <DropdownMenu.Item data-testid="area-menu-add-child" onSelect={() => choose("add-child")}>
      Add child…
    </DropdownMenu.Item>
    <DropdownMenu.Separator />
    <DropdownMenu.Item data-testid="area-menu-remove" variant="destructive" onSelect={() => choose("remove")}>
      Remove…
    </DropdownMenu.Item>
  </DropdownMenu.Content>
</DropdownMenu.Root>
