import { render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import PrioritySelect from "./PrioritySelect.svelte";

const user = userEvent.setup({ pointerEventsCheck: 0 });

describe("PrioritySelect", () => {
  it("shows 'None' when value is empty string", () => {
    render(PrioritySelect, { value: "", onchange: vi.fn() });

    const trigger = screen.getByTestId("priority-select");
    expect(trigger).toHaveTextContent("None");
  });

  it("shows the priority value when set", () => {
    render(PrioritySelect, { value: "high", onchange: vi.fn() });

    const trigger = screen.getByTestId("priority-select");
    expect(trigger).toHaveTextContent("high");
  });

  it("fires onchange with empty string when 'None' is selected", async () => {
    const onchange = vi.fn();
    render(PrioritySelect, { value: "high", onchange });

    await user.click(screen.getByTestId("priority-select"));

    // Should have None option first, plus all priorities
    const options = screen.getAllByRole("option");
    const labels = options.map((o) => o.textContent?.trim());
    expect(labels).toContain("None");
    expect(labels).toContain("critical");
    expect(labels).toContain("high");
    expect(labels).toContain("normal");
    expect(labels).toContain("low");
    expect(labels).toContain("deferred");

    await user.click(screen.getByRole("option", { name: "None" }));
    expect(onchange).toHaveBeenCalledWith("");
  });

  it("fires onchange with priority value when a priority is selected", async () => {
    const onchange = vi.fn();
    render(PrioritySelect, { value: "", onchange });

    await user.click(screen.getByTestId("priority-select"));
    await user.click(screen.getByRole("option", { name: "critical" }));
    expect(onchange).toHaveBeenCalledWith("critical");
  });
});
