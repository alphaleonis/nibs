<script lang="ts">
  import * as AlertDialog from "$lib/components/ui/alert-dialog/index.js";
  import type { ConfirmDialogState } from "$lib/composables/useConfirmDialog.svelte";

  interface Props {
    /** The confirm-dialog composable, rendered and driven here: confirm runs
     *  `action`, Save runs `saveAction`, and every dismissal (Cancel / Escape /
     *  overlay) runs `dismiss()`, which fires the dismissal owner. */
    confirm: ConfirmDialogState;
    testId?: string;
  }

  let { confirm, testId = "confirm-dialog" }: Props = $props();

  function handleOpenChange(newOpen: boolean) {
    // A close request (Cancel, Escape) is a dismissal: call dismiss(), not close().
    if (!newOpen) {
      confirm.dismiss();
    }
  }
</script>

{#if confirm.open}
  <AlertDialog.Root open={true} onOpenChange={handleOpenChange}>
    <AlertDialog.Content
      data-testid={testId}
      class="z-[var(--z-modal-top)]"
      overlayProps={{ "data-testid": `${testId}-overlay`, class: "z-[var(--z-modal-top)]" }}
      onOverlayClick={() => confirm.dismiss()}
    >
      <AlertDialog.Header>
        <AlertDialog.Title data-testid={`${testId}-title`}>{confirm.title}</AlertDialog.Title>
        <AlertDialog.Description data-testid={`${testId}-message`}>{confirm.message}</AlertDialog.Description>
      </AlertDialog.Header>
      <AlertDialog.Footer>
        <AlertDialog.Cancel
          data-testid={`${testId}-cancel`}
        >
          Cancel
        </AlertDialog.Cancel>
        <AlertDialog.Action
          data-testid={`${testId}-confirm`}
          variant={confirm.variant === "danger" ? "destructive" : "default"}
          class={confirm.variant === "warning" ? "bg-warning text-[var(--warning-foreground,white)] border-warning hover:bg-warning-hover" : ""}
          onclick={() => confirm.action?.()}
        >
          {confirm.label}
        </AlertDialog.Action>
        {#if confirm.saveAction}
          <!-- Opt-in Save (the dirty-nav guard), rightmost as the primary. -->
          <AlertDialog.Action
            data-testid={`${testId}-save`}
            variant="default"
            onclick={() => confirm.saveAction?.()}
          >
            {confirm.saveLabel ?? "Save"}
          </AlertDialog.Action>
        {/if}
      </AlertDialog.Footer>
    </AlertDialog.Content>
  </AlertDialog.Root>
{/if}
