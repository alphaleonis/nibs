<script lang="ts">
  import * as AlertDialog from "$lib/components/ui/alert-dialog/index.js";

  interface Props {
    open: boolean;
    title: string;
    message: string;
    confirmLabel: string;
    variant: "danger" | "warning";
    confirmDisabled?: boolean;
    testId?: string;
    onconfirm: () => void;
    oncancel: () => void;
  }

  let { open, title, message, confirmLabel, variant, confirmDisabled = false, testId = "confirm-dialog", onconfirm, oncancel }: Props = $props();

  function handleOpenChange(newOpen: boolean) {
    if (!newOpen) {
      oncancel();
    }
  }
</script>

{#if open}
  <AlertDialog.Root open={true} onOpenChange={handleOpenChange}>
    <AlertDialog.Content
      data-testid={testId}
      class="z-[var(--z-modal-top)]"
      overlayProps={{ "data-testid": `${testId}-overlay`, class: "z-[var(--z-modal-top)]" }}
      onOverlayClick={oncancel}
    >
      <AlertDialog.Header>
        <AlertDialog.Title data-testid={`${testId}-title`}>{title}</AlertDialog.Title>
        <AlertDialog.Description data-testid={`${testId}-message`}>{message}</AlertDialog.Description>
      </AlertDialog.Header>
      <AlertDialog.Footer>
        <AlertDialog.Cancel
          data-testid={`${testId}-cancel`}
        >
          Cancel
        </AlertDialog.Cancel>
        <AlertDialog.Action
          data-testid={`${testId}-confirm`}
          variant={variant === "danger" ? "destructive" : "default"}
          class={variant === "warning" ? "bg-warning text-[var(--warning-foreground,white)] border-warning hover:bg-warning-hover" : ""}
          disabled={confirmDisabled}
          onclick={onconfirm}
        >
          {confirmLabel}
        </AlertDialog.Action>
      </AlertDialog.Footer>
    </AlertDialog.Content>
  </AlertDialog.Root>
{/if}
