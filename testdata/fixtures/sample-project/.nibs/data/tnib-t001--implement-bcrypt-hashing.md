---
# tnib-t001
version: 2
title: Implement bcrypt password hashing
status: completed
type: task
priority: high
estimate: s
created_at: 2026-02-20T10:00:00Z
updated_at: 2026-02-25T16:00:00Z
parent: tnib-f001
order: a
---

## Description

Implement bcrypt hashing with cost factor 12 for password storage. Use `golang.org/x/crypto/bcrypt` package.

## Verification

- [x] Passwords stored as bcrypt hashes, never plaintext
- [x] Cost factor configurable via environment variable
- [x] Timing-safe comparison for login verification
