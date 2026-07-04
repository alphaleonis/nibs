import { render, screen, waitFor } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import SettingsSheet from "./SettingsSheet.svelte";

// bits-ui scroll lock sets pointer-events: none on <body>, so disable the check
const user = userEvent.setup({ pointerEventsCheck: 0 });

// Factory (not a shared object) so each test gets a fresh `ondensitychange`
// spy — a module-level `vi.fn()` would accumulate call history across tests.
const defaultProps = () => ({
  rowDensity: "compact" as const,
  ondensitychange: vi.fn(),
});

describe("SettingsSheet", () => {
  it("opens a right-side sheet with Settings title and Appearance section, no dimming overlay, closes on Escape", async () => {
    render(SettingsSheet, { ...defaultProps() });

    // Closed initially
    expect(screen.queryByText("Appearance")).not.toBeInTheDocument();

    // Gear button opens the sheet
    await user.click(screen.getByTitle("Settings"));

    // Title + Appearance section visible
    expect(screen.getByText("Settings")).toBeInTheDocument();
    expect(screen.getByText("Appearance")).toBeInTheDocument();

    // Non-modal: no dimming overlay rendered
    expect(document.querySelector('[data-slot="sheet-overlay"]')).toBeNull();

    // Escape closes it (the single Escape-close assertion in this file)
    await user.keyboard("{Escape}");
    expect(screen.queryByText("Appearance")).not.toBeInTheDocument();
  });

  it("shows a Row density radiogroup with Compact and Comfortable options", async () => {
    render(SettingsSheet, { ...defaultProps() });

    await user.click(screen.getByTitle("Settings"));

    const group = screen.getByRole("radiogroup", { name: /row density/i });
    expect(group).toBeInTheDocument();

    const options = screen.getAllByRole("radio");
    expect(options).toHaveLength(2);
    expect(screen.getByRole("radio", { name: /compact/i })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /comfortable/i })).toBeInTheDocument();
  });

  it("marks the option matching rowDensity as aria-checked", async () => {
    render(SettingsSheet, { ...defaultProps(), rowDensity: "comfortable" });

    await user.click(screen.getByTitle("Settings"));

    expect(screen.getByRole("radio", { name: /comfortable/i })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByRole("radio", { name: /compact/i })).toHaveAttribute("aria-checked", "false");
  });

  it("clicking the other density option calls ondensitychange with that value", async () => {
    const ondensitychange = vi.fn();
    render(SettingsSheet, { ...defaultProps(), rowDensity: "compact", ondensitychange });

    await user.click(screen.getByTitle("Settings"));
    await user.click(screen.getByRole("radio", { name: /comfortable/i }));

    expect(ondensitychange).toHaveBeenCalledWith("comfortable");
  });

  it("clicking from comfortable back to compact calls ondensitychange with compact", async () => {
    const ondensitychange = vi.fn();
    render(SettingsSheet, { ...defaultProps(), rowDensity: "comfortable", ondensitychange });

    await user.click(screen.getByTitle("Settings"));
    await user.click(screen.getByRole("radio", { name: /compact/i }));

    expect(ondensitychange).toHaveBeenCalledWith("compact");
  });

  it("supports keyboard navigation of the density radiogroup (arrow keys move selection)", async () => {
    const ondensitychange = vi.fn();
    render(SettingsSheet, { ...defaultProps(), rowDensity: "compact", ondensitychange });

    await user.click(screen.getByTitle("Settings"));

    // Focus the selected radio, then use the arrow key — the WAI-ARIA radiogroup
    // pattern the bits-ui RadioGroup implements for free (roving tabindex + arrows).
    screen.getByRole("radio", { name: /compact/i }).focus();
    await user.keyboard("{ArrowRight}");

    expect(ondensitychange).toHaveBeenCalledWith("comfortable");
  });

  it("exposes the sheet as a dialog whose accessible name is Settings", async () => {
    render(SettingsSheet, { ...defaultProps() });

    await user.click(screen.getByTitle("Settings"));

    // Accessible name resolves via Sheet.Title / aria-labelledby
    expect(screen.getByRole("dialog", { name: "Settings" })).toBeInTheDocument();
  });

  it("does not trap focus (trapFocus={false}): focus can settle outside the sheet", async () => {
    // A focusable element outside the portaled sheet. A modal (trapFocus) scope
    // installs a capturing focusin handler that yanks focus back inside; a
    // non-modal one does not — so this element must be able to hold focus.
    const outside = document.createElement("button");
    outside.textContent = "outside";
    document.body.appendChild(outside);

    try {
      render(SettingsSheet, { ...defaultProps() });
      await user.click(screen.getByTitle("Settings"));
      expect(screen.getByText("Appearance")).toBeInTheDocument();

      outside.focus();
      // If trapFocus were reverted to its modal default, focus would be pulled
      // back into the sheet and this assertion would fail.
      expect(outside).toHaveFocus();
    } finally {
      outside.remove();
    }
  });

  it("does not lock body scroll (preventScroll={false}): body pointer-events stay interactive", async () => {
    render(SettingsSheet, { ...defaultProps() });

    await user.click(screen.getByTitle("Settings"));
    expect(screen.getByText("Appearance")).toBeInTheDocument();

    // bits-ui's scroll lock (preventScroll default) sets pointer-events:none on
    // <body>; with preventScroll={false} the background stays interactive.
    expect(document.body.style.pointerEvents).not.toBe("none");
  });

  it("dismisses when clicking outside the sheet (interact-outside close)", async () => {
    render(SettingsSheet, { ...defaultProps() });
    await user.click(screen.getByTitle("Settings"));
    expect(screen.getByText("Appearance")).toBeInTheDocument();

    // bits-ui's interact-outside geometrically checks the pointer landed beyond the
    // content rect (isClickTrulyOutside). jsdom's getBoundingClientRect is all-zero
    // and user-event clicks at (0,0), so a plain click reads as "inside". Dispatch a
    // pointerdown with coords past the rect to represent a genuine outside click; the
    // handler is debounced (~10ms), so assert via waitFor.
    await user.pointer({
      keys: "[MouseLeft]",
      target: document.body,
      coords: { clientX: 9999, clientY: 9999 },
    });

    await waitFor(() =>
      expect(screen.queryByText("Appearance")).not.toBeInTheDocument(),
    );
  });

  it("dismisses via the visible close (X) button", async () => {
    render(SettingsSheet, { ...defaultProps() });

    await user.click(screen.getByTitle("Settings"));
    expect(screen.getByText("Appearance")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /close/i }));
    expect(screen.queryByText("Appearance")).not.toBeInTheDocument();
  });
});
