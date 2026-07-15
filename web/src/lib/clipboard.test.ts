import { describe, it, expect, vi, beforeEach } from "vitest";

const { mockToastSuccess, mockToastError } = vi.hoisted(() => {
  return { mockToastSuccess: vi.fn(), mockToastError: vi.fn() };
});

vi.mock("svelte-sonner", () => ({
  toast: {
    success: mockToastSuccess,
    error: mockToastError,
  },
}));

import { copyToClipboard } from "./clipboard";

function stubClipboard(writeText: ReturnType<typeof vi.fn>) {
  Object.defineProperty(navigator, "clipboard", {
    value: { writeText },
    writable: true,
    configurable: true,
  });
}

describe("copyToClipboard", () => {
  beforeEach(() => {
    mockToastSuccess.mockReset();
    mockToastError.mockReset();
  });

  it("writes the text to the clipboard and shows a success toast", async () => {
    const writeText = vi.fn().mockResolvedValue(undefined);
    stubClipboard(writeText);

    await copyToClipboard("nibs-xyz");

    expect(writeText).toHaveBeenCalledWith("nibs-xyz");
    expect(mockToastSuccess).toHaveBeenCalledWith('Copied "nibs-xyz" to clipboard');
    expect(mockToastError).not.toHaveBeenCalled();
  });

  it("names the copied text instead of quoting it when a label is given", async () => {
    // Long text (a nib body) must not be inlined into the toast.
    const writeText = vi.fn().mockResolvedValue(undefined);
    stubClipboard(writeText);
    const body = "# Heading\n\n" + "long ".repeat(200);

    await copyToClipboard(body, "body");

    expect(writeText).toHaveBeenCalledWith(body);
    expect(mockToastSuccess).toHaveBeenCalledWith("Copied body to clipboard");
  });

  it("treats an empty label as a label, not as an absent one", async () => {
    // The branch turns on presence, not truthiness. Under a truthiness test an
    // empty label falls through to quoting the text — the unbounded branch the
    // label parameter exists to avoid, reached by the least conservative input.
    const writeText = vi.fn().mockResolvedValue(undefined);
    stubClipboard(writeText);
    const body = "a whole nib body that must never be inlined into a toast";

    await copyToClipboard(body, "");

    expect(mockToastSuccess).not.toHaveBeenCalledWith(expect.stringContaining(body));
  });

  it("shows an error toast when the clipboard write rejects", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("denied"));
    stubClipboard(writeText);

    await copyToClipboard("nibs-xyz");

    expect(writeText).toHaveBeenCalledWith("nibs-xyz");
    expect(mockToastError).toHaveBeenCalledWith("Failed to copy to clipboard");
    expect(mockToastSuccess).not.toHaveBeenCalled();
  });
});
