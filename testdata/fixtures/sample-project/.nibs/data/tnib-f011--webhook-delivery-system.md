---
# tnib-f011
version: 1
title: Webhook delivery system
status: todo
type: feature
priority: normal
estimate: l
created_at: 2026-03-05T14:00:00Z
updated_at: 2026-03-15T10:00:00Z
parent: tnib-e004
order: a
---

HTTP webhook system for notifying external services of task events (created, updated, deleted, assigned).

## Acceptance Criteria

- [ ] Register webhook URLs with event type filters
- [ ] HMAC-SHA256 signature verification
- [ ] Retry with exponential backoff (max 5 attempts)
- [ ] Delivery log with status and response time
