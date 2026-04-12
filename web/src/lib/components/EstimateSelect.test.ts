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

  it("renders None option and all estimate options", async () => {
    render(EstimateSelect, { value: "m", onchange: vi.fn() });

    await user.click(screen.getByTestId("estimate-select"));

    const options = screen.getAllByRole("option");
    expect(options).toHaveLength(5); // None + 4 estimates

    // Each estimate option includes the abbreviation and label
    const texts = options.map((o) => o.textContent?.trim());
    expect(texts).toContain("None");
    expect(texts?.some((t) => t?.includes("Small"))).toBe(true);
    expect(texts?.some((t) => t?.includes("Medium"))).toBe(true);
    expect(texts?.some((t) => t?.includes("Large"))).toBe(true);
    expect(texts?.some((t) => t?.includes("Extra Large"))).toBe(true);
  });

  it("fires onchange with estimate key when selected", async () => {
    const onchange = vi.fn();
    render(EstimateSelect, { value: "m", onchange });

    await user.click(screen.getByTestId("estimate-select"));
    const largeOption = screen.getAllByRole("option").find(
      (o) => o.getAttribute("data-value") === "l",
    );
    expect(largeOption).toBeTruthy();
    await user.click(largeOption!);
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
