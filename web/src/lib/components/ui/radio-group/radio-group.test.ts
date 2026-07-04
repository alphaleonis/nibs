import { render, screen } from "@testing-library/svelte";
import { describe, it, expect } from "vitest";
import RadioGroupHarness from "./radio-group.harness.svelte";

// Locks the CANONICAL shadcn radio contract for the vendored ui/radio-group
// primitive: each item is a WAI-ARIA radio with the correct aria-checked; the
// checked item renders the Circle indicator (data-slot="radio-group-item-indicator")
// while the unchecked item does not; and the item wears the round disc shape
// (rounded-full), not the segmented-pill skin it was generalized out of — so it
// stays reusable for a vanilla radio list.
describe("ui/radio-group (canonical radio)", () => {
  it("renders each item as a radio with aria-checked reflecting the selected value", () => {
    render(RadioGroupHarness, { value: "a" });

    const a = screen.getByRole("radio", { name: /option a/i });
    const b = screen.getByRole("radio", { name: /option b/i });

    expect(a).toHaveAttribute("aria-checked", "true");
    expect(b).toHaveAttribute("aria-checked", "false");
  });

  it("renders the Circle indicator only inside the checked item", () => {
    render(RadioGroupHarness, { value: "a" });

    const a = screen.getByRole("radio", { name: /option a/i });
    const b = screen.getByRole("radio", { name: /option b/i });

    // The checked item shows the indicator (Circle svg); the unchecked one omits it.
    expect(
      a.querySelector('[data-slot="radio-group-item-indicator"] svg'),
    ).not.toBeNull();
    expect(
      b.querySelector('[data-slot="radio-group-item-indicator"] svg'),
    ).toBeNull();
  });

  it("wears the canonical round disc shape, not a segmented pill", () => {
    render(RadioGroupHarness, { value: "a" });

    const a = screen.getByRole("radio", { name: /option a/i });

    // The generalization (nibs-qj7m) restored the canonical disc; a regression to
    // the old segmented-pill skin would reintroduce rounded-sm / px-2.5.
    expect(a.className).toMatch(/rounded-full/);
    expect(a.className).not.toMatch(/rounded-sm|px-2\.5/);
  });
});
