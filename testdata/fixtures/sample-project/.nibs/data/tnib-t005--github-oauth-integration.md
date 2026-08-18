---
# tnib-t005
version: 1
title: GitHub OAuth provider integration
status: in-progress
type: task
priority: normal
estimate: m
created_at: 2026-03-10T09:00:00Z
updated_at: 2026-03-24T14:00:00Z
parent: tnib-f002
order: b
---

## Description

Implement GitHub OAuth2 login. Similar to Google flow but uses GitHub's user API for profile data instead of ID token.

## Verification

- [x] Redirect to GitHub authorization page
- [x] Handle callback and token exchange
- [ ] Extract email from GitHub user API (may require email scope)
- [ ] Account linking when GitHub email matches existing user
