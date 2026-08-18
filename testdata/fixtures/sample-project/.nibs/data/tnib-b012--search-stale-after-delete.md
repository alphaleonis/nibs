---
# tnib-b012
version: 1
title: Search results not updating after task deletion
status: in-progress
type: bug
priority: normal
estimate: s
created_at: 2026-03-20T14:00:00Z
updated_at: 2026-03-25T11:00:00Z
order: u
---

## Steps to Reproduce

1. Create a task with title "quarterly review"
2. Delete the task
3. Search for "quarterly" — deleted task still appears in results

## Expected vs Actual

**Expected:** Deleted tasks excluded from search results immediately
**Actual:** Search index retains deleted tasks until server restart

## Root Cause

Delete handler removes from database but doesn't update the Bleve search index. Need to call `index.Delete(taskID)` in the delete path.
