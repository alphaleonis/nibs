---
# tnib-t042
version: 2
title: Implement batch API endpoint
status: todo
type: task
priority: normal
estimate: m
created_at: 2026-03-18T09:30:00Z
updated_at: 2026-03-20T10:00:00Z
parent: tnib-f020
order: b
---

## Description

POST /tasks/batch endpoint that accepts an array of task IDs and an operation (update status, assign, move, delete).

## Verification

- [ ] Accepts up to 100 task IDs per request
- [ ] Supports operations: update_status, assign, move_to_project, delete
- [ ] Atomic: all succeed or all fail (transaction)
- [ ] Returns individual results for each task
