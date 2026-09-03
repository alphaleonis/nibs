import { describe, it, expect } from "vitest";
import { NO_MILESTONE, fromSelectValue, milestoneChoices, toSelectValue } from "./milestones";
import type { MilestoneOption } from "./milestones";

const M1: MilestoneOption = { id: "nibs-m1", title: "Foundations", status: "completed" };
const M2: MilestoneOption = { id: "nibs-m2", title: "Areas", status: "in-progress" };
const M3: MilestoneOption = { id: "nibs-m3", title: "Parked wave", status: "deferred" };

describe("milestoneChoices", () => {
  it("keeps the given order, which is the order the waves are planned in", () => {
    const choices = milestoneChoices([M1, M2, M3], { status: "todo", milestone: "" });
    expect(choices.map((c) => c.id)).toEqual(["nibs-m1", "nibs-m2", "nibs-m3"]);
  });

  it("lists a released milestone rather than dropping it, and says why it is refused", () => {
    const [foundations, areas, parked] = milestoneChoices([M1, M2, M3], {
      status: "todo",
      milestone: "",
    });
    // Listed, not filtered: this picker is also the only place the axis is
    // displayed, so a silently shorter list reads as a store with fewer
    // milestones.
    expect(foundations.refusal).toContain("completed");
    expect(areas.refusal).toBeNull();
    // Deferred is HOLDING: parked work is coming back, so its queue stays open.
    expect(parked.refusal).toBeNull();
  });

  it("never refuses the assignment the subject already carries", () => {
    // Reachable and legitimate: the milestone completed while this open nib
    // still pointed at it. Refusing it would draw the nib's own value as
    // illegal, and the user could not even see where it is planned.
    const [foundations] = milestoneChoices([M1], { status: "todo", milestone: "nibs-m1" });
    expect(foundations.refusal).toBeNull();
  });

  it("offers a released milestone to a closed subject", () => {
    const [foundations] = milestoneChoices([M1], { status: "completed", milestone: "" });
    expect(foundations.refusal).toBeNull();
  });
});

describe("the None sentinel", () => {
  it("round-trips an assignment through a Select value", () => {
    expect(toSelectValue("")).toBe(NO_MILESTONE);
    expect(fromSelectValue(NO_MILESTONE)).toBe("");
    expect(toSelectValue("nibs-m2")).toBe("nibs-m2");
    expect(fromSelectValue("nibs-m2")).toBe("nibs-m2");
  });

  it("is not the empty string, which a Select reads as no selection at all", () => {
    expect(NO_MILESTONE).not.toBe("");
  });
});
