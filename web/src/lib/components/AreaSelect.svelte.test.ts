import { render, screen, within } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import AreaSelect from "./AreaSelect.svelte";
import { VIEW_SPINE_KEY } from "../contexts";
import { makeViewSpine } from "../viewSpine";
import { createAreaVocabulary, type AreaNode } from "../areas";

const user = userEvent.setup({ pointerEventsCheck: 0 });

// A parent with one child plus a sibling root, so the flat list carries a real
// depth run. `rgb(...)` on `cli` is a color `cssColor` refuses — legal in
// areas.yml, but not narrow enough to reach an inline style.
const AREAS: AreaNode[] = [
  { path: "web", name: "web", description: "The web UI", color: "#3366ff", depth: 0 },
  { path: "web/dashboard", name: "dashboard", description: "", color: "teal", depth: 1 },
  { path: "cli", name: "cli", description: "", color: "rgb(1, 2, 3)", depth: 0 },
];

function ctx(areas: AreaNode[] = AREAS) {
  const spine = makeViewSpine(createAreaVocabulary(areas));
  return new Map<string, unknown>([[VIEW_SPINE_KEY, () => spine]]);
}

function renderSelect(props: { value: string; onchange?: () => void }, areas = AREAS) {
  return render(AreaSelect, {
    props: { onchange: vi.fn(), ...props },
    context: ctx(areas),
  });
}

function option(path: string): HTMLElement {
  return screen.getAllByRole("option").find((o) => o.getAttribute("data-value") === path)!;
}

/** The row's content span — the one carrying the depth indent. */
function labelSpan(row: HTMLElement): HTMLElement {
  return row.querySelector<HTMLElement>("span[style]")!;
}

function swatch(row: HTMLElement): HTMLElement {
  return within(row).getByTestId("area-color");
}

describe("AreaSelect", () => {
  it("offers None plus every declared path, parents included", async () => {
    // A non-leaf is a legal assignment (`Areas.ValidateStored` accepts any
    // declared node), so listing only the leaves would hide `web` and `cli`.
    renderSelect({ value: "" });
    await user.click(screen.getByTestId("area-select"));

    expect(screen.getAllByRole("option")).toHaveLength(4);
    expect(screen.getByRole("option", { name: "None" })).toBeTruthy();
    expect(option("web")).toBeTruthy();
    expect(option("web/dashboard")).toBeTruthy();
    expect(option("cli")).toBeTruthy();
  });

  it("labels a row by its segment and indents it by depth", async () => {
    // A nested row is drawn inside the parent that supplies the rest of the
    // path, so it carries the segment — the same choice `areaForest` makes.
    renderSelect({ value: "" });
    await user.click(screen.getByTestId("area-select"));

    const nested = option("web/dashboard");
    expect(nested).toHaveTextContent("dashboard");
    expect(nested).not.toHaveTextContent("web/dashboard");
    expect(labelSpan(nested).style.paddingLeft).toBe("0.75rem");
    expect(labelSpan(option("web")).style.paddingLeft).toBe("0rem");
  });

  it("shows the FULL stored path in the trigger, not the segment", () => {
    // The collapsed state has no parent row to read the rest of the path from,
    // so "dashboard" alone would not say which area is assigned — and would
    // disagree with the Area column.
    renderSelect({ value: "web/dashboard" });
    expect(screen.getByTestId("area-select")).toHaveTextContent("web/dashboard");
  });

  it("shows None for an unassigned nib, and selects the None item", async () => {
    renderSelect({ value: "" });
    expect(screen.getByTestId("area-select")).toHaveTextContent("None");

    await user.click(screen.getByTestId("area-select"));
    expect(screen.getByRole("option", { name: "None" })).toHaveAttribute("aria-selected", "true");
  });

  it("clears the assignment as \"\", not as the None sentinel", async () => {
    // The sentinel exists only because a Select reads "" as no selection; it
    // must never reach the mutation.
    const onchange = vi.fn();
    renderSelect({ value: "web", onchange });

    await user.click(screen.getByTestId("area-select"));
    await user.click(screen.getByRole("option", { name: "None" }));

    expect(onchange).toHaveBeenCalledWith("");
  });

  it("assigns the picked area by its full path", async () => {
    const onchange = vi.fn();
    renderSelect({ value: "", onchange });

    await user.click(screen.getByTestId("area-select"));
    await user.click(option("web/dashboard"));

    expect(onchange).toHaveBeenCalledWith("web/dashboard");
  });

  it("falls back to the raw value when the vocabulary no longer declares it", () => {
    // Reachable two ways: an area retired from `areas:` while nibs still carry
    // it, and the tick before the config query resolves. Blanking the trigger
    // would read as unassigned.
    renderSelect({ value: "web/legacy" });
    expect(screen.getByTestId("area-select")).toHaveTextContent("web/legacy");
  });

  it("draws a swatch for a declared color, and none for one cssColor refuses", async () => {
    renderSelect({ value: "" });
    await user.click(screen.getByTestId("area-select"));

    // The inline declaration, not the computed style: jsdom's getComputedStyle
    // resolves a named color to rgb() and `rem` to px, so a computed comparison
    // would test that resolution rather than what the component wrote.
    expect(swatch(option("web")).style.backgroundColor).toBe("rgb(51, 102, 255)");
    expect(swatch(option("web/dashboard")).style.backgroundColor).toBe("teal");
    // `rgb(1, 2, 3)` is legal in areas.yml but not one of the two shapes
    // `cssColor` narrows to, so nothing is drawn rather than an inline style
    // carrying config text.
    expect(within(option("cli")).queryByTestId("area-color")).toBeNull();
  });

  it("offers only None when the project declares no areas", async () => {
    renderSelect({ value: "" }, []);
    await user.click(screen.getByTestId("area-select"));

    expect(screen.getAllByRole("option")).toHaveLength(1);
  });
});
