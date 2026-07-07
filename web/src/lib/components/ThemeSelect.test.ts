import { render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import ThemeSelect from "./ThemeSelect.svelte";

const user = userEvent.setup({ pointerEventsCheck: 0 });

describe("ThemeSelect", () => {
  it("renders all themes and fires onchange with the selected value", async () => {
    const onchange = vi.fn();
    render(ThemeSelect, { value: "graphite", onchange });

    // Trigger shows the current theme's label
    const trigger = screen.getByTestId("theme-select");
    expect(trigger).toHaveTextContent("Graphite");

    // Open the dropdown
    await user.click(trigger);

    // All themes should be present as options (labels shown)
    const labels = screen.getAllByRole("option").map((o) => o.textContent?.trim());
    expect(labels).toContain("Graphite");
    expect(labels).toContain("Midnight");
    expect(labels).toContain("Dracula");

    // Selecting emits the theme value (not the label)
    await user.click(screen.getByRole("option", { name: "Dracula" }));
    expect(onchange).toHaveBeenCalledWith("dracula");
  });

  it("does not open or emit when disabled", async () => {
    const onchange = vi.fn();
    render(ThemeSelect, { value: "midnight", onchange, disabled: true });

    const trigger = screen.getByTestId("theme-select");
    expect(trigger).toHaveTextContent("Midnight");

    await user.click(trigger);
    expect(screen.queryByRole("option")).not.toBeInTheDocument();
    expect(onchange).not.toHaveBeenCalled();
  });
});
