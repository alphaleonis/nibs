import { render, screen, waitFor, fireEvent, within } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import SettingsSheet from "./SettingsSheet.svelte";
import { tick } from "svelte";

// The panel is non-modal (no scroll lock / pointer-events:none on <body>), but
// keep the check disabled for parity with other portaled-content suites.
const user = userEvent.setup({ pointerEventsCheck: 0 });

// Factory (not a shared object) so each test gets a fresh `ondensitychange`
// spy — a module-level `vi.fn()` would accumulate call history across tests.
const defaultProps = () => ({
  rowDensity: "compact" as const,
  ondensitychange: vi.fn(),
  fontSize: "medium" as const,
  onfontsizechange: vi.fn(),
  blockedEmphasis: "pill" as const,
  onemphasischange: vi.fn(),
  regionBands: "on-drag" as const,
  onregionbandschange: vi.fn(),
  theme: "graphite" as const,
  onthemechange: vi.fn(),
  detailPanelPosition: "right" as const,
  onpositionchange: vi.fn(),
  openDetailOn: "single" as const,
  onopendetailchange: vi.fn(),
});

describe("SettingsSheet", () => {
  it("opens a non-modal panel with Settings title + Appearance section; closed initially; no overlay", async () => {
    render(SettingsSheet, { ...defaultProps() });

    // Closed initially
    expect(screen.queryByText("Appearance")).not.toBeInTheDocument();

    // Gear button opens the panel
    await user.click(screen.getByRole("button", { name: "Settings" }));

    // Title + Appearance section visible
    expect(screen.getByText("Settings")).toBeInTheDocument();
    expect(screen.getByText("Appearance")).toBeInTheDocument();

    // Non-modal contract: the panel pins to one edge (`fixed inset-y-0 right-0`)
    // and must NOT render a full-screen dimming backdrop — so no element covers
    // the whole viewport via `fixed inset-0`.
    expect(document.querySelector(".fixed.inset-0")).toBeNull();
  });

  it("exposes the panel as a NON-MODAL dialog (aria-modal=false) named Settings", async () => {
    render(SettingsSheet, { ...defaultProps() });

    await user.click(screen.getByRole("button", { name: "Settings" }));

    // The p07b fix: role=dialog with aria-modal="false" (was "true" under bits-ui Dialog).
    const dialog = screen.getByRole("dialog", { name: "Settings" });
    expect(dialog).toBeInTheDocument();
    expect(dialog).toHaveAttribute("aria-modal", "false");
  });

  it("does not lock body scroll: body pointer-events stay interactive while open", async () => {
    render(SettingsSheet, { ...defaultProps() });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    expect(screen.getByText("Appearance")).toBeInTheDocument();

    // Real non-modal contract: the background stays interactive, so <body> must
    // not have pointer-events disabled while the panel is open.
    expect(document.body.style.pointerEvents).not.toBe("none");
    // Regression trip-wires: a modal bits-ui Dialog would stamp these on <body>.
    // The hand-wired non-modal <aside> must never reintroduce a modal primitive.
    expect(document.body.hasAttribute("data-scroll-locked")).toBe(false);
    expect(document.body.hasAttribute("inert")).toBe(false);
  });

  it("closes on Escape", async () => {
    render(SettingsSheet, { ...defaultProps() });
    await user.click(screen.getByRole("button", { name: "Settings" }));
    expect(screen.getByText("Appearance")).toBeInTheDocument();

    await user.keyboard("{Escape}");

    // Fly-out transition delays unmount, so assert via waitFor.
    await waitFor(() =>
      expect(screen.queryByText("Appearance")).not.toBeInTheDocument(),
    );
  });

  it("closes on Escape even when focus has moved to a background element", async () => {
    render(SettingsSheet, { ...defaultProps() });
    await user.click(screen.getByRole("button", { name: "Settings" }));
    expect(screen.getByText("Appearance")).toBeInTheDocument();

    // Non-modal panels don't trap focus: the user may Tab into the (still
    // interactive) background. Simulate that with a real outside element that
    // holds focus, then press Escape. A keydown handler bound to the portaled
    // <aside> would never see this event — the Escape listener must be at the
    // document level for the panel to dismiss.
    const outside = document.createElement("button");
    document.body.appendChild(outside);
    try {
      outside.focus();
      expect(document.activeElement).toBe(outside);

      await user.keyboard("{Escape}");

      await waitFor(() =>
        expect(screen.queryByText("Appearance")).not.toBeInTheDocument(),
      );
    } finally {
      // Remove even if an assertion throws — otherwise the stray <button> leaks
      // into the shared per-file document exactly when this regression breaks.
      outside.remove();
    }
  });

  it("closes when clicking outside the panel", async () => {
    render(SettingsSheet, { ...defaultProps() });
    await user.click(screen.getByRole("button", { name: "Settings" }));
    expect(screen.getByText("Appearance")).toBeInTheDocument();

    // clickOutside listens for a real pointerdown on document; a pointerdown on
    // <body> lands outside the panel (and outside the trigger) and dismisses it.
    fireEvent.pointerDown(document.body);

    await waitFor(() =>
      expect(screen.queryByText("Appearance")).not.toBeInTheDocument(),
    );
  });

  it("does not close on a pointerdown inside the panel content", async () => {
    render(SettingsSheet, { ...defaultProps() });
    await user.click(screen.getByRole("button", { name: "Settings" }));
    expect(screen.getByText("Appearance")).toBeInTheDocument();

    // The clickOutside `node.contains` branch: a pointerdown on real panel
    // content (through the bind:this + Portal wiring) must NOT dismiss.
    fireEvent.pointerDown(screen.getByText("Appearance"));

    // Flush pending effects: an erroneous dismissal would set open=false and
    // unmount the panel, so a surviving panel proves no dismissal occurred.
    await tick();
    expect(screen.getByText("Appearance")).toBeInTheDocument();
  });

  it("does not close on a pointerdown on the gear trigger (ignore path)", async () => {
    render(SettingsSheet, { ...defaultProps() });
    const trigger = screen.getByRole("button", { name: "Settings" });
    await user.click(trigger);
    expect(screen.getByText("Appearance")).toBeInTheDocument();

    // The clickOutside `ignore` branch: a pointerdown on the trigger must NOT
    // dismiss (so the trigger's own click toggles cleanly without a double-fire).
    fireEvent.pointerDown(trigger);

    await tick();
    expect(screen.getByText("Appearance")).toBeInTheDocument();
  });

  it("re-opens and refocuses the panel after a close (wasOpen cycle)", async () => {
    render(SettingsSheet, { ...defaultProps() });
    const trigger = screen.getByRole("button", { name: "Settings" });

    // First open/close cycle.
    await user.click(trigger);
    await waitFor(() =>
      expect(
        screen.getByRole("dialog", { name: "Settings" }).contains(document.activeElement),
      ).toBe(true),
    );
    await user.keyboard("{Escape}");
    await waitFor(() =>
      expect(screen.queryByText("Appearance")).not.toBeInTheDocument(),
    );

    // Second open must refocus into the panel again — guards the `wasOpen`
    // transition guard from getting stuck after the first cycle.
    await user.click(trigger);
    await waitFor(() =>
      expect(
        screen.getByRole("dialog", { name: "Settings" }).contains(document.activeElement),
      ).toBe(true),
    );
  });

  it("closes via the visible close (X) button", async () => {
    render(SettingsSheet, { ...defaultProps() });
    await user.click(screen.getByRole("button", { name: "Settings" }));
    expect(screen.getByText("Appearance")).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /close/i }));

    await waitFor(() =>
      expect(screen.queryByText("Appearance")).not.toBeInTheDocument(),
    );
  });

  it("moves focus into the panel on open and returns focus to the gear on close", async () => {
    render(SettingsSheet, { ...defaultProps() });
    const trigger = screen.getByRole("button", { name: "Settings" });

    await user.click(trigger);

    const dialog = screen.getByRole("dialog", { name: "Settings" });
    // Focus moves into the panel (the panel itself is focusable, tabindex=-1).
    await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true));

    await user.keyboard("{Escape}");

    // On close, focus returns to the gear trigger.
    await waitFor(() => expect(trigger).toHaveFocus());
  });

  it("shows a Row density radiogroup with Compact and Comfortable options", async () => {
    render(SettingsSheet, { ...defaultProps() });

    await user.click(screen.getByRole("button", { name: "Settings" }));

    const group = screen.getByRole("radiogroup", { name: /row density/i });
    expect(group).toBeInTheDocument();

    // Scope to the density group — the sheet has several other radiogroups
    // (font size, blocked emphasis, detail panel position), so a document-wide
    // radio query would return more than these two.
    const options = within(group).getAllByRole("radio");
    expect(options).toHaveLength(2);
    expect(screen.getByRole("radio", { name: /compact/i })).toBeInTheDocument();
    expect(screen.getByRole("radio", { name: /comfortable/i })).toBeInTheDocument();
  });

  it("marks the option matching rowDensity as aria-checked", async () => {
    render(SettingsSheet, { ...defaultProps(), rowDensity: "comfortable" });

    await user.click(screen.getByRole("button", { name: "Settings" }));

    expect(screen.getByRole("radio", { name: /comfortable/i })).toHaveAttribute("aria-checked", "true");
    expect(screen.getByRole("radio", { name: /compact/i })).toHaveAttribute("aria-checked", "false");
  });

  it("clicking the other density option calls ondensitychange with that value", async () => {
    const ondensitychange = vi.fn();
    render(SettingsSheet, { ...defaultProps(), rowDensity: "compact", ondensitychange });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    await user.click(screen.getByRole("radio", { name: /comfortable/i }));

    expect(ondensitychange).toHaveBeenCalledWith("comfortable");
  });

  it("clicking from comfortable back to compact calls ondensitychange with compact", async () => {
    const ondensitychange = vi.fn();
    render(SettingsSheet, { ...defaultProps(), rowDensity: "comfortable", ondensitychange });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    await user.click(screen.getByRole("radio", { name: /compact/i }));

    expect(ondensitychange).toHaveBeenCalledWith("compact");
  });

  it("supports keyboard navigation of the density radiogroup (arrow keys move selection)", async () => {
    const ondensitychange = vi.fn();
    render(SettingsSheet, { ...defaultProps(), rowDensity: "compact", ondensitychange });

    await user.click(screen.getByRole("button", { name: "Settings" }));

    // Focus the selected radio, then use the arrow key — the WAI-ARIA radiogroup
    // pattern the bits-ui RadioGroup implements for free (roving tabindex + arrows).
    screen.getByRole("radio", { name: /compact/i }).focus();
    await user.keyboard("{ArrowRight}");

    expect(ondensitychange).toHaveBeenCalledWith("comfortable");
  });

  it("shows a Blocked emphasis radiogroup with Subtle, Pill, and Pill+dim options", async () => {
    render(SettingsSheet, { ...defaultProps() });

    await user.click(screen.getByRole("button", { name: "Settings" }));

    const group = screen.getByRole("radiogroup", { name: /blocked emphasis/i });
    expect(group).toBeInTheDocument();

    const options = within(group).getAllByRole("radio");
    expect(options).toHaveLength(3);
    expect(within(group).getByRole("radio", { name: "Subtle" })).toBeInTheDocument();
    expect(within(group).getByRole("radio", { name: "Pill" })).toBeInTheDocument();
    expect(within(group).getByRole("radio", { name: "Pill+dim" })).toBeInTheDocument();
  });

  it("marks the option matching blockedEmphasis as aria-checked", async () => {
    render(SettingsSheet, { ...defaultProps(), blockedEmphasis: "pill-dim" });

    await user.click(screen.getByRole("button", { name: "Settings" }));

    const group = screen.getByRole("radiogroup", { name: /blocked emphasis/i });
    expect(within(group).getByRole("radio", { name: "Pill+dim" })).toHaveAttribute("aria-checked", "true");
    expect(within(group).getByRole("radio", { name: "Pill" })).toHaveAttribute("aria-checked", "false");
  });

  it("clicking Subtle calls onemphasischange with 'subtle'", async () => {
    const onemphasischange = vi.fn();
    render(SettingsSheet, { ...defaultProps(), blockedEmphasis: "pill", onemphasischange });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    const group = screen.getByRole("radiogroup", { name: /blocked emphasis/i });
    await user.click(within(group).getByRole("radio", { name: "Subtle" }));

    expect(onemphasischange).toHaveBeenCalledWith("subtle");
  });

  it("clicking Pill+dim calls onemphasischange with 'pill-dim'", async () => {
    const onemphasischange = vi.fn();
    render(SettingsSheet, { ...defaultProps(), blockedEmphasis: "pill", onemphasischange });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    const group = screen.getByRole("radiogroup", { name: /blocked emphasis/i });
    await user.click(within(group).getByRole("radio", { name: "Pill+dim" }));

    expect(onemphasischange).toHaveBeenCalledWith("pill-dim");
  });

  it("shows a Detail panel position radiogroup with Right and Bottom options", async () => {
    render(SettingsSheet, { ...defaultProps() });

    await user.click(screen.getByRole("button", { name: "Settings" }));

    const group = screen.getByRole("radiogroup", { name: /detail panel position/i });
    expect(group).toBeInTheDocument();

    const options = within(group).getAllByRole("radio");
    expect(options).toHaveLength(2);
    expect(within(group).getByRole("radio", { name: /right/i })).toBeInTheDocument();
    expect(within(group).getByRole("radio", { name: /bottom/i })).toBeInTheDocument();
  });

  it("marks the option matching detailPanelPosition as aria-checked", async () => {
    render(SettingsSheet, { ...defaultProps(), detailPanelPosition: "bottom" });

    await user.click(screen.getByRole("button", { name: "Settings" }));

    const group = screen.getByRole("radiogroup", { name: /detail panel position/i });
    expect(within(group).getByRole("radio", { name: /bottom/i })).toHaveAttribute("aria-checked", "true");
    expect(within(group).getByRole("radio", { name: /right/i })).toHaveAttribute("aria-checked", "false");
  });

  it("clicking Bottom calls onpositionchange with 'bottom'", async () => {
    const onpositionchange = vi.fn();
    render(SettingsSheet, { ...defaultProps(), detailPanelPosition: "right", onpositionchange });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    const group = screen.getByRole("radiogroup", { name: /detail panel position/i });
    await user.click(within(group).getByRole("radio", { name: /bottom/i }));

    expect(onpositionchange).toHaveBeenCalledWith("bottom");
  });

  it("clicking from bottom back to Right calls onpositionchange with 'right'", async () => {
    const onpositionchange = vi.fn();
    render(SettingsSheet, { ...defaultProps(), detailPanelPosition: "bottom", onpositionchange });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    const group = screen.getByRole("radiogroup", { name: /detail panel position/i });
    await user.click(within(group).getByRole("radio", { name: /right/i }));

    expect(onpositionchange).toHaveBeenCalledWith("right");
  });

  it("shows a Behavior section with an Open detail on radiogroup", async () => {
    render(SettingsSheet, { ...defaultProps() });

    await user.click(screen.getByRole("button", { name: "Settings" }));

    expect(screen.getByText("Behavior")).toBeInTheDocument();

    const group = screen.getByRole("radiogroup", { name: /open detail on/i });
    expect(group).toBeInTheDocument();

    const options = within(group).getAllByRole("radio");
    expect(options).toHaveLength(2);
    expect(within(group).getByRole("radio", { name: /single click/i })).toBeInTheDocument();
    expect(within(group).getByRole("radio", { name: /double click/i })).toBeInTheDocument();
  });

  it("marks the option matching openDetailOn as aria-checked", async () => {
    render(SettingsSheet, { ...defaultProps(), openDetailOn: "double" });

    await user.click(screen.getByRole("button", { name: "Settings" }));

    const group = screen.getByRole("radiogroup", { name: /open detail on/i });
    expect(within(group).getByRole("radio", { name: /double click/i })).toHaveAttribute("aria-checked", "true");
    expect(within(group).getByRole("radio", { name: /single click/i })).toHaveAttribute("aria-checked", "false");
  });

  it("clicking Double click calls onopendetailchange with 'double'", async () => {
    const onopendetailchange = vi.fn();
    render(SettingsSheet, { ...defaultProps(), openDetailOn: "single", onopendetailchange });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    const group = screen.getByRole("radiogroup", { name: /open detail on/i });
    await user.click(within(group).getByRole("radio", { name: /double click/i }));

    expect(onopendetailchange).toHaveBeenCalledWith("double");
  });

  it("clicking from double back to Single click calls onopendetailchange with 'single'", async () => {
    const onopendetailchange = vi.fn();
    render(SettingsSheet, { ...defaultProps(), openDetailOn: "double", onopendetailchange });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    const group = screen.getByRole("radiogroup", { name: /open detail on/i });
    await user.click(within(group).getByRole("radio", { name: /single click/i }));

    expect(onopendetailchange).toHaveBeenCalledWith("single");
  });

  it("shows a Font size radiogroup with Small, Medium, and Large options", async () => {
    render(SettingsSheet, { ...defaultProps() });

    await user.click(screen.getByRole("button", { name: "Settings" }));

    const group = screen.getByRole("radiogroup", { name: /font size/i });
    expect(group).toBeInTheDocument();

    const options = within(group).getAllByRole("radio");
    expect(options).toHaveLength(3);
    expect(within(group).getByRole("radio", { name: "Small" })).toBeInTheDocument();
    expect(within(group).getByRole("radio", { name: "Medium" })).toBeInTheDocument();
    expect(within(group).getByRole("radio", { name: "Large" })).toBeInTheDocument();
  });

  it("marks the option matching fontSize as aria-checked", async () => {
    render(SettingsSheet, { ...defaultProps(), fontSize: "large" });

    await user.click(screen.getByRole("button", { name: "Settings" }));

    const group = screen.getByRole("radiogroup", { name: /font size/i });
    expect(within(group).getByRole("radio", { name: "Large" })).toHaveAttribute("aria-checked", "true");
    expect(within(group).getByRole("radio", { name: "Medium" })).toHaveAttribute("aria-checked", "false");
  });

  it("clicking Small calls onfontsizechange with 'small'", async () => {
    const onfontsizechange = vi.fn();
    render(SettingsSheet, { ...defaultProps(), fontSize: "medium", onfontsizechange });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    const group = screen.getByRole("radiogroup", { name: /font size/i });
    await user.click(within(group).getByRole("radio", { name: "Small" }));

    expect(onfontsizechange).toHaveBeenCalledWith("small");
  });

  it("clicking Large calls onfontsizechange with 'large'", async () => {
    const onfontsizechange = vi.fn();
    render(SettingsSheet, { ...defaultProps(), fontSize: "medium", onfontsizechange });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    const group = screen.getByRole("radiogroup", { name: /font size/i });
    await user.click(within(group).getByRole("radio", { name: "Large" }));

    expect(onfontsizechange).toHaveBeenCalledWith("large");
  });

  it("shows a Theme control reflecting the current theme", async () => {
    render(SettingsSheet, { ...defaultProps(), theme: "dracula" });

    await user.click(screen.getByRole("button", { name: "Settings" }));

    expect(screen.getByText("Theme")).toBeInTheDocument();
    // The ThemeSelect trigger shows the current theme's label.
    expect(screen.getByTestId("theme-select")).toHaveTextContent("Dracula");
  });

  it("emits onthemechange with the selected theme", async () => {
    const onthemechange = vi.fn();
    render(SettingsSheet, { ...defaultProps(), theme: "graphite", onthemechange });

    await user.click(screen.getByRole("button", { name: "Settings" }));
    await user.click(screen.getByTestId("theme-select"));
    await user.click(screen.getByRole("option", { name: "Midnight" }));

    expect(onthemechange).toHaveBeenCalledWith("midnight");
  });

  it("Escape dismissing the open Theme dropdown keeps the Settings panel open", async () => {
    render(SettingsSheet, { ...defaultProps() });
    await user.click(screen.getByRole("button", { name: "Settings" }));
    expect(screen.getByText("Appearance")).toBeInTheDocument();

    // Open the Theme select; its popover content portals to document.body with
    // data-slot="select-content".
    await user.click(screen.getByTestId("theme-select"));
    await waitFor(() =>
      expect(document.querySelector("[data-slot='select-content']")).not.toBeNull(),
    );

    // Escape must be consumed by the select popover only — bits-ui's escape-layer
    // calls preventDefault() but never stopPropagation(), so without a guard the
    // sheet's own document-level Escape handler would ALSO fire and close the panel.
    await user.keyboard("{Escape}");

    // The select popover closes...
    await waitFor(() =>
      expect(document.querySelector("[data-slot='select-content']")).toBeNull(),
    );

    // ...but the Settings panel stays open. Wait past the fly-out duration (200ms)
    // so a buggy close (which only unmounts after the transition) is observable.
    await new Promise((r) => setTimeout(r, 300));
    expect(screen.getByText("Appearance")).toBeInTheDocument();

    // A SECOND Escape — now that no select popover is open — falls through the
    // guard and closes the panel. Pins the documented "the select tears its
    // content down; a second Escape then closes the panel" contract, so the guard
    // can never regress into swallowing Escape permanently.
    await user.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByText("Appearance")).not.toBeInTheDocument());
  });

  it("does not close on a pointerdown inside the portaled Theme select popover (isInsideOrTrigger)", async () => {
    render(SettingsSheet, { ...defaultProps() });
    await user.click(screen.getByRole("button", { name: "Settings" }));
    expect(screen.getByText("Appearance")).toBeInTheDocument();

    // Open the Theme select; its content portals to document.body as a body-level
    // sibling of the panel <aside>. A pointerdown inside it must be treated as
    // "inside" by isInsideOrTrigger's select-content branch and NOT dismiss the
    // whole panel on the very click that picks an option.
    await user.click(screen.getByTestId("theme-select"));
    const content = await waitFor(() => {
      const el = document.querySelector("[data-slot='select-content']");
      expect(el).not.toBeNull();
      return el as Element;
    });

    fireEvent.pointerDown(content);

    // Flush pending effects: an erroneous dismissal would unmount the panel, so a
    // surviving panel proves the select-content pointerdown was ignored.
    await tick();
    expect(screen.getByText("Appearance")).toBeInTheDocument();
  });
});

// nibs-ke8o: the ordering rules became a preference with two modes. "Always" is
// deliberately not offered — it is the behaviour that shipped first and the one
// the measurement argues against — so the control must show exactly two.
it("offers the two ordering-rule modes and reports a change", async () => {
  const props = defaultProps();
  render(SettingsSheet, { ...props });
  await user.click(screen.getByRole("button", { name: /settings/i }));

  const control = await screen.findByRole("radiogroup", { name: "Ordering rules" });
  const options = within(control).getAllByRole("radio");
  expect(options.map((o) => o.textContent?.trim())).toEqual(["While dragging", "Never"]);

  await user.click(within(control).getByRole("radio", { name: "Never" }));
  expect(props.onregionbandschange).toHaveBeenCalledWith("never");
});
