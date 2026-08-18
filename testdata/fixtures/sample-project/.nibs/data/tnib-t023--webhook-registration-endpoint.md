---
# tnib-t023
version: 1
title: Build webhook registration endpoint
status: todo
type: task
priority: normal
estimate: m
created_at: 2026-03-08T14:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-f011
order: a
---

## Description

REST endpoint to register, list, update, and delete webhook subscriptions. Each webhook specifies a URL and event types.

## Verification

- [ ] POST /webhooks to register
- [ ] GET /webhooks to list
- [ ] DELETE /webhooks/:id to remove
- [ ] Validation: URL must be HTTPS, event types must be valid
