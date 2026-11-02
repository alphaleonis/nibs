<!--
  @component
  A modal that hosts one form, on the same surface as `ConfirmDialog`. Mount it
  only while open, so the form inside reaches its stores only then.
-->
<script lang="ts">
  import type { Snippet } from "svelte";
  import * as Dialog from "$lib/components/ui/dialog/index.js";
  import { Button } from "$lib/components/ui/button/index.js";

  interface Props {
    title: string;
    /** A line under the title, such as the path of the area the form acts on. */
    description?: string;
    /** Every dismissal: Escape, an overlay click and the Cancel button. */
    onclose: () => void;
    /** Whether to draw a Cancel button under a form that has none of its own. */
    cancel?: boolean;
    testId?: string;
    children: Snippet;
  }

  let { title, description = "", onclose, cancel = false, testId = "form-dialog", children }: Props = $props();
</script>

<Dialog.Root
  open={true}
  onOpenChange={(open) => {
    if (!open) onclose();
  }}
>
  <Dialog.Content
    data-testid={testId}
    showCloseButton={false}
    class="z-[var(--z-modal-top)]"
    overlayProps={{ class: "z-[var(--z-modal-top)]" }}
  >
    <Dialog.Header>
      <Dialog.Title>{title}</Dialog.Title>
      {#if description}
        <Dialog.Description>{description}</Dialog.Description>
      {/if}
    </Dialog.Header>
    <!-- min-w-0 so a wide form row shrinks to the dialog instead of overflowing it. -->
    <div class="min-w-0">{@render children()}</div>
    {#if cancel}
      <Dialog.Footer>
        <Button variant="outline" onclick={onclose}>Cancel</Button>
      </Dialog.Footer>
    {/if}
  </Dialog.Content>
</Dialog.Root>
