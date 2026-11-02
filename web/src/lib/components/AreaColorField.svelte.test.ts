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
const input = () => screen.getByTestId("area-color-input");

describe("AreaColorField", () => {
  it("picking a swatch stores its name", async () => {
    const box = renderField("");

    await user.click(swatch("teal"));

    expect(box.value).toBe("teal");
    expect(input()).toHaveValue("teal");
  });

  it("No color stores the empty string", async () => {
    const box = renderField("teal");

    await user.click(noColor());

    expect(box.value).toBe("");
    expect(input()).toHaveValue("");
  });

  it("the current palette color is marked selected", async () => {
    // Matched in any letter case, as the resolver draws it.
    renderField("Teal");

    expect(swatch("teal")).toHaveAttribute("aria-pressed", "true");
    for (const name of AREA_PALETTE.filter((n) => n !== "teal")) {
      expect(swatch(name)).toHaveAttribute("aria-pressed", "false");
    }
    expect(noColor()).toHaveAttribute("aria-pressed", "false");

    await user.click(noColor());
    expect(noColor()).toHaveAttribute("aria-pressed", "true");
    expect(swatch("teal")).toHaveAttribute("aria-pressed", "false");

    await user.type(input(), "#4488ff");
    for (const name of AREA_PALETTE) expect(swatch(name)).toHaveAttribute("aria-pressed", "false");
    expect(noColor()).toHaveAttribute("aria-pressed", "false");
  });

  it("a custom value is kept verbatim", async () => {
    const seeded = renderField("#4488ff80");
    expect(input()).toHaveValue("#4488ff80");
    expect(seeded.value).toBe("#4488ff80");

    await user.clear(input());
    await user.type(input(), "rebeccapurple");
    expect(seeded.value).toBe("rebeccapurple");

    await user.clear(input());
    await user.type(input(), "#4488ff80");
    expect(seeded.value).toBe("#4488ff80");
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

  it("the preview hints when the color cannot be drawn", async () => {
    renderField("teal");
    const preview = screen.getByTestId("area-color-preview");
    expect(preview.style.backgroundColor).toBe("var(--area-teal)");
    expect(screen.queryByTestId("area-color-hint")).toBeNull();

    // A name in letters that no browser knows, then a shape `cssColor` refuses.
    for (const undrawable of ["notacolor", "rgb(1, 2, 3)"]) {
      await user.clear(input());
      await user.type(input(), undrawable);
      expect(screen.getByTestId("area-color-hint")).toBeVisible();
      expect(preview.style.backgroundColor).toBe("");
      expect(input()).toHaveAccessibleDescription(screen.getByTestId("area-color-hint").textContent!);
    }

    // No color is a choice, not a fault.
    await user.clear(input());
    expect(screen.queryByTestId("area-color-hint")).toBeNull();
  });

  it("swatches are keyboard reachable and named", async () => {
    const box = renderField("");

    // Tab order: No color, then the palette in order, then the text input.
    await user.tab();
    expect(noColor()).toHaveFocus();
    for (const name of AREA_PALETTE) {
      await user.tab();
      expect(swatch(name)).toHaveFocus();
    }
    await user.tab();
    expect(input()).toHaveFocus();

    await user.tab({ shift: true });
    await user.keyboard(" ");
    expect(box.value).toBe("gray");

    await user.tab({ shift: true });
    await user.keyboard("{Enter}");
    expect(box.value).toBe("pink");
  });
});
