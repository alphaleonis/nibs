---
# tnib-m002
version: 2
title: v1.1 Team Collaboration
status: draft
type: milestone
priority: normal
created_at: 2026-03-10T09:00:00Z
updated_at: 2026-03-20T11:00:00Z
order: b
---

Add real-time collaboration features, notification system, and activity feeds. Depends on v1.0 infrastructure being stable.

## Goal

Enable teams to collaborate in real-time on shared task boards with live updates and notifications.

## Key Decisions

- WebSocket for real-time (not SSE) — need bidirectional communication
- Event sourcing for activity feed to support retroactive queries
