import { describe, it, expect } from "vitest";
import { getBodyTemplate } from "./bodyTemplates";

describe("getBodyTemplate", () => {
  it("returns a template for task type", () => {
    const template = getBodyTemplate("task");
    expect(template).not.toBe("");
    expect(template).toContain("## Description");
    expect(template).toContain("## Verification");
  });

  it("returns a template for bug type", () => {
    const template = getBodyTemplate("bug");
    expect(template).not.toBe("");
    expect(template).toContain("## Steps to Reproduce");
    expect(template).toContain("## Expected vs Actual");
    expect(template).toContain("## Root Cause");
  });

  it("returns a template for epic type", () => {
    const template = getBodyTemplate("epic");
    expect(template).not.toBe("");
    expect(template).toContain("## Objective");
    expect(template).toContain("## Acceptance Criteria");
    expect(template).toContain("## Scope Boundaries");
  });

  it("returns a template for milestone type", () => {
    const template = getBodyTemplate("milestone");
    expect(template).not.toBe("");
    expect(template).toContain("## Goal");
    expect(template).toContain("## Current Focus");
    expect(template).toContain("## Key Decisions");
  });

  it("returns a template for research type", () => {
    const template = getBodyTemplate("research");
    expect(template).not.toBe("");
    expect(template).toContain("## Question");
    expect(template).toContain("## Findings");
    expect(template).toContain("## Decision");
    expect(template).toContain("## Follow-ups");
  });

  it("returns empty string for feature type", () => {
    expect(getBodyTemplate("feature")).toBe("");
  });

  it("returns empty string for unknown types", () => {
    expect(getBodyTemplate("unknown")).toBe("");
    expect(getBodyTemplate("")).toBe("");
    expect(getBodyTemplate("foo")).toBe("");
  });
});
