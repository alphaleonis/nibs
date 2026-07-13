import { describe, it, expect } from "vitest";
import { getValidChildTypes, isLeafType, canHaveChildren, VALID_CHILD_TYPES, typeRank } from "./typeHierarchy";

describe("typeRank", () => {
  it("orders container types above leaf types", () => {
    expect(typeRank("milestone")).toBe(3);
    expect(typeRank("epic")).toBe(2);
    expect(typeRank("feature")).toBe(1);
    expect(typeRank("bug")).toBe(1);
    expect(typeRank("task")).toBe(0);
  });

  it("ranks research at the leaf tier (0)", () => {
    expect(typeRank("research")).toBe(0);
  });

  it("treats unknown and empty types as leaf tier (0)", () => {
    expect(typeRank("unknown")).toBe(0);
    expect(typeRank("")).toBe(0);
  });
});

describe("getValidChildTypes", () => {
  it("milestone can only have epic children", () => {
    expect(getValidChildTypes("milestone")).toEqual(["epic"]);
  });

  it("epic can have feature, task, and bug children", () => {
    expect(getValidChildTypes("epic")).toEqual(["feature", "task", "bug"]);
  });

  it("feature can only have task children (a bug cannot be a feature child)", () => {
    // Backend (internal/nibtypes/hierarchy.go) restricts a bug's parents to
    // milestone|epic, so a bug must never be offered as a child of a feature.
    expect(getValidChildTypes("feature")).toEqual(["task"]);
  });

  it("task has no valid children", () => {
    expect(getValidChildTypes("task")).toEqual([]);
  });

  it("bug has no valid children", () => {
    expect(getValidChildTypes("bug")).toEqual([]);
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

  it("bug is a leaf type", () => {
    expect(isLeafType("bug")).toBe(true);
  });

  it("milestone is not a leaf type", () => {
    expect(isLeafType("milestone")).toBe(false);
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
  it("milestone can have children", () => {
    expect(canHaveChildren("milestone")).toBe(true);
  });

  it("epic can have children", () => {
    expect(canHaveChildren("epic")).toBe(true);
  });

  it("feature can have children", () => {
    expect(canHaveChildren("feature")).toBe(true);
  });

  it("task cannot have children", () => {
    expect(canHaveChildren("task")).toBe(false);
  });

  it("bug cannot have children", () => {
    expect(canHaveChildren("bug")).toBe(false);
  });

  it("unknown type cannot have children", () => {
    expect(canHaveChildren("unknown")).toBe(false);
  });
});
