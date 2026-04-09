import { describe, it, expect } from "vitest";
import {
  createNib,
  updateNib,
  deleteNib,
  archiveNib,
  setParent,
  reorderNib,
  batch,
  sequence,
  reorderChain,
  deleteBatch,
  archiveBatch,
  setStatusBatch,
  setPriorityBatch,
  reparentBatch,
  reparentAndReorder,
} from "./commands";

describe("leaf command factories", () => {
  it("createNib returns a create-nib command with the input", () => {
    const cmd = createNib({ title: "My task", type: "task" });
    expect(cmd).toEqual({
      kind: "create-nib",
      input: { title: "My task", type: "task" },
    });
  });

  it("updateNib returns an update-nib command with id, input, and optional ifMatch", () => {
    const cmd = updateNib("nibs-abc1", { title: "New title" }, "etag-123");
    expect(cmd).toEqual({
      kind: "update-nib",
      id: "nibs-abc1",
      input: { title: "New title" },
      ifMatch: "etag-123",
    });
  });

  it("updateNib omits ifMatch when not provided", () => {
    const cmd = updateNib("nibs-abc1", { status: "completed" });
    expect(cmd).toEqual({
      kind: "update-nib",
      id: "nibs-abc1",
      input: { status: "completed" },
    });
    expect(cmd).not.toHaveProperty("ifMatch");
  });

  it("deleteNib returns a delete-nib command with id", () => {
    const cmd = deleteNib("nibs-abc1");
    expect(cmd).toEqual({ kind: "delete-nib", id: "nibs-abc1" });
  });

  it("archiveNib returns an archive-nib command with id", () => {
    const cmd = archiveNib("nibs-abc1");
    expect(cmd).toEqual({ kind: "archive-nib", id: "nibs-abc1" });
  });

  it("setParent returns a set-parent command with id and parentId", () => {
    const cmd = setParent("nibs-abc1", "nibs-parent1");
    expect(cmd).toEqual({
      kind: "set-parent",
      id: "nibs-abc1",
      parentId: "nibs-parent1",
    });
  });

  it("setParent accepts null parentId for unparenting", () => {
    const cmd = setParent("nibs-abc1", null);
    expect(cmd).toEqual({
      kind: "set-parent",
      id: "nibs-abc1",
      parentId: null,
    });
  });

  it("reorderNib returns a reorder-nib command with positioning options", () => {
    const cmd = reorderNib("nibs-abc1", { afterId: "nibs-xyz9" });
    expect(cmd).toEqual({
      kind: "reorder-nib",
      id: "nibs-abc1",
      afterId: "nibs-xyz9",
    });
  });

  it("reorderNib supports beforeId", () => {
    const cmd = reorderNib("nibs-abc1", { beforeId: "nibs-xyz9" });
    expect(cmd).toEqual({
      kind: "reorder-nib",
      id: "nibs-abc1",
      beforeId: "nibs-xyz9",
    });
  });

  it("reorderNib supports first flag", () => {
    const cmd = reorderNib("nibs-abc1", { first: true });
    expect(cmd).toEqual({
      kind: "reorder-nib",
      id: "nibs-abc1",
      first: true,
    });
  });
});

describe("composition factories", () => {
  it("batch wraps leaf commands in a batch command", () => {
    const cmds = [deleteNib("a"), deleteNib("b")];
    const cmd = batch(cmds);
    expect(cmd).toEqual({
      kind: "batch",
      commands: [
        { kind: "delete-nib", id: "a" },
        { kind: "delete-nib", id: "b" },
      ],
    });
  });

  it("sequence wraps steps in a sequence command", () => {
    const step1 = reorderNib("a", { afterId: "target" });
    const step2 = (prev: any) => reorderNib("b", { afterId: prev.data?.reorderNib?.id ?? "a" });
    const cmd = sequence([step1, step2]);
    expect(cmd.kind).toBe("sequence");
    expect(cmd.steps).toHaveLength(2);
    expect(cmd.steps[0]).toEqual(step1);
    expect(typeof cmd.steps[1]).toBe("function");
  });
});

describe("domain composition factories", () => {
  it("deleteBatch creates a batch of delete commands", () => {
    const cmd = deleteBatch(["a", "b", "c"]);
    expect(cmd).toEqual({
      kind: "batch",
      commands: [
        { kind: "delete-nib", id: "a" },
        { kind: "delete-nib", id: "b" },
        { kind: "delete-nib", id: "c" },
      ],
    });
  });

  it("archiveBatch creates a batch of archive commands", () => {
    const cmd = archiveBatch(["x", "y"]);
    expect(cmd).toEqual({
      kind: "batch",
      commands: [
        { kind: "archive-nib", id: "x" },
        { kind: "archive-nib", id: "y" },
      ],
    });
  });

  it("setStatusBatch creates a batch of update commands with status", () => {
    const cmd = setStatusBatch(["a", "b"], "completed");
    expect(cmd).toEqual({
      kind: "batch",
      commands: [
        { kind: "update-nib", id: "a", input: { status: "completed" } },
        { kind: "update-nib", id: "b", input: { status: "completed" } },
      ],
    });
  });

  it("setPriorityBatch creates a batch of update commands with priority", () => {
    const cmd = setPriorityBatch(["a", "b"], "high");
    expect(cmd).toEqual({
      kind: "batch",
      commands: [
        { kind: "update-nib", id: "a", input: { priority: "high" } },
        { kind: "update-nib", id: "b", input: { priority: "high" } },
      ],
    });
  });

  it("reparentBatch creates a batch of set-parent commands", () => {
    const cmd = reparentBatch(["a", "b"], "parent-1");
    expect(cmd).toEqual({
      kind: "batch",
      commands: [
        { kind: "set-parent", id: "a", parentId: "parent-1" },
        { kind: "set-parent", id: "b", parentId: "parent-1" },
      ],
    });
  });

  it("reorderChain with 'after' zone: first step has afterId=target, rest are factory functions", () => {
    const cmd = reorderChain(["a", "b", "c"], "target", "after");
    expect(cmd.kind).toBe("sequence");
    expect(cmd.steps).toHaveLength(3);

    // First step is a static reorder command
    expect(cmd.steps[0]).toEqual({
      kind: "reorder-nib",
      id: "a",
      afterId: "target",
    });

    // Remaining steps are factory functions that chain afterId from prev result
    expect(typeof cmd.steps[1]).toBe("function");
    expect(typeof cmd.steps[2]).toBe("function");

    // Simulate calling the factory with a previous result
    const factory1 = cmd.steps[1] as (prev: any) => any;
    const result1 = factory1({ ok: true, data: { reorderNib: { id: "a" } } });
    expect(result1).toEqual({
      kind: "reorder-nib",
      id: "b",
      afterId: "a",
    });

    const factory2 = cmd.steps[2] as (prev: any) => any;
    const result2 = factory2({ ok: true, data: { reorderNib: { id: "b" } } });
    expect(result2).toEqual({
      kind: "reorder-nib",
      id: "c",
      afterId: "b",
    });
  });

  it("reorderChain with 'before' zone: first step has beforeId=target, rest chain afterId", () => {
    const cmd = reorderChain(["a", "b"], "target", "before");
    expect(cmd.kind).toBe("sequence");
    expect(cmd.steps).toHaveLength(2);

    // First step uses beforeId
    expect(cmd.steps[0]).toEqual({
      kind: "reorder-nib",
      id: "a",
      beforeId: "target",
    });

    // Second step chains afterId from previous
    const factory = cmd.steps[1] as (prev: any) => any;
    const result = factory({ ok: true, data: { reorderNib: { id: "a" } } });
    expect(result).toEqual({
      kind: "reorder-nib",
      id: "b",
      afterId: "a",
    });
  });

  it("reorderChain with single item returns a one-step sequence", () => {
    const cmd = reorderChain(["a"], "target", "after");
    expect(cmd.kind).toBe("sequence");
    expect(cmd.steps).toHaveLength(1);
    expect(cmd.steps[0]).toEqual({
      kind: "reorder-nib",
      id: "a",
      afterId: "target",
    });
  });

  it("reparentAndReorder: single item, before zone — atomic reorder with parentId", () => {
    const cmd = reparentAndReorder(["a"], "new-parent", "target", "before");
    expect(cmd.kind).toBe("sequence");
    expect(cmd.steps).toHaveLength(1);
    expect(cmd.steps[0]).toEqual({
      kind: "reorder-nib", id: "a", beforeId: "target", parentId: "new-parent",
    });
  });

  it("reparentAndReorder: multiple items, after zone — chains atomic reorders", () => {
    const cmd = reparentAndReorder(["a", "b"], "new-parent", "target", "after");
    expect(cmd.kind).toBe("sequence");
    expect(cmd.steps).toHaveLength(2);
    // First reorder is static with parentId
    expect(cmd.steps[0]).toEqual({
      kind: "reorder-nib", id: "a", afterId: "target", parentId: "new-parent",
    });
    // Second reorder chains from previous
    expect(typeof cmd.steps[1]).toBe("function");
    const factory = cmd.steps[1] as (prev: any) => any;
    const result = factory({ ok: true, data: { reorderNib: { id: "a" } } });
    expect(result).toEqual({
      kind: "reorder-nib", id: "b", afterId: "a", parentId: "new-parent",
    });
  });

  it("reparentAndReorder: null parent sends empty string for root-level move", () => {
    const cmd = reparentAndReorder(["a"], null, "target", "after");
    expect(cmd.steps[0]).toEqual({
      kind: "reorder-nib", id: "a", afterId: "target", parentId: "",
    });
  });
});
