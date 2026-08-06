import { render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import SuggestionList from "./SuggestionList.svelte";

const user = userEvent.setup({ pointerEventsCheck: 0 });

describe("SuggestionList", () => {
  it("renders one option per item", () => {
    render(SuggestionList, { items: ["type", "tags"], onselect: vi.fn() });

    const options = screen.getAllByRole("option");
    expect(options).toHaveLength(2);
    expect(options[0]).toHaveTextContent("type");
    expect(options[1]).toHaveTextContent("tags");
  });

  it("renders nothing when there are no items", () => {
    render(SuggestionList, { items: [], onselect: vi.fn() });
    expect(screen.queryByRole("listbox")).not.toBeInTheDocument();
  });

  it("marks the active row with aria-selected", () => {
    render(SuggestionList, { items: ["bug", "feature"], activeIndex: 1, onselect: vi.fn() });

    const options = screen.getAllByRole("option");
    expect(options[0]).toHaveAttribute("aria-selected", "false");
    expect(options[1]).toHaveAttribute("aria-selected", "true");
  });

  it("calls onselect with the item and index when clicked", async () => {
    const onselect = vi.fn();
    render(SuggestionList, { items: ["bug", "feature"], onselect });

    await user.click(screen.getByText("feature"));
    expect(onselect).toHaveBeenCalledWith("feature", 1);
  });

  it("prevents default on mousedown so the anchoring input keeps focus", () => {
    render(SuggestionList, { items: ["bug"], onselect: vi.fn() });

    const option = screen.getByRole("option");
    const event = new MouseEvent("mousedown", { bubbles: true, cancelable: true });
    option.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(true);
  });

  it("honors custom test ids", () => {
    render(SuggestionList, {
      items: ["bug"],
      onselect: vi.fn(),
      testId: "filter-suggestions",
      itemTestId: "filter-suggestion",
    });

    expect(screen.getByTestId("filter-suggestions")).toBeInTheDocument();
    expect(screen.getByTestId("filter-suggestion")).toBeInTheDocument();
  });
});
