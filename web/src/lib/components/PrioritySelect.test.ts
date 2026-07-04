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

    // Should have None option first, plus all priorities (with indicator symbols)
    const options = screen.getAllByRole("option");
    expect(options).toHaveLength(5); // None + 4 priorities

    await user.click(screen.getByRole("option", { name: "None" }));
    expect(onchange).toHaveBeenCalledWith("");
  });

  it("fires onchange with priority value when a priority is selected", async () => {
    const onchange = vi.fn();
    render(PrioritySelect, { value: "", onchange });

    await user.click(screen.getByTestId("priority-select"));
    // "critical" has the ‼ indicator, so accessible name includes it
    const criticalOption = screen.getAllByRole("option").find(
      (o) => o.getAttribute("data-value") === "critical",
    );
    expect(criticalOption).toBeTruthy();
    await user.click(criticalOption!);
    expect(onchange).toHaveBeenCalledWith("critical");
  });
});
