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
    // B is the installed owner: dismissing the live confirm runs B (only), once.
    dialog.dismiss();
    expect(onDismissB).toHaveBeenCalledTimes(1);
    expect(onDismissA).toHaveBeenCalledTimes(1);
  });

  it("two overlapping confirms BOTH settle exactly once — neither promise leaks", () => {
    const dialog = createConfirmDialog();
    const onDismissA = vi.fn();
    const onDismissB = vi.fn();

    dialog.showConfirm(opts({ title: "A", onDismiss: onDismissA }));
    dialog.showConfirm(opts({ title: "B", onDismiss: onDismissB }));
    dialog.dismiss(); // dismiss the live one (B)

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
    dialog.dismiss();
    expect(onDismissDiscard).toHaveBeenCalledTimes(1);
  });

  it("an explicit close() drops the owner without running it (Discard/Save settle themselves)", () => {
    const dialog = createConfirmDialog();
    const onDismiss = vi.fn();

    dialog.showConfirm(opts({ onDismiss }));
    // The Discard/Save handlers call close() and settle their promise directly, so
    // close() must not ALSO fire onDismiss (which would double-settle / mis-settle).
    dialog.close();
    expect(onDismiss).not.toHaveBeenCalled();
    // close() DROPPED the owner (did not retain it): a following dismiss() can't
    // resurrect and fire it.
    dialog.dismiss();
    expect(onDismiss).not.toHaveBeenCalled();
  });
});

describe("useConfirmDialog · dismiss()", () => {
  // dismiss() is the structural home of the Cancel/Escape/overlay settlement —
  // capture the owner, close, fire the owner. These tests pin THAT composable logic
  // (capture→close→fire, once-only, fire-after-close). ConfirmDialog wires every
  // dismissal route to dismiss(), and that component seam — Cancel/Escape/overlay
  // firing the dismissal OWNER, not a bare close() that would drop it and reintroduce
  // the nibs-an5d leak — is covered in ConfirmDialog.test.ts. (nibs-an5d/nibs-imgm)

  it("fires the current confirm's onDismiss exactly once and closes the dialog", () => {
    const dialog = createConfirmDialog();
    const onDismiss = vi.fn();

    dialog.showConfirm(opts({ onDismiss }));
    dialog.dismiss();

    expect(onDismiss).toHaveBeenCalledTimes(1);
    expect(dialog.open).toBe(false);
  });

  it("runs the owner AFTER close() — the dialog is already closed when the owner fires", () => {
    const dialog = createConfirmDialog();
    // Observe dialog.open from INSIDE the owner, at the instant it runs.
    let openWhenOwnerRan: boolean | null = null;

    dialog.showConfirm(opts({ onDismiss: () => { openWhenOwnerRan = dialog.open; } }));
    dialog.dismiss();

    // The owner must fire AFTER close(), so a caller settling its promise here never
    // observes the dialog still open mid-teardown. Pins the capture→close→fire ORDER
    // the production comment calls out: a fire-BEFORE-close reorder would surface
    // `open === true` to the owner and fail this.
    expect(openWhenOwnerRan).toBe(false);
  });

  it("on a confirm with NO owner (Delete/Archive) just closes without throwing", () => {
    const dialog = createConfirmDialog();

    dialog.showConfirm(opts({ title: "Delete nib", variant: "danger" }));
    expect(() => dialog.dismiss()).not.toThrow();
    expect(dialog.open).toBe(false);
  });

  it("does not double-fire the owner when called twice", () => {
    const dialog = createConfirmDialog();
    const onDismiss = vi.fn();

    dialog.showConfirm(opts({ onDismiss }));
    dialog.dismiss();
    // The dialog is already closed and its owner nulled; a second dismiss is a no-op.
    dialog.dismiss();
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("after a supersession settles only the CURRENT owner — the superseded one is not re-run (nibs-an5d)", () => {
    const dialog = createConfirmDialog();
    const onDismissA = vi.fn();
    const onDismissB = vi.fn();

    dialog.showConfirm(opts({ title: "A", onDismiss: onDismissA }));
    // B supersedes A → showConfirm already settled A's owner exactly once.
    dialog.showConfirm(opts({ title: "B", onDismiss: onDismissB }));
    expect(onDismissA).toHaveBeenCalledTimes(1);
    expect(onDismissB).not.toHaveBeenCalled();

    // Dismissing the live confirm settles only B; A is never answered by it.
    dialog.dismiss();
    expect(onDismissA).toHaveBeenCalledTimes(1);
    expect(onDismissB).toHaveBeenCalledTimes(1);
  });
});
