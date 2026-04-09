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

  it("isDraggedItem returns true for dragged items", () => {
    const state = new DragState();
    state.startDrag(["nib-001", "nib-002"]);
    expect(state.isDraggedItem("nib-001")).toBe(true);
    expect(state.isDraggedItem("nib-002")).toBe(true);
    expect(state.isDraggedItem("nib-003")).toBe(false);
  });
});
