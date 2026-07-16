import { describe, it, expect, vi } from "vitest";
import { createConfirmDialog } from "./useConfirmDialog.svelte";
import type { ConfirmDialogOptions } from "./useConfirmDialog.svelte";

// The single shared confirm dialog is reused for every confirm (dirty-guard,
// Delete, Archive, …). These tests pin the ownership contract that makes that
// safe under OVERLAPPING confirms (nibs-an5d): a confirm that is superseded, or
// dismissed, must run its OWN `onDismiss` exactly once and never a later
// confirm's — otherwise a caller awaiting a promise through the dialog leaks
// (never settles) or is answered by the wrong dialog.

/** Minimal confirm options; override the pieces a test cares about. */
function opts(over: Partial<ConfirmDialogOptions> = {}): ConfirmDialogOptions {
  return {
    title: "t",
    message: "m",
    label: "OK",
    variant: "warning",
    action: () => {},
    ...over,
  };
}

/** Emulate the host's dismissal wiring (App.svelte's `oncancel`): capture the
 *  current owner, close, then run it — the order that makes a Delete/Archive
 *  confirm (no owner) a no-op on any pending discard promise. */
function hostDismiss(dialog: ReturnType<typeof createConfirmDialog>): void {
  const onDismiss = dialog.dismissAction;
  dialog.close();
  onDismiss?.();
}

describe("useConfirmDialog · overlapping-confirm ownership", () => {
  it("settles the confirm it supersedes: the replaced confirm's onDismiss runs once", () => {
    const dialog = createConfirmDialog();
    const onDismissA = vi.fn();
    const onDismissB = vi.fn();

    dialog.showConfirm(opts({ title: "A", onDismiss: onDismissA }));
    expect(onDismissA).not.toHaveBeenCalled();

    // B reuses the single dialog → A is superseded and must be settled so whoever
    // awaited A does not hang forever.
    dialog.showConfirm(opts({ title: "B", onDismiss: onDismissB }));
    expect(onDismissA).toHaveBeenCalledTimes(1);
    // B is the live confirm; it has not been dismissed yet.
    expect(onDismissB).not.toHaveBeenCalled();
    expect(dialog.dismissAction).toBe(onDismissB);
  });

  it("two overlapping confirms BOTH settle exactly once — neither promise leaks", () => {
    const dialog = createConfirmDialog();
    const onDismissA = vi.fn();
    const onDismissB = vi.fn();

    dialog.showConfirm(opts({ title: "A", onDismiss: onDismissA }));
    dialog.showConfirm(opts({ title: "B", onDismiss: onDismissB }));
    hostDismiss(dialog); // dismiss the live one (B)

    // A settled on supersession, B settled on dismissal — both once, none abandoned.
    expect(onDismissA).toHaveBeenCalledTimes(1);
    expect(onDismissB).toHaveBeenCalledTimes(1);
  });

  it("a confirm with NO owner (Delete/Archive) superseding a discard settles the discard and never answers it wrongly", () => {
    const dialog = createConfirmDialog();
    const onDismissDiscard = vi.fn();

    // The dirty-guard discard confirm installs an owner (its awaited promise).
    dialog.showConfirm(opts({ title: "Unsaved changes", onDismiss: onDismissDiscard }));
    // A Delete confirm reuses the dialog but awaits nothing → no owner.
    dialog.showConfirm(opts({ title: "Delete nib", variant: "danger" }));

    // The discard was superseded → settled once (its owner decides "cancel").
    expect(onDismissDiscard).toHaveBeenCalledTimes(1);

    // Cancelling the Delete must NOT re-run (wrongly answer) the discard's owner.
    hostDismiss(dialog);
    expect(onDismissDiscard).toHaveBeenCalledTimes(1);
  });

  it("an explicit close() drops the owner without running it (Discard/Save settle themselves)", () => {
    const dialog = createConfirmDialog();
    const onDismiss = vi.fn();

    dialog.showConfirm(opts({ onDismiss }));
    // The Discard/Save handlers call close() and settle their promise directly, so
    // close() must not ALSO fire onDismiss (which would double-settle / mis-settle).
    dialog.close();
    expect(dialog.dismissAction).toBeNull();
    expect(onDismiss).not.toHaveBeenCalled();
  });
});
