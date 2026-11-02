import { cleanup, render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect } from "vitest";
import AreaColorField from "./AreaColorField.svelte";
import { AREA_PALETTE } from "../areas";
import { reactiveBox } from "../testing/reactiveBox.svelte";

const user = userEvent.setup({ pointerEventsCheck: 0 });

/** Renders the field bound to a box, so a test reads what the field wrote back. */
function renderField(initial: string) {
  const box = reactiveBox(initial);
  render(AreaColorField, {
    props: {
      path: "web",
      get value() {
        return box.value;
      },
      set value(next: string) {
        box.value = next;
      },
    },
  });
  return box;
}

const label = (name: string) => name[0].toUpperCase() + name.slice(1);
const swatch = (name: string) => screen.getByRole("button", { name: label(name) });
const noColor = () => screen.getByRole("button", { name: "No color" });
const custom = () => screen.queryByTestId("area-swatch-custom");

describe("AreaColorField", () => {
  it("picking a swatch stores its name", async () => {
    const box = renderField("");

    await user.click(swatch("teal"));

    expect(box.value).toBe("teal");
  });

  it("No color stores the empty string", async () => {
    const box = renderField("teal");

    await user.click(noColor());

    expect(box.value).toBe("");
  });

  it("the current palette color is marked selected", async () => {
    // Matched in any letter case, as the resolver draws it.
    renderField("Teal");

    expect(swatch("teal")).toHaveAttribute("aria-pressed", "true");
    for (const name of AREA_PALETTE.filter((n) => n !== "teal")) {
      expect(swatch(name)).toHaveAttribute("aria-pressed", "false");
    }
    expect(noColor()).toHaveAttribute("aria-pressed", "false");
    expect(custom()).toBeNull();

    await user.click(noColor());
    expect(noColor()).toHaveAttribute("aria-pressed", "true");
    expect(swatch("teal")).toHaveAttribute("aria-pressed", "false");
  });

  it("a color outside the palette gets its own swatch, selected and kept verbatim", async () => {
    const box = renderField("#4488ff80");

    expect(custom()).toHaveAttribute("aria-pressed", "true");
    expect(custom()).toHaveAccessibleName("Custom color #4488ff80");
    expect(custom()!.style.backgroundColor).not.toBe("");
    for (const name of AREA_PALETTE) expect(swatch(name)).toHaveAttribute("aria-pressed", "false");
    expect(noColor()).toHaveAttribute("aria-pressed", "false");

    await user.click(swatch("teal"));
    expect(box.value).toBe("teal");
    expect(custom()).toHaveAttribute("aria-pressed", "false");

    await user.click(custom()!);
    expect(box.value).toBe("#4488ff80");
  });

  it("no custom swatch is drawn for an empty value", () => {
    renderField("");
    expect(custom()).toBeNull();
  });

  // A write-back would make the Edit dialog send a color the user never changed.
  it("an untouched value keeps its letter case", async () => {
    for (const stored of ["Teal", "#4488FF80"]) {
      const box = renderField(stored);
      expect(box.value).toBe(stored);

      await user.tab();
      await user.tab();
      expect(swatch("red")).toHaveFocus();
      expect(box.value).toBe(stored);
      cleanup();
    }
  });

  it("a custom color the browser cannot draw shows an empty dashed swatch", () => {
    // A name in letters that no browser knows, then a shape `cssColor` refuses.
    for (const undrawable of ["notacolor", "rgb(1, 2, 3)"]) {
      renderField(undrawable);
      expect(custom()).toHaveAttribute("aria-pressed", "true");
      expect(custom()!.style.backgroundColor).toBe("");
      expect(custom()).toHaveClass("border-dashed");
      cleanup();
    }
  });

  it("swatches are keyboard reachable and named", async () => {
    const box = renderField("");

    // Tab order: No color, then the palette in order.
    await user.tab();
    expect(noColor()).toHaveFocus();
    for (const name of AREA_PALETTE) {
      await user.tab();
      expect(swatch(name)).toHaveFocus();
    }

    await user.keyboard(" ");
    expect(box.value).toBe("gray");

    await user.tab({ shift: true });
    await user.keyboard("{Enter}");
    expect(box.value).toBe("pink");
  });
});
