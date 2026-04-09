import { render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import FilterBar from "./FilterBar.svelte";
import type { NibFilter } from "../types";

// bits-ui scroll lock sets pointer-events: none on <body>, so disable the check
const user = userEvent.setup({ pointerEventsCheck: 0 });

const defaultProps = {
  filter: {} as NibFilter,
  onchange: vi.fn(),
  availableTags: [] as string[],
};

describe("FilterBar", () => {
  it("renders keyword input and dropdown buttons for Type, Priority, State, and Effort", () => {
    render(FilterBar, { ...defaultProps });

    expect(screen.getByPlaceholderText(/keyword/i)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /type/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /priority/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /state/i })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /effort/i })).toBeInTheDocument();
  });

  it("fires onchange with updated search when typing in keyword input", async () => {
    const onchange = vi.fn();
    render(FilterBar, { ...defaultProps, onchange });

    const input = screen.getByPlaceholderText(/keyword/i);
    await user.type(input, "login");

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0]).toMatchObject({ search: "login" });
  });

  it("clears search to undefined when keyword input is emptied", async () => {
    const onchange = vi.fn();
    render(FilterBar, { ...defaultProps, filter: { search: "old" }, onchange });

    const input = screen.getByPlaceholderText(/keyword/i);
    await user.clear(input);

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0].search).toBeUndefined();
  });

  it("opens a checkbox panel when a dropdown button is clicked", async () => {
    render(FilterBar, { ...defaultProps });

    // No checkboxes visible initially
    expect(screen.queryByRole("menuitemcheckbox", { name: "bug" })).not.toBeInTheDocument();

    // Click Type button
    await user.click(screen.getByRole("button", { name: /type/i }));

    // Type checkboxes should be visible
    expect(screen.getByRole("menuitemcheckbox", { name: "bug" })).toBeInTheDocument();
    expect(screen.getByRole("menuitemcheckbox", { name: "feature" })).toBeInTheDocument();
    expect(screen.getByRole("menuitemcheckbox", { name: "task" })).toBeInTheDocument();
    expect(screen.getByRole("menuitemcheckbox", { name: "milestone" })).toBeInTheDocument();
    expect(screen.getByRole("menuitemcheckbox", { name: "epic" })).toBeInTheDocument();
  });

  it("closes an open panel when the same dropdown button is clicked again", async () => {
    render(FilterBar, { ...defaultProps });

    const typeBtn = screen.getByRole("button", { name: /type/i });

    // Open
    await user.click(typeBtn);
    expect(screen.getByRole("menuitemcheckbox", { name: "bug" })).toBeInTheDocument();

    // Close
    await user.click(typeBtn);
    expect(screen.queryByRole("menuitemcheckbox", { name: "bug" })).not.toBeInTheDocument();
  });

  it("closes an open dropdown when another dropdown trigger is clicked", async () => {
    render(FilterBar, { ...defaultProps });

    // Open the Type dropdown
    await user.click(screen.getByRole("button", { name: /type/i }));
    expect(screen.getByRole("menuitemcheckbox", { name: "bug" })).toBeInTheDocument();

    // Click the Priority dropdown trigger — Type dropdown should close
    await user.click(screen.getByRole("button", { name: /priority/i }));
    expect(screen.queryByRole("menuitemcheckbox", { name: "bug" })).not.toBeInTheDocument();
    // Priority checkboxes should now be visible
    expect(screen.getByRole("menuitemcheckbox", { name: "high" })).toBeInTheDocument();
  });

  it("closes an open dropdown when Escape is pressed", async () => {
    render(FilterBar, { ...defaultProps });

    // Open the Type dropdown
    await user.click(screen.getByRole("button", { name: /type/i }));
    expect(screen.getByRole("menuitemcheckbox", { name: "bug" })).toBeInTheDocument();

    // Press Escape
    await user.keyboard("{Escape}");
    expect(screen.queryByRole("menuitemcheckbox", { name: "bug" })).not.toBeInTheDocument();
  });

  it("checking a type checkbox immediately emits filter with that type", async () => {
    const onchange = vi.fn();
    render(FilterBar, { ...defaultProps, onchange });

    await user.click(screen.getByRole("button", { name: /type/i }));
    await user.click(screen.getByRole("menuitemcheckbox", { name: "bug" }));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0]).toMatchObject({ type: ["bug"] });
  });

  it("checking a priority checkbox emits filter with that priority", async () => {
    const onchange = vi.fn();
    render(FilterBar, { ...defaultProps, onchange });

    await user.click(screen.getByRole("button", { name: /priority/i }));
    await user.click(screen.getByRole("menuitemcheckbox", { name: "high" }));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0]).toMatchObject({ priority: ["high"] });
  });

  it("checking a state checkbox emits filter with that status", async () => {
    const onchange = vi.fn();
    render(FilterBar, { ...defaultProps, onchange });

    await user.click(screen.getByRole("button", { name: /state/i }));
    await user.click(screen.getByRole("menuitemcheckbox", { name: "in-progress" }));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0]).toMatchObject({ status: ["in-progress"] });
  });

  it("checking an effort checkbox emits filter with that estimate", async () => {
    const onchange = vi.fn();
    render(FilterBar, { ...defaultProps, onchange });

    await user.click(screen.getByRole("button", { name: /effort/i }));
    await user.click(screen.getByRole("menuitemcheckbox", { name: "l" }));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0]).toMatchObject({ estimate: ["l"] });
  });

  it("unchecking the last checkbox in a category removes the filter field", async () => {
    const onchange = vi.fn();
    render(FilterBar, { ...defaultProps, filter: { type: ["bug"] }, onchange });

    await user.click(screen.getByRole("button", { name: /type/i }));
    await user.click(screen.getByRole("menuitemcheckbox", { name: "bug" }));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0].type).toBeUndefined();
  });

  it("does not render Tags button when availableTags is empty", () => {
    render(FilterBar, { ...defaultProps, availableTags: [] });
    expect(screen.queryByRole("button", { name: /tags/i })).not.toBeInTheDocument();
  });

  it("renders Tags dropdown with checkboxes when availableTags has items", async () => {
    const onchange = vi.fn();
    render(FilterBar, {
      ...defaultProps,
      onchange,
      availableTags: ["frontend", "backend"],
    });

    const tagsBtn = screen.getByRole("button", { name: /tags/i });
    expect(tagsBtn).toBeInTheDocument();

    await user.click(tagsBtn);
    expect(screen.getByRole("menuitemcheckbox", { name: "frontend" })).toBeInTheDocument();
    expect(screen.getByRole("menuitemcheckbox", { name: "backend" })).toBeInTheDocument();

    // Check one
    await user.click(screen.getByRole("menuitemcheckbox", { name: "frontend" }));
    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0]).toMatchObject({ tags: ["frontend"] });
  });

  it("Clear all button is disabled when no advanced filters are active", () => {
    render(FilterBar, { ...defaultProps, filter: {} });
    expect(screen.getByRole("button", { name: /clear all/i })).toBeDisabled();
  });

  it("Clear all button is disabled when only search is active (not an advanced filter)", () => {
    render(FilterBar, { ...defaultProps, filter: { search: "hello" } });
    expect(screen.getByRole("button", { name: /clear all/i })).toBeDisabled();
  });

  it("Clear all button resets advanced filters but preserves search", async () => {
    const onchange = vi.fn();
    render(FilterBar, {
      ...defaultProps,
      filter: { search: "auth", type: ["bug"], priority: ["high"], status: ["todo"], estimate: ["l"] },
      onchange,
    });

    await user.click(screen.getByRole("button", { name: /clear all/i }));

    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0].search).toBe("auth");
    expect(lastCall[0].type).toBeUndefined();
    expect(lastCall[0].priority).toBeUndefined();
    expect(lastCall[0].status).toBeUndefined();
    expect(lastCall[0].estimate).toBeUndefined();
  });

  it("dropdown panel Clear button clears the category when selections exist", async () => {
    const onchange = vi.fn();
    render(FilterBar, { ...defaultProps, filter: { type: ["bug", "feature"] }, onchange });

    await user.click(screen.getByRole("button", { name: /type/i }));

    const clearItem = screen.getByRole("menuitem", { name: /clear/i });
    expect(clearItem).toBeInTheDocument();

    await user.click(clearItem);
    const lastCall = onchange.mock.calls[onchange.mock.calls.length - 1];
    expect(lastCall[0].type).toBeUndefined();
  });

  it("dropdown panel Clear button is disabled when no selections in category", async () => {
    render(FilterBar, { ...defaultProps, filter: {} });

    await user.click(screen.getByRole("button", { name: /type/i }));

    const clearItem = screen.getByRole("menuitem", { name: /clear/i });
    expect(clearItem).toBeInTheDocument();
    expect(clearItem).toHaveAttribute("data-disabled", "");
  });

  it("dropdown buttons show active count badge when category has selections", () => {
    render(FilterBar, {
      ...defaultProps,
      filter: { type: ["bug", "feature"], priority: ["high"] },
    });

    // Type button should show count 2
    const typeBtn = screen.getByRole("button", { name: /type/i });
    expect(typeBtn).toHaveTextContent("2");

    // Priority button should show count 1
    const priorityBtn = screen.getByRole("button", { name: /priority/i });
    expect(priorityBtn).toHaveTextContent("1");

    // State and Effort badge should be invisible (no active selections)
    const stateBtn = screen.getByRole("button", { name: /state/i });
    expect(stateBtn).toHaveTextContent("State");
    const stateBadge = stateBtn.querySelector("span");
    expect(stateBadge?.classList.contains("invisible")).toBe(true);

    const effortBtn = screen.getByRole("button", { name: /effort/i });
    expect(effortBtn).toHaveTextContent("Effort");
    const effortBadge = effortBtn.querySelector("span");
    expect(effortBadge?.classList.contains("invisible")).toBe(true);
  });
});
