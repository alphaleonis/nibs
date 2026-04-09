---
# tnib-b007
version: 1
title: Push notifications not requesting permission on mobile
status: draft
type: bug
priority: deferred
estimate: s
created_at: 2026-03-14T09:00:00Z
updated_at: 2026-03-18T09:00:00Z
parent: tnib-e006
order: c
---

## Steps to Reproduce

1. Open app on mobile Safari (iOS 16+)
2. No permission prompt appears for push notifications
3. Notification settings show "Not Determined"

## Expected vs Actual

**Expected:** Permission prompt shown on first relevant action (e.g., enabling notifications)
**Actual:** Permission never requested because Service Worker registration fails silently on mobile Safari

## Root Cause

Service Worker scope is misconfigured — needs investigation into Safari PWA requirements.
