/** State for the single shared confirm dialog. */

export interface ConfirmDialogOptions {
  title: string;
  message: string;
  label: string;
  variant: "danger" | "warning";
  action: () => void;
  /** Optional secondary Save action, for the dirty-nav guard only. */
  saveLabel?: string;
  saveAction?: () => void;
  /** Runs once when THIS confirm ends without its primary or Save action:
   *  dismissed, or superseded by a later `showConfirm`. Only settle your own
   *  promise here. On supersession it runs after the new confirm is installed,
   *  so touching dialog state would clobber the live one. */
  onDismiss?: () => void;
}

export interface ConfirmDialogState {
  readonly open: boolean;
  readonly title: string;
  readonly message: string;
  readonly label: string;
  readonly variant: "danger" | "warning";
  readonly action: (() => void) | null;
  /** null unless this confirm requested a Save action. */
  readonly saveLabel: string | null;
  readonly saveAction: (() => void) | null;
  showConfirm: (opts: ConfirmDialogOptions) => void;
  close: () => void;
  /** Close the live confirm, then run its `onDismiss`. Route every dismissal
   *  (Cancel / Escape / overlay) here, never through a bare `close()`. */
  dismiss: () => void;
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
  let dismissAction: (() => void) | null = $state(null);

  function showConfirm(opts: ConfirmDialogOptions) {
    // Settle the superseded confirm after installing this one, so its awaiting
    // caller does not hang.
    const superseded = dismissAction;
    title = opts.title;
    message = opts.message;
    label = opts.label;
    variant = opts.variant;
    action = opts.action;
    // Reset so an earlier Save action never shows on a later confirm.
    saveLabel = opts.saveLabel ?? null;
    saveAction = opts.saveAction ?? null;
    dismissAction = opts.onDismiss ?? null;
    open = true;
    superseded?.();
  }

  function close() {
    open = false;
    action = null;
    saveAction = null;
    saveLabel = null;
    dismissAction = null;
  }

  function dismiss() {
    // Capture before close() clears it.
    const owner = dismissAction;
    close();
    owner?.();
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
    dismiss,
  };
}
