---
# tnib-t007
version: 2
title: Implement token generation and expiry
status: todo
type: task
priority: normal
estimate: m
created_at: 2026-03-05T09:30:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-f003
order: b
---

## Description

Generate cryptographically secure reset tokens with 1-hour TTL. Tokens are single-use and stored hashed in the database.

## Verification

- [ ] Tokens generated with crypto/rand (32 bytes, base64url encoded)
- [ ] Stored as SHA-256 hash in database
- [ ] Expired tokens rejected with clear error message
- [ ] Used tokens cannot be reused
