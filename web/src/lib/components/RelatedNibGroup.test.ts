import { render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import RelatedNibGroup from "./RelatedNibGroup.svelte";

const user = userEvent.setup({ pointerEventsCheck: 0 });

describe("RelatedNibGroup", () => {
  it("renders label and items with status dots", () => {
    render(RelatedNibGroup, {
      label: "Children",
      items: [{ id: "a", title: "Foo", status: "todo" }],
      testId: "detail-related-children",
    });

    // Container has the correct testid
    const container = screen.getByTestId("detail-related-children");
    expect(container).toBeInTheDocument();

    // Toggle button shows the label
    const toggle = screen.getByTestId("detail-group-toggle");
    expect(toggle).toBeInTheDocument();
    expect(toggle).toHaveTextContent("Children");

    // Item link renders title
    const link = screen.getByTestId("detail-related-link");
    expect(link).toHaveTextContent("Foo");

    // Status dot is present inside the link
    const dot = link.querySelector(".status-dot");
    expect(dot).toBeInTheDocument();
  });

  it("fires onnibselect when an item is clicked", async () => {
    const handler = vi.fn();
    render(RelatedNibGroup, {
      label: "Children",
      items: [
        { id: "a", title: "Foo", status: "todo" },
        { id: "b", title: "Bar", status: "completed" },
      ],
      onnibselect: handler,
      testId: "detail-related-children",
    });

    const links = screen.getAllByTestId("detail-related-link");
    await user.click(links[0]);
    expect(handler).toHaveBeenCalledWith("a");

    await user.click(links[1]);
    expect(handler).toHaveBeenCalledWith("b");
    expect(handler).toHaveBeenCalledTimes(2);
  });

  it("toggle collapses and expands items with correct aria-expanded", async () => {
    render(RelatedNibGroup, {
      label: "Children",
      items: [{ id: "a", title: "Foo", status: "todo" }],
      testId: "detail-related-children",
    });

    const toggle = screen.getByTestId("detail-group-toggle");

    // Starts expanded
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByTestId("detail-related-link")).toBeInTheDocument();

    // Click to collapse
    await user.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "false");
    expect(screen.queryByTestId("detail-related-link")).not.toBeInTheDocument();

    // Click again to re-expand
    await user.click(toggle);
    expect(toggle).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByTestId("detail-related-link")).toBeInTheDocument();
  });

  it("shows action button when onaction is provided and calls it on click", async () => {
    const actionHandler = vi.fn();
    render(RelatedNibGroup, {
      label: "Children",
      items: [{ id: "a", title: "Foo", status: "todo" }],
      onaction: actionHandler,
      testId: "detail-related-children",
    });

    const addBtn = screen.getByTestId("detail-related-add-child");
    expect(addBtn).toBeInTheDocument();

    await user.click(addBtn);
    expect(actionHandler).toHaveBeenCalledOnce();
  });

  it("hides action button when onaction is not provided", () => {
    render(RelatedNibGroup, {
      label: "Parent",
      items: [{ id: "a", title: "Foo", status: "todo" }],
      testId: "detail-related-parent",
    });

    expect(screen.queryByTestId("detail-related-add-child")).not.toBeInTheDocument();
  });
});
