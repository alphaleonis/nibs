---
# tnib-t026
version: 2
title: Set up WebSocket server infrastructure
status: draft
type: task
priority: normal
estimate: l
created_at: 2026-03-14T09:00:00Z
updated_at: 2026-03-18T09:00:00Z
parent: tnib-f014
order: a
---

## Description

WebSocket server using gorilla/websocket with connection management, heartbeat/ping, and room-based pub/sub.

## Verification

- [ ] WebSocket endpoint at /ws with JWT authentication
- [ ] Connection pool with room-based subscriptions
- [ ] Heartbeat ping every 30s, disconnect after 90s silence
- [ ] Graceful shutdown with client notification
