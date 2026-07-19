import { render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import ConfirmDialog from "./ConfirmDialog.svelte";
import { createConfirmDialog } from "$lib/composables/useConfirmDialog.svelte";
import type { ConfirmDialogOptions } from "$lib/composables/useConfirmDialog.svelte";

// ConfirmDialog is driven by the live composable it's handed: it reads its display
// state from `confirm` and drives every route — confirm runs `action`, Save runs
// `saveAction`, and each dismissal route (Cancel / Escape / overlay) runs
// `dismiss()`, which fires the current confirm's dismissal OWNER (its onDismiss).
// These tests exercise that real seam with a real `createConfirmDialog()`, so the
// dismissal-owner fire is covered AT THE COMPONENT — the binding that a regression
// to a bare `close()` (dropping the owner, reintroducing the nibs-an5d leak) used to
// be able to break silently at a host `oncancel`. (nibs-i567)

/** A live composable with a confirm already shown. Override the pieces a test
 *  cares about. */
function shown(over: Partial<ConfirmDialogOptions> = {}) {
  const dialog = createConfirmDialog();
  dialog.showConfirm({
    title: "Delete nib?",
    message: "This will permanently delete the nib.",
    label: "Delete",
    variant: "danger",
    action: () => {},
    ...over,
  });
  return dialog;
}

describe("ConfirmDialog", () => {
  it("renders title, message, and button labels when open", () => {
    render(ConfirmDialog, { confirm: shown() });

    expect(screen.getByTestId("confirm-dialog-title")).toHaveTextContent("Delete nib?");
    expect(screen.getByTestId("confirm-dialog-message")).toHaveTextContent("This will permanently delete the nib.");
    expect(screen.getByTestId("confirm-dialog-confirm")).toHaveTextContent("Delete");
    expect(screen.getByTestId("confirm-dialog-cancel")).toHaveTextContent("Cancel");
  });

  it("does not render when the composable has no open confirm", () => {
    // A fresh composable (showConfirm never called) is closed.
    render(ConfirmDialog, { confirm: createConfirmDialog() });

    expect(screen.queryByTestId("confirm-dialog")).not.toBeInTheDocument();
    expect(screen.queryByTestId("confirm-dialog-overlay")).not.toBeInTheDocument();
  });

  it("runs the confirm action when the confirm button is clicked", async () => {
    const user = userEvent.setup();
    const action = vi.fn();
    render(ConfirmDialog, { confirm: shown({ action }) });

    await user.click(screen.getByTestId("confirm-dialog-confirm"));
    expect(action).toHaveBeenCalledOnce();
  });

  // ─── Dismissal fires the OWNER, not a bare close (the nibs-an5d guard) ───────
  // Each of these clicks a dismissal route and asserts the composable's dismissal
  // OWNER (onDismiss) ran. Rewiring the component's dismissal to `confirm.close()`
  // (which nulls the owner WITHOUT firing it) drops the owner and turns these red —
  // the exact regression that shipped once and is now caught at the seam.

  it("fires the dismissal owner (onDismiss) when the Cancel button is clicked", async () => {
    const user = userEvent.setup();
    const onDismiss = vi.fn();
    const dialog = shown({ variant: "warning", label: "Discard", onDismiss });
    render(ConfirmDialog, { confirm: dialog });

    await user.click(screen.getByTestId("confirm-dialog-cancel"));
    expect(onDismiss).toHaveBeenCalledOnce();
    expect(dialog.open).toBe(false);
  });

  it("fires the dismissal owner (onDismiss) when Escape is pressed", async () => {
    const user = userEvent.setup();
    const onDismiss = vi.fn();
    const dialog = shown({ variant: "warning", label: "Discard", onDismiss });
    render(ConfirmDialog, { confirm: dialog });

    await user.keyboard("{Escape}");
    expect(onDismiss).toHaveBeenCalledOnce();
    expect(dialog.open).toBe(false);
  });

  it("fires the dismissal owner (onDismiss) when the overlay/backdrop is clicked", async () => {
    const user = userEvent.setup();
    const onDismiss = vi.fn();
    const dialog = shown({ variant: "warning", label: "Discard", onDismiss });
    render(ConfirmDialog, { confirm: dialog });

    await user.click(screen.getByTestId("confirm-dialog-overlay"));
    expect(onDismiss).toHaveBeenCalledOnce();
    expect(dialog.open).toBe(false);
  });

  it("does not dismiss when the dialog body is clicked", async () => {
    const user = userEvent.setup();
    const onDismiss = vi.fn();
    render(ConfirmDialog, { confirm: shown({ onDismiss }) });

    await user.click(screen.getByTestId("confirm-dialog"));
    expect(onDismiss).not.toHaveBeenCalled();
  });

  it("uses alertdialog ARIA role for accessibility", () => {
    render(ConfirmDialog, { confirm: shown() });

    const dialog = screen.getByRole("alertdialog");
    expect(dialog).toBeInTheDocument();
  });

  it("fires the dismissal owner only once on overlay click", async () => {
    const user = userEvent.setup();
    const onDismiss = vi.fn();
    render(ConfirmDialog, { confirm: shown({ onDismiss }) });

    await user.click(screen.getByTestId("confirm-dialog-overlay"));
    // Wait for any async side effects (e.g., bits-ui onOpenChange).
    await new Promise((r) => setTimeout(r, 50));
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("renders with warning variant", () => {
    render(ConfirmDialog, {
      confirm: shown({ title: "Archive nib?", label: "Archive", variant: "warning" }),
    });

    expect(screen.getByTestId("confirm-dialog-title")).toHaveTextContent("Archive nib?");
    expect(screen.getByTestId("confirm-dialog-confirm")).toHaveTextContent("Archive");
  });

  // ─── Opt-in secondary Save action ───────────────

  it("does NOT render a Save button when the confirm has no saveAction (Delete/Archive unaffected)", () => {
    render(ConfirmDialog, { confirm: shown() });

    // The default confirm (no saveAction) shows only Cancel + confirm, never Save.
    expect(screen.queryByTestId("confirm-dialog-save")).not.toBeInTheDocument();
    expect(screen.getByTestId("confirm-dialog-cancel")).toBeInTheDocument();
    expect(screen.getByTestId("confirm-dialog-confirm")).toBeInTheDocument();
  });

  it("renders a Save button and runs saveAction when clicked (opt-in)", async () => {
    const user = userEvent.setup();
    const saveAction = vi.fn();
    render(ConfirmDialog, {
      confirm: shown({
        title: "Unsaved changes",
        label: "Discard",
        variant: "warning",
        saveLabel: "Save",
        saveAction,
      }),
    });

    const saveButton = screen.getByTestId("confirm-dialog-save");
    expect(saveButton).toHaveTextContent("Save");

    await user.click(saveButton);
    expect(saveAction).toHaveBeenCalledOnce();
  });

  it("does not fire the dismissal owner when the Save button is clicked", async () => {
    // The dirty-nav guard relies on Save winning over dismissal: Save is an
    // AlertDialog.Action, which does NOT request a close, so clicking it runs only
    // saveAction and never routes through dismiss() — the dismissal owner must not
    // fire and usurp the Save result.
    const user = userEvent.setup();
    const saveAction = vi.fn();
    const onDismiss = vi.fn();
    render(ConfirmDialog, {
      confirm: shown({ label: "Discard", variant: "warning", saveAction, onDismiss }),
    });

    await user.click(screen.getByTestId("confirm-dialog-save"));
    await new Promise((r) => setTimeout(r, 50));
    expect(saveAction).toHaveBeenCalledOnce();
    expect(onDismiss).not.toHaveBeenCalled();
  });

  it("uses the provided saveLabel for the Save button", () => {
    render(ConfirmDialog, {
      confirm: shown({
        label: "Discard",
        variant: "warning",
        saveLabel: "Save & continue",
        saveAction: () => {},
      }),
    });

    expect(screen.getByTestId("confirm-dialog-save")).toHaveTextContent("Save & continue");
  });
});
