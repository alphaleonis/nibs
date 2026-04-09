import { render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import TypeSelect from "./TypeSelect.svelte";

const user = userEvent.setup({ pointerEventsCheck: 0 });

describe("TypeSelect", () => {
  it("renders all types and fires onchange", async () => {
    const onchange = vi.fn();
    render(TypeSelect, { value: "bug", onchange });

    const trigger = screen.getByTestId("type-select");
    expect(trigger).toHaveTextContent("bug");

    await user.click(trigger);

    const options = screen.getAllByRole("option");
    const labels = options.map((o) => o.textContent?.trim());
    expect(labels).toContain("milestone");
    expect(labels).toContain("epic");
    expect(labels).toContain("bug");
    expect(labels).toContain("feature");
    expect(labels).toContain("task");

    await user.click(screen.getByRole("option", { name: "feature" }));
    expect(onchange).toHaveBeenCalledWith("feature");
  });
});
