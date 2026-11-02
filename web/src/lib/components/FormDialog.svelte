<!--
  @component
  A modal that hosts one form, on the same surface as `ConfirmDialog`. Mount it
  only while open, so the form inside reaches its stores only then. The form
  draws its own actions, Cancel included.
-->
<script lang="ts">
  import type { Snippet } from "svelte";
  import * as Dialog from "$lib/components/ui/dialog/index.js";

  interface Props {
    title: string;
    /** A line under the title, such as the path of the area the form acts on. */
    description?: string;
    /** Every dismissal: Escape and an overlay click. */
    onclose: () => void;
    testId?: string;
    children: Snippet;
  }

  let { title, description = "", onclose, testId = "form-dialog", children }: Props = $props();
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
  </Dialog.Content>
</Dialog.Root>
