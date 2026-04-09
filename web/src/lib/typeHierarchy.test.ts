import { describe, it, expect } from "vitest";
import { getValidChildTypes, isLeafType, canHaveChildren, VALID_CHILD_TYPES } from "./typeHierarchy";

describe("getValidChildTypes", () => {
  it("milestone can only have epic children", () => {
    expect(getValidChildTypes("milestone")).toEqual(["epic"]);
  });

  it("epic can have feature, task, and bug children", () => {
    expect(getValidChildTypes("epic")).toEqual(["feature", "task", "bug"]);
  });

  it("feature can have task and bug children", () => {
    expect(getValidChildTypes("feature")).toEqual(["task", "bug"]);
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
