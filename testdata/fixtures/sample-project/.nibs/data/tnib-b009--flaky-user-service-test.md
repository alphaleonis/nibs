---
# tnib-b009
version: 2
title: Flaky test in user_service_test.go
status: todo
type: bug
priority: high
estimate: s
created_at: 2026-03-18T09:00:00Z
updated_at: 2026-03-22T10:00:00Z
order: o
---

## Steps to Reproduce

1. Run `go test ./internal/service/ -count=100`
2. TestUserService_ConcurrentUpdate fails ~5% of the time

## Expected vs Actual

**Expected:** Test passes consistently
**Actual:** Intermittent failure with "context deadline exceeded" — race condition in test setup

## Root Cause

Test uses a shared database connection pool without proper per-test isolation. When tests run in parallel, the pool is exhausted and some tests timeout waiting for a connection. Need to use `t.Parallel()` with per-test transactions.
