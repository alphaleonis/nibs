import { render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import SegmentedControl from "./SegmentedControl.svelte";

// bits-ui scroll lock can set pointer-events: none on <body>, so disable the check
const user = userEvent.setup({ pointerEventsCheck: 0 });

// Factory (not a shared object) so each test gets a fresh `onchange` spy — a
// module-level `vi.fn()` would accumulate call history across tests.
const defaultProps = () => ({
  value: "compact",
  options: [
    { value: "compact", label: "Compact" },
    { value: "comfortable", label: "Comfortable" },
  ],
  ariaLabel: "Row density",
  onchange: vi.fn(),
});

describe("SegmentedControl", () => {
  it("renders a radiogroup with the given ariaLabel and one radio per option", () => {
    render(SegmentedControl, { ...defaultProps() });

    expect(
      screen.getByRole("radiogroup", { name: /row density/i }),
    ).toBeInTheDocument();

    const radios = screen.getAllByRole("radio");
    expect(radios).toHaveLength(2);
    expect(screen.getByRole("radio", { name: /compact/i })).toBeInTheDocument();
    expect(
      screen.getByRole("radio", { name: /comfortable/i }),
    ).toBeInTheDocument();
  });

  it("marks the option equal to value as aria-checked and the others as not", () => {
    render(SegmentedControl, { ...defaultProps(), value: "comfortable" });

    expect(
      screen.getByRole("radio", { name: /comfortable/i }),
    ).toHaveAttribute("aria-checked", "true");
    expect(screen.getByRole("radio", { name: /compact/i })).toHaveAttribute(
      "aria-checked",
      "false",
    );
  });

  it("clicking another option calls onchange with its value", async () => {
    const onchange = vi.fn();
    render(SegmentedControl, { ...defaultProps(), value: "compact", onchange });

    await user.click(screen.getByRole("radio", { name: /comfortable/i }));

    expect(onchange).toHaveBeenCalledWith("comfortable");
  });

  it("supports arrow-key navigation (roving tabindex) and calls onchange", async () => {
    const onchange = vi.fn();
    render(SegmentedControl, { ...defaultProps(), value: "compact", onchange });

    // Focus the selected radio, then arrow to the next — the WAI-ARIA radiogroup
    // pattern bits-ui's RadioGroup implements for free (roving tabindex + arrows).
    screen.getByRole("radio", { name: /compact/i }).focus();
    await user.keyboard("{ArrowRight}");

    expect(onchange).toHaveBeenCalledWith("comfortable");
    expect(screen.getByRole("radio", { name: /comfortable/i })).toHaveFocus();
  });
});
