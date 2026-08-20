---
# tnib-t043
version: 2
title: Add structured logging
status: completed
type: task
priority: normal
estimate: m
tags:
    - observability
created_at: 2026-02-22T09:00:00Z
updated_at: 2026-03-05T16:00:00Z
order: l
---

## Description

Replace fmt.Printf logging with structured JSON logging using slog. Add request IDs, user IDs, and timing to all log entries.

## Verification

- [x] slog with JSON handler configured
- [x] Request ID middleware injects trace ID
- [x] All HTTP handlers log request/response with timing
- [x] Log levels: DEBUG, INFO, WARN, ERROR used appropriately
