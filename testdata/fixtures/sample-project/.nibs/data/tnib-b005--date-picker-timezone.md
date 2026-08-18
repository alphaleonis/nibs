---
# tnib-b005
version: 1
title: Date picker shows wrong timezone for remote users
status: todo
type: bug
priority: normal
estimate: m
created_at: 2026-03-10T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-e003
order: d
---

## Steps to Reproduce

1. Set system timezone to UTC+9 (Tokyo)
2. Open task with due date "2026-03-15"
3. Date picker shows "March 14" (off by one due to UTC conversion)

## Expected vs Actual

**Expected:** Date picker shows the date as entered, in user's local timezone
**Actual:** Date displayed relative to UTC, causing off-by-one for users east of UTC

## Root Cause

Dates stored as UTC timestamps but rendered without timezone conversion. Should store as date-only (no time component) or convert to user's timezone on display.
