---
# tnib-b001
version: 2
title: OAuth callback URL mismatch in production config
status: todo
type: bug
priority: high
estimate: s
created_at: 2026-03-18T14:00:00Z
updated_at: 2026-03-22T09:00:00Z
parent: tnib-f002
order: c
---

## Steps to Reproduce

1. Deploy to production environment
2. Click "Login with Google"
3. Complete Google consent screen
4. Observe "redirect_uri_mismatch" error

## Expected vs Actual

**Expected:** User redirected back to app and logged in
**Actual:** Google returns `redirect_uri_mismatch` error because production config still has staging callback URL

## Root Cause

`OAUTH_CALLBACK_URL` environment variable in production is set to `https://staging.taskflow.dev/auth/callback` instead of `https://taskflow.dev/auth/callback`. Terraform config has the correct value but it wasn't applied after the last deploy.
