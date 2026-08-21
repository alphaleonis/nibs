import { describe, it, expect } from "vitest";
import { getValidChildTypes, isLeafType, canHaveChildren, VALID_CHILD_TYPES, typeRank } from "./typeHierarchy";

describe("typeRank", () => {
  it("orders container types above leaf types", () => {
    expect(typeRank("epic")).toBe(2);
    expect(typeRank("feature")).toBe(1);
    expect(typeRank("bug")).toBe(1);
    expect(typeRank("task")).toBe(0);
  });

  it("ranks research at the leaf tier (0)", () => {
    expect(typeRank("research")).toBe(0);
  });

  it("ranks milestone above every container: the milestone-grouped view keys on rank until the Phase-8 membership-based view", () => {
    expect(typeRank("milestone")).toBe(3);
  });

  it("treats unknown and empty types as leaf tier (0)", () => {
    expect(typeRank("unknown")).toBe(0);
    expect(typeRank("")).toBe(0);
  });
});

// These mirror the backend exactly (internal/nibtypes/hierarchy.go); order
// follows the canonical type order milestone, epic, bug, feature, task, research.
describe("getValidChildTypes", () => {
  it("milestone can have no children at all", () => {
    expect(getValidChildTypes("milestone")).toEqual([]);
  });

  it("epic can have bug, feature, task, and research children", () => {
    expect(getValidChildTypes("epic")).toEqual(["bug", "feature", "task", "research"]);
  });

  it("bug can have task and research children", () => {
    expect(getValidChildTypes("bug")).toEqual(["task", "research"]);
  });

  it("feature can have task and research children (a bug cannot be a feature child)", () => {
    // A bug's parents are milestone|epic only, so it is never a feature child.
    expect(getValidChildTypes("feature")).toEqual(["task", "research"]);
  });

  it("task has no valid children", () => {
    expect(getValidChildTypes("task")).toEqual([]);
  });

  it("research has no valid children", () => {
    expect(getValidChildTypes("research")).toEqual([]);
  });

  it("unknown type returns empty array", () => {
    expect(getValidChildTypes("unknown")).toEqual([]);
    expect(getValidChildTypes("")).toEqual([]);
  });
});

describe("isLeafType", () => {
  it("task is a leaf type", () => {
    expect(isLeafType("task")).toBe(true);
  });

  it("research is a leaf type", () => {
    expect(isLeafType("research")).toBe(true);
  });

  it("bug is NOT a leaf type (it can parent task/research)", () => {
    expect(isLeafType("bug")).toBe(false);
  });

  it("milestone is a leaf type: nothing nests under a waypoint", () => {
    expect(isLeafType("milestone")).toBe(true);
  });

  it("epic is not a leaf type", () => {
    expect(isLeafType("epic")).toBe(false);
  });

  it("feature is not a leaf type", () => {
    expect(isLeafType("feature")).toBe(false);
  });

  it("unknown type returns false", () => {
    expect(isLeafType("unknown")).toBe(false);
  });
});

describe("canHaveChildren", () => {
  it("milestone cannot have children", () => {
    expect(canHaveChildren("milestone")).toBe(false);
  });

  it("epic can have children", () => {
    expect(canHaveChildren("epic")).toBe(true);
  });

  it("feature can have children", () => {
    expect(canHaveChildren("feature")).toBe(true);
  });

  it("bug can have children (task/research)", () => {
    expect(canHaveChildren("bug")).toBe(true);
  });

  it("task cannot have children", () => {
    expect(canHaveChildren("task")).toBe(false);
  });

  it("research cannot have children", () => {
    expect(canHaveChildren("research")).toBe(false);
  });

  it("unknown type cannot have children", () => {
    expect(canHaveChildren("unknown")).toBe(false);
  });
});
