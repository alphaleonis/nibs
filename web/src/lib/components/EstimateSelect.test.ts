import { render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import EstimateSelect from "./EstimateSelect.svelte";

const user = userEvent.setup({ pointerEventsCheck: 0 });

describe("EstimateSelect", () => {
  it("shows label text when value is set", () => {
    render(EstimateSelect, { value: "m", onchange: vi.fn() });

    const trigger = screen.getByTestId("estimate-select");
    expect(trigger).toHaveTextContent("Medium");
  });

  it("shows 'None' when value is empty string", () => {
    render(EstimateSelect, { value: "", onchange: vi.fn() });

    const trigger = screen.getByTestId("estimate-select");
    expect(trigger).toHaveTextContent("None");
  });

  it("renders None option and all estimate labels", async () => {
    render(EstimateSelect, { value: "m", onchange: vi.fn() });

    await user.click(screen.getByTestId("estimate-select"));

    const options = screen.getAllByRole("option");
    const labels = options.map((o) => o.textContent?.trim());
    expect(labels).toContain("None");
    expect(labels).toContain("Small");
    expect(labels).toContain("Medium");
    expect(labels).toContain("Large");
    expect(labels).toContain("Extra Large");
  });

  it("fires onchange with estimate key when selected", async () => {
    const onchange = vi.fn();
    render(EstimateSelect, { value: "m", onchange });

    await user.click(screen.getByTestId("estimate-select"));
    await user.click(screen.getByRole("option", { name: "Large" }));
    expect(onchange).toHaveBeenCalledWith("l");
  });

  it("fires onchange with empty string when None is selected", async () => {
    const onchange = vi.fn();
    render(EstimateSelect, { value: "m", onchange });

    await user.click(screen.getByTestId("estimate-select"));
    await user.click(screen.getByRole("option", { name: "None" }));
    expect(onchange).toHaveBeenCalledWith("");
  });
});
