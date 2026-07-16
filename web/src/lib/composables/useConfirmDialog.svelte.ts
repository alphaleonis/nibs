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
  /** Called when THIS confirm is abandoned without the user picking its primary
   *  (Discard) or Save action — i.e. dismissed (Cancel / Escape / overlay) OR
   *  superseded by a later `showConfirm` before it was answered. It runs exactly
   *  once and only for its own invocation, so a caller awaiting a promise can
   *  settle it here knowing a LATER dialog can never trigger it. This is what
   *  makes the single shared dialog safe under overlapping confirms: without it,
   *  a superseding confirm silently orphaned the previous one's pending promise
   *  (nibs-an5d). Omitted by Delete/Archive confirms, which await nothing. */
  onDismiss?: () => void;
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
  /** The current confirm's dismissal owner (from `onDismiss`), or null. The host
   *  runs it on a Cancel/Escape/overlay dismissal AFTER `close()`. `showConfirm`
   *  runs the PREVIOUS one automatically when a new confirm supersedes it. */
  readonly dismissAction: (() => void) | null;
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
  let dismissAction: (() => void) | null = $state(null);

  function showConfirm(opts: ConfirmDialogOptions) {
    // A new confirm reuses the single dialog component, so it SUPERSEDES any
    // still-pending one. Settle that superseded confirm (run its dismissal owner)
    // so a caller awaiting it never hangs — the exact leak nibs-an5d fixed. Capture
    // it BEFORE overwriting the slot, run it AFTER the new state is installed so a
    // re-entrant showConfirm from within it (should one ever occur) can't be
    // clobbered. Nulled here because the superseded owner is one-shot.
    const superseded = dismissAction;
    title = opts.title;
    message = opts.message;
    label = opts.label;
    variant = opts.variant;
    action = opts.action;
    // Reset to null when this confirm doesn't opt into a Save action, so a prior
    // dirty-guard invocation never leaks a Save button onto a later Delete/Archive.
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

  return {
    get open() { return open; },
    get title() { return title; },
    get message() { return message; },
    get label() { return label; },
    get variant() { return variant; },
    get action() { return action; },
    get saveLabel() { return saveLabel; },
    get saveAction() { return saveAction; },
    get dismissAction() { return dismissAction; },
    showConfirm,
    close,
  };
}
