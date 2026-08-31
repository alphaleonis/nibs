import { describe, it, expect } from "vitest";
import { DragState } from "./drag.svelte";

describe("DragState", () => {
  it("starts with no drag", () => {
    const state = new DragState();
    expect(state.isDragging).toBe(false);
    expect(state.draggedIds).toEqual([]);
    expect(state.dropTargetId).toBeNull();
    expect(state.dropZone).toBeNull();
    expect(state.dropValid).toBe(false);
  });

  it("startDrag sets draggedIds and isDragging", () => {
    const state = new DragState();
    state.startDrag(["nib-001"]);
    expect(state.isDragging).toBe(true);
    expect(state.draggedIds).toEqual(["nib-001"]);
  });

  it("startDrag with multiple ids", () => {
    const state = new DragState();
    state.startDrag(["nib-001", "nib-002", "nib-003"]);
    expect(state.isDragging).toBe(true);
    expect(state.draggedIds).toEqual(["nib-001", "nib-002", "nib-003"]);
  });

  it("setDropTarget sets target state", () => {
    const state = new DragState();
    state.startDrag(["nib-001"]);
    state.setDropTarget("nib-002", "before", true);
    expect(state.dropTargetId).toBe("nib-002");
    expect(state.dropZone).toBe("before");
    expect(state.dropValid).toBe(true);
  });

  it("setDropTarget can mark invalid", () => {
    const state = new DragState();
    state.startDrag(["nib-001"]);
    state.setDropTarget("nib-002", "reparent", false);
    expect(state.dropValid).toBe(false);
  });

  it("clearDropTarget resets target state", () => {
    const state = new DragState();
    state.startDrag(["nib-001"]);
    state.setDropTarget("nib-002", "after", true);
    state.clearDropTarget();
    expect(state.dropTargetId).toBeNull();
    expect(state.dropZone).toBeNull();
    expect(state.dropValid).toBe(false);
  });

  it("endDrag resets everything", () => {
    const state = new DragState();
    state.startDrag(["nib-001"]);
    state.setDropTarget("nib-002", "before", true);
    state.cursorX = 100;
    state.cursorY = 200;
    state.endDrag();
    expect(state.isDragging).toBe(false);
    expect(state.draggedIds).toEqual([]);
    expect(state.dropTargetId).toBeNull();
    expect(state.dropZone).toBeNull();
    expect(state.dropValid).toBe(false);
    expect(state.cursorX).toBe(0);
    expect(state.cursorY).toBe(0);
  });

  // The accepted plan's two facts travel together, because a refusal has
  // neither: the badge must not be able to show a destination for a drop that
  // cannot happen, nor the row an axis-colored indicator.
  it("setDropTarget carries the accepted plan's sentence and region", () => {
    const state = new DragState();
    state.startDrag(["nib-001"]);
    state.setDropTarget("nib-002", "before", true, {
      label: "Reorder in the Q3 Launch queue",
      region: { axis: "milestone", milestoneId: "M1" },
    });
    expect(state.dropLabel).toBe("Reorder in the Q3 Launch queue");
    expect(state.dropRegion).toEqual({ axis: "milestone", milestoneId: "M1" });
  });

  it("a target set with no accepted plan carries neither", () => {
    const state = new DragState();
    state.startDrag(["nib-001"]);
    state.setDropTarget("nib-002", "before", true, {
      label: "Reorder in the top level",
      region: { axis: "parent", parentId: null },
    });
    // The refusal case, and the shape every pre-existing caller passes.
    state.setDropTarget("nib-003", "after", false);
    expect(state.dropLabel).toBeNull();
    expect(state.dropRegion).toBeNull();
  });

  it("clearDropTarget and endDrag drop the accepted plan too", () => {
    const accepted = { label: "Reorder in the top level", region: { axis: "parent", parentId: null } } as const;

    const cleared = new DragState();
    cleared.startDrag(["nib-001"]);
    cleared.setDropTarget("nib-002", "after", true, { ...accepted });
    cleared.clearDropTarget();
    expect(cleared.dropLabel).toBeNull();
    expect(cleared.dropRegion).toBeNull();

    const ended = new DragState();
    ended.startDrag(["nib-001"]);
    ended.setDropTarget("nib-002", "after", true, { ...accepted });
    ended.endDrag();
    expect(ended.dropLabel).toBeNull();
    expect(ended.dropRegion).toBeNull();
  });

  it("isDraggedItem returns true for dragged items", () => {
    const state = new DragState();
    state.startDrag(["nib-001", "nib-002"]);
    expect(state.isDraggedItem("nib-001")).toBe(true);
    expect(state.isDraggedItem("nib-002")).toBe(true);
    expect(state.isDraggedItem("nib-003")).toBe(false);
  });
});
