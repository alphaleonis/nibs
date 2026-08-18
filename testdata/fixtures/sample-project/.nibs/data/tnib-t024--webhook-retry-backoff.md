---
# tnib-t024
version: 1
title: Implement retry with exponential backoff
status: todo
type: task
priority: normal
estimate: m
created_at: 2026-03-08T14:30:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-f011
order: b
---

## Description

Background worker that retries failed webhook deliveries with exponential backoff: 1m, 5m, 30m, 2h, 12h.

## Verification

- [ ] Failed deliveries queued for retry
- [ ] Backoff schedule: 1m → 5m → 30m → 2h → 12h
- [ ] Max 5 attempts before marking as permanently failed
- [ ] Webhook disabled after 10 consecutive failures
