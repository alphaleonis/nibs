import { render, screen, waitFor } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import TypePickerPopover from "./TypePickerPopover.svelte";

// bits-ui scroll lock sets pointer-events: none on <body>, so disable the check
const user = userEvent.setup({ pointerEventsCheck: 0 });

describe("TypePickerPopover", () => {
  it("renders valid child types for an epic parent", async () => {
    render(TypePickerPopover, {
      parentType: "epic",
      onselect: vi.fn(),
      oncancel: vi.fn(),
    });

    await waitFor(() => {
      const items = screen.getAllByTestId("type-picker-item");
      expect(items).toHaveLength(4);
      expect(items[0]).toHaveTextContent("bug");
      expect(items[1]).toHaveTextContent("feature");
      expect(items[2]).toHaveTextContent("task");
      expect(items[3]).toHaveTextContent("research");
    });
  });

  it("renders no items for a milestone parent: a milestone takes no children", async () => {
    // The add-child affordance is absent on milestone rows, so this picker
    // should never open for one — but if it does, it must render empty rather
    // than offer an illegal child type.
    render(TypePickerPopover, {
      parentType: "milestone",
      onselect: vi.fn(),
      oncancel: vi.fn(),
    });

    await waitFor(() => {
      expect(screen.getByTestId("type-picker-popover")).toBeInTheDocument();
    });
    expect(screen.queryAllByTestId("type-picker-item")).toHaveLength(0);
  });

  it("calls onselect when a type is clicked", async () => {
    const onselect = vi.fn();
    render(TypePickerPopover, {
      parentType: "epic",
      onselect,
      oncancel: vi.fn(),
    });

    await waitFor(() => {
      expect(screen.getAllByTestId("type-picker-item").length).toBeGreaterThan(0);
    });

    const taskItem = screen.getAllByTestId("type-picker-item").find(
      el => el.textContent?.trim() === "task"
    );
    expect(taskItem).toBeDefined();
    await user.click(taskItem!);

    expect(onselect).toHaveBeenCalledWith("task");
  });

  it("renders task and research for a feature parent (a bug cannot be a feature child)", async () => {
    render(TypePickerPopover, {
      parentType: "feature",
      onselect: vi.fn(),
      oncancel: vi.fn(),
    });

    await waitFor(() => {
      const items = screen.getAllByTestId("type-picker-item");
      expect(items).toHaveLength(2);
      expect(items[0]).toHaveTextContent("task");
      expect(items[1]).toHaveTextContent("research");
    });
  });
});
