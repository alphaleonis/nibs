---
# tnib-e001
version: 1
title: User Authentication
status: in-progress
type: epic
priority: high
created_at: 2026-02-18T10:00:00Z
updated_at: 2026-03-24T16:00:00Z
parent: tnib-m001
documents:
    - docs/auth-architecture.md
order: a
---

## Objective

Implement a complete authentication system supporting email/password, OAuth2 social providers, and secure session management.

## Acceptance Criteria

- [x] Users can register and log in with email/password
- [x] Passwords hashed with bcrypt (cost 12)
- [ ] OAuth2 login via Google and GitHub
- [ ] Password reset via email link
- [ ] Sessions invalidated on password change

## Scope Boundaries

- MFA is out of scope for v1.0
- SAML/SSO deferred to enterprise tier
