---
# tnib-b002
version: 1
title: Session tokens not invalidated on password change
status: todo
type: bug
priority: critical
estimate: m
created_at: 2026-03-15T10:00:00Z
updated_at: 2026-03-20T09:00:00Z
parent: tnib-e001
tags:
    - security
order: e
---

## Steps to Reproduce

1. Log in on Device A
2. Change password on Device A
3. Check Device B — still logged in with old session

## Expected vs Actual

**Expected:** All other sessions invalidated after password change
**Actual:** Existing sessions remain valid indefinitely

## Root Cause

Password change handler updates the password hash but doesn't increment the session generation counter. The session validation middleware doesn't check generation — it only validates the session token exists in Redis.
