---
# tnib-b014
version: 1
title: Race condition in concurrent task updates
status: todo
type: bug
priority: critical
estimate: l
created_at: 2026-03-22T09:00:00Z
updated_at: 2026-03-25T10:00:00Z
order: w
---

## Steps to Reproduce

1. Open the same task in two browser tabs
2. Change status to "done" in tab A
3. Immediately change assignee in tab B (before tab A's change is reflected)
4. Tab B's update overwrites tab A's status change

## Expected vs Actual

**Expected:** Optimistic concurrency detects the conflict and prompts user to resolve
**Actual:** Last write wins — tab B's update silently reverts tab A's status change

## Root Cause

API uses PUT (full replace) instead of PATCH (partial update). No ETag/If-Match headers for optimistic concurrency control. Need to implement either:
1. ETags with If-Match headers (preferred)
2. Field-level versioning
