---
# tnib-t010
version: 2
title: Implement REST API endpoints
status: completed
type: task
priority: high
estimate: m
created_at: 2026-02-22T09:00:00Z
updated_at: 2026-03-05T16:00:00Z
parent: tnib-f004
order: b
---

## Description

Implement CRUD REST endpoints: POST /tasks, GET /tasks, GET /tasks/:id, PATCH /tasks/:id, DELETE /tasks/:id.

## Verification

- [x] All five endpoints implemented with proper HTTP methods
- [x] Pagination on list endpoint (cursor-based)
- [x] Filtering by status, assignee, priority
- [x] Proper error responses (400, 404, 409, 500)
