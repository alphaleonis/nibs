---
# tnib-f003
version: 1
title: Password reset flow
status: todo
type: feature
priority: normal
estimate: m
created_at: 2026-03-01T09:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-e001
order: c
---

Email-based password reset with secure token generation and configurable expiry.

## Acceptance Criteria

- [ ] "Forgot password" page sends reset email
- [ ] Token valid for 1 hour, single-use
- [ ] Rate limiting on reset requests
- [ ] Password strength validation on new password
