import { render } from "@testing-library/svelte";
import { describe, it, expect } from "vitest";
import BlockedBadge from "./BlockedBadge.svelte";

describe("BlockedBadge", () => {
  it("renders the pill variant by default with a lock icon, 'Blocked' label, and a count tooltip", () => {
    const { container } = render(BlockedBadge, { count: 3 });

    const badge = container.querySelector("[data-testid='blocked-badge']") as HTMLElement;
    expect(badge).toBeInTheDocument();
    expect(badge.textContent).toContain("Blocked");
    expect(badge.getAttribute("title")).toBe("Blocked by 3 nib(s)");
    // Lock icon renders as an SVG.
    expect(badge.querySelector("svg")).toBeInTheDocument();
    // The bare icon-only element must not be present in pill mode.
    expect(container.querySelector("[data-testid='blocked-icon']")).not.toBeInTheDocument();
  });

  it("renders the pill when variant is explicitly 'pill'", () => {
    const { container } = render(BlockedBadge, { count: 1, variant: "pill" });
    expect(container.querySelector("[data-testid='blocked-badge']")).toBeInTheDocument();
  });

  it("renders the icon variant as a bare lock with a tooltip and no 'Blocked' label", () => {
    const { container } = render(BlockedBadge, { count: 2, variant: "icon" });

    const icon = container.querySelector("[data-testid='blocked-icon']") as HTMLElement;
    expect(icon).toBeInTheDocument();
    expect(icon.getAttribute("title")).toBe("Blocked by 2 nib(s)");
    expect(icon.textContent).not.toContain("Blocked");
    expect(icon.querySelector("svg")).toBeInTheDocument();
    // No pill in icon mode.
    expect(container.querySelector("[data-testid='blocked-badge']")).not.toBeInTheDocument();
  });
});
