<script lang="ts">
  import { Button } from "$lib/components/ui/button/index.js";
  import AreaDeclareForm from "./AreaDeclareForm.svelte";
  import AreaRenameForm from "./AreaRenameForm.svelte";
  import AreaDetailsForm from "./AreaDetailsForm.svelte";
  import AreaRemoveForm from "./AreaRemoveForm.svelte";

  interface Props {
    /** The declared path of the area this panel acts on. */
    path: string;
    /** The node's own segment, which is what a rename sets. */
    name?: string;
    /** What the store declares for this area today. */
    description?: string;
    color?: string;
    /**
     * Dismiss the panel: an edit landed. Backing out of a sub-form returns to
     * the menu instead, which is this panel's own state.
     */
    onclose?: () => void;
  }

  let { path, name = "", description = "", color = "", onclose }: Props = $props();

  // Each entry swaps the panel's body rather than opening a second surface, so
  // only the form in use reaches the mutation store.
  let mode: "menu" | "add-child" | "rename" | "details" | "remove" = $state("menu");
</script>

<!-- Placement, dismissal and the surface itself belong to the Popover this is
     rendered into; what is left here is the panel's body. -->
<div data-testid="area-row-panel" class="flex flex-col gap-1">
  {#if mode === "menu"}
    <span class="px-2 py-1 text-caption text-muted-foreground">{path}</span>
    <Button variant="ghost" size="sm" class="justify-start" onclick={() => (mode = "rename")}>
      Rename
    </Button>
    <!-- Both open the one form: description and color are declared together, and
         a save sends whichever of them actually changed. -->
    <Button variant="ghost" size="sm" class="justify-start" onclick={() => (mode = "details")}>
      Description
    </Button>
    <Button variant="ghost" size="sm" class="justify-start" onclick={() => (mode = "details")}>
      Color
    </Button>
    <Button variant="ghost" size="sm" class="justify-start" onclick={() => (mode = "add-child")}>
      Add child
    </Button>
    <Button variant="ghost" size="sm" class="justify-start" onclick={() => (mode = "remove")}>
      Remove
    </Button>
  {:else if mode === "add-child"}
    <!-- Cancel goes back to the menu the user came from; only a landed
         declaration dismisses the panel. -->
    <AreaDeclareForm
      parentPath={path}
      onclose={() => onclose?.()}
      oncancel={() => (mode = "menu")}
    />
  {:else if mode === "rename"}
    <AreaRenameForm {path} {name} onclose={() => onclose?.()} />
  {:else if mode === "details"}
    <AreaDetailsForm {path} {description} {color} onclose={() => onclose?.()} />
  {:else}
    <AreaRemoveForm {path} onclose={() => onclose?.()} />
  {/if}
</div>
