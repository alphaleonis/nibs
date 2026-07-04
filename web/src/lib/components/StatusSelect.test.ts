import { render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import StatusSelect from "./StatusSelect.svelte";

const user = userEvent.setup({ pointerEventsCheck: 0 });

describe("StatusSelect", () => {
  it("renders all statuses and fires onchange", async () => {
    const onchange = vi.fn();
    render(StatusSelect, { value: "todo", onchange });

    // Trigger should show current value
    const trigger = screen.getByTestId("status-select");
    expect(trigger).toHaveTextContent("todo");

    // Open dropdown
    await user.click(trigger);

    // All statuses should be present as options
    const options = screen.getAllByRole("option");
    const labels = options.map((o) => o.textContent?.trim());
    expect(labels).toContain("draft");
    expect(labels).toContain("todo");
    expect(labels).toContain("in-progress");
    expect(labels).toContain("deferred");
    expect(labels).toContain("completed");
    expect(labels).toContain("scrapped");

    // Select a different status
    await user.click(screen.getByRole("option", { name: "in-progress" }));
    expect(onchange).toHaveBeenCalledWith("in-progress");
  });
});
