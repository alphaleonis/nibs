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

  it("shows an error toast when the clipboard write rejects", async () => {
    const writeText = vi.fn().mockRejectedValue(new Error("denied"));
    stubClipboard(writeText);

    await copyToClipboard("nibs-xyz");

    expect(writeText).toHaveBeenCalledWith("nibs-xyz");
    expect(mockToastError).toHaveBeenCalledWith("Failed to copy to clipboard");
    expect(mockToastSuccess).not.toHaveBeenCalled();
  });
});
