---
# tnib-b006
version: 2
title: Webhook payloads missing updated_at timestamp
status: todo
type: bug
priority: normal
estimate: s
created_at: 2026-03-10T14:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-e004
order: d
---

## Steps to Reproduce

1. Register a webhook for task.updated events
2. Update a task
3. Inspect the webhook payload

## Expected vs Actual

**Expected:** Payload includes `updated_at` timestamp
**Actual:** `updated_at` field is missing from the payload

## Root Cause

Webhook serializer uses `TaskSummary` struct which doesn't include `UpdatedAt` field. Need to add it to the struct and the JSON serialization.
