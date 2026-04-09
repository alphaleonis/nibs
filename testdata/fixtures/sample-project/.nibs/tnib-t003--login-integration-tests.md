---
# tnib-t003
version: 1
title: Write login integration tests
status: completed
type: task
priority: normal
estimate: s
created_at: 2026-02-25T09:00:00Z
updated_at: 2026-03-03T11:00:00Z
parent: tnib-f001
order: c
---

## Description

Integration tests for the full login flow including registration, login, session management, and rate limiting.

## Verification

- [x] Happy path: register → login → authenticated request
- [x] Wrong password returns 401
- [x] Rate limiting kicks in after threshold
- [x] Session cookie is HTTP-only and Secure
