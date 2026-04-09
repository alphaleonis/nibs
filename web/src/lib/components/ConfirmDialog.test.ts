import { render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import ConfirmDialog from "./ConfirmDialog.svelte";

describe("ConfirmDialog", () => {
  const defaultProps = {
    open: true,
    title: "Delete nib?",
    message: "This will permanently delete the nib.",
    confirmLabel: "Delete",
    variant: "danger" as const,
    onconfirm: vi.fn(),
    oncancel: vi.fn(),
  };

  it("renders title, message, and button labels when open", () => {
    render(ConfirmDialog, { ...defaultProps });

    expect(screen.getByTestId("confirm-dialog-title")).toHaveTextContent("Delete nib?");
    expect(screen.getByTestId("confirm-dialog-message")).toHaveTextContent("This will permanently delete the nib.");
    expect(screen.getByTestId("confirm-dialog-confirm")).toHaveTextContent("Delete");
    expect(screen.getByTestId("confirm-dialog-cancel")).toHaveTextContent("Cancel");
  });

  it("does not render when closed (open=false)", () => {
    render(ConfirmDialog, { ...defaultProps, open: false });

    expect(screen.queryByTestId("confirm-dialog")).not.toBeInTheDocument();
    expect(screen.queryByTestId("confirm-dialog-overlay")).not.toBeInTheDocument();
  });

  it("calls onconfirm when confirm button clicked", async () => {
    const user = userEvent.setup();
    const onconfirm = vi.fn();
    render(ConfirmDialog, { ...defaultProps, onconfirm });

    await user.click(screen.getByTestId("confirm-dialog-confirm"));
    expect(onconfirm).toHaveBeenCalledOnce();
  });

  it("calls oncancel when cancel button clicked", async () => {
    const user = userEvent.setup();
    const oncancel = vi.fn();
    render(ConfirmDialog, { ...defaultProps, oncancel });

    await user.click(screen.getByTestId("confirm-dialog-cancel"));
    expect(oncancel).toHaveBeenCalledOnce();
  });

  it("calls oncancel when Escape key pressed", async () => {
    const user = userEvent.setup();
    const oncancel = vi.fn();
    render(ConfirmDialog, { ...defaultProps, oncancel });

    await user.keyboard("{Escape}");
    expect(oncancel).toHaveBeenCalledOnce();
  });

  it("calls oncancel when overlay/backdrop clicked", async () => {
    const user = userEvent.setup();
    const oncancel = vi.fn();
    render(ConfirmDialog, { ...defaultProps, oncancel });

    await user.click(screen.getByTestId("confirm-dialog-overlay"));
    expect(oncancel).toHaveBeenCalledOnce();
  });

  it("does not call oncancel when dialog body is clicked", async () => {
    const user = userEvent.setup();
    const oncancel = vi.fn();
    render(ConfirmDialog, { ...defaultProps, oncancel });

    await user.click(screen.getByTestId("confirm-dialog"));
    expect(oncancel).not.toHaveBeenCalled();
  });

  it("uses alertdialog ARIA role for accessibility", () => {
    render(ConfirmDialog, { ...defaultProps });

    const dialog = screen.getByRole("alertdialog");
    expect(dialog).toBeInTheDocument();
  });

  it("does not double-fire oncancel on overlay click", async () => {
    const user = userEvent.setup();
    const oncancel = vi.fn();
    render(ConfirmDialog, { ...defaultProps, oncancel });

    await user.click(screen.getByTestId("confirm-dialog-overlay"));
    // Wait for any async side effects (e.g., bits-ui onOpenChange)
    await new Promise((r) => setTimeout(r, 50));
    expect(oncancel).toHaveBeenCalledTimes(1);
  });

  it("renders with warning variant", () => {
    render(ConfirmDialog, {
      ...defaultProps,
      title: "Archive nib?",
      confirmLabel: "Archive",
      variant: "warning" as const,
    });

    expect(screen.getByTestId("confirm-dialog-title")).toHaveTextContent("Archive nib?");
    expect(screen.getByTestId("confirm-dialog-confirm")).toHaveTextContent("Archive");
  });
});
