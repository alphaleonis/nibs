---
# tnib-t002
version: 2
title: Add rate limiting to login endpoint
status: completed
type: task
priority: normal
estimate: s
created_at: 2026-02-22T09:00:00Z
updated_at: 2026-03-01T14:00:00Z
parent: tnib-f001
order: b
---

## Description

Rate limit login attempts to 5 per 15 minutes per IP using Redis-backed sliding window counter.

## Verification

- [x] 429 response after 5 failed attempts
- [x] Counter resets after 15 minutes
- [x] Separate counters per IP address
- [x] X-RateLimit-* headers in response
