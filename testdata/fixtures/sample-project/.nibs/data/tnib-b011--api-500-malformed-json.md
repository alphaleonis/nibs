---
# tnib-b011
version: 2
title: API returns 500 on malformed JSON body
status: todo
type: bug
priority: high
estimate: s
created_at: 2026-03-18T10:00:00Z
updated_at: 2026-03-22T09:00:00Z
order: q
area: api
---

## Steps to Reproduce

1. Send POST /api/tasks with body: `{invalid json`
2. Server returns 500 Internal Server Error

## Expected vs Actual

**Expected:** 400 Bad Request with message "Invalid JSON: unexpected character at position 1"
**Actual:** 500 Internal Server Error with generic message — json.Decoder error is not caught by error middleware

## Root Cause

Error middleware only handles our custom `AppError` type. stdlib `json.SyntaxError` and `json.UnmarshalTypeError` fall through to the default 500 handler. Need to add type switches in the error middleware.
