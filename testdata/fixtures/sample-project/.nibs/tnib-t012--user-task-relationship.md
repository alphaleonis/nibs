---
# tnib-t012
version: 1
title: Create user-task relationship model
status: completed
type: task
priority: normal
estimate: s
created_at: 2026-03-01T10:00:00Z
updated_at: 2026-03-10T15:00:00Z
parent: tnib-f005
order: a
---

## Description

Create the many-to-many relationship model between users and tasks for assignments. Support multiple assignees per task.

## Verification

- [x] Join table with user_id, task_id, role (assignee/reviewer)
- [x] Unique constraint on (user_id, task_id, role)
- [x] Cascade delete when task is deleted
