---
# tnib-t004
version: 1
title: Google OAuth provider integration
status: completed
type: task
priority: normal
estimate: m
created_at: 2026-03-01T09:00:00Z
updated_at: 2026-03-15T16:00:00Z
parent: tnib-f002
order: a
---

## Description

Implement Google OAuth2 login using the authorization code flow. Extract email and profile from ID token.

## Verification

- [x] Redirect to Google consent screen
- [x] Handle callback and exchange code for tokens
- [x] Create or link user account from Google profile
- [x] Handle denied permissions gracefully
