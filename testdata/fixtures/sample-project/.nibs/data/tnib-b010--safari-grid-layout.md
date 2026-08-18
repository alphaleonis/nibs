---
# tnib-b010
version: 1
title: CSS grid layout breaks on Safari 15
status: todo
type: bug
priority: normal
estimate: m
tags:
    - browser-compat
created_at: 2026-03-15T09:00:00Z
updated_at: 2026-03-20T10:00:00Z
order: p
---

## Steps to Reproduce

1. Open task board in Safari 15.x
2. Observe columns overlapping when board has >5 columns

## Expected vs Actual

**Expected:** Columns sized correctly with horizontal scroll when needed
**Actual:** Columns overlap because Safari 15 doesn't support `subgrid` — falls back to `auto` sizing

## Root Cause

We use `grid-template-columns: subgrid` which Safari didn't support until 16.0. Need fallback for Safari 15 using explicit column definitions.
