import { render, screen } from "@testing-library/svelte";
import { userEvent } from "@testing-library/user-event";
import { describe, it, expect, vi } from "vitest";
import MilestoneSelect from "./MilestoneSelect.svelte";
import { MILESTONES_KEY } from "../contexts";
import type { MilestoneOption } from "../milestones";

const user = userEvent.setup({ pointerEventsCheck: 0 });

const MILESTONES: MilestoneOption[] = [
  { id: "nibs-m1", title: "Foundations", status: "completed" },
  { id: "nibs-m2", title: "Areas", status: "in-progress" },
];

function ctx(milestones: MilestoneOption[] = MILESTONES) {
  return new Map<string, unknown>([[MILESTONES_KEY, () => milestones]]);
}

function renderSelect(props: { value: string; subjectStatus: string; onchange?: () => void }, milestones = MILESTONES) {
  return render(MilestoneSelect, {
    props: { onchange: vi.fn(), ...props },
    context: ctx(milestones),
  });
}

describe("MilestoneSelect", () => {
  it("shows None for an unassigned nib", () => {
    renderSelect({ value: "", subjectStatus: "todo" });
    expect(screen.getByTestId("milestone-select")).toHaveTextContent("None");
  });

  it("shows the milestone's TITLE, not the id it stores", () => {
    renderSelect({ value: "nibs-m2", subjectStatus: "todo" });
    const trigger = screen.getByTestId("milestone-select");
    expect(trigger).toHaveTextContent("Areas");
    expect(trigger).not.toHaveTextContent("nibs-m2");
  });

  it("falls back to the id when the assignment names a milestone the list lacks", () => {
    // Reachable two ways: the tick before the query resolves, and an assignment
    // pointing at a deleted nib. Blanking the trigger would read as unassigned.
    renderSelect({ value: "nibs-gone", subjectStatus: "todo" });
    expect(screen.getByTestId("milestone-select")).toHaveTextContent("nibs-gone");
  });

  it("offers None plus every milestone", async () => {
    renderSelect({ value: "", subjectStatus: "todo" });
    await user.click(screen.getByTestId("milestone-select"));

    expect(screen.getAllByRole("option")).toHaveLength(3);
    expect(screen.getByRole("option", { name: "None" })).toBeTruthy();
  });

  it("assigns the picked milestone by id", async () => {
    const onchange = vi.fn();
    renderSelect({ value: "", subjectStatus: "todo", onchange });

    await user.click(screen.getByTestId("milestone-select"));
    await user.click(screen.getAllByRole("option").find((o) => o.getAttribute("data-value") === "nibs-m2")!);

    expect(onchange).toHaveBeenCalledWith("nibs-m2");
  });

  it("clears the assignment as \"\", not as the None sentinel", async () => {
    // The sentinel exists only because a Select reads "" as no selection; it
    // must never reach the mutation.
    const onchange = vi.fn();
    renderSelect({ value: "nibs-m2", subjectStatus: "todo", onchange });

    await user.click(screen.getByTestId("milestone-select"));
    await user.click(screen.getByRole("option", { name: "None" }));

    expect(onchange).toHaveBeenCalledWith("");
  });

  it("disables a released milestone for open work rather than hiding it", async () => {
    renderSelect({ value: "", subjectStatus: "todo" });
    await user.click(screen.getByTestId("milestone-select"));

    const foundations = screen.getAllByRole("option").find((o) => o.getAttribute("data-value") === "nibs-m1")!;
    expect(foundations).toHaveAttribute("data-disabled");
  });

  it("offers a released milestone to closed work", async () => {
    // The retro-assignment exemption: finished work may be recorded against a
    // finished wave.
    renderSelect({ value: "", subjectStatus: "completed" });
    await user.click(screen.getByTestId("milestone-select"));

    const foundations = screen.getAllByRole("option").find((o) => o.getAttribute("data-value") === "nibs-m1")!;
    expect(foundations).not.toHaveAttribute("data-disabled");
  });

  it("never disables the assignment the nib already carries", async () => {
    renderSelect({ value: "nibs-m1", subjectStatus: "todo" });
    await user.click(screen.getByTestId("milestone-select"));

    const foundations = screen.getAllByRole("option").find((o) => o.getAttribute("data-value") === "nibs-m1")!;
    expect(foundations).not.toHaveAttribute("data-disabled");
  });

  it("offers only None when no milestones exist", async () => {
    renderSelect({ value: "", subjectStatus: "todo" }, []);
    await user.click(screen.getByTestId("milestone-select"));

    expect(screen.getAllByRole("option")).toHaveLength(1);
  });
});
