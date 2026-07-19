import { render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import PrioritySelect from "./PrioritySelect.svelte";

const user = userEvent.setup({ pointerEventsCheck: 0 });

describe("PrioritySelect", () => {
  it("renders empty value as the effective 'normal' priority", () => {
    render(PrioritySelect, { value: "", onchange: vi.fn() });

    const trigger = screen.getByTestId("priority-select");
    expect(trigger).toHaveTextContent("normal");
    expect(trigger).not.toHaveTextContent("None");
  });

  it("shows the priority value when set", () => {
    render(PrioritySelect, { value: "high", onchange: vi.fn() });

    const trigger = screen.getByTestId("priority-select");
    expect(trigger).toHaveTextContent("high");
  });

  it("offers exactly the 4 configured priorities and no 'None' option", async () => {
    render(PrioritySelect, { value: "high", onchange: vi.fn() });

    await user.click(screen.getByTestId("priority-select"));

    const options = screen.getAllByRole("option");
    expect(options).toHaveLength(4);
    expect(screen.queryByRole("option", { name: "None" })).toBeNull();
  });

  it("fires onchange with 'normal' when demoting via the normal option", async () => {
    const onchange = vi.fn();
    render(PrioritySelect, { value: "high", onchange });

    await user.click(screen.getByTestId("priority-select"));

    // Locate by data-value since accessible names may include indicator symbols.
    const normalOption = screen.getAllByRole("option").find(
      (o) => o.getAttribute("data-value") === "normal",
    );
    expect(normalOption).toBeTruthy();
    await user.click(normalOption!);
    expect(onchange).toHaveBeenCalledWith("normal");
  });

  it("fires onchange with the picked priority from an empty value", async () => {
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
