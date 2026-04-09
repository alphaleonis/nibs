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
}

export interface ConfirmDialogState {
  readonly open: boolean;
  readonly title: string;
  readonly message: string;
  readonly label: string;
  readonly variant: "danger" | "warning";
  readonly action: (() => void) | null;
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

  function showConfirm(opts: ConfirmDialogOptions) {
    title = opts.title;
    message = opts.message;
    label = opts.label;
    variant = opts.variant;
    action = opts.action;
    open = true;
  }

  function close() {
    open = false;
    action = null;
  }

  return {
    get open() { return open; },
    get title() { return title; },
    get message() { return message; },
    get label() { return label; },
    get variant() { return variant; },
    get action() { return action; },
    showConfirm,
    close,
  };
}
