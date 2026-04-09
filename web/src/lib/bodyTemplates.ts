/**
 * Body templates for nib types, mirroring the Go templates from
 * internal/bodytemplate/bodytemplate.go.
 */

const TASK_TEMPLATE = `## Description

[Describe what needs to be done]

## Verification

- [ ] [How to verify the work is complete]`;

const BUG_TEMPLATE = `## Steps to Reproduce

1. [First step]
2. [Second step]
3. [Observed behavior]

## Expected vs Actual

**Expected:** [What should happen]
**Actual:** [What happens instead]

## Root Cause

[Analysis of why the bug occurs, filled in during investigation]`;

const EPIC_TEMPLATE = `## Objective

[What this epic achieves and why it matters]

## Acceptance Criteria

- [ ] [Criterion that must be met for this epic to be complete]

## Scope Boundaries

**In scope:** [What is included]
**Out of scope:** [What is explicitly excluded]`;

const MILESTONE_TEMPLATE = `## Goal

[What this milestone achieves and the target date/condition]

## Current Focus

[What is actively being worked on right now]

## Key Decisions

- [Decision and rationale]`;

const RESEARCH_TEMPLATE = `## Question

[What are we trying to learn or decide?]

## Findings

[What we discovered during investigation]

## Decision

[What we decided and why]

## Follow-ups

- [ ] [Action items that came out of this research]`;

const templates: Record<string, string> = {
  task: TASK_TEMPLATE,
  bug: BUG_TEMPLATE,
  epic: EPIC_TEMPLATE,
  milestone: MILESTONE_TEMPLATE,
  research: RESEARCH_TEMPLATE,
};

/**
 * Returns the markdown body template for a nib type.
 * Returns empty string for types without a defined template (e.g. "feature").
 */
export function getBodyTemplate(type: string): string {
  return templates[type] ?? "";
}
