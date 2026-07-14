/**
 * Composable for managing confirm dialog state.
 *
 * Provides reactive state (open, title, message, label, variant, action)
 * and a `showConfirm(opts)` method to trigger the dialog.
 */

export interface ConfirmDialogOptions {
  title: string;
  message: string;
  label: string;
  variant: "danger" | "warning";
  action: () => void;
  /** Optional secondary "Save" action. Present ONLY for the dirty-nav
   *  guard, which offers Save alongside Discard/Cancel; omitted by Delete/Archive
   *  confirms, which must render no Save button. */
  saveLabel?: string;
  saveAction?: () => void;
}

export interface ConfirmDialogState {
  readonly open: boolean;
  readonly title: string;
  readonly message: string;
  readonly label: string;
  readonly variant: "danger" | "warning";
  readonly action: (() => void) | null;
  /** The opt-in secondary Save action + its label, or null when this confirm
   *  didn't request one (Delete/Archive). */
  readonly saveLabel: string | null;
  readonly saveAction: (() => void) | null;
  showConfirm: (opts: ConfirmDialogOptions) => void;
  close: () => void;
}

export function createConfirmDialog(): ConfirmDialogState {
  let open = $state(false);
  let title = $state("");
  let message = $state("");
  let label = $state("");
  let variant: "danger" | "warning" = $state("danger");
  let action: (() => void) | null = $state(null);
  let saveLabel: string | null = $state(null);
  let saveAction: (() => void) | null = $state(null);

  function showConfirm(opts: ConfirmDialogOptions) {
    title = opts.title;
    message = opts.message;
    label = opts.label;
    variant = opts.variant;
    action = opts.action;
    // Reset to null when this confirm doesn't opt into a Save action, so a prior
    // dirty-guard invocation never leaks a Save button onto a later Delete/Archive.
    saveLabel = opts.saveLabel ?? null;
    saveAction = opts.saveAction ?? null;
    open = true;
  }

  function close() {
    open = false;
    action = null;
    saveAction = null;
    saveLabel = null;
  }

  return {
    get open() { return open; },
    get title() { return title; },
    get message() { return message; },
    get label() { return label; },
    get variant() { return variant; },
    get action() { return action; },
    get saveLabel() { return saveLabel; },
    get saveAction() { return saveAction; },
    showConfirm,
    close,
  };
}
