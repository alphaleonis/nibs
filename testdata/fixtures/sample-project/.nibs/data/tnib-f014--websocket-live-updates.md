---
# tnib-f014
version: 2
title: WebSocket live updates
status: draft
type: feature
priority: normal
estimate: xl
created_at: 2026-03-12T09:00:00Z
updated_at: 2026-03-18T09:00:00Z
parent: tnib-e005
documents:
    - docs/websocket-rfc.md
order: a
---

Real-time task board updates via WebSocket connections with optimistic UI and server reconciliation.

## Acceptance Criteria

- [ ] WebSocket server with connection lifecycle management
- [ ] Client-side optimistic updates with rollback on conflict
- [ ] Presence indicators (who's viewing what)
- [ ] Graceful reconnection with state sync
