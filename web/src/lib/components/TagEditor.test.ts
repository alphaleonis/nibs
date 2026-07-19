import { render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import TagEditor from "./TagEditor.svelte";

const user = userEvent.setup({ pointerEventsCheck: 0 });

/** Reveal the free-text input by clicking the Add-tag / "+" affordance. */
async function reveal() {
  await user.click(screen.getByTestId("tag-add"));
  return screen.getByTestId("tag-input") as HTMLInputElement;
}

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

  describe("add affordance", () => {
    it("shows an 'Add tag' button (not an input) when there are no tags", () => {
      render(TagEditor, { tags: [], onadd: vi.fn(), onremove: vi.fn() });

      expect(screen.getByTestId("tag-add")).toHaveTextContent("Add tag");
      expect(screen.queryByTestId("tag-input")).not.toBeInTheDocument();
    });

    it("shows a '+' affordance after the chips when tags exist", () => {
      render(TagEditor, { tags: ["auth"], onadd: vi.fn(), onremove: vi.fn() });

      expect(screen.getByTestId("tag-add")).toBeInTheDocument();
      expect(screen.queryByTestId("tag-input")).not.toBeInTheDocument();
    });

    it("reveals the input when the add affordance is clicked", async () => {
      render(TagEditor, { tags: [], onadd: vi.fn(), onremove: vi.fn() });

      expect(screen.queryByTestId("tag-input")).not.toBeInTheDocument();
      await reveal();
      expect(screen.getByTestId("tag-input")).toBeInTheDocument();
    });

    it("hides Add / '+' when disabled and renders chips read-only", () => {
      render(TagEditor, { tags: ["auth"], onadd: vi.fn(), onremove: vi.fn(), disabled: true });

      expect(screen.queryByTestId("tag-add")).not.toBeInTheDocument();
      expect(screen.queryByTestId("tag-input")).not.toBeInTheDocument();
      expect(screen.queryByTestId("tag-remove")).not.toBeInTheDocument();
      expect(screen.getByTestId("tag-chip")).toHaveTextContent("auth");
    });
  });

  describe("free-typed tags", () => {
    it("calls onadd with valid tag on Enter", async () => {
      const onadd = vi.fn();
      render(TagEditor, { tags: [], onadd, onremove: vi.fn() });

      const input = await reveal();
      await user.type(input, "new-tag{Enter}");

      expect(onadd).toHaveBeenCalledWith("new-tag");
    });

    it("shows error and does not call onadd for invalid tag", async () => {
      const onadd = vi.fn();
      render(TagEditor, { tags: [], onadd, onremove: vi.fn() });

      const input = await reveal();
      await user.type(input, "INVALID{Enter}");

      expect(onadd).not.toHaveBeenCalled();
      expect(screen.getByText(/Tags must be lowercase/)).toBeInTheDocument();
    });

    it("clears input after successful add", async () => {
      const onadd = vi.fn();
      render(TagEditor, { tags: [], onadd, onremove: vi.fn() });

      const input = await reveal();
      await user.type(input, "valid-tag{Enter}");

      expect(input.value).toBe("");
    });

    it("shows error when tag already exists", async () => {
      const onadd = vi.fn();
      render(TagEditor, { tags: ["auth"], onadd, onremove: vi.fn() });

      const input = await reveal();
      await user.type(input, "auth{Enter}");

      expect(onadd).not.toHaveBeenCalled();
      expect(screen.getByText(/Tag already exists/)).toBeInTheDocument();
    });

    it("does not clear input when async onadd rejects", async () => {
      const onadd = vi.fn().mockRejectedValue(new Error("mutation failed"));
      render(TagEditor, { tags: [], onadd, onremove: vi.fn() });

      const input = await reveal();
      await user.type(input, "new-tag{Enter}");

      expect(onadd).toHaveBeenCalledWith("new-tag");
      expect(input.value).toBe("new-tag");
      expect(screen.getByText(/Failed to add tag/)).toBeInTheDocument();
    });

    it("clears input when async onadd resolves successfully", async () => {
      const onadd = vi.fn().mockResolvedValue(undefined);
      render(TagEditor, { tags: [], onadd, onremove: vi.fn() });

      const input = await reveal();
      await user.type(input, "new-tag{Enter}");

      expect(onadd).toHaveBeenCalledWith("new-tag");
      expect(input.value).toBe("");
    });

    it("clears error on new input", async () => {
      render(TagEditor, { tags: [], onadd: vi.fn(), onremove: vi.fn() });

      const input = await reveal();
      await user.type(input, "INVALID{Enter}");
      expect(screen.getByText(/Tags must be lowercase/)).toBeInTheDocument();

      await user.type(input, "a");
      expect(screen.queryByText(/Tags must be lowercase/)).not.toBeInTheDocument();
    });

    it("closes the input on Escape", async () => {
      render(TagEditor, { tags: [], onadd: vi.fn(), onremove: vi.fn() });

      const input = await reveal();
      await user.type(input, "{Escape}");

      expect(screen.queryByTestId("tag-input")).not.toBeInTheDocument();
      expect(screen.getByTestId("tag-add")).toBeInTheDocument();
    });
  });

  describe("suggestions", () => {
    it("offers available tags, excluding already-applied ones", async () => {
      render(TagEditor, {
        tags: ["auth"],
        suggestions: ["auth", "ui", "web-ui"],
        onadd: vi.fn(),
        onremove: vi.fn(),
      });

      await reveal();
      const options = screen.getAllByTestId("tag-suggestion").map((o) => o.textContent?.trim());
      expect(options).toEqual(["ui", "web-ui"]);
      expect(options).not.toContain("auth");
    });

    it("filters suggestions by the typed query", async () => {
      render(TagEditor, {
        tags: [],
        suggestions: ["auth", "ui", "web-ui"],
        onadd: vi.fn(),
        onremove: vi.fn(),
      });

      const input = await reveal();
      await user.type(input, "web");

      const options = screen.getAllByTestId("tag-suggestion").map((o) => o.textContent?.trim());
      expect(options).toEqual(["web-ui"]);
    });

    it("adds a suggestion when clicked", async () => {
      const onadd = vi.fn();
      render(TagEditor, {
        tags: [],
        suggestions: ["auth", "ui"],
        onadd,
        onremove: vi.fn(),
      });

      await reveal();
      await user.click(screen.getByText("ui"));

      expect(onadd).toHaveBeenCalledWith("ui");
    });

    it("selects a suggestion via keyboard (ArrowDown + Enter)", async () => {
      const onadd = vi.fn();
      render(TagEditor, {
        tags: [],
        suggestions: ["auth", "ui"],
        onadd,
        onremove: vi.fn(),
      });

      const input = await reveal();
      await user.type(input, "{ArrowDown}{Enter}");

      expect(onadd).toHaveBeenCalledWith("auth");
    });

    it("adds a valid free-typed tag over the highlighted suggestion when none is active", async () => {
      const onadd = vi.fn();
      render(TagEditor, {
        tags: [],
        suggestions: ["auth", "ui"],
        onadd,
        onremove: vi.fn(),
      });

      const input = await reveal();
      // No ArrowDown: activeIndex stays -1, so Enter commits the typed text.
      await user.type(input, "brand-new{Enter}");

      expect(onadd).toHaveBeenCalledWith("brand-new");
    });
  });
});
