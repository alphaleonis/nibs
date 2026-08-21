---
# tnib-f001
version: 2
title: Email/password login
status: completed
type: feature
priority: high
estimate: m
created_at: 2026-02-20T09:00:00Z
updated_at: 2026-03-05T17:00:00Z
parent: tnib-e001
order: a
---

Standard email/password authentication with bcrypt hashing, rate limiting, and session cookies.

## Acceptance Criteria

- [x] Registration with email validation
- [x] Login with bcrypt password verification
- [x] Rate limiting (5 attempts per 15 minutes)
- [x] Secure HTTP-only session cookies
