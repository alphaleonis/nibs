<script lang="ts">
  import * as AlertDialog from "$lib/components/ui/alert-dialog/index.js";
  import type { ConfirmDialogState } from "$lib/composables/useConfirmDialog.svelte";

  interface Props {
    /** The confirm-dialog composable. The dialog reads its display state from
     *  here AND drives it: confirm runs `action`, the opt-in Save runs
     *  `saveAction`, and every dismissal route (Cancel / Escape / overlay) runs
     *  `dismiss()` — which fires the current confirm's dismissal owner. Handing
     *  the whole composable in (rather than per-route callbacks the host wires by
     *  hand) means the dismissal owner-fire lives IN the component and can't be
     *  silently rewritten to a bare `close()` at a host binding, which would drop
     *  the owner and reintroduce the nibs-an5d promise leak. (nibs-an5d/nibs-imgm) */
    confirm: ConfirmDialogState;
    testId?: string;
  }

  let { confirm, testId = "confirm-dialog" }: Props = $props();

  function handleOpenChange(newOpen: boolean) {
    // Cancel button and Escape both route a close request through here. Anything
    // that closes the dialog without the user choosing confirm/Save is a dismissal,
    // so it must run the dismissal owner via dismiss() — never a bare close().
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
          <!-- Opt-in Save action: the recommended, safe choice for the
               dirty-nav guard, rendered rightmost as the primary button. Only
               present when the live confirm supplied a saveAction (never for
               Delete/Archive). -->
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
