---
# tnib-b004
version: 2
title: Task title silently truncated at 255 characters
status: todo
type: bug
priority: low
estimate: s
created_at: 2026-03-12T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-e002
order: f
---

## Steps to Reproduce

1. Create a task with a title longer than 255 characters via API
2. Read the task back

## Expected vs Actual

**Expected:** Either accept full title (up to our stated 500 char limit) or return 400 if too long
**Actual:** Title silently truncated to 255 chars by database column constraint

## Root Cause

Database column is `VARCHAR(255)` but API validation allows up to 500 chars. Either widen the column to 500 or lower the API limit to 255.
