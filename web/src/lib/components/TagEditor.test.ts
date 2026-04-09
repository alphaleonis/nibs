import { render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import TagEditor from "./TagEditor.svelte";

const user = userEvent.setup({ pointerEventsCheck: 0 });

describe("TagEditor", () => {
  it("renders tag chips", () => {
    render(TagEditor, { tags: ["auth", "ui"], onadd: vi.fn(), onremove: vi.fn() });

    const chips = screen.getAllByTestId("tag-chip");
    expect(chips).toHaveLength(2);
    expect(chips[0]).toHaveTextContent("auth");
    expect(chips[1]).toHaveTextContent("ui");
  });

  it("calls onremove when X button is clicked", async () => {
    const onremove = vi.fn();
    render(TagEditor, { tags: ["auth", "ui"], onadd: vi.fn(), onremove });

    const removeButtons = screen.getAllByTestId("tag-remove");
    await user.click(removeButtons[0]);

    expect(onremove).toHaveBeenCalledWith("auth");
  });

  it("calls onadd with valid tag on Enter", async () => {
    const onadd = vi.fn();
    render(TagEditor, { tags: [], onadd, onremove: vi.fn() });

    const input = screen.getByTestId("tag-input");
    await user.type(input, "new-tag{Enter}");

    expect(onadd).toHaveBeenCalledWith("new-tag");
  });

  it("shows error and does not call onadd for invalid tag", async () => {
    const onadd = vi.fn();
    render(TagEditor, { tags: [], onadd, onremove: vi.fn() });

    const input = screen.getByTestId("tag-input");
    await user.type(input, "INVALID{Enter}");

    expect(onadd).not.toHaveBeenCalled();
    // Error message should be displayed
    expect(screen.getByText(/Tags must be lowercase/)).toBeInTheDocument();
  });

  it("clears input after successful add", async () => {
    const onadd = vi.fn();
    render(TagEditor, { tags: [], onadd, onremove: vi.fn() });

    const input = screen.getByTestId("tag-input") as HTMLInputElement;
    await user.type(input, "valid-tag{Enter}");

    expect(input.value).toBe("");
  });

  it("shows error when tag already exists", async () => {
    const onadd = vi.fn();
    render(TagEditor, { tags: ["auth"], onadd, onremove: vi.fn() });

    const input = screen.getByTestId("tag-input");
    await user.type(input, "auth{Enter}");

    expect(onadd).not.toHaveBeenCalled();
    expect(screen.getByText(/Tag already exists/)).toBeInTheDocument();
  });

  it("does not clear input when async onadd rejects", async () => {
    const onadd = vi.fn().mockRejectedValue(new Error("mutation failed"));
    render(TagEditor, { tags: [], onadd, onremove: vi.fn() });

    const input = screen.getByTestId("tag-input") as HTMLInputElement;
    await user.type(input, "new-tag{Enter}");

    expect(onadd).toHaveBeenCalledWith("new-tag");
    // Input should NOT be cleared because the async onadd failed
    expect(input.value).toBe("new-tag");
    // Should show error message
    expect(screen.getByText(/Failed to add tag/)).toBeInTheDocument();
  });

  it("clears input when async onadd resolves successfully", async () => {
    const onadd = vi.fn().mockResolvedValue(undefined);
    render(TagEditor, { tags: [], onadd, onremove: vi.fn() });

    const input = screen.getByTestId("tag-input") as HTMLInputElement;
    await user.type(input, "new-tag{Enter}");

    expect(onadd).toHaveBeenCalledWith("new-tag");
    expect(input.value).toBe("");
  });

  it("clears error on new input", async () => {
    render(TagEditor, { tags: [], onadd: vi.fn(), onremove: vi.fn() });

    const input = screen.getByTestId("tag-input");
    // Type invalid first to trigger error
    await user.type(input, "INVALID{Enter}");
    expect(screen.getByText(/Tags must be lowercase/)).toBeInTheDocument();

    // Typing more should clear the error
    await user.type(input, "a");
    expect(screen.queryByText(/Tags must be lowercase/)).not.toBeInTheDocument();
  });
});
