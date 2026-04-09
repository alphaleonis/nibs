package bodytemplate

// BodyTemplate returns the markdown body template for a nib type.
// Returns empty string for types without a defined template.
func BodyTemplate(typeName string) string {
	switch typeName {
	case "task":
		return `## Description

[Describe what needs to be done]

## Verification

- [ ] [How to verify the work is complete]`

	case "bug":
		return `## Steps to Reproduce

1. [First step]
2. [Second step]
3. [Observed behavior]

## Expected vs Actual

**Expected:** [What should happen]
**Actual:** [What happens instead]

## Root Cause

[Analysis of why the bug occurs, filled in during investigation]`

	case "epic":
		return `## Objective

[What this epic achieves and why it matters]

## Acceptance Criteria

- [ ] [Criterion that must be met for this epic to be complete]

## Scope Boundaries

**In scope:** [What is included]
**Out of scope:** [What is explicitly excluded]`

	case "milestone":
		return `## Goal

[What this milestone achieves and the target date/condition]

## Current Focus

[What is actively being worked on right now]

## Key Decisions

- [Decision and rationale]`

	case "research":
		return `## Question

[What are we trying to learn or decide?]

## Findings

[What we discovered during investigation]

## Decision

[What we decided and why]

## Follow-ups

- [ ] [Action items that came out of this research]`

	default:
		// "feature" intentionally has no template — features vary too widely
		// for a standard section structure to be useful.
		return ""
	}
}
