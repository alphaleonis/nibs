---
# tnib-b003
version: 2
title: Assigning task to deactivated user shows no error
status: in-progress
type: bug
priority: high
estimate: s
created_at: 2026-03-20T09:00:00Z
updated_at: 2026-03-24T15:00:00Z
parent: tnib-e002
order: g
---

## Steps to Reproduce

1. Deactivate user "alice@example.com" via admin panel
2. Assign a task to "alice@example.com" via API
3. Request succeeds with 200 OK

## Expected vs Actual

**Expected:** 400 Bad Request with message "Cannot assign to deactivated user"
**Actual:** Assignment succeeds silently; task shows as assigned to a ghost user

## Root Cause

Assignment endpoint only checks if user ID exists in users table, doesn't check `is_active` flag. Need to add `WHERE is_active = true` to the user lookup query.
